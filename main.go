package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func failOnError(err error, msg string) {
	if err != nil {
		log.Panicf("%s: %s", msg, err)
	}
}

const (
	QueueName = "hello"
)

type Msg struct {
	Seq       int
	Timestamp int64
	Type      string
}

func (m *Msg) String() string {
	return fmt.Sprintf("Seq %d, Timestamp %s, Type: %s", m.Seq, time.UnixMilli(m.Timestamp).Format(time.RFC3339), m.Type)
}

func (m *Msg) Send(ctx context.Context, timeOut time.Duration, ch *amqp.Channel, q *amqp.Queue) error {
	ctx, cancel := context.WithTimeout(ctx, timeOut)
	defer cancel()

	exchgName := ""
	mandatory := false
	immediate := false
	return ch.PublishWithContext(ctx,
		exchgName,
		q.Name,
		mandatory,
		immediate,
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(m.String()),
		})
}

func Client(ctx context.Context, amqpURL string) error {
	log.Println("Client started", "Using URL:", amqpURL)
	conn, err := amqp.Dial(amqpURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()
	log.Printf("Client %s is connected to RabbitMQ broker %s", conn.LocalAddr(), conn.RemoteAddr())

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()
	log.Printf("Client is opened a channel: %s", conn.LocalAddr())

	queueName := QueueName
	queueDurable := false
	queueDeleteWhenUnused := false
	queueExclusive := false
	queueNoWait := false
	q, err := ch.QueueDeclare(
		queueName,
		queueDurable,
		queueDeleteWhenUnused,
		queueExclusive,
		queueNoWait,
		nil,
	)
	failOnError(err, "Failed to declare a queue")
	log.Printf("Client %s is declared a queue %s", conn.LocalAddr(), q.Name)

	ticker := time.NewTicker(1 * time.Second)

	seq := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("Client %s is exiting", conn.LocalAddr())
			return nil
		case tick := <-ticker.C:
			msg := Msg{
				Seq:       seq,
				Timestamp: tick.UnixMilli(),
				Type:      "Ping",
			}
			seq++
			go func() {
				err := msg.Send(ctx, 5*time.Second, ch, &q)
				if err != nil {
					log.Printf("Failed to send message, seq %d: %v", msg.Seq, err)
				}
			}()
		}
	}
}

func Server(ctx context.Context, amqpURL string) error {
	log.Println("Server started", "Using URL:", amqpURL)

	conn, err := amqp.Dial(amqpURL)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()
	log.Printf("Server %s is connected to RabbitMQ broker %s", conn.LocalAddr(), conn.RemoteAddr())

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()
	log.Printf("Server %s is opened a channel", conn.LocalAddr())

	queueName := QueueName
	durable := false
	deleteWhenUnused := false
	exclusive := false
	noWait := false
	q, err := ch.QueueDeclare(
		queueName,
		durable,
		deleteWhenUnused,
		exclusive,
		noWait,
		nil,
	)
	failOnError(err, "Failed to declare a queue")
	log.Printf("Server %s is declared a queue %s", conn.LocalAddr(), q.Name)

	log.Printf("Server %s is starting to consume messages from queue %s", conn.LocalAddr(), q.Name)
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	for {
		select {
		case <-ctx.Done():
			log.Printf("Server %s is exiting", conn.LocalAddr())
			return nil
		case msg := <-msgs:
			log.Printf("Server %s received a message: %s", conn.LocalAddr(), string(msg.Body))

		}
	}
}

const (
	AmqpURL = "amqp://localhost:5672/"
)

func main() {
	var wg sync.WaitGroup

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	clientCtx, clientCancel := context.WithCancel(context.Background())

	serverCtx, serverCancel := context.WithCancel(context.Background())

	amqpURL := AmqpURL

	wg.Add(1)
	go func() {
		err := Client(clientCtx, amqpURL)
		if err != nil {
			log.Fatalf("Client error: %v", err)
		} else {
			log.Println("Client exited successfully")
		}
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		err := Server(serverCtx, amqpURL)
		if err != nil {
			log.Fatalf("Server error: %v", err)
		} else {
			log.Println("Server exited successfully")
		}
		wg.Done()
	}()

	<-sigs
	clientCancel()
	serverCancel()
	wg.Wait()
}
