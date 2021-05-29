package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func walkDir(dir string) (files []string, err error) {
	err = filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	return
}

func isFileExists(path string) (os.FileInfo, bool) {
	f, err := os.Stat(path)
	return f, err == nil || os.IsExist(err)
}

func isDir(path string) bool {
	f, flag := isFileExists(path)
	return flag && f.IsDir()
}

func upload(ctx Context, conn *websocket.Conn) (int, error) {
	m := Message{}
	m.Cmd = ctx.Cmd
	m.Target = filepath.ToSlash(ctx.Target)
	fs, err := os.Stat(ctx.Source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "os.Stat: %v\n", err)
		return errFile, err
	}
	m.Length = int(fs.Size())
	m.Hash, err = hashFile(ctx.Source)
	if err != nil {
		return errHash, err
	}
	m.Wait = ctx.Wait

	err = conn.WriteJSON(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "writeJSON: %v\n", err)
		return errSocket, err
	}

	f, err := os.Open(ctx.Source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open file[%s] : %v\n", ctx.Source, err)
		return errFile, err
	}

	defer f.Close()

	fmt.Fprintf(os.Stdout, "\n********************************************************************************\n")
	fmt.Fprintf(os.Stdout, "\nstart upload file[%s], length: [%d]\n", ctx.Source, m.Length)

	buf := make([]byte, 4096)

	var count = 0

	for {
		n, err := f.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "Read file[%s] : %v\n", ctx.Source, err)
				return errReadFile, err
			}
			fmt.Fprintf(os.Stdout, "\nUpload Completed,waiting for server response\n")
			break
		}
		fmt.Fprintf(os.Stdout, ".")
		count++

		if count > 80 {
			fmt.Fprintf(os.Stdout, "\n")
			count = 0
		}
		err = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
		if err != nil {
			fmt.Fprintf(os.Stderr, "write websocket: %v", err)
			return errSocket, err
		}
	}
	return readResponse(ctx, conn)
}

func uploadFolder(ctx Context, conn *websocket.Conn) (int, error) {
	fs, err := walkDir(ctx.Source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk dir[%s] : %v\n", ctx.Source, err)
		return errWalkDir, err
	}
	fmt.Fprintf(os.Stdout, "total files:[%d]\n", len(fs))

	for _, v := range fs {
		var cmd = ctx
		cmd.Source = v
		str, _ := filepath.Rel(ctx.Source, v)
		cmd.Target = filepath.Join(cmd.Target, str)
		r, err := upload(cmd, conn)
		if err != nil {
			return r, err
		}
	}
	return errOk, nil
}

func runShell(ctx Context, conn *websocket.Conn) (int, error) {
	m := Message{
		Cmd:    ctx.Cmd,
		Target: ctx.Target,
		Length: 0,
		Hash:   "",
		Dir:    ctx.Dir,
		Wait:   ctx.Wait,
	}
	err := conn.WriteJSON(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write websocket: %v\n", err)
		return errSocket, err
	}
	return readResponse(ctx, conn)
}

func readResponse(ctx Context, conn *websocket.Conn) (int, error) {
	if ctx.Timeout > 0 {
		err := conn.SetReadDeadline(time.Now().Add(time.Duration(ctx.Timeout) * time.Second))
		if err != nil {
			fmt.Fprintf(os.Stderr, "SetReadDeadline(%d): %v\n", ctx.Timeout, err)
			return errTimeout, err
		}
	}
	for {
		op, message, err := conn.ReadMessage()
		if e, ok := err.(net.Error); ok && e.Timeout() {
			fmt.Fprintf(os.Stderr, "ReadMessage timeout: %v\n", err)
			return errTimeout, err
		}
		if err != nil {
			// This was an error, but not a timeout
			fmt.Fprintf(os.Stderr, "ReadMessage : %v\n", err)
			return errSocket, err
		}

		if op == websocket.TextMessage {
			resp := Response{}
			err = json.Unmarshal(message, &resp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unmarshal result json: %v\n", err)
				return errFormat, nil
			}
			fmt.Fprintf(os.Stdout, "\nServer response:\nResult:%d\nExitCode:%d\nMessage:%s\n",
				resp.Result, resp.ExitCode, resp.Message)
			return resp.ExitCode, nil
		}
		fmt.Fprint(os.Stdout, string(message))
	}
}

//RunClient is start websocket client and block
func RunClient(ctx Context) (int, error) {
	var t *tls.Config = nil
	if len(ctx.CertDir) > 0 {
		caFile := filepath.Join(ctx.CertDir, "ca.cert.pem")
		clientCertFile := filepath.Join(ctx.CertDir, "client.cert.pem")
		clientKeyFile := filepath.Join(ctx.CertDir, "client.key.pem")

		cert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
		if err != nil {
			return errCert, err
		}

		t = &tls.Config{
			RootCAs:            loadCert(caFile),
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{cert},
			ServerName:         "/",
		}
	}

	dialer := websocket.Dialer{
		NetDial: func(network, addr string) (conn net.Conn, err error) {
			return net.Dial(network, addr)
		},
		TLSClientConfig: t,
	}
	conn, _, err := dialer.Dial(ctx.Host, nil)
	if err != nil {
		return errSocket, err

	}
	defer conn.Close()

	if strings.EqualFold(ctx.Cmd, "upload") {
		ok := isDir(ctx.Source)
		if ok {
			return uploadFolder(ctx, conn)
		}
		return upload(ctx, conn)

	}
	return runShell(ctx, conn)
}
