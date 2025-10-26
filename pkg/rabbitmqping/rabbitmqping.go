package rabbitmqping

import (
	"context"
	"log"

	pkgctx "example.com/rbmq-demo/pkg/ctx"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
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
				Error:    err,
				Metadata: metadata,
			}
			evChan <- ev
			return
		}

		// todo: do more
		log.Println("RabbitMQ connection obtained", conn)

	}()

	return evChan
}
