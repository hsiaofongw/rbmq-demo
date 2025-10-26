package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"os"
	"os/signal"
	"syscall"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")
var logEchoReplies = flag.Bool("log-echo-replies", false, "log echo replies")

func main() {
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	agent := pkgnodereg.NodeRegistrationAgent{
		ServerAddress:  *addr,
		WebSocketPath:  *path,
		NodeName:       *nodeName,
		LogEchoReplies: *logEchoReplies,
	}

	attributes := make(pkgconnreg.ConnectionAttributes)
	attributes[pkgnodereg.AttributeKeyPingCapability] = "true"

	// For now, just use node name, later we will use a broker generated name
	attributes[pkgnodereg.AttributeKeyRabbitMQQueueName] = *nodeName

	agent.NodeAttributes = attributes

	var err error
	if err = agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	errCh := agent.Run()

	pingCfgs := []pkgsimpleping.PingConfiguration{
		{
			Destination: "8.8.8.8",
			Count:       3,
			Timeout:     10 * time.Second,
			Interval:    1 * time.Second,
		},
		{
			Destination: "1.1.1.1",
			Count:       3,
			Timeout:     10 * time.Second,
			Interval:    1 * time.Second,
		},
	}
	pingers := make([]pkgpinger.Pinger, 0)
	for _, cfg := range pingCfgs {
		pingers = append(pingers, pkgsimpleping.NewSimplePinger(&cfg))
	}

	eventCh := pkgpinger.StartMultiplePings(context.Background(), pingers)

	for ev := range eventCh {
		fmt.Println(ev.String())
	}

	<-sigs

	log.Println("Shutting down pinger...")
	// The pinger is now run in a goroutine, so we don't need to stop it here
	// directly. The channel will close when the pinger finishes.

	log.Println("Shutting down agent...")
	err = agent.Shutdown()
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
