package main

import (
	"appdeploy/server"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

func main() {
	flag.Parse()

	if len(server.AppCtx.Listen) > 0 {
		go func() {
			t := time.NewTicker(time.Second * 30)
			for {
				select {
				case <-t.C:
					fmt.Fprintf(os.Stdout, "gotoutine:%d\n", runtime.NumGoroutine())
				}
			}
		}()
		s := server.NewServer()
		s.Run(server.AppCtx)
		return
	}
	rv, err := server.RunClient(server.AppCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
	}
	os.Exit(rv)
}
