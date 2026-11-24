// Package listeneridentity performs the one-time data upgrade that gives every
// stored listener and every node a listener key.
//
// Listener keys are the identity the node API, the online-user state and the
// traffic accounting resolve by. Rows written before listener keys existed
// carry none, and a request-time fallback is not an acceptable substitute: it
// would have to guess which listener a node meant on every single call. The
// upgrade therefore persists the identity once, before the server starts
// serving.
package listeneridentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
)

// Deps declares the collaborators the upgrade needs. The upgrade only reads
// and updates node/server rows, so it accepts the network-owned capability
// rather than the application's full repository store.
type Deps struct {
	Store interface {
		Node() repository.NodeRepo
	}
}

// Service runs the listener-identity upgrade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Run assigns the missing listener identities. It is idempotent: a listener or
// node that already carries a key is left untouched, so repeated startups and
// reconfigurations converge on the same result.
//
// It fails instead of guessing. A node whose protocol type matches several of
// its server's listeners cannot be mapped without picking one arbitrarily, and
// picking wrong would bill its traffic at another listener's ratio, so the
// upgrade reports the conflict and refuses to continue.
func (s *Service) Run(ctx context.Context) error {
	servers, err := s.deps.Store.Node().QueryAllServers(ctx)
	if err != nil {
		return fmt.Errorf("list servers: %w", err)
	}

	protocolsByServer := make(map[int64][]node.Protocol, len(servers))
	for _, server := range servers {
		protocols, err := server.UnmarshalProtocols()
		if err != nil {
			return fmt.Errorf("unmarshal protocols of server %d: %w", server.Id, err)
		}
		changed := false
		for i := range protocols {
			if protocols[i].ListenerKey != "" {
				continue
			}
			protocols[i].ListenerKey = node.LegacyListenerKey(server.Id, protocols[i])
			changed = true
		}
		protocolsByServer[server.Id] = protocols
		if !changed {
			continue
		}
		if err := s.persistProtocols(ctx, server, protocols); err != nil {
			return err
		}
	}

	return s.bindNodes(ctx, protocolsByServer)
}

func (s *Service) persistProtocols(ctx context.Context, server *node.Server, protocols []node.Protocol) error {
	current := server.Protocols
	// MarshalProtocols enforces the listener-key and port invariants, so a
	// configuration the upgrade cannot make valid is reported here rather than
	// silently stored.
	if err := server.MarshalProtocols(protocols); err != nil {
		return fmt.Errorf("assign listener keys of server %d: %w", server.Id, err)
	}
	updated, err := s.deps.Store.Node().UpdateServerProtocolsIfCurrent(ctx, server.Id, current, server.Protocols)
	if err != nil {
		return fmt.Errorf("persist listener keys of server %d: %w", server.Id, err)
	}
	if !updated {
		// An administrator changed the configuration while the upgrade ran.
		// That write already carries listener keys, so there is nothing left
		// to assign.
		logger.WithContext(ctx).Infow("[ListenerIdentity] server protocols changed during upgrade, skipping",
			logger.Field("server_id", server.Id),
		)
		return nil
	}
	logger.WithContext(ctx).Infow("[ListenerIdentity] assigned listener keys",
		logger.Field("server_id", server.Id),
		logger.Field("listeners", len(protocols)),
	)
	return nil
}

func (s *Service) bindNodes(ctx context.Context, protocolsByServer map[int64][]node.Protocol) error {
	nodes, err := s.deps.Store.Node().ListNodes(ctx, &node.FilterNodeParams{})
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}
	for _, item := range nodes {
		if item.ListenerKey != "" {
			continue
		}
		protocols, ok := protocolsByServer[item.ServerId]
		if !ok {
			logger.WithContext(ctx).Errorw("[ListenerIdentity] node references a missing server",
				logger.Field("node_id", item.Id),
				logger.Field("server_id", item.ServerId),
			)
			continue
		}
		listener, err := node.ResolveListener(protocols, "", item.Protocol)
		switch {
		case errors.Is(err, node.ErrListenerAmbiguous):
			return fmt.Errorf(
				"node %d cannot be bound: server %d runs several %s listeners; set nodes.listener_key for it and restart",
				item.Id, item.ServerId, item.Protocol,
			)
		case errors.Is(err, node.ErrListenerNotFound):
			// The node points at a protocol its server does not configure, so
			// it already serves nothing. Leaving it unbound keeps the upgrade
			// from inventing traffic accounting for it.
			logger.WithContext(ctx).Errorw("[ListenerIdentity] node protocol is missing on its server",
				logger.Field("node_id", item.Id),
				logger.Field("server_id", item.ServerId),
				logger.Field("protocol", item.Protocol),
			)
			continue
		case err != nil:
			return fmt.Errorf("resolve listener of node %d: %w", item.Id, err)
		}
		if err := s.deps.Store.Node().UpdateNodeListenerKey(ctx, item.Id, listener.ListenerKey); err != nil {
			return fmt.Errorf("persist listener key of node %d: %w", item.Id, err)
		}
		logger.WithContext(ctx).Infow("[ListenerIdentity] bound node to listener",
			logger.Field("node_id", item.Id),
			logger.Field("server_id", item.ServerId),
			logger.Field("listener_key", listener.ListenerKey),
		)
	}
	return nil
}
