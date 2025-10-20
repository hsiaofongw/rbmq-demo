package connreg

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	pkgsafemap "example.com/rbmq-demo/pkg/safemap"
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

type AttributesAnnouncementPayload struct {
	Attributes  ConnectionAttributes `json:"attributes,omitempty"`
	Withdrawals []string             `json:"withdrawals,omitempty"`
}

func (echopayload *EchoPayload) CalculateDelays(now time.Time) (rtt time.Duration, onTrip time.Duration, backTrip time.Duration) {
	rtt = now.Sub(time.UnixMilli(int64(echopayload.Timestamp)))
	onTrip = time.UnixMilli(int64(echopayload.ServerTimestamp)).Sub(time.UnixMilli(int64(echopayload.Timestamp)))
	backTrip = now.Sub(time.UnixMilli(int64(echopayload.ServerTimestamp)))

	return rtt, onTrip, backTrip
}

type ConnectionAttributes map[string]string

type ConnRegistryData struct {
	NodeName      *string              `json:"node_name,omitempty"`
	ConnectedAt   uint64               `json:"connected_at"`
	RegisteredAt  *uint64              `json:"registered_at,omitempty"`
	LastHeartbeat *uint64              `json:"last_heartbeat,omitempty"`
	WsConn        *websocket.Conn      `json:"-"`
	Attributes    ConnectionAttributes `json:"attributes,omitempty"`
}

func cloneConnRegistryData(dataany interface{}) interface{} {
	data, ok := dataany.(*ConnRegistryData)
	if !ok {
		panic(fmt.Errorf("failed to convert dataany to *ConnRegistryData"))
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal connection registry data: %v", err)
		panic(err)
	}

	var cloned *ConnRegistryData
	err = json.Unmarshal(bytes, &cloned)
	if err != nil {
		panic(err)
	}
	return cloned
}

type ConnRegistry struct {
	datastore pkgsafemap.DataStore
}

func (cr *ConnRegistry) OpenConnection(conn *websocket.Conn) {
	now := uint64(time.Now().Unix())
	key := conn.RemoteAddr().String()
	connRegData := &ConnRegistryData{
		ConnectedAt: now,
		WsConn:      conn,
		Attributes:  make(ConnectionAttributes),
	}
	cr.datastore.Set(key, connRegData)
}

func (cr *ConnRegistry) CloseConnection(conn *websocket.Conn) {
	key := conn.RemoteAddr().String()
	cr.datastore.Delete(key)
}

func (cr *ConnRegistry) Register(conn *websocket.Conn, payload RegisterPayload) error {
	log.Printf("Registering connection from %s, node name: %s", conn.RemoteAddr(), payload.NodeName)
	key := conn.RemoteAddr().String()

	_, found := cr.datastore.Get(key, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		now := uint64(time.Now().Unix())
		if entry == nil {
			return fmt.Errorf("connection from %s not found in registry", conn.RemoteAddr())
		}
		entry.NodeName = &payload.NodeName
		entry.RegisteredAt = &now
		return nil
	})

	if !found {
		return fmt.Errorf("connection from %s not found in registry", conn.RemoteAddr())
	}
	return nil
}

func (cr *ConnRegistry) UpdateHeartbeat(conn *websocket.Conn) error {
	key := conn.RemoteAddr().String()
	_, found := cr.datastore.Get(key, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		now := uint64(time.Now().Unix())
		entry.LastHeartbeat = &now

		return nil
	})

	if !found {
		return fmt.Errorf("connection from %s not found in registry", conn.RemoteAddr())
	}
	return nil
}

func (cr *ConnRegistry) SetAttributes(conn *websocket.Conn, announcement *AttributesAnnouncementPayload) error {
	connkey := conn.RemoteAddr().String()
	_, found := cr.datastore.Get(connkey, func(valany interface{}) error {
		entry := valany.(*ConnRegistryData)
		attrs := make(ConnectionAttributes)
		for k, v := range entry.Attributes {
			attrs[k] = v
		}
		for _, withdrawal := range announcement.Withdrawals {
			delete(attrs, withdrawal)
		}
		for k, v := range announcement.Attributes {
			attrs[k] = v
		}
		entry.Attributes = attrs
		return nil
	})
	if !found {
		return fmt.Errorf("connection from %s not found in registry", conn.RemoteAddr())
	}
	return nil
}

func (cr *ConnRegistry) Dump() map[string]*ConnRegistryData {
	dummped := cr.datastore.Dump(cloneConnRegistryData)
	result := make(map[string]*ConnRegistryData)
	for k, v := range dummped {
		result[k] = v.(*ConnRegistryData)
	}
	return result
}

func (cr *ConnRegistry) Count() int {
	return cr.datastore.Len()
}

func NewConnRegistry(datastore pkgsafemap.DataStore) *ConnRegistry {
	connReg := &ConnRegistry{
		datastore: datastore,
	}
	return connReg
}
