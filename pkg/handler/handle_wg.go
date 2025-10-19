package handler

import (
	"encoding/json"
	"log"
	"net/http"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	"github.com/gorilla/websocket"
)

type WebsocketHandler struct {
	upgrader *websocket.Upgrader
	cr       *pkgconnreg.ConnRegistry
}

func NewWebsocketHandler(upgrader *websocket.Upgrader, cr pkgconnreg.ConnRegistry) *WebsocketHandler {
	return &WebsocketHandler{
		upgrader: upgrader,
	}
}

type MessagePayload struct {
	Register *pkgconnreg.RegisterPayload `json:"register,omitempty"`
	Echo     *pkgconnreg.EchoPayload     `json:"echo,omitempty"`
}

func (h *WebsocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := h.upgrader
	cr := h.cr
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v", err)
		return
	}

	cr.OpenConnection(conn)

	defer func() {
		log.Printf("Closing WebSocket connection: %s", conn.RemoteAddr())
		err := conn.Close()
		if err != nil {
			log.Printf("Failed to close WebSocket connection for %s: %v", conn.RemoteAddr(), err)
		}
		cr.CloseConnection(conn)
	}()

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Failed to read message from %s: %v", conn.RemoteAddr(), err)
			break
		}

		switch msgType {
		case websocket.TextMessage:
			var payload MessagePayload
			err := json.Unmarshal(msg, &payload)
			if err != nil {
				log.Printf("Failed to unmarshal message from %s: %v", conn.RemoteAddr(), err)
				break
			}
			if payload.Register != nil {
				cr.Register(conn, *payload.Register)
			}
			if payload.Echo != nil {
				cr.UpdateHeartbeat(conn)
			}
		default:
			log.Printf("Received unknown message type from %s: %d", conn.RemoteAddr(), msgType)
		}

		log.Printf("Received message from %s: %s", conn.RemoteAddr(), string(msg))
		err = conn.WriteMessage(msgType, msg)
		if err != nil {
			log.Printf("Failed to write message to %s: %v", conn.RemoteAddr(), err)
			break
		}
	}
}
