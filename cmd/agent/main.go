package main

import (
	"flag"
	"fmt"
	"log"

	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	pkgconnreg "example.com/rbmq-demo/pkg/connreg"
	pkgnodereg "example.com/rbmq-demo/pkg/nodereg"
	probing "github.com/prometheus-community/pro-bing"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "/ws", "websocket path")
var nodeName = flag.String("node-name", "agent-1", "node name")
var logEchoReplies = flag.Bool("log-echo-replies", false, "log echo replies")

type PktRepresentation struct {
	Rtt    uint64 `json:"rtt"`
	Addr   string `json:"ip_addr"`
	Nbytes int    `json:"nbytes"`
	Seq    int    `json:"seq"`
	TTL    int    `json:"ttl"`
	ID     int    `json:"id"`
	Dup    bool   `json:"dup"`
}

func (pktrepr *PktRepresentation) String() string {
	j, err := json.Marshal(pktrepr)
	if err != nil {
		log.Printf("Failed to marshal pkt: %v", err)
		return ""
	}
	return string(j)
}

func NewPktRepresentation(pkt *probing.Packet, dup bool) *PktRepresentation {

	r := PktRepresentation{
		Rtt:    uint64(pkt.Rtt.Milliseconds()),
		Addr:   pkt.Addr,
		Nbytes: pkt.Nbytes,
		Seq:    pkt.Seq,
		TTL:    pkt.TTL,
		ID:     pkt.ID,
		Dup:    dup,
	}

	return &r
}

type PingStatsRepresentation struct {
	// PacketsRecv is the number of packets received.
	PacketsRecv int `json:"packets_recv"`

	// PacketsSent is the number of packets sent.
	PacketsSent int `json:"packets_sent"`

	// PacketsRecvDuplicates is the number of duplicate responses there were to a sent packet.
	PacketsRecvDuplicates int `json:"packets_recv_duplicates"`

	// PacketLoss is the percentage of packets lost.
	PacketLoss float64 `json:"packet_loss"`

	// IPAddr is the address of the host being pinged.
	IPAddr string `json:"ip_addr"`

	// Rtts is all of the round-trip times sent via this pinger.
	Rtts []int `json:"rtts"`

	// TTLs is all of the TTLs received via this pinger.
	TTLs []int `json:"ttls"`

	// MinRtt is the minimum round-trip time sent via this pinger.
	MinRtt uint64 `json:"min_rtt"`

	// MaxRtt is the maximum round-trip time sent via this pinger.
	MaxRtt uint64 `json:"max_rtt"`

	// AvgRtt is the average round-trip time sent via this pinger.
	AvgRtt uint64 `json:"avg_rtt"`

	// StdDevRtt is the standard deviation of the round-trip times sent via
	// this pinger.
	StdDevRtt uint64 `json:"std_dev_rtt"`
}

func NewPingStatsRepresentation(stats *probing.Statistics) *PingStatsRepresentation {
	r := PingStatsRepresentation{
		PacketsRecv:           stats.PacketsRecv,
		PacketsSent:           stats.PacketsSent,
		PacketsRecvDuplicates: stats.PacketsRecvDuplicates,
		PacketLoss:            stats.PacketLoss,
		IPAddr:                stats.IPAddr.String(),
	}
	rtts := make([]int, 0)
	for _, rtt := range stats.Rtts {
		rtts = append(rtts, int(rtt.Milliseconds()))
	}
	r.Rtts = rtts
	ttls := make([]int, 0)
	for _, ttl := range stats.TTLs {
		ttls = append(ttls, int(ttl))
	}
	r.TTLs = ttls
	r.MinRtt = uint64(stats.MinRtt.Milliseconds())
	r.MaxRtt = uint64(stats.MaxRtt.Milliseconds())
	r.AvgRtt = uint64(stats.AvgRtt.Milliseconds())
	r.StdDevRtt = uint64(stats.StdDevRtt.Milliseconds())
	return &r
}

func (statsrepr *PingStatsRepresentation) String() string {
	j, err := json.Marshal(statsrepr)
	if err != nil {
		log.Printf("Failed to marshal stats: %v", err)
		return ""
	}
	return string(j)
}

type PingEventType string

const (
	PingEventTypePktRecv    PingEventType = "pkt_recv"
	PingEventTypePktDupRecv PingEventType = "pkt_dup_recv"
	PingEventTypePingStats  PingEventType = "ping_stats"
)

type PingEvent struct {
	Type PingEventType `json:"type"`
	Data interface{}   `json:"data"`
}

func (ev *PingEvent) String() string {
	j, err := json.Marshal(ev)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return ""
	}
	return string(j)
}

func main() {
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	agent := pkgnodereg.NodeRegistrationAgent{
		ServerAddress:  *addr,
		WebSocketPath:  *path,
		NodeName:       *nodeName,
		LogEchoReplies: *logEchoReplies,
	}

	attributes := make(pkgconnreg.ConnectionAttributes)
	attributes[pkgnodereg.AttributeKeyPingCapability] = "true"

	// For now, just use node name, later we will use a broker generated name
	attributes[pkgnodereg.AttributeKeyRabbitMQQueueName] = *nodeName

	agent.NodeAttributes = attributes

	if err := agent.Init(); err != nil {
		log.Fatalf("Failed to initialize agent: %v", err)
	}

	errCh := make(chan error)
	go func() {
		errCh <- agent.Run()
	}()

	destination := "8.8.8.8"
	pinger, err := probing.NewPinger(destination)
	if err != nil {
		log.Fatalf("Failed to create pinger: %v", err)
	}

	var counter *int = new(int)
	*counter = 0

	pinger.OnRecv = func(pkt *probing.Packet) {
		ev := PingEvent{
			Type: PingEventTypePktRecv,
			Data: NewPktRepresentation(pkt, false),
		}
		fmt.Println(ev.String())
		*counter = *counter + 1
		if *counter > 3 {
			log.Println("Stopping pinger after 10 packets")
			pinger.Stop()
		}
	}
	pinger.OnDuplicateRecv = func(pkt *probing.Packet) {
		ev := PingEvent{
			Type: PingEventTypePktDupRecv,
			Data: NewPktRepresentation(pkt, true),
		}
		fmt.Println(ev.String())
	}
	pinger.OnFinish = func(stats *probing.Statistics) {
		ev := PingEvent{
			Type: PingEventTypePingStats,
			Data: NewPingStatsRepresentation(stats),
		}
		fmt.Println(ev.String())
	}

	pinger.SetPrivileged(true)
	err = pinger.Run()
	if err != nil {
		log.Fatalf("Failed to run pinger: %v", err)
	}

	<-sigs

	log.Println("Shutting down pinger...")
	pinger.Stop()

	log.Println("Shutting down agent...")
	err = agent.Shutdown()
	if err != nil {
		log.Fatalf("Failed to shutdown agent: %v", err)
	}

	err = <-errCh
	if err != nil {
		log.Printf("Agent exited with error: %v", err)
	} else {
		log.Println("Agent exited successfully")
	}

}
