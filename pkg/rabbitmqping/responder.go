package rabbitmqping

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQResponder struct {
	URL            string
	closeCh        chan interface{}
	shutdownMutex  sync.Mutex
	closed         bool
	initalized     bool
	queueNameIsSet bool
	queueName      chan string
}

func (rbmqResponder *RabbitMQResponder) Init() error {
	rbmqResponder.closeCh = make(chan interface{})
	rbmqResponder.queueName = make(chan string, 1)
	rbmqResponder.initalized = true
	return nil
}

func (rbmqResponder *RabbitMQResponder) GetQueueNameWithContext(ctx context.Context) (string, error) {
	if !rbmqResponder.initalized {
		panic("RabbitMQResponder not initialized")
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case queueName := <-rbmqResponder.queueName:
		return queueName, nil
	}
}

func (rbmqResponder *RabbitMQResponder) GetQueueName() <-chan string {
	if !rbmqResponder.initalized {
		panic("RabbitMQResponder not initialized")
	}

	queueNameCh := make(chan string)

	go func() {
		defer close(queueNameCh)
		queueName := <-rbmqResponder.queueName
		queueNameCh <- queueName

	}()

	return queueNameCh
}

func (rbmqResponder *RabbitMQResponder) setQueueName(queueName string) {
	if !rbmqResponder.initalized {
		panic("RabbitMQResponder not initialized")
	}

	if rbmqResponder.queueNameIsSet {
		panic("RabbitMQResponder queue name already set")
	}
	rbmqResponder.queueNameIsSet = true

	// Note: the rbmqResponder.queueName must be a buffered channel with size 1,
	// to guarantee non-blocking write and blocking read (when empty).
	rbmqResponder.queueName <- queueName
}

func (rbmqResponder *RabbitMQResponder) ServeRPC(ctx context.Context) <-chan error {
	if !rbmqResponder.initalized {
		panic("RabbitMQResponder not initialized")
	}

	errChan := make(chan error)

	go func() {
		defer close(errChan)

		conn, err := amqp.Dial(rbmqResponder.URL)
		if err != nil {
			errChan <- fmt.Errorf("failed to dial RabbitMQ: %w", err)
			return
		}
		defer conn.Close()

		ch, err := conn.Channel()
		if err != nil {
			errChan <- fmt.Errorf("failed to open a channel: %w", err)
		}
		defer ch.Close()

		q, err := ch.QueueDeclare(
			"",    // auto generated queue name
			false, // durable
			true,  // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			errChan <- fmt.Errorf("failed to declare a queue: %w", err)
			return
		}

		log.Println("Declared a queue", "queue", q.Name)
		rbmqResponder.setQueueName(q.Name)

		err = ch.Qos(1, 0, false) // prefetch count, prefetch size, global
		if err != nil {
			errChan <- fmt.Errorf("failed to set QOS: %w", err)
			return
		}

		msgs, err := ch.Consume(
			q.Name, // queue
			"",     // consumer
			false,  // auto-ack
			false,  // exclusive
			false,  // no-local
			false,  // no-wait
			nil,    // arguments
		)

		if err != nil {
			errChan <- fmt.Errorf("failed to register a consumer: %w", err)
			return
		}

		for {
			select {
			case <-rbmqResponder.closeCh:
				log.Println("Shutting down RabbitMQ responder...")
				close(rbmqResponder.closeCh)
				return
			case msg := <-msgs:
				log.Println("Received a message", "exchg", msg.Exchange, "routing_key", msg.RoutingKey, "correlation_id", msg.CorrelationId, "message_id", msg.MessageId, "queue", q.Name, "reply_to", msg.ReplyTo)

				var pingCfg pkgsimpleping.PingConfiguration
				err := json.Unmarshal(msg.Body, &pingCfg)
				if err != nil {
					log.Println("Failed to unmarshal the message:", string(msg.Body), "error:", err)
					msg.Ack(false)
					continue
				}

				log.Println("Received a PingConfiguration from broker:", conn.RemoteAddr(), "destination:", pingCfg.Destination)

				pinger := pkgsimpleping.NewSimplePinger(&pingCfg)

				log.Println("Starting to ping destination:", pingCfg.Destination)
				pingEvents := pinger.Ping(ctx)

				log.Println("Retrieving ping events about destination:", pingCfg.Destination)
				for ev := range pingEvents {
					evJson, err := json.Marshal(ev)
					if err != nil {
						log.Println("Failed to marshal the event:", ev, "error:", err)
						continue
					}
					log.Println("Ping reply:", "type", ev.Type, "metadata", ev.Metadata, "error", ev.Error)

					err = ch.PublishWithContext(ctx,
						"",          // exchange
						msg.ReplyTo, // routing key
						false,       // mandatory
						false,       // immediate
						amqp.Publishing{
							ContentType:   "application/json",
							CorrelationId: msg.CorrelationId,
							Body:          evJson,
						},
					)
					if err != nil {
						log.Println("Failed to send back the ping event (ping reply) to the ping requester:", ev, "error", err, "queue", msg.ReplyTo, "correlation_id", msg.CorrelationId)
						continue
					}
				}

				msg.Ack(false)
			}
		}
	}()

	return errChan
}

var ErrRabbitMQResponderAlreadyClosed = fmt.Errorf("RabbitMQ responder already closed")

func (rbmqResponder *RabbitMQResponder) Shutdown() error {
	rbmqResponder.shutdownMutex.Lock()
	defer rbmqResponder.shutdownMutex.Unlock()

	if rbmqResponder.closed {
		return ErrRabbitMQResponderAlreadyClosed
	}

	rbmqResponder.closed = true

	close(rbmqResponder.closeCh)
	return nil
}
