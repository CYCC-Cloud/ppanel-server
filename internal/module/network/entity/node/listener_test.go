package node

import (
	"errors"
	"testing"
)

func TestResolveListenerByKeySelectsExactlyOneListener(t *testing.T) {
	protocols := []Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	}
	listener, err := ResolveListener(protocols, "listener-b", "")
	if err != nil {
		t.Fatalf("ResolveListener() error = %v", err)
	}
	if listener.Port != 8443 {
		t.Fatalf("resolved port = %d, want 8443", listener.Port)
	}
}

// A protocol type is only a usable selector while it names one listener. If a
// server runs two of the same type, guessing would bill traffic at the wrong
// listener's ratio, so resolution must fail instead.
func TestResolveListenerRejectsAmbiguousProtocolType(t *testing.T) {
	protocols := []Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	}
	if _, err := ResolveListener(protocols, "", "trojan"); !errors.Is(err, ErrListenerAmbiguous) {
		t.Fatalf("ResolveListener() error = %v, want ErrListenerAmbiguous", err)
	}
}

func TestResolveListenerAcceptsUniqueProtocolTypeAndItsAlias(t *testing.T) {
	protocols := []Protocol{
		{ListenerKey: "listener-hysteria", Type: "hysteria", Port: 443, Enable: true},
		{ListenerKey: "listener-trojan", Type: "trojan", Port: 444, Enable: true},
	}
	for _, requested := range []string{"hysteria", "hysteria2", "HYSTERIA2"} {
		listener, err := ResolveListener(protocols, "", requested)
		if err != nil {
			t.Fatalf("ResolveListener(%q) error = %v", requested, err)
		}
		if listener.ListenerKey != "listener-hysteria" {
			t.Fatalf("ResolveListener(%q) = %q, want listener-hysteria", requested, listener.ListenerKey)
		}
	}
}

func TestResolveListenerReportsMissingListener(t *testing.T) {
	protocols := []Protocol{{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true}}
	for _, tc := range []struct{ key, protocol string }{
		{key: "listener-missing"},
		{protocol: "vmess"},
		{},
	} {
		if _, err := ResolveListener(protocols, tc.key, tc.protocol); !errors.Is(err, ErrListenerNotFound) {
			t.Fatalf("ResolveListener(%q, %q) error = %v, want ErrListenerNotFound", tc.key, tc.protocol, err)
		}
	}
}

// The upgrade of pre-listener-key data must land on the same key every time it
// runs, otherwise a restart would re-key listeners the nodes already reference.
func TestLegacyListenerKeyIsDeterministicAndPerListener(t *testing.T) {
	first := LegacyListenerKey(7, Protocol{Type: "hysteria2", Port: 443})
	again := LegacyListenerKey(7, Protocol{Type: "hysteria", Port: 443})
	if first != again {
		t.Fatalf("LegacyListenerKey() = %q and %q, want a stable key across protocol aliases", first, again)
	}
	if other := LegacyListenerKey(7, Protocol{Type: "hysteria", Port: 8443}); other == first {
		t.Fatal("LegacyListenerKey() collided across two listeners of one server")
	}
	if other := LegacyListenerKey(8, Protocol{Type: "hysteria", Port: 443}); other == first {
		t.Fatal("LegacyListenerKey() collided across two servers")
	}
}

func TestMarshalProtocolsAllowsRepeatedProtocolTypesWithDistinctListeners(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if len(protocols) != 2 {
		t.Fatalf("len(protocols) = %d, want 2", len(protocols))
	}
}

func TestMarshalProtocolsRejectsInvalidListenerConfigurations(t *testing.T) {
	cases := []struct {
		name      string
		protocols []Protocol
	}{
		{
			name:      "missing listener key",
			protocols: []Protocol{{Type: "trojan", Port: 443, Enable: true}},
		},
		{
			name: "duplicate listener key",
			protocols: []Protocol{
				{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
				{ListenerKey: "listener-a", Type: "vmess", Port: 8443, Enable: true},
			},
		},
		{
			name: "two enabled listeners share a port",
			protocols: []Protocol{
				{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
				{ListenerKey: "listener-b", Type: "vmess", Port: 443, Enable: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := new(Server)
			if err := server.MarshalProtocols(tc.protocols); err == nil {
				t.Fatal("MarshalProtocols() error = nil, want a validation error")
			}
			if server.Protocols != "" {
				t.Fatalf("Protocols = %q, want the invalid configuration left unstored", server.Protocols)
			}
		})
	}
}

// A disabled listener keeps its port so it can be re-enabled later, so only
// enabled listeners contend for one.
func TestMarshalProtocolsAllowsDisabledListenerToShareAPort(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "vmess", Port: 443},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
}
