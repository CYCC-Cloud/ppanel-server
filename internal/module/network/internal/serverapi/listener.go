package serverapi

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// resolveRequestListener selects the listener a node-facing request targets.
// The listener key names exactly one listener; a bare protocol type is only
// accepted while it names exactly one, so a node that predates listener keys
// keeps working and is never silently served a sibling listener's config.
func resolveRequestListener(protocols []node.Protocol, common dto.ServerCommon) (node.Protocol, error) {
	return node.ResolveListener(protocols, common.ListenerKey, common.Protocol)
}

func (s *Service) resolveServerListener(ctx context.Context, serverID int64, common dto.ServerCommon) (*node.Server, node.Protocol, error) {
	server, err := s.deps.Servers.FindOneServer(ctx, serverID)
	if err != nil {
		return nil, node.Protocol{}, err
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		return nil, node.Protocol{}, err
	}
	listener, err := resolveRequestListener(protocols, common)
	if err != nil {
		return nil, node.Protocol{}, err
	}
	return server, listener, nil
}
