package rabbitmqping

import (
	"context"
	"fmt"
	"log"

	"encoding/json"

	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQResponder struct {
	URL       string
	QueueName string
}

func (rbmqResponder *RabbitMQResponder) ServeRPC(ctx context.Context) <-chan error {
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
			rbmqResponder.QueueName, // queue name
			false,                   // durable
			false,                   // delete when unused
			false,                   // exclusive
			false,                   // no-wait
			nil,                     // arguments
		)
		if err != nil {
			errChan <- fmt.Errorf("failed to declare a queue: %w", err)
			return
		}

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

		for msg := range msgs {
			log.Println("Received a message:", string(msg.Body), "from", conn.RemoteAddr())

			var pingCfg pkgsimpleping.PingConfiguration
			err := json.Unmarshal(msg.Body, &pingCfg)
			if err != nil {
				log.Println("Failed to unmarshal the message:", string(msg.Body), "error:", err)
				msg.Ack(false)
				continue
			}

			pinger := pkgsimpleping.NewSimplePinger(&pingCfg)
			pingEvents := pinger.Ping(ctx)
			for ev := range pingEvents {
				evJson, err := json.Marshal(ev)
				if err != nil {
					log.Println("Failed to marshal the event:", ev, "error:", err)
					continue
				}
				log.Println("Ping event:", string(evJson))

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
					log.Println("Failed to publish the event:", ev, "error:", err)
					continue
				}
			}

			msg.Ack(false)
		}
	}()

	return errChan
}
