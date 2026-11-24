package node

import "strings"

// NormalizeCertPinSHA256 canonicalizes a node-reported TLS certificate
// fingerprint to lowercase hex. It returns "" when the value is not a SHA256
// hex digest, so callers can treat invalid input as "not reported".
func NormalizeCertPinSHA256(raw string) string {
	pin := strings.ToLower(strings.TrimSpace(raw))
	if len(pin) != 64 {
		return ""
	}
	for _, r := range pin {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return pin
}

// ApplyReportedCertPin records the certificate fingerprint a node reported for
// one of its server's listeners. Only self-signed entries (cert_mode=self)
// accept the report; the node computes the fingerprint solely for certificates
// it generated itself, so a pin against any other cert_mode is stale by
// definition. The listener key identifies exactly one listener, so a server
// running several listeners of the same protocol type never has a sibling's
// fingerprint written over its own. It returns true when the stored protocols
// changed.
func (m *Server) ApplyReportedCertPin(listenerKey, fingerprint string) (bool, error) {
	pin := NormalizeCertPinSHA256(fingerprint)
	if pin == "" || listenerKey == "" {
		return false, nil
	}
	protocols, err := m.UnmarshalProtocols()
	if err != nil {
		return false, err
	}
	changed := false
	for i := range protocols {
		if protocols[i].ListenerKey != listenerKey {
			continue
		}
		if normalizeNone(protocols[i].CertMode) != "self" || protocols[i].CertPinSHA256 == pin {
			continue
		}
		protocols[i].CertPinSHA256 = pin
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, m.MarshalProtocols(protocols)
}
