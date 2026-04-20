package traffic

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/stretchr/testify/require"
)

func TestRatioForListenerUsesListenerKey(t *testing.T) {
	t.Parallel()

	ratio, err := ratioForListener([]node.Protocol{
		{Type: "vmess", ListenerKey: "listener-a", Ratio: 1.5},
		{Type: "vmess", ListenerKey: "listener-b", Ratio: 2.5},
	}, "listener-b")

	require.NoError(t, err)
	require.Equal(t, float32(2.5), ratio)
}

func TestRatioForListenerRejectsUnknownListenerKey(t *testing.T) {
	t.Parallel()

	_, err := ratioForListener([]node.Protocol{{Type: "vmess", ListenerKey: "listener-a", Ratio: 1.5}}, "missing")

	require.EqualError(t, err, "listener not found")
}
