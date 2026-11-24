package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateNode stores a new node bound to one of its server's listeners.
func (s *Service) CreateNode(ctx context.Context, req *dto.CreateNodeRequest) error {
	nodeStore := s.deps.Store.Node()
	server, err := nodeStore.FindOneServer(ctx, req.ServerId)
	if err != nil {
		logger.WithContext(ctx).Errorw("[CreateNode] FindOneServer error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find server %d", req.ServerId)
	}
	data := node.Node{
		Name:    req.Name,
		Tags:    slicesx.StringSliceToString(req.Tags),
		Enabled: req.Enabled,
		Address: req.Address,
	}
	if err := bindNodeToListener(&data, server, req.ListenerKey, req.Protocol, req.Port); err != nil {
		logger.WithContext(ctx).Errorw("[CreateNode] Bind listener error", logger.Field("error", err.Error()))
		return err
	}
	if err := nodeStore.InsertNode(ctx, &data); err != nil {
		logger.WithContext(ctx).Errorw("[CreateNode] Insert database error", logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseInsertError, "[CreateNode] Insert database error")
	}
	return nodeStore.ClearServerCache(ctx, data.ServerId)
}
