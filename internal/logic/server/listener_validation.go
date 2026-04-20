package server

import (
	"errors"

	"github.com/perfect-panel/server/internal/model/node"
)

func validateServerListenerKey(serverInfo *node.Server, listenerKey string) error {
	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		return err
	}

	for i := range protocols {
		if protocols[i].ListenerKey == listenerKey {
			return nil
		}
	}

	return errors.New("listener_key is invalid")
}
