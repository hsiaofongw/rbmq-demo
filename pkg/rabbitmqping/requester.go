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

	respondWithError := func(err error) {
		log.Println("RabbitMQPinger error:", err)
		ev := pkgpinger.PingEvent{
			Type:     pkgpinger.PingEventTypeError,
			Error:    err,
			Metadata: metadata,
		}
		evChan <- ev
	}

	go func() {
		defer close(evChan)

		conn, err := pkgctx.GetRabbitMQConnection(ctx)
		if err != nil {
			respondWithError(fmt.Errorf("failed to obtain a RabbitMQ connection within RabbitMQPinger: %w", err))
			return
		}

		ch, err := conn.Channel()
		if err != nil {
			respondWithError(fmt.Errorf("failed to open a RabbitMQ channel within RabbitMQPinger: %w", err))
			return
		}
		defer ch.Close()

		// queue for reading RPC responses from the RPC server
		q, err := ch.QueueDeclare(
			"",    // queue name, generated randomly
			false, // durable
			true,  // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
		if err != nil {
			respondWithError(fmt.Errorf("failed to declare a RabbitMQ queue within RabbitMQPinger: %w", err))
			return
		}

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
			respondWithError(fmt.Errorf("failed to register a consumer within RabbitMQPinger: %w", err))
			return
		}

		msgBody, err := json.Marshal(rbmqPinger.PingCfg)
		if err != nil {
			respondWithError(fmt.Errorf("failed to marshal the ping configuration within RabbitMQPinger: %w", err))
			return
		}

		corrId := uuid.New().String()

		ctx, cancel := context.WithTimeout(ctx, rbmqPinger.PingCfg.Timeout)
		defer cancel()

		msgId := uuid.New().String()

		exchgName := ""

		err = ch.PublishWithContext(ctx,
			exchgName,             // exchange
			rbmqPinger.RoutingKey, // routing key
			false,                 // mandatory
			false,                 // immediate
			amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: corrId,
				ReplyTo:       q.Name,
				Body:          msgBody,
				MessageId:     msgId,
			},
		)

		log.Println("Published a message", "exchg", exchgName, "routing_key", rbmqPinger.RoutingKey, "correlation_id", corrId, "message_id", msgId, "reply_to", q.Name)

		if err != nil {
			respondWithError(fmt.Errorf("failed to publish the message to the RabbitMQ exchange: %w", err))
			return
		}

		for msg := range msgs {
			log.Println("Received a message", "exchg", msg.Exchange, "routing_key", msg.RoutingKey, "correlation_id", msg.CorrelationId, "message_id", msg.MessageId)
			if msg.CorrelationId == corrId {
				if len(msg.Body) == 0 {
					// A nill message body signals the end of the message stream
					break
				}

				var pingEvent pkgpinger.PingEvent
				err := json.Unmarshal(msg.Body, &pingEvent)
				if err != nil {
					respondWithError(fmt.Errorf("failed to unmarshal the message: %w", err))
					continue
				}
				pingEvent.Metadata = metadata
				evChan <- pingEvent
			}
		}
	}()

	return evChan
}
