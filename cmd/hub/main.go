package main

import (
	"flag"
	"log"
	"net/http"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkghandler "example.com/rbmq-demo/pkg/handler"
	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var upgrader = websocket.Upgrader{}

func main() {
	flag.Parse()

	sm := pkgsafemap.NewSafeMap()
	cr := pkgconnreg.NewConnRegistry(sm)

	wsHandler := pkghandler.NewWebsocketHandler(&upgrader, cr)
	connsHandler := pkghandler.NewConnsHandler(cr)

	muxer := http.NewServeMux()
	muxer.Handle("/ws", wsHandler)
	muxer.Handle("/conns", connsHandler)

	server := http.Server{
		Addr:    *addr,
		Handler: muxer,
	}

	log.Println("Server started", "Using address:", *addr)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
