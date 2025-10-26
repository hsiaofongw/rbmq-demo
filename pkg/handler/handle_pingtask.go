package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	pkgctx "example.com/rbmq-demo/pkg/ctx"
	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgrabbitmqping "example.com/rbmq-demo/pkg/rabbitmqping"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
	amqp "github.com/rabbitmq/amqp091-go"
)

type PingTaskHandler struct {
	RabbitMQConnection *amqp.Connection
}

func NewPingTaskHandler(ctx context.Context) (*PingTaskHandler, error) {
	rbmqConn, err := pkgctx.GetRabbitMQConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get RabbitMQ connection: %w", err)
	}

	return &PingTaskHandler{
		RabbitMQConnection: rbmqConn,
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

	var pingers []pkgpinger.Pinger = nil
	ctx := context.Background()

	if form.From == nil {
		// Create pingers for all targets
		pingers = make([]pkgpinger.Pinger, 0)
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
		ch, err := handler.RabbitMQConnection.Channel()
		if err != nil {
			respondError(w, fmt.Errorf("failed to open a RabbitMQ channel to broker: %w", err), http.StatusInternalServerError)
			return
		}
		defer ch.Close()

		ctx = pkgctx.WithRabbitMQChannel(ctx, ch)

		pingers = make([]pkgpinger.Pinger, 0)
		for _, from := range form.From {
			for _, target := range form.Targets {
				// todo: get routing key from node discovery agent, if routing key is not found, skip
				rabbitmqPinger := pkgrabbitmqping.RabbitMQPinger{
					From:       from,
					RoutingKey: from,
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
