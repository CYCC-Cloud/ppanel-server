package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterNodeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterNodeListLogic Filter Node List
func NewFilterNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterNodeListLogic {
	return &FilterNodeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterNodeListLogic) FilterNodeList(req *types.FilterNodeListRequest) (resp *types.FilterNodeListResponse, err error) {
	total, data, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:   req.Page,
		Size:   req.Size,
		Search: req.Search,
	})

	if err != nil {
		l.Errorw("[FilterNodeList] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterNodeList] Query Database Error")
	}

	list := make([]types.Node, 0)
	serverListeners := make(map[int64]map[string]node.Protocol)
	for _, datum := range data {
		listeners, ok := serverListeners[datum.ServerId]
		if !ok {
			serverInfo, findErr := l.svcCtx.NodeModel.FindOneServer(l.ctx, datum.ServerId)
			if findErr != nil {
				l.Errorw("[FilterNodeList] Query Server Error: ", logger.Field("error", findErr.Error()), logger.Field("server_id", datum.ServerId))
				listeners = map[string]node.Protocol{}
			} else {
				protocols, unmarshalErr := serverInfo.UnmarshalProtocols()
				if unmarshalErr != nil {
					l.Errorw("[FilterNodeList] Unmarshal Protocols Error: ", logger.Field("error", unmarshalErr.Error()), logger.Field("server_id", datum.ServerId))
					listeners = map[string]node.Protocol{}
				} else {
					listeners = make(map[string]node.Protocol, len(protocols))
					for _, protocol := range protocols {
						listeners[protocol.ListenerKey] = protocol
					}
				}
			}
			serverListeners[datum.ServerId] = listeners
		}
		listener := listeners[datum.ListenerKey]
		list = append(list, types.Node{
			Id:           datum.Id,
			Name:         datum.Name,
			Tags:         tool.RemoveDuplicateElements(strings.Split(datum.Tags, ",")...),
			Port:         datum.Port,
			Address:      datum.Address,
			ServerId:     datum.ServerId,
			Protocol:     datum.Protocol,
			ListenerKey:  datum.ListenerKey,
			ListenerName: listener.ListenerName,
			Enabled:      datum.Enabled,
			Sort:         datum.Sort,
			CreatedAt:    datum.CreatedAt.UnixMilli(),
			UpdatedAt:    datum.UpdatedAt.UnixMilli(),
		})
	}

	return &types.FilterNodeListResponse{
		List:  list,
		Total: total,
	}, nil
}
