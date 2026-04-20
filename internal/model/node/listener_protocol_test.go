package node

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalProtocolsAllowsDuplicateTypesWithDistinctListenerKeys(t *testing.T) {
	server := &Server{}

	err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", ListenerKey: "tcp-a", Port: 443, Enable: true},
		{Type: "vmess", ListenerKey: "tcp-b", Port: 8443, Enable: true},
	})

	require.NoError(t, err)
	require.Contains(t, server.Protocols, `"listener_key":"tcp-a"`)
	require.Contains(t, server.Protocols, `"listener_key":"tcp-b"`)
}

func TestMarshalProtocolsRejectsDuplicateListenerKeys(t *testing.T) {
	server := &Server{}

	err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", ListenerKey: "shared", Port: 443, Enable: true},
		{Type: "trojan", ListenerKey: "shared", Port: 8443, Enable: true},
	})

	require.EqualError(t, err, "duplicate listener key: shared")
}

func TestMarshalProtocolsRejectsDuplicateEnabledPorts(t *testing.T) {
	server := &Server{}

	err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", ListenerKey: "tcp-a", Port: 443, Enable: true},
		{Type: "trojan", ListenerKey: "tcp-b", Port: 443, Enable: true},
	})

	require.EqualError(t, err, "duplicate enabled port: 443")
}
