package rabbitmqping

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"

	pkgctx "example.com/rbmq-demo/pkg/ctx"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	"github.com/google/uuid"
)

// RabbitMQPinger is also an implementation of the Pinger interface
type RabbitMQPinger struct {
	From       string
	RoutingKey string
	PingCfg    pkgsimpleping.PingConfiguration
}

func (rbmqPinger *RabbitMQPinger) Ping(ctx context.Context) <-chan pkgpinger.PingEvent {

	evChan := make(chan pkgpinger.PingEvent)

	metadata := map[string]string{
		pkgpinger.MetadataRabbitMQRoutingKey: rbmqPinger.RoutingKey,
		pkgpinger.MetadataKeyFrom:            rbmqPinger.From,
	}

	go func() {
		defer close(evChan)

		conn, err := pkgctx.GetRabbitMQConnection(ctx)
		if err != nil {
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to obtain a RabbitMQ connection within RabbitMQPinger: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
			return
		}

		ch, err := conn.Channel()
		if err != nil {
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to open a RabbitMQ channel within RabbitMQPinger: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
		}
		defer ch.Close()

		// queue for reading RPC responses from the RPC server
		q, err := ch.QueueDeclare(
			"",    // queue name, generated randomly
			false, // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to declare a RabbitMQ queue within RabbitMQPinger: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
			return
		}
		defer ch.QueueDelete(q.Name, false, false, false)

		// these 'msgs' are responses from the RPC server
		msgs, err := ch.Consume(
			q.Name, // queue
			"",     // consumer
			true,   // auto-ack
			false,  // exclusive
			false,  // no-local
			false,  // no-wait
			nil,    // arguments
		)

		if err != nil {
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to register a consumer within RabbitMQPinger: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
			return
		}

		msgBody, err := json.Marshal(rbmqPinger.PingCfg)
		if err != nil {
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to marshal the ping configuration within RabbitMQPinger: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
			return
		}

		corrId := uuid.New().String()

		ctx, cancel := context.WithTimeout(ctx, rbmqPinger.PingCfg.Timeout)
		defer cancel()

		err = ch.PublishWithContext(ctx,
			"",                    // exchange
			rbmqPinger.RoutingKey, // routing key
			false,                 // mandatory
			false,                 // immediate
			amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: corrId,
				ReplyTo:       q.Name,
				Body:          msgBody,
			},
		)

		if err != nil {
			log.Println("Failed to publish the message to the RabbitMQ exchange:", err)
			ev := pkgpinger.PingEvent{
				Type:     pkgpinger.PingEventTypeError,
				Error:    fmt.Errorf("failed to publish the message to the RabbitMQ exchange: %w", err),
				Metadata: metadata,
			}
			evChan <- ev
			return
		}

		for msg := range msgs {
			if msg.CorrelationId == corrId {
				var pingEvent pkgpinger.PingEvent
				err := json.Unmarshal(msg.Body, &pingEvent)
				if err != nil {
					log.Println("Failed to unmarshal the message:", string(msg.Body), "error:", err)
				}
				pingEvent.Metadata = metadata
				evChan <- pingEvent
			}
		}

	}()

	return evChan
}
