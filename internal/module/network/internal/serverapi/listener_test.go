package serverapi

import (
	"errors"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

func TestServerUserListCacheKeySeparatesListeners(t *testing.T) {
	first := serverUserListCacheKey(1, "listener-a")
	second := serverUserListCacheKey(1, "listener-b")
	if first == second {
		t.Fatalf("listener cache keys collided: %q", first)
	}
}

func TestResolveRequestListenerPrefersTheListenerKey(t *testing.T) {
	protocols := []node.Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	}
	listener, err := resolveRequestListener(protocols, dto.ServerCommon{ListenerKey: "listener-b", Protocol: "trojan"})
	if err != nil {
		t.Fatalf("resolveRequestListener() error = %v", err)
	}
	if listener.Port != 8443 {
		t.Fatalf("resolved port = %d, want 8443", listener.Port)
	}
}

func TestResolveRequestListenerRejectsAmbiguousLegacyRequest(t *testing.T) {
	protocols := []node.Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true},
	}
	_, err := resolveRequestListener(protocols, dto.ServerCommon{Protocol: "trojan"})
	if !errors.Is(err, node.ErrListenerAmbiguous) {
		t.Fatalf("resolveRequestListener() error = %v, want ErrListenerAmbiguous", err)
	}
}

func TestResolveRequestListenerAcceptsLegacyRequestForAUniqueListener(t *testing.T) {
	protocols := []node.Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true},
		{ListenerKey: "listener-hysteria", Type: "hysteria", Port: 444, Enable: true},
	}
	listener, err := resolveRequestListener(protocols, dto.ServerCommon{Protocol: Hysteria2})
	if err != nil {
		t.Fatalf("resolveRequestListener() error = %v", err)
	}
	if listener.ListenerKey != "listener-hysteria" {
		t.Fatalf("resolved listener = %q, want listener-hysteria", listener.ListenerKey)
	}
}
