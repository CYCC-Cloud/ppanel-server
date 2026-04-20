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

type CreateNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateNodeLogic Create Node
func NewCreateNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNodeLogic {
	return &CreateNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateNodeLogic) CreateNode(req *types.CreateNodeRequest) error {
	data := node.Node{
		Name:     req.Name,
		Tags:     tool.StringSliceToString(req.Tags),
		Enabled:  req.Enabled,
		Address:  req.Address,
		ServerId: req.ServerId,
	}
	serverInfo, err := l.svcCtx.NodeModel.FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		l.Errorw("[CreateNode] Query Server Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[CreateNode] Query Server Error")
	}
	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		l.Errorw("[CreateNode] Unmarshal Protocols Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "invalid server protocols"), "[CreateNode] Unmarshal Protocols Error")
	}
	if err = applyListenerToNode(&data, protocols, req.ListenerKey); err != nil {
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, err.Error()), "[CreateNode] %s", err.Error())
	}
	err = l.svcCtx.NodeModel.InsertNode(l.ctx, &data)
	if err != nil {
		l.Errorw("[CreateNode] Insert Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "[CreateNode] Insert Database Error")
	}

	return nil
}
