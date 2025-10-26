package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	rbmqResponder := pkgrabbitmqping.RabbitMQResponder{
		URL: *rabbitMQBrokerURL,
	}
	log.Println("Initializing RabbitMQ responder...")
	rbmqResponder.Init()

	log.Println("Starting RabbitMQ responder...")
	rabbitMQErrCh := rbmqResponder.ServeRPC(context.Background())

	attributes := make(pkgconnreg.ConnectionAttributes)
	attributes[pkgnodereg.AttributeKeyPingCapability] = "true"

	log.Println("NodeName to advertise is:", *nodeName)
	attributes[pkgnodereg.AttributeKeyNodeName] = *nodeName

	log.Println("Waiting for queue name to be generated...")
	queueNameTimeoutCtx, canceller := context.WithTimeout(context.Background(), 10*time.Second)
	queueName, err := rbmqResponder.GetQueueNameWithContext(queueNameTimeoutCtx)
	if err != nil {
		log.Fatalf("Failed to get queue name: %v", err)
	}
	canceller()
	log.Println("QueueName to advertise will be:", queueName)

	attributes[pkgnodereg.AttributeKeyRabbitMQQueueName] = queueName
	agent := pkgnodereg.NodeRegistrationAgent{
		ServerAddress:  *addr,
		WebSocketPath:  *path,
		NodeName:       *nodeName,
		LogEchoReplies: *logEchoReplies,
	}
	agent.NodeAttributes = attributes
	log.Println("Node attributes will be announced as:", attributes)

	log.Println("Initializing node registration agent...")
	if err = agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	log.Println("Starting node registration agent...")
	nodeRegAgentErrCh := agent.Run()

	<-sigs

	log.Println("Shutting down ping responder...")
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
