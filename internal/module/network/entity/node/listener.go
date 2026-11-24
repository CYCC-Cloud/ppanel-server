package node

import (
	"errors"
	"fmt"
	"uuid"
)

// A server may run several listeners of the same protocol type, so the
// listener key is the identity every listener-scoped flow resolves by: node
// binding, response caches, online-user state and reported certificate
// fingerprints. This file is the only owner of that resolution so no caller
// invents its own matching rule.

var (
	// ErrListenerNotFound reports that no listener carries the requested
	// identity.
	ErrListenerNotFound = errors.New("listener not found")
	// ErrListenerAmbiguous reports that a protocol type alone cannot name a
	// listener because the server runs more than one of that type.
	ErrListenerAmbiguous = errors.New("listener is ambiguous: resolve it by listener key")
)

// GenerateListenerKey mints the identity of a newly created listener.
func GenerateListenerKey() string {
	return uuid.NewV7().String()
}

// LegacyListenerKey derives the identity of a listener that predates listener
// keys. It is deterministic so a repeated upgrade assigns the same key to the
// same listener, and it embeds the server, protocol type and inbound port so
// keys never collide inside a server.
func LegacyListenerKey(serverID int64, protocol Protocol) string {
	return fmt.Sprintf("legacy-%d-%s-%d", serverID, normalizeProtocolType(protocol.Type), protocol.Port)
}

// FindListener returns the listener carrying listenerKey.
func FindListener(protocols []Protocol, listenerKey string) (Protocol, error) {
	if listenerKey == "" {
		return Protocol{}, ErrListenerNotFound
	}
	for _, protocol := range protocols {
		if protocol.ListenerKey == listenerKey {
			return protocol, nil
		}
	}
	return Protocol{}, ErrListenerNotFound
}

// ResolveListener selects the listener a request targets. A listener key names
// exactly one listener. A bare protocol type is only accepted while it names
// exactly one listener, so a node that has not learned listener keys yet keeps
// working without ever being silently pointed at a sibling listener.
func ResolveListener(protocols []Protocol, listenerKey, protocolType string) (Protocol, error) {
	if listenerKey != "" {
		return FindListener(protocols, listenerKey)
	}
	target := normalizeProtocolType(protocolType)
	if target == "" {
		return Protocol{}, ErrListenerNotFound
	}
	var (
		found   Protocol
		matches int
	)
	for _, protocol := range protocols {
		if normalizeProtocolType(protocol.Type) != target {
			continue
		}
		found = protocol
		matches++
	}
	switch matches {
	case 0:
		return Protocol{}, ErrListenerNotFound
	case 1:
		return found, nil
	default:
		return Protocol{}, ErrListenerAmbiguous
	}
}
