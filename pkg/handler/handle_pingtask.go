package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgctx "example.com/rbmq-demo/pkg/ctx"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgrabbitmqping "example.com/rbmq-demo/pkg/rabbitmqping"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	amqp "github.com/rabbitmq/amqp091-go"
)

type PingTaskHandler struct {
	RabbitMQConnection *amqp.Connection
	ConnRegistry       *pkgconnreg.ConnRegistry
}

func NewPingTaskHandler(ctx context.Context, connRegistry *pkgconnreg.ConnRegistry) (*PingTaskHandler, error) {
	rbmqConn, err := pkgctx.GetRabbitMQConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get RabbitMQ connection: %w", err)
	}

	return &PingTaskHandler{
		RabbitMQConnection: rbmqConn,
		ConnRegistry:       connRegistry,
	}, nil
}

type PingTaskApplicationForm struct {
	From       []string `json:"from,omitempty"`
	Targets    []string `json:"targets"`
	IntervalMs *uint64  `json:"interval,omitempty"`
	Count      *uint64  `json:"count,omitempty"`
	TimeoutMs  *uint64  `json:"timeout,omitempty"`
}

const defaultIntervalMs = 1000
const defaultCount = 3
const defaultTimeoutMs = 10 * 1000

func getRoutingKey(connRegistry *pkgconnreg.ConnRegistry, from string) *string {
	if connRegistry == nil {
		k := from
		return &k
	}

	regData, err := connRegistry.SearchByAttributes(pkgconnreg.ConnectionAttributes{
		pkgnodereg.AttributeKeyPingCapability: "true",
		pkgnodereg.AttributeKeyNodeName:       from,
	})
	if err != nil {
		log.Printf("Failed to search by attributes: %v", err)
		return nil
	}

	if regData == nil {
		// didn't found
		return nil
	}

	routingKey, ok := regData.Attributes[pkgnodereg.AttributeKeyRabbitMQQueueName]
	if ok {
		return &routingKey
	}

	f := from

	return &f
}

func respondError(w http.ResponseWriter, err error, status int) {
	w.WriteHeader(status)
	type errorResponse struct {
		Error string `json:"error"`
	}
	respbytes, err := json.Marshal(errorResponse{Error: err.Error()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, string(respbytes), status)
}

func (handler *PingTaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for streaming response
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Parse the request body
	var form PingTaskApplicationForm
	if err := json.NewDecoder(r.Body).Decode(&form); err != nil {
		respondError(w, fmt.Errorf("failed to parse request body: %w", err), http.StatusBadRequest)
		return
	}

	if len(form.Targets) == 0 {
		respondError(w, fmt.Errorf("no targets specified"), http.StatusBadRequest)
		return
	}
	// use simple ping
	var count int = defaultCount
	var timeout time.Duration = defaultTimeoutMs * time.Millisecond
	var interval time.Duration = defaultIntervalMs * time.Millisecond
	if form.Count != nil && *form.Count > 0 {
		count = int(*form.Count)
	}
	if form.TimeoutMs != nil && *form.TimeoutMs > 0 {
		timeout = time.Duration(*form.TimeoutMs) * time.Millisecond
	}
	if form.IntervalMs != nil && *form.IntervalMs > 0 {
		interval = time.Duration(*form.IntervalMs) * time.Millisecond
	}

	pingers := make([]pkgpinger.Pinger, 0)
	ctx := context.Background()

	if form.From == nil {
		// Create pingers for all targets
		for _, target := range form.Targets {
			cfg := &pkgsimpleping.PingConfiguration{
				Destination: target,
				Count:       count,
				Timeout:     timeout,
				Interval:    interval,
			}
			pingers = append(pingers, pkgsimpleping.NewSimplePinger(cfg))
		}
	} else {
		ctx = pkgctx.WithRabbitMQConnection(ctx, handler.RabbitMQConnection)
		for _, from := range form.From {
			for _, target := range form.Targets {
				routingKey := getRoutingKey(handler.ConnRegistry, from)
				if routingKey == nil {
					// the node might be currently offline, skip it for now
					continue
				}

				log.Printf("Sending ping to %s via RabbitMQ routing key %s", target, *routingKey)

				rabbitmqPinger := pkgrabbitmqping.RabbitMQPinger{
					From:       from,
					RoutingKey: *routingKey,
					PingCfg: pkgsimpleping.PingConfiguration{
						Destination: target,
						Count:       count,
						Timeout:     timeout,
						Interval:    interval,
					},
				}
				pingers = append(pingers, &rabbitmqPinger)
			}
		}
	}

	if len(pingers) == 0 {
		respondError(w, fmt.Errorf("no pingers to start"), http.StatusInternalServerError)
		return
	}

	// Start multiple pings in parallel
	eventCh := pkgpinger.StartMultiplePings(ctx, pingers)

	// Stream events as line-delimited JSON
	encoder := json.NewEncoder(w)
	for ev := range eventCh {
		if err := encoder.Encode(ev); err != nil {
			log.Printf("Failed to encode event: %v", err)
			respondError(w, fmt.Errorf("failed to encode event: %w", err), http.StatusInternalServerError)
			return
		}
		// Flush the response to send immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}
