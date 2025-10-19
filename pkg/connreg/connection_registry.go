package connreg

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type RegisterPayload struct {
	NodeName string `json:"node_name"`
}

type EchoDirection string

const (
	EchoDirectionC2S EchoDirection = "ping"
	EchoDirectionS2C EchoDirection = "pong"
)

type EchoPayload struct {
	Direction       EchoDirection `json:"direction"`
	CorrelationID   string        `json:"correlation_id"`
	ServerTimestamp uint64        `json:"server_timestamp"`
	Timestamp       uint64        `json:"timestamp"`
	SeqID           uint64        `json:"seq_id"`
}

type ConnRegistryData struct {
	NodeName      *string         `json:"node_name,omitempty"`
	ConnectedAt   uint64          `json:"connected_at"`
	RegisteredAt  *uint64         `json:"registered_at,omitempty"`
	LastHeartbeat *uint64         `json:"last_heartbeat,omitempty"`
	WsConn        *websocket.Conn `json:"-"`
}

type ConnRegistry map[string]*ConnRegistryData

func (cr ConnRegistry) OpenConnection(conn *websocket.Conn) {
	now := uint64(time.Now().Unix())
	n := len(cr)
	cr[conn.RemoteAddr().String()] = &ConnRegistryData{
		ConnectedAt: now,
		WsConn:      conn,
	}
	log.Printf("Opening connection from %s, number of connections: %d -> %d", conn.RemoteAddr(), n, len(cr))
}

func (cr ConnRegistry) DropConnections(keys []string) {
	for _, key := range keys {
		n := len(cr)
		delete(cr, key)
		log.Printf("Dropped connection %s, number of connections: %d -> %d", key, n, len(cr))
	}
}

func (cr ConnRegistry) CloseConnection(conn *websocket.Conn) {
	cr.DropConnections([]string{conn.RemoteAddr().String()})
}

func (cr ConnRegistry) Register(conn *websocket.Conn, payload RegisterPayload) {
	log.Printf("Registering connection from %s, node name: %s", conn.RemoteAddr(), payload.NodeName)
	now := uint64(time.Now().Unix())
	entry := cr[conn.RemoteAddr().String()]
	if entry == nil {
		log.Printf("Connection from %s not found in registry", conn.RemoteAddr())
		return
	}
	entry.NodeName = &payload.NodeName
	entry.RegisteredAt = &now
}

func (cr ConnRegistry) UpdateHeartbeat(conn *websocket.Conn) {
	log.Printf("Updating heartbeat for connection from %s", conn.RemoteAddr())
	now := uint64(time.Now().Unix())
	entry := cr[conn.RemoteAddr().String()]
	if entry == nil {
		log.Printf("Connection from %s not found in registry", conn.RemoteAddr())
		return
	}
	entry.LastHeartbeat = &now
}

func NewConnRegistry() ConnRegistry {
	return make(ConnRegistry)
}
