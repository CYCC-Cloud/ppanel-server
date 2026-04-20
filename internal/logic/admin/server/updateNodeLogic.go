package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateNodeLogic Update Node
func NewUpdateNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateNodeLogic {
	return &UpdateNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateNodeLogic) UpdateNode(req *types.UpdateNodeRequest) error {
	data, err := l.svcCtx.NodeModel.FindOneNode(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[UpdateNode] Query Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "[UpdateNode] Query Database Error")
	}
	data.Name = req.Name
	data.Tags = tool.StringSliceToString(req.Tags)
	data.ServerId = req.ServerId
	data.Address = req.Address
	data.Enabled = req.Enabled
	serverInfo, err := l.svcCtx.NodeModel.FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		l.Errorw("[UpdateNode] Query Server Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[UpdateNode] Query Server Error")
	}
	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		l.Errorw("[UpdateNode] Unmarshal Protocols Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "invalid server protocols"), "[UpdateNode] Unmarshal Protocols Error")
	}
	if err = applyListenerToNode(data, protocols, req.ListenerKey); err != nil {
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, err.Error()), "[UpdateNode] %s", err.Error())
	}
	err = l.svcCtx.NodeModel.UpdateNode(l.ctx, data)
	if err != nil {
		l.Errorw("[UpdateNode] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "[UpdateNode] Update Database Error")
	}
	return l.svcCtx.NodeModel.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{data.ServerId},
		Search:   "",
	})
}
