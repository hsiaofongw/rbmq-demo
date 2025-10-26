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

type TaskUpdate struct {
	TaskMsg  *amqp.Delivery
	Err      error
	Envelope *amqp.Publishing
}

func (rbmqResponder *RabbitMQResponder) handleTask(ctx context.Context, taskMsg *amqp.Delivery) <-chan TaskUpdate {
	log.Println("Received a message", "exchg", taskMsg.Exchange, "routing_key", taskMsg.RoutingKey, "correlation_id", taskMsg.CorrelationId, "message_id", taskMsg.MessageId, "reply_to", taskMsg.ReplyTo)

	updatesCh := make(chan TaskUpdate)

	go func(taskMsg *amqp.Delivery) {
		defer close(updatesCh)

		var pingCfg pkgsimpleping.PingConfiguration
		err := json.Unmarshal(taskMsg.Body, &pingCfg)
		if err != nil {
			updatesCh <- TaskUpdate{
				Err:     fmt.Errorf("failed to unmarshal the message: %w, message_id %s, correlation_id, %s, reply_to %s", err, taskMsg.MessageId, taskMsg.CorrelationId, taskMsg.ReplyTo),
				TaskMsg: taskMsg,
			}
			return
		}

		log.Printf("Message %s corr_id %s is a PingConfiguration, destination %s", taskMsg.MessageId, taskMsg.CorrelationId, pingCfg.Destination)
		pinger := pkgsimpleping.NewSimplePinger(&pingCfg)

		log.Println("Starting to ping destination:", pingCfg.Destination, "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId, "queue", taskMsg.ReplyTo)
		pingEvents := pinger.Ping(ctx)

		log.Println("Retrieving ping events about destination:", pingCfg.Destination)
		for ev := range pingEvents {
			evJson, err := json.Marshal(ev)
			if err != nil {
				log.Println("Failed to marshal ping event:", ev, "error:", err, "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId, "queue", taskMsg.ReplyTo)
				continue
			}
			log.Println("Ping reply:", "type", ev.Type, "metadata", ev.Metadata, "error", ev.Error)
			updatesCh <- TaskUpdate{
				Envelope: &amqp.Publishing{
					ContentType:   "application/json",
					CorrelationId: taskMsg.CorrelationId,
					Body:          evJson,
				},
				TaskMsg: taskMsg,
			}
		}
		updatesCh <- TaskUpdate{
			TaskMsg: taskMsg,
		}
	}(taskMsg)

	return updatesCh
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

		taskUpdatesCh := make(chan TaskUpdate)
		taskUpdatesChCloser := make(chan interface{})
		defer close(taskUpdatesChCloser)
		go func() {
			defer close(taskUpdatesCh)
			log.Println("Starting to handle task updates...")
			for {
				select {
				case <-taskUpdatesChCloser:
					log.Println("Task updates channel closed, shutting down...")
					return
				case taskUpdate := <-taskUpdatesCh:
					if taskUpdate.Err != nil {
						log.Println("Failed to handle task:", taskUpdate.Err, "message_id", taskUpdate.TaskMsg.MessageId, "correlation_id", taskUpdate.TaskMsg.CorrelationId, "queue", taskUpdate.TaskMsg.ReplyTo)
					} else if taskUpdate.Envelope != nil {
						err = ch.PublishWithContext(ctx,
							"",                         // exchange
							taskUpdate.TaskMsg.ReplyTo, // routing key
							false,                      // mandatory
							false,                      // immediate
							*taskUpdate.Envelope,
						)
						if err != nil {
							log.Println("Failed to send back the ping event (ping reply) to the ping requester:", taskUpdate.Envelope, "error", err, "queue", taskUpdate.TaskMsg.ReplyTo, "correlation_id", taskUpdate.TaskMsg.CorrelationId)
							continue
						}
					} else if taskUpdate.TaskMsg != nil {
						taskUpdate.TaskMsg.Ack(false)
					}
				}
			}
		}()

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

		taskMsgs, err := ch.Consume(
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
				log.Println("Shutting down RabbitMQ responder...", "queue", q.Name)
				return
			case taskMsg := <-taskMsgs:
				go func(taskMsg amqp.Delivery) {
					log.Println("Starting to handle task:", "message_id", taskMsg.MessageId, "correlation_id", taskMsg.CorrelationId, "queue", taskMsg.ReplyTo)
					for taskUpdate := range rbmqResponder.handleTask(ctx, &taskMsg) {
						log.Println("New task update:", "message_id", taskUpdate.TaskMsg.MessageId, "correlation_id", taskUpdate.TaskMsg.CorrelationId, "queue", taskUpdate.TaskMsg.ReplyTo)
						taskUpdatesCh <- taskUpdate
					}
				}(taskMsg)
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
