package server

import (
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
)

//Message is deploy command protocol, json format
type Message struct {
	Cmd    string `json:"cmd"`
	Target string `json:"target"`
	Length int    `json:"length"`
	Hash   string `json:"hash"`
	Dir    string `json:"dir"`
	Wait   bool   `json:"wait"`
}

//Response is server response to client
type Response struct {
	Result   int    `json:"result"`
	ExitCode int    `json:"exit_code"`
	Message  string `json:"message"`
}

//Error define
const (
	errOk = iota
	errCmd
	errFile
	errReadFile
	errWalkDir
	errSocket
	errHash
	errSaveFile
	errCert
	errExecShell
	errTimeout
	errFormat
	errMemory
	errPipeline
)

//Context is commandline args
type Context struct {
	Host    string
	Listen  string
	Source  string
	Target  string
	Dir     string
	Cmd     string
	Timeout int
	Wait    bool
	CertDir string
}

//AppCtx is Context instance
var AppCtx Context

func init() {
	flag.StringVar(&AppCtx.Host, "host", "", "host")
	flag.StringVar(&AppCtx.Listen, "listen", "", "listen")
	flag.StringVar(&AppCtx.Source, "source", "", "source")
	flag.StringVar(&AppCtx.Target, "target", "", "target")
	flag.StringVar(&AppCtx.Dir, "dir", "", "dir")
	flag.StringVar(&AppCtx.Cmd, "cmd", "", "cmd")
	flag.IntVar(&AppCtx.Timeout, "timeout", 0, "timeout")
	flag.BoolVar(&AppCtx.Wait, "wait", false, "wait")
	flag.StringVar(&AppCtx.CertDir, "certdir", "", "certdir")
}

func loadCert(file string) *x509.CertPool {
	pool := x509.NewCertPool()
	if ca, e := ioutil.ReadFile(file); e != nil {
		log.Fatalf("ReadFile:[%s] : %v ", file, e)
	} else {
		pool.AppendCertsFromPEM(ca)
	}
	return pool
}

func hashFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open file[%s] : %v\n", file, err)
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		fmt.Fprintf(os.Stderr, "io.Copy: %v\n", err)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashBytes(data []byte) string {
	h := md5.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
