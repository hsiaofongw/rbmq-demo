package pinger

import (
	"context"
	"encoding/json"
	"log"
	"sync"
)

type PingEventType string

const (
	PingEventTypePktRecv    PingEventType = "pkt_recv"
	PingEventTypePktDupRecv PingEventType = "pkt_dup_recv"
	PingEventTypePingStats  PingEventType = "ping_stats"
	PingEventTypeError      PingEventType = "error"
)

type PingEvent struct {
	Type     PingEventType     `json:"type"`
	Data     interface{}       `json:"data,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Error    error             `json:"error,omitempty"`
}

const MetadataRabbitMQRoutingKey = "rabbitmq_routing_key"
const MetadataKeyFrom = "from"

func (ev *PingEvent) String() string {
	j, err := json.Marshal(ev)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return ""
	}
	return string(j)
}

type Pinger interface {
	// the Ping action of a Pinger is guaranted to return (it has its own timeout mechanism),
	// so the caller does not need to define the timeout timer in outer scope.
	Ping(ctx context.Context) <-chan PingEvent
}

func StartMultiplePings(ctx context.Context, pingers []Pinger) <-chan PingEvent {
	// Create the output channel
	eventCh := make(chan PingEvent)

	// Start a goroutine that manages all the pinger goroutines
	go func() {
		defer close(eventCh)

		// If no configurations, just return
		if len(pingers) == 0 {
			return
		}

		// Use WaitGroup to wait for all pingers to complete
		var wg sync.WaitGroup

		// Start all pingers
		for i := range pingers {
			wg.Add(1)
			go func(pinger Pinger) {
				defer wg.Done()

				// Get the event channel for this pinger
				pingCh := pinger.Ping(ctx)

				// Forward all events from this pinger to the output channel
				for ev := range pingCh {
					eventCh <- ev
				}
			}(pingers[i])
		}

		// Wait for all pingers to complete
		wg.Wait()
	}()

	return eventCh
}
