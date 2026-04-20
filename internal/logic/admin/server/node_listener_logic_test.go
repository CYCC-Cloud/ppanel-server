package server

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/stretchr/testify/require"
)

func TestApplyListenerToNodeUsesListenerKey(t *testing.T) {
	t.Parallel()

	protocols := []node.Protocol{
		{Type: "vmess", ListenerKey: "listener-a", ListenerName: "Alpha", Port: 443},
		{Type: "vmess", ListenerKey: "listener-b", ListenerName: "Beta", Port: 8443},
	}

	tests := []struct {
		name     string
		nodeData node.Node
	}{
		{
			name:     "create node",
			nodeData: node.Node{},
		},
		{
			name: "update node",
			nodeData: node.Node{
				Protocol:    "trojan",
				Port:        9443,
				ListenerKey: "legacy",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := tt.nodeData
			err := applyListenerToNode(&data, protocols, "listener-b")

			require.NoError(t, err)
			require.Equal(t, "vmess", data.Protocol)
			require.Equal(t, uint16(8443), data.Port)
			require.Equal(t, "listener-b", data.ListenerKey)
		})
	}
}

func TestApplyListenerToNodeRejectsUnknownListenerKey(t *testing.T) {
	t.Parallel()

	data := node.Node{}
	err := applyListenerToNode(&data, []node.Protocol{{Type: "vmess", ListenerKey: "listener-a", Port: 443}}, "missing")

	require.EqualError(t, err, "listener not found")
}

func TestEnsureListenerKeysPreservesIncomingKeysAndGeneratesOnlyMissing(t *testing.T) {
	t.Parallel()

	existing := []node.Protocol{
		{Type: "vmess", ListenerKey: "existing-a"},
		{Type: "trojan", ListenerKey: "existing-b"},
		{Type: "vmess", ListenerKey: "existing-c"},
	}
	protocols := []node.Protocol{
		{Type: "vmess", ListenerKey: "payload-first"},
		{Type: "trojan"},
		{Type: "vmess", ListenerKey: "payload-last"},
	}

	ensureListenerKeys(protocols, existing)

	require.Equal(t, "payload-first", protocols[0].ListenerKey)
	require.NotEmpty(t, protocols[1].ListenerKey)
	require.NotEqual(t, "existing-a", protocols[1].ListenerKey)
	require.NotEqual(t, "existing-b", protocols[1].ListenerKey)
	require.NotEqual(t, "existing-c", protocols[1].ListenerKey)
	require.Equal(t, "payload-last", protocols[2].ListenerKey)
}

func TestEnsureListenerKeysGeneratesFreshKeysForMissingEntries(t *testing.T) {
	t.Parallel()

	existing := []node.Protocol{
		{Type: "vmess", ListenerKey: "existing-a"},
		{Type: "trojan", ListenerKey: "existing-b"},
	}
	protocols := []node.Protocol{
		{Type: "shadowsocks"},
		{Type: "vmess", ListenerKey: "payload-b"},
		{Type: "trojan"},
	}

	ensureListenerKeys(protocols, existing)

	require.NotEmpty(t, protocols[0].ListenerKey)
	require.NotEqual(t, "existing-a", protocols[0].ListenerKey)
	require.NotEqual(t, "existing-b", protocols[0].ListenerKey)
	require.Equal(t, "payload-b", protocols[1].ListenerKey)
	require.NotEmpty(t, protocols[2].ListenerKey)
	require.NotEqual(t, "existing-a", protocols[2].ListenerKey)
	require.NotEqual(t, "existing-b", protocols[2].ListenerKey)
	require.NotEqual(t, "payload-b", protocols[2].ListenerKey)
}
