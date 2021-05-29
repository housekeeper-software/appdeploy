package server

import (
	"crypto/tls"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//Server is a websocket server
type Server struct {
	upgrader websocket.Upgrader
	server   *http.Server
}

//NewServer is Create websocket server
func NewServer() *Server {
	return &Server{
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 30 * time.Second,
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

//Run is start websocket server and block
func (s *Server) Run(ctx Context) {
	if len(ctx.CertDir) < 1 {
		s.server = &http.Server{Addr: ctx.Listen}
		http.HandleFunc("/", s.handleConn)
		fmt.Fprintf(os.Stdout, "server listen: %s\n", ctx.Listen)
		if err := s.server.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stdout, "%+v\n", err)
		}
		return
	}
	caFile := filepath.Join(ctx.CertDir, "ca.cert.pem")
	serverCertFile := filepath.Join(ctx.CertDir, "server.cert.pem")
	serverKeyFile := filepath.Join(ctx.CertDir, "server.key.pem")

	s.server = &http.Server{
		Addr: ctx.Listen,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
			ClientCAs:          loadCert(caFile),
			ClientAuth:         tls.VerifyClientCertIfGiven,
		},
	}
	http.HandleFunc("/", s.handleConn)

	fmt.Fprintf(os.Stdout, "server listen: %s\n", ctx.Listen)

	if err := s.server.ListenAndServeTLS(serverCertFile, serverKeyFile); err != nil {
		fmt.Fprintf(os.Stdout, "%+v\n", err)
	}
}

func (s *Server) handleConn(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Upgrade: %+v\n", err)
		return
	}
	conn := newWsConn(ws)
	go conn.run()
}
