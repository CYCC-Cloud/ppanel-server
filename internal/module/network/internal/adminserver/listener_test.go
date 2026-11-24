package adminserver

import (
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

func newListenerServer(t *testing.T, protocols ...node.Protocol) *node.Server {
	t.Helper()
	server := &node.Server{Id: 1, Name: "server", Address: "example.com"}
	if err := server.MarshalProtocols(protocols); err != nil {
		t.Fatalf("marshal protocols: %v", err)
	}
	return server
}

// The node's protocol always comes from the listener it is bound to, so the
// two can never drift apart.
func TestBindNodeToListenerTakesProtocolFromTheListener(t *testing.T) {
	server := newListenerServer(t,
		node.Protocol{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		node.Protocol{ListenerKey: "listener-b", Type: "vmess", Port: 8443, Enable: true},
	)
	data := new(node.Node)
	if err := bindNodeToListener(data, server, "listener-b", "trojan", 0); err != nil {
		t.Fatalf("bindNodeToListener() error = %v", err)
	}
	if data.ListenerKey != "listener-b" || data.Protocol != "vmess" {
		t.Fatalf("node = %+v, want it bound to listener-b/vmess", data)
	}
	// No connect port was requested, so the listener's inbound port applies.
	if data.Port != 8443 {
		t.Fatalf("node port = %d, want 8443", data.Port)
	}
	if data.ServerId != server.Id {
		t.Fatalf("node server id = %d, want %d", data.ServerId, server.Id)
	}
}

// A node may be reached on a different port than the listener binds to, so an
// explicitly requested connect port wins.
func TestBindNodeToListenerKeepsRequestedConnectPort(t *testing.T) {
	server := newListenerServer(t, node.Protocol{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true})
	data := new(node.Node)
	if err := bindNodeToListener(data, server, "listener-a", "", 20443); err != nil {
		t.Fatalf("bindNodeToListener() error = %v", err)
	}
	if data.Port != 20443 {
		t.Fatalf("node port = %d, want 20443", data.Port)
	}
}

// An admin request that names only a protocol type cannot pick between two
// listeners of that type; binding the node to the wrong one would bill its
// traffic at the wrong ratio.
func TestBindNodeToListenerRejectsAmbiguousProtocolType(t *testing.T) {
	server := newListenerServer(t,
		node.Protocol{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		node.Protocol{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	)
	data := new(node.Node)
	if err := bindNodeToListener(data, server, "", "trojan", 0); err == nil {
		t.Fatal("bindNodeToListener() error = nil, want a rejection")
	}
	if data.ListenerKey != "" {
		t.Fatalf("node listener key = %q, want it left unbound", data.ListenerKey)
	}
}

func TestBindNodeToListenerRejectsUnknownListener(t *testing.T) {
	server := newListenerServer(t, node.Protocol{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true})
	data := new(node.Node)
	if err := bindNodeToListener(data, server, "listener-missing", "", 0); err == nil {
		t.Fatal("bindNodeToListener() error = nil, want a rejection")
	}
}

// A listener key supplied by the request is kept, so an update never renames
// the listener the nodes and traffic already reference.
func TestEnsureListenerKeyPreservesASuppliedKey(t *testing.T) {
	protocol := node.Protocol{ListenerKey: "listener-a", Type: "trojan"}
	ensureListenerKey(&protocol)
	if protocol.ListenerKey != "listener-a" {
		t.Fatalf("listener key = %q, want listener-a", protocol.ListenerKey)
	}
}

func TestEnsureListenerKeyGeneratesDistinctKeysForNewListeners(t *testing.T) {
	first := node.Protocol{Type: "trojan"}
	second := node.Protocol{Type: "trojan"}
	ensureListenerKey(&first)
	ensureListenerKey(&second)
	if first.ListenerKey == "" || second.ListenerKey == "" {
		t.Fatalf("keys = %q and %q, want both generated", first.ListenerKey, second.ListenerKey)
	}
	if first.ListenerKey == second.ListenerKey {
		t.Fatalf("two new listeners share the key %q", first.ListenerKey)
	}
}
