package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxFileSize = 512000000
)

type response struct {
	msgType int
	data    []byte
}

type wsConn struct {
	conn     *websocket.Conn
	isClosed bool
	writeCh  chan response
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	quit     bool
	pending  bool
}

func newWsConn(conn *websocket.Conn) *wsConn {
	s := &wsConn{
		conn:     conn,
		isClosed: false,
		writeCh:  make(chan response, 10),
		quit:     false,
		pending:  false,
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

func (w *wsConn) close() {
	if w.isClosed {
		return
	}
	w.conn.Close()
	w.isClosed = true
}

func (w *wsConn) run() {
	defer func() {
		w.close()
		if v := recover(); v != nil {
			log.Println("capture a panic in wsConn:", v)
		}
		fmt.Fprintf(os.Stdout, "ws conn[%s] routine exit\n", w.conn.RemoteAddr().String())
	}()

	done := make(chan struct{})

	go w.handleRead(done)

	for {
		select {
		case <-done:
			//read routine exit
			w.close()
			return
		case resp := <-w.writeCh:
			err := w.conn.WriteMessage(resp.msgType, resp.data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ws conn[%s] write message: %v\n", w.conn.RemoteAddr().String(), err)
			}
		}
	}
}

func (w *wsConn) handleRead(done chan<- struct{}) {
	defer func() {
		w.quit = true
		if w.pending {
			w.cancel()
			w.wg.Wait()
		}
		done <- struct{}{}
	}()

	var err error
	for {
		req := Message{}
		err = w.conn.ReadJSON(&req)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				fmt.Fprintf(os.Stderr, "ReadJSON: %v\n", err)
			}
			return
		}
		if strings.EqualFold(req.Cmd, "upload") {
			r := w.handleUpload(req)
			w.sendResponse(r)
		} else {
			if w.pending {
				r := Response{
					Result:   errPipeline,
					ExitCode: 0,
					Message:  "disallow pipeline shell command!",
				}
				w.sendResponse(r)
			} else {
				if strings.EqualFold(req.Cmd, "shell") ||
					strings.EqualFold(req.Cmd, "popen") {
					w.pending = true
					w.wg.Add(1)
					go w.handleRequest(req)
				} else {
					r := Response{
						Result:   errCmd,
						ExitCode: 0,
						Message:  fmt.Sprintf("Unsupport command:%s", req.Cmd),
					}
					w.sendResponse(r)
				}
			}
		}
	}
}

