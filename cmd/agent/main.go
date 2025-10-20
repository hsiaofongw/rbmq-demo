package main

import (
	"flag"
	"log"

	"os"
	"os/signal"
	"syscall"

	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")

func main() {
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	agent := pkgnodereg.NodeRegistrationAgent{
		ServerAddress: *addr,
		WebSocketPath: *path,
		NodeName:      *nodeName,
	}

	if err := agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	errCh := make(chan error)
	go func() {
		errCh <- agent.Run()
	}()

	<-sigs

	log.Println("Shutting down agent...")
	err := agent.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown agent: %v", err)
	}

	err = <-errCh
	if err != nil {
		log.Printf("Agent exited with error: %v", err)
	} else {
		log.Println("Agent exited successfully")
	}

}
