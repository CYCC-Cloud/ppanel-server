package adminserver

import (
	"context"
	"encoding/json"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// recordingPublisher captures the config invalidations a mutation announces.
type recordingPublisher struct {
	events []recordedEvent
}

type recordedEvent struct {
	serverID  int64
	protocols []string
}

func (p *recordingPublisher) ConfigChanged(_ context.Context, serverID int64, protocols []string) {
	p.events = append(p.events, recordedEvent{serverID: serverID, protocols: protocols})
}

func newConfigEventService(t *testing.T, seeded ...node.Protocol) (*Service, *recordingPublisher) {
	t.Helper()
	db, store := newTxNodeStore(t)
	// Seeded as stored JSON: these tests are about announcements, not about
	// the protocol write validation a fork feature may tighten.
	data, err := json.Marshal(seeded)
	if err != nil {
		t.Fatalf("marshal protocols: %v", err)
	}
	server := &node.Server{Id: 1, Name: "server", Address: "203.0.113.10", Protocols: string(data)}
	if err := db.Create(server).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	publisher := &recordingPublisher{}
	return NewService(Deps{Store: store, ConfigEvents: publisher}), publisher
}

func trojanProtocol() node.Protocol {
	return node.Protocol{Type: "trojan", Port: 443, Enable: true, Security: "tls", SNI: "a.example.com", CertMode: "self", Transport: "tcp"}
}

// A protocol change is what a node has to reload, so the invalidation must
// name the protocols involved.
func TestUpdateServerAnnouncesChangedProtocols(t *testing.T) {
	service, publisher := newConfigEventService(t, trojanProtocol())
	err := service.UpdateServer(context.Background(), &dto.UpdateServerRequest{
		Id:      1,
		Name:    "server",
		Address: "203.0.113.10",
		Protocols: []dto.Protocol{
			{Type: "trojan", Port: 443, Enable: true, Security: "tls", SNI: "a.example.com", CertMode: "self", Transport: "tcp"},
			{Type: "vmess", Port: 9443, Enable: true, Transport: "tcp"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateServer() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %+v, want exactly one config invalidation", publisher.events)
	}
	event := publisher.events[0]
	if event.serverID != 1 {
		t.Fatalf("event server id = %d, want 1", event.serverID)
	}
	if len(event.protocols) != 2 {
		t.Fatalf("event protocols = %v, want both changed protocol types", event.protocols)
	}
}

// A node-config override changes what the node pulls even though no protocol
// changed, so it must be announced too.
func TestUpdateServerNodeConfigAnnouncesTheChange(t *testing.T) {
	service, publisher := newConfigEventService(t, trojanProtocol())
	err := service.UpdateServerNodeConfig(context.Background(), &dto.UpdateServerNodeConfigRequest{
		ServerID: 1,
		ServerNodeConfigOverride: dto.ServerNodeConfigOverride{
			IPStrategy:      "ipv4_only",
			InheritDNS:      true,
			InheritBlock:    true,
			InheritOutbound: true,
		},
	})
	if err != nil {
		t.Fatalf("UpdateServerNodeConfig() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %+v, want exactly one config invalidation", publisher.events)
	}
	if publisher.events[0].serverID != 1 {
		t.Fatalf("event server id = %d, want 1", publisher.events[0].serverID)
	}
	if publisher.events[0].protocols != nil {
		t.Fatalf("event protocols = %v, want none for a non-protocol change", publisher.events[0].protocols)
	}
}

// A failed mutation must not announce anything: subscribers would refresh
// against configuration that never changed.
func TestUpdateServerDoesNotAnnounceARejectedChange(t *testing.T) {
	service, publisher := newConfigEventService(t, trojanProtocol())
	err := service.UpdateServer(context.Background(), &dto.UpdateServerRequest{
		Id:        1,
		Name:      "server",
		Address:   "203.0.113.10",
		Protocols: []dto.Protocol{{Type: ""}},
	})
	if err == nil {
		t.Fatal("UpdateServer() error = nil, want the protocol without a type rejected")
	}
	if len(publisher.events) != 0 {
		t.Fatalf("events = %+v, want none for a rejected change", publisher.events)
	}
}
