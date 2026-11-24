package node

import (
	"strings"
	"testing"
)

const testCertPin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func TestNormalizeCertPinSHA256(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "lowercase", raw: testCertPin, want: testCertPin},
		{name: "uppercase", raw: strings.ToUpper(testCertPin), want: testCertPin},
		{name: "padded", raw: "  " + testCertPin + "\n", want: testCertPin},
		{name: "empty", raw: ""},
		{name: "not hex", raw: strings.Repeat("z", 64)},
		{name: "short", raw: testCertPin[:63]},
		{name: "long", raw: testCertPin + "a"},
	}
	for _, tc := range cases {
		if got := NormalizeCertPinSHA256(tc.raw); got != tc.want {
			t.Fatalf("%s: NormalizeCertPinSHA256(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestApplyReportedCertPinUpdatesSelfSignedProtocol(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-vmess", Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
		{ListenerKey: "listener-trojan", Type: "trojan", Port: 444, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("listener-vmess", strings.ToUpper(testCertPin))
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if protocols[0].CertPinSHA256 != testCertPin {
		t.Fatalf("vmess CertPinSHA256 = %q, want %q", protocols[0].CertPinSHA256, testCertPin)
	}
	if protocols[1].CertPinSHA256 != "" {
		t.Fatalf("trojan CertPinSHA256 = %q, want empty (other listeners must not be touched)", protocols[1].CertPinSHA256)
	}

	// A repeated report with the same fingerprint must be a no-op so the
	// heartbeat does not invalidate the server cache every cycle.
	changed, err = server.ApplyReportedCertPin("listener-vmess", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() repeat error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() repeat changed = true, want false")
	}
}

// Two listeners of the same protocol type terminate different certificates,
// so a fingerprint reported by one must never be stamped onto the other.
func TestApplyReportedCertPinIsolatesSameTypeListeners(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-a", Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "a.example.com", CertMode: "self"},
		{ListenerKey: "listener-b", Type: "vmess", Port: 8443, Enable: true, Security: "tls", SNI: "b.example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("listener-b", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if protocols[0].CertPinSHA256 != "" {
		t.Fatalf("listener-a CertPinSHA256 = %q, want empty", protocols[0].CertPinSHA256)
	}
	if protocols[1].CertPinSHA256 != testCertPin {
		t.Fatalf("listener-b CertPinSHA256 = %q, want %q", protocols[1].CertPinSHA256, testCertPin)
	}
}

func TestApplyReportedCertPinIgnoresUnknownListener(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-a", Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	for _, listenerKey := range []string{"", "listener-missing"} {
		changed, err := server.ApplyReportedCertPin(listenerKey, testCertPin)
		if err != nil {
			t.Fatalf("ApplyReportedCertPin(%q) error = %v", listenerKey, err)
		}
		if changed {
			t.Fatalf("ApplyReportedCertPin(%q) changed = true, want false", listenerKey)
		}
	}
}

func TestApplyReportedCertPinUpdatesNowhere(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{{
		ListenerKey: "listener-nowhere",
		Type:        "nowhere", Port: 443, Version: 3, Enable: true, Security: "tls",
		Network: "mix", SNI: "nowhere.example", ALPN: []string{"nw3"}, CertMode: "self",
	}}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("listener-nowhere", strings.ToUpper(testCertPin))
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if len(protocols) != 1 || protocols[0].CertPinSHA256 != testCertPin {
		t.Fatalf("Nowhere CertPinSHA256 = %#v, want %q", protocols, testCertPin)
	}
}

func TestApplyReportedCertPinIgnoresNonSelfCertMode(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-vless", Type: "vless", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "dns"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("listener-vless", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() changed = true, want false for cert_mode=dns")
	}
}

func TestApplyReportedCertPinIgnoresInvalidFingerprint(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{ListenerKey: "listener-vmess", Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	for _, raw := range []string{"", "not-a-fingerprint", testCertPin[:32]} {
		changed, err := server.ApplyReportedCertPin("listener-vmess", raw)
		if err != nil {
			t.Fatalf("ApplyReportedCertPin(%q) error = %v", raw, err)
		}
		if changed {
			t.Fatalf("ApplyReportedCertPin(%q) changed = true, want false", raw)
		}
	}
}
