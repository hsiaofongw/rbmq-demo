package nodereg

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgframing "example.com/rbmq-demo/pkg/framing"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	AttributeKeyPingCapability    = "CapabilityPing"
	AttributeKeyRabbitMQQueueName = "RabbitMQQueueName"
)

type NodeRegistrationAgent struct {
	ServerAddress  string
	WebSocketPath  string
	NodeName       string
	CorrelationID  *string
	SeqID          *uint64
	TickInterval   *time.Duration
	intialized     bool
	closed         bool
	closeMutex     sync.Mutex
	closeCh        chan struct{}
	NodeAttributes pkgconnreg.ConnectionAttributes
	LogEchoReplies bool
}

func (agent *NodeRegistrationAgent) Init() error {
	if agent.ServerAddress == "" {
		return fmt.Errorf("server address is required")
	}

	if agent.WebSocketPath == "" {
		agent.WebSocketPath = "/ws"
		log.Printf("Using default web socket path: %s", agent.WebSocketPath)
	}

	if agent.NodeName == "" {
		return fmt.Errorf("node name is required")
	}

	if agent.CorrelationID == nil {
		corrId := uuid.New().String()
		agent.CorrelationID = &corrId
		log.Printf("Using default correlation ID: %s", corrId)
	}

	if agent.SeqID == nil {
		seqId := uint64(0)
		agent.SeqID = &seqId
		log.Printf("Will start at sequence ID: %d", seqId)
	}

	if agent.TickInterval == nil {
		agent.TickInterval = new(time.Duration)
		*agent.TickInterval = 1 * time.Second
		log.Printf("Using default tick interval: %s", *agent.TickInterval)
	}

	agent.closeCh = make(chan struct{})

	agent.intialized = true
	return nil
}

func (agent *NodeRegistrationAgent) runReceiver(c *websocket.Conn) error {
	for {
		msgTy, msg, err := c.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed to read message from %s: %v", c.RemoteAddr(), err)
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
				payload.Echo.CorrelationID == *agent.CorrelationID &&
				payload.Echo.Direction == pkgconnreg.EchoDirectionS2C {

				rtt, onTrip, backTrip := payload.Echo.CalculateDelays(time.Now())
				if agent.LogEchoReplies {
					log.Printf("Received echo reply: Seq: %d, RTT: %d ms, On-trip: %d ms, Back-trip: %d ms", payload.Echo.SeqID, rtt.Milliseconds(), onTrip.Milliseconds(), backTrip.Milliseconds())
				}
			}

		default:
			log.Printf("Received unknown message type from %s: %d", c.RemoteAddr(), msgTy)
		}
	}
}

func (agent *NodeRegistrationAgent) sendMessage(c *websocket.Conn, msg interface{}) error {
	jsonMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}
	err = c.WriteMessage(websocket.TextMessage, jsonMsg)
	if err != nil {
		return fmt.Errorf("failed to write message: %v", err)
	}
	return nil
}

// Connect and start the loop
func (agent *NodeRegistrationAgent) Run() error {
	if !agent.intialized {
		return fmt.Errorf("agent not initialized")
	}

	u := url.URL{
		Scheme: "ws",
		Host:   agent.ServerAddress,
		Path:   agent.WebSocketPath,
	}

	errCh := make(chan error)

	go func() {
		defer close(errCh)

		log.Printf("Agent %s started, connecting to %s", agent.NodeName, u.String())
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			errCh <- fmt.Errorf("failed to dial %s: %v", u.String(), err)
			return
		}
		defer c.Close()

		log.Printf("Connected to server %s: remote address: %s", u.String(), c.RemoteAddr())

		receiverExit := make(chan error)
		go func() {
			receiverExit <- agent.runReceiver(c)
		}()

		ticker := time.NewTicker(*agent.TickInterval)
		defer ticker.Stop()

		log.Printf("Using node name: %s", agent.NodeName)
		registerPayload := pkgconnreg.RegisterPayload{
			NodeName: agent.NodeName,
		}
		registerMsg := pkgframing.MessagePayload{
			Register: &registerPayload,
		}
		if agent.NodeAttributes != nil {
			registerMsg.AttributesAnnouncement = &pkgconnreg.AttributesAnnouncementPayload{
				Attributes: agent.NodeAttributes,
			}
			s, _ := json.Marshal(registerMsg.AttributesAnnouncement)
			log.Printf("Will announcing attributes: %+v", string(s))
		}
		log.Printf("Sending register message")
		err = agent.sendMessage(c, registerMsg)
		if err != nil {
			errCh <- fmt.Errorf("failed to send register message: %v", err)
			return
		}

		for {
			select {
			case receiverErr := <-receiverExit:
				var err error
				if receiverErr != nil {
					err = fmt.Errorf("receiver exited with error: %v", receiverErr)
				}
				errCh <- err
				return
			case <-ticker.C:
				msg := pkgframing.MessagePayload{
					Echo: &pkgconnreg.EchoPayload{
						Direction:     pkgconnreg.EchoDirectionC2S,
						CorrelationID: *agent.CorrelationID,
						Timestamp:     uint64(time.Now().UnixMilli()),
						SeqID:         *agent.SeqID,
					},
				}
				nextSeq := *agent.SeqID + 1
				*agent.SeqID = nextSeq

				err := agent.sendMessage(c, msg)
				if err != nil {
					errCh <- fmt.Errorf("failed to send echo message: %v", err)
					return
				}
			case <-agent.closeCh:
				agent.closed = true
				errCh <- nil
				return
			}
		}
	}()

	return <-errCh
}

func (agent *NodeRegistrationAgent) Shutdown() error {
	agent.closeMutex.Lock()
	defer agent.closeMutex.Unlock()

	if !agent.intialized {
		return fmt.Errorf("agent not initialized")
	}

	if agent.closed {
		return fmt.Errorf("agent already closed")
	}

	close(agent.closeCh)
	return nil
}
