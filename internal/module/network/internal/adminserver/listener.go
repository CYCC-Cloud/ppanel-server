package adminserver

import (
	"fmt"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ensureListenerKey gives a newly configured listener its identity. An
// identity supplied by the request is preserved, so an update never renames a
// listener the nodes and the online state already reference.
func ensureListenerKey(protocol *node.Protocol) {
	if protocol.ListenerKey == "" {
		protocol.ListenerKey = node.GenerateListenerKey()
	}
}

// adoptListenerKey handles a requested listener that names no key. A legacy
// client updating the sole stored listener of its type keeps that stable
// identity; with several stored listeners of the type, the request must name
// every one of them, otherwise it is ambiguous which one this entry replaces.
func adoptListenerKey(protocol *node.Protocol, stored []node.Protocol, requestedOfType int, requestedKeys map[string]bool) error {
	if len(stored) == 1 && requestedOfType == 1 {
		protocol.ListenerKey = stored[0].ListenerKey
		return nil
	}
	for _, existing := range stored {
		if !requestedKeys[existing.ListenerKey] {
			return fmt.Errorf("listener key is required to disambiguate %s listeners: %w",
				protocol.Type, xerr.NewErrCodeMsg(xerr.InvalidParams, "ambiguous listener"))
		}
	}
	return nil
}

// bindNodeToListener points a node at exactly one listener of its server. The
// node's protocol always comes from the resolved listener, and its connect
// port falls back to the listener's inbound port when the request leaves it
// unset.
func bindNodeToListener(data *node.Node, server *node.Server, listenerKey, protocolType string, port uint16) error {
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		return xerr.Wrapf(err, xerr.InvalidParams, "protocols unmarshal error")
	}
	listener, err := node.ResolveListener(protocols, listenerKey, protocolType)
	if err != nil {
		return xerr.Wrapf(err, xerr.InvalidParams, "resolve node listener")
	}
	data.ServerId = server.Id
	data.ListenerKey = listener.ListenerKey
	data.Protocol = listener.Type
	if port != 0 {
		data.Port = port
	} else {
		data.Port = listener.Port
	}
	return nil
}