func (w *wsConn) sendResponse(resp Response) {
	if w.quit {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	w.writeCh <- response{websocket.TextMessage, data}
}

func (w *wsConn) handleRequest(req Message) {
	defer func() {
		w.wg.Done()
	}()

	if strings.EqualFold(req.Cmd, "shell") {
		r := w.handleShell(req)
		w.sendResponse(r)
		return
	}
	if strings.EqualFold(req.Cmd, "popen") {
		r := w.handlePopen(req)
		w.sendResponse(r)
		return
	}
}

func (w *wsConn) handleUpload(req Message) Response {
	if req.Length > maxFileSize {
		return Response{
			Result:   errMemory,
			ExitCode: 0,
			Message:  fmt.Sprintf("file too big[%d]", req.Length),
		}
	}
	var buffer bytes.Buffer
	for {
		err := w.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		mt, data, err := w.conn.ReadMessage()
		if err != nil {
			return Response{errSocket, 0, "socket error:" + err.Error()}
		}
		if mt != websocket.BinaryMessage {
			return Response{errFormat, 0, "error packet type, expect binary message!"}
		}
		buffer.Write(data)

		if buffer.Len() == req.Length {
			hash := hashBytes(buffer.Bytes())
			n := strings.Compare(hash, req.Hash)
			if n != 0 {
				return Response{errHash, 0, "file hash mismatch!"}
			}
			path := filepath.Dir(req.Target)
			err := os.MkdirAll(path, os.ModePerm)
			if err != nil {
				return Response{errSaveFile, 0,
					fmt.Sprintf("create dir[%s] failed : %s", path, err.Error())}
			}
			err = ioutil.WriteFile(req.Target, buffer.Bytes(), 0644)
			if err != nil {
				return Response{errSaveFile, 0,
					fmt.Sprintf("write file[%s] failed: %s", req.Target, err.Error())}
			}
			return Response{errOk, 0,
				fmt.Sprintf("upload file[%s] success", req.Target)}
		}
	}
}

func (w *wsConn) handleShell(req Message) Response {
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/c"
	} else {
		shell = "/bin/sh"
		flag = "-c"
	}
	if len(req.Dir) > 0 {
		err := os.Chdir(req.Dir)
		if err != nil {
			fmt.Fprintf(os.Stdout, "can not change dir: %s, %+v", req.Dir, err)
		}
	}

	cmd := exec.CommandContext(w.ctx, shell, flag, req.Target)
	if len(req.Dir) > 0 {
		cmd.Dir = req.Dir
	}
	cmd.Env = os.Environ()
	err := cmd.Start()
	if err != nil {
		return Response{errExecShell, 0,
			fmt.Sprintf("start command[%s %s %s] failed : %s", shell, flag, req.Target, err.Error())}
	}
	if !req.Wait {
		cmd.Process.Release()
		return Response{errOk, 0,
			fmt.Sprintf("start command[%s %s %s] success,not wait", shell, flag, req.Target)}
	}

	s := fmt.Sprintf("start command[%s %s %s] success, waiting for completed", shell, flag, req.Target)
	w.writeCh <- response{
		msgType: websocket.BinaryMessage,
		data:    []byte(s),
	}

	err = cmd.Wait()

	if err != nil {
		var exitCode = 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		}
		return Response{0, exitCode,
			fmt.Sprintf("commnd[%s,%s,%s] failed: %s", shell, flag, req.Target, err.Error())}
	}
	return Response{0, cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus(),
		fmt.Sprintf("commnd[%s,%s,%s] success", shell, flag, req.Target)}
}

func (w *wsConn) handlePopen(req Message) Response {
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/c"
	} else {
		shell = "/bin/sh"
		flag = "-c"
	}
	if len(req.Dir) > 0 {
		err := os.Chdir(req.Dir)
		if err != nil {
			fmt.Fprintf(os.Stdout, "Chdir[%s]: %+v\n", req.Dir, err)
		}
	}
	cmd := exec.CommandContext(w.ctx, shell, flag, req.Target)
	if len(req.Dir) > 0 {
		cmd.Dir = req.Dir
	}
	cmd.Env = os.Environ()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Response{errExecShell, 0,
			fmt.Sprintf("StderrPipe: %s", err.Error())}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Response{errExecShell, 0,
			fmt.Sprintf("StdoutPipe: %s", err.Error())}
	}
	done := make(chan struct{})
	go func() {
		merged := io.MultiReader(stderr, stdout)
		scanner := bufio.NewScanner(merged)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			s := scanner.Text() + "\n"
			if !w.quit {
				w.writeCh <- response{websocket.BinaryMessage, []byte(s)}
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Printf("scanner return %v\n", err)
		}
		done <- struct{}{}
	}()

	rv := 0
	var e = errors.New("OK")

	if err := cmd.Start(); nil != err {
		log.Printf("starting program: %s, %s", cmd.Path, err.Error())
		rv = errExecShell
		e = err
	}
	<-done
	exitCode := 0
	if rv == 0 {
		if err := cmd.Wait(); err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// The program has exited with an exit code != 0

				// This works on both Unix and Windows. Although package
				// syscall is generally platform dependent, WaitStatus is
				// defined for both Unix and Windows and in both cases has
				// an ExitStatus() method with the same signature.
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					log.Printf("Exit Status: %d", status.ExitStatus())
					exitCode = status.ExitStatus()
				}
			} else {
				log.Printf("cmd.Wait: %v", err)
				e = err
			}
		}
	}
	return Response{rv, exitCode,
		fmt.Sprintf("popen(%s %s %s) completed: %s", shell, flag, req.Target, e.Error())}
}
