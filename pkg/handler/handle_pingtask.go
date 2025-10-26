package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	pkgpinger "example.com/rbmq-demo/pkg/pinger"
	pkgsimpleping "example.com/rbmq-demo/pkg/simpleping"
)

type PingTaskHandler struct{}

func NewPingTaskHandler() *PingTaskHandler {
	return &PingTaskHandler{}
}

type PingTaskApplicationForm struct {
	Targets []string `json:"targets"`
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

	// Create pingers for all targets
	pingers := make([]pkgpinger.Pinger, 0, len(form.Targets))
	for _, target := range form.Targets {
		cfg := &pkgsimpleping.PingConfiguration{
			Destination: target,
			Count:       3,
			Timeout:     10 * time.Second,
			Interval:    1 * time.Second,
		}
		pingers = append(pingers, pkgsimpleping.NewSimplePinger(cfg))
	}

	// Start multiple pings in parallel
	eventCh := pkgpinger.StartMultiplePings(pingers)

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
