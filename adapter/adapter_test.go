package adapter

import (
	"testing"
	"time"
)

func TestAdapter_Proxies(t *testing.T) {
	adapter := NewAdapter(tpl,
		WithServers(testNodes()),
		WithUserInfo(User{
			Password:     "test-password",
			ExpiredAt:    time.Now().Add(24 * time.Hour),
			Traffic:      1000,
			SubscribeURL: "https://example.com/subscribe",
		}),
	)

	proxies, err := adapter.Proxies(testNodes())
	if err != nil {
		t.Fatalf("Proxies() error = %v", err)
	}

	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}

	proxy := proxies[0]
	if proxy.Name != "Trojan Node" {
		t.Fatalf("expected proxy name %q, got %q", "Trojan Node", proxy.Name)
	}
	if proxy.Server != "node.example.com" {
		t.Fatalf("expected proxy server %q, got %q", "node.example.com", proxy.Server)
	}
	if proxy.Port != 443 {
		t.Fatalf("expected proxy port %d, got %d", 443, proxy.Port)
	}
	if proxy.Type != "trojan" {
		t.Fatalf("expected proxy type %q, got %q", "trojan", proxy.Type)
	}
	if len(proxy.Tags) != 2 || proxy.Tags[0] != "premium" || proxy.Tags[1] != "hk" {
		t.Fatalf("expected split tags [premium hk], got %#v", proxy.Tags)
	}
	if proxy.Security != "tls" {
		t.Fatalf("expected security %q, got %q", "tls", proxy.Security)
	}
	if proxy.SNI != "edge.example.com" {
		t.Fatalf("expected SNI %q, got %q", "edge.example.com", proxy.SNI)
	}
	if !proxy.AllowInsecure {
		t.Fatalf("expected AllowInsecure to be true")
	}
	if proxy.Fingerprint != "chrome" {
		t.Fatalf("expected fingerprint %q, got %q", "chrome", proxy.Fingerprint)
	}
	if proxy.Sort != 2 {
		t.Fatalf("expected sort %d, got %d", 2, proxy.Sort)
	}
}

func TestAdapter_Client(t *testing.T) {
	adapter := NewAdapter(tpl,
		WithServers(testNodes()),
		WithSiteName("TestSite"),
		WithSubscribeName("TestSubscribe"),
		WithUserInfo(User{
			Password: "test-password",
		}),
	)

	client, err := adapter.Client()
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}

	if client.SiteName != "TestSite" {
		t.Fatalf("expected site name %q, got %q", "TestSite", client.SiteName)
	}
	if client.SubscribeName != "TestSubscribe" {
		t.Fatalf("expected subscribe name %q, got %q", "TestSubscribe", client.SubscribeName)
	}
	if len(client.Proxies) != 1 {
		t.Fatalf("expected 1 proxy in client, got %d", len(client.Proxies))
	}
	if client.Proxies[0].Type != "trojan" {
		t.Fatalf("expected client proxy type %q, got %q", "trojan", client.Proxies[0].Type)
	}
}
