package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"os"
	"os/signal"
	"syscall"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgrabbitmqping "example.com/rbmq-demo/pkg/rabbitmqping"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")
var logEchoReplies = flag.Bool("log-echo-replies", false, "log echo replies")
var rabbitMQBrokerURL = flag.String("rabbitmq-broker-url", "amqp://localhost:5672/", "RabbitMQ broker URL")

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
	attributes[pkgnodereg.AttributeKeyNodeName] = *nodeName

	queueName := fmt.Sprintf("ping.%s", *nodeName)
	attributes[pkgnodereg.AttributeKeyRabbitMQQueueName] = queueName
	agent.NodeAttributes = attributes

	rbmqResponder := pkgrabbitmqping.RabbitMQResponder{
		URL:       *rabbitMQBrokerURL,
		QueueName: queueName,
	}

	rabbitMQErrCh := rbmqResponder.ServeRPC(context.Background())

	var err error
	if err = agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	nodeRegAgentErrCh := agent.Run()

	<-sigs

	log.Println("Shutting down pinger...")
	err = rbmqResponder.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown RabbitMQ responder: %v", err)
	}

	log.Println("Shutting down agent...")
	err = agent.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown agent: %v", err)
	}

	err = <-nodeRegAgentErrCh
	if err != nil {
		log.Printf("Agent exited with error: %v", err)
	} else {
		log.Println("Agent exited successfully")
	}

	err = <-rabbitMQErrCh
	if err != nil {
		log.Printf("RabbitMQ responder exited with error: %v", err)
	} else {
		log.Println("RabbitMQ responder exited successfully")
	}

}
