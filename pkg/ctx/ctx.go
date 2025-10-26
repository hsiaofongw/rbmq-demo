package ctx

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ContextKey string

const ContextKeyRabbitMQChannel = ContextKey("rabbitmq_channel")
const ContextKeyRabbitMQConnection = ContextKey("rabbitmq_connection")

func WithRabbitMQChannel(ctx context.Context, ch *amqp.Channel) context.Context {
	return context.WithValue(ctx, ContextKeyRabbitMQChannel, ch)
}

func GetRabbitMQChannel(ctx context.Context) (*amqp.Channel, error) {
	ch, ok := ctx.Value(ContextKeyRabbitMQChannel).(*amqp.Channel)
	if !ok {
		return nil, fmt.Errorf("rabbitmq channel not found in context")
	}
	return ch, nil
}

func WithRabbitMQConnection(ctx context.Context, conn *amqp.Connection) context.Context {
	return context.WithValue(ctx, ContextKeyRabbitMQConnection, conn)
}

func GetRabbitMQConnection(ctx context.Context) (*amqp.Connection, error) {
	conn, ok := ctx.Value(ContextKeyRabbitMQConnection).(*amqp.Connection)
	if !ok {
		return nil, fmt.Errorf("rabbitmq connection not found in context")
	}
	return conn, nil
}
