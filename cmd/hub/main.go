package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkghandler "example.com/rbmq-demo/pkg/handler"
	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var upgrader = websocket.Upgrader{}

const serverShutdownTimeout = 30 * time.Second

func main() {
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sm := pkgsafemap.NewSafeMap()
	defer sm.Close()

	cr := pkgconnreg.NewConnRegistry(sm)

	wsHandler := pkghandler.NewWebsocketHandler(&upgrader, cr)
	connsHandler := pkghandler.NewConnsHandler(cr)

	muxer := http.NewServeMux()
	muxer.Handle("/ws", wsHandler)
	muxer.Handle("/conns", connsHandler)

	server := http.Server{
		Handler: muxer,
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen on address %s: %v", *addr, err)
	}
	log.Printf("Listening on %s", listener.Addr())

	go func() {
		log.Printf("Starting server on %s", listener.Addr())
		err = server.Serve(listener)
		if err != nil {
			if err != http.ErrServerClosed {
				log.Fatalf("Failed to serve: %v", err)
			}
		}
	}()

	<-sigs
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()
	err = server.Shutdown(ctx)
	if err != nil {
		log.Printf("Failed to shutdown server: %v", err)
	}
	log.Println("Server shut down successfully")
}
