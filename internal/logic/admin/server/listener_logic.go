package server

import (
	"errors"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/node"
)

func ensureListenerKeys(protocols []node.Protocol, existing []node.Protocol) {
	_ = existing
	for i := range protocols {
		if protocols[i].ListenerKey != "" {
			continue
		}
		protocols[i].ListenerKey = uuid.NewString()
	}
}

func findListener(protocols []node.Protocol, listenerKey string) (*node.Protocol, error) {
	for i := range protocols {
		if protocols[i].ListenerKey == listenerKey {
			return &protocols[i], nil
		}
	}

	return nil, errors.New("listener not found")
}

func applyListenerToNode(data *node.Node, protocols []node.Protocol, listenerKey string) error {
	listener, err := findListener(protocols, listenerKey)
	if err != nil {
		return err
	}

	data.ListenerKey = listener.ListenerKey
	data.Protocol = listener.Type
	data.Port = listener.Port

	return nil
}
