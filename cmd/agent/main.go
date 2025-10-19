package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgframing "example.com/rbmq-demo/pkg/framing"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")

func main() {
	flag.Parse()

	log.Println("Agent started", "Using address:", *addr)

	u := url.URL{Scheme: "ws", Host: *addr, Path: *path}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	log.Printf("Connected to server %s: remote address: %s", u.String(), c.RemoteAddr())

	correlationID := uuid.New().String()
	log.Printf("Using correlation ID: %s", correlationID)

	receiverExit := make(chan struct{})
	go func() {
		defer close(receiverExit)
		for {
			msgTy, msg, err := c.ReadMessage()
			if err != nil {
				log.Printf("Failed to read message from %s: %v", c.RemoteAddr(), err)
				break
			}

			switch msgTy {
			case websocket.TextMessage:
				var payload pkgframing.MessagePayload
				err := json.Unmarshal(msg, &payload)
				if err != nil {
					log.Printf("Failed to unmarshal message from %s: %v", c.RemoteAddr(), err)
					continue
				}

				if payload.Echo != nil &&
					payload.Echo.CorrelationID == correlationID &&
					payload.Echo.Direction == pkgconnreg.EchoDirectionS2C {
					log.Printf("Received echo response from %s: %s", c.RemoteAddr(), string(msg))

					now := uint64(time.Now().UnixMilli())
					rtt := time.Duration(now-payload.Echo.Timestamp) * time.Millisecond
					onTrip := time.Duration(payload.Echo.ServerTimestamp-payload.Echo.Timestamp) * time.Millisecond
					backTrip := time.Duration(now-payload.Echo.ServerTimestamp) * time.Millisecond

					log.Printf("Seq: %d, RTT: %d ms, On-trip: %d ms, Back-trip: %d ms", payload.Echo.SeqID, rtt.Milliseconds(), onTrip.Milliseconds(), backTrip.Milliseconds())

				}

			default:
				log.Printf("Received unknown message type from %s: %d", c.RemoteAddr(), msgTy)
			}
		}
	}()

	tickIntvl := 1 * time.Second
	log.Printf("Setting tick interval to %s", tickIntvl)
	ticker := time.NewTicker(tickIntvl)
	defer ticker.Stop()

	var seq *uint64 = new(uint64)
	*seq = 0
	log.Printf("Starting sequence number at %d", *seq)

	log.Printf("Using node name: %s", *nodeName)
	registerPayload := pkgconnreg.RegisterPayload{
		NodeName: *nodeName,
	}
	registerMsg := pkgframing.MessagePayload{
		Register: &registerPayload,
	}
	registerJSON, err := json.Marshal(registerMsg)
	if err != nil {
		log.Printf("Failed to marshal register message: %v", err)
		return
	}
	log.Printf("Sending register message: %s", string(registerJSON))
	err = c.WriteMessage(websocket.TextMessage, registerJSON)
	if err != nil {
		log.Printf("Failed to write register message: %v", err)
		return
	}

	for {
		select {
		case <-receiverExit:
			log.Printf("Receiver exited, stopping sender")
			return
		case tick := <-ticker.C:
			msg := pkgframing.MessagePayload{
				Echo: &pkgconnreg.EchoPayload{
					Direction:     pkgconnreg.EchoDirectionC2S,
					CorrelationID: correlationID,
					Timestamp:     uint64(tick.UnixMilli()),
					SeqID:         uint64(*seq),
				},
			}
			nextSeq := *seq + 1
			*seq = nextSeq

			jsonMsg, err := json.Marshal(msg)
			if err != nil {
				log.Printf("Failed to marshal echo message: %v", err)
				continue
			}
			log.Printf("Sending echo message: %s", string(jsonMsg))
			err = c.WriteMessage(websocket.TextMessage, jsonMsg)
			if err != nil {
				log.Printf("Failed to write echo message: %v", err)
				return
			}
		}
	}

}
