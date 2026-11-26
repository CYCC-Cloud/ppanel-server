package storefront

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// QueryUserSubscribeNodeList lists the owner's subscriptions in their term
// with the nodes each may use. A subscription that may not use the service
// now lists no nodes, by the same rule the node user list and delivery
// apply; the plan's nodes are loaded once per plan.
func (s *Service) QueryUserSubscribeNodeList(ctx context.Context) (*dto.QueryUserSubscribeNodeListResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	userSubscribes, err := s.deps.UserSubs.QueryUserSubscribe(ctx, u.Id, usersub.InTermStatuses.Values()...)
	if err != nil {
		log.Errorw("[QueryUserSubscribeNodeList] Query subscriptions failed", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscriptions of user %d", u.Id)
	}

	resp := &dto.QueryUserSubscribeNodeListResponse{}
	nodesByPlan := make(map[int64][]*node.Node)
	listed := newListedServers()
	now := timeutil.Now()
	for _, details := range userSubscribes {
		if details == nil {
			continue
		}
		userSubscribe := subscribeFromDetails(details)
		nodes, err := s.subscriptionNodes(ctx, userSubscribe, details.Subscribe, nodesByPlan, listed, now)
		if err != nil {
			return nil, err
		}
		info := dto.UserSubscribeInfo{
			Id:          userSubscribe.Id,
			Nodes:       nodes,
			Traffic:     userSubscribe.Traffic,
			Upload:      userSubscribe.Upload,
			Download:    userSubscribe.Download,
			Token:       userSubscribe.Token,
			UserId:      userSubscribe.UserId,
			OrderId:     userSubscribe.OrderId,
			SubscribeId: userSubscribe.SubscribeId,
			StartTime:   userSubscribe.StartTime.Unix(),
			ExpireTime:  userSubscribe.ExpireTime.Unix(),
			Status:      userSubscribe.Status,
			CreatedAt:   userSubscribe.CreatedAt.Unix(),
			UpdatedAt:   userSubscribe.UpdatedAt.Unix(),
			IsTryOut:    s.deps.isTrialPlan(userSubscribe.SubscribeId),
		}
		if userSubscribe.FinishedAt != nil {
			info.FinishedAt = userSubscribe.FinishedAt.Unix()
		}
		resp.List = append(resp.List, info)
	}
	s.fillServerLoads(ctx, resp.List, listed)
	return resp, nil
}

func subscribeFromDetails(item *usersub.SubscribeDetails) *usersub.Subscribe {
	return &usersub.Subscribe{
		Id: item.Id, UserId: item.UserId, OrderId: item.OrderId, SubscribeId: item.SubscribeId,
		StartTime: item.StartTime, ExpireTime: item.ExpireTime, FinishedAt: item.FinishedAt,
		Traffic: item.Traffic, Download: item.Download, Upload: item.Upload,
		Token: item.Token, UUID: item.UUID, Status: item.Status, Note: item.Note,
		EntitlementSource: item.EntitlementSource, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

// subscriptionNodes returns the nodes the subscription may use now: none
// unless it is servable, otherwise the enabled nodes of its plan. A
// subscription whose plan was deleted has no nodes left to list.
func (s *Service) subscriptionNodes(ctx context.Context, userSub *usersub.Subscribe, plan *subscribe.Subscribe, nodesByPlan map[int64][]*node.Node, listed *listedServers, now time.Time) ([]*dto.UserSubscribeNodeInfo, error) {
	if !userSub.ServableAt(now) {
		return nil, nil
	}
	if plan == nil {
		var err error
		if plan, err = s.deps.Plans.FindOne(ctx, userSub.SubscribeId); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				logger.WithContext(ctx).Infow("[QueryUserSubscribeNodeList] Plan of the subscription no longer exists", logger.Field("subscribe_id", userSub.SubscribeId), logger.Field("user_subscribe_id", userSub.Id))
				return nil, nil
			}
			logger.WithContext(ctx).Errorw("[QueryUserSubscribeNodeList] Find plan failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", userSub.SubscribeId))
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find plan %d", userSub.SubscribeId)
		}
	}
	nodes, cached := nodesByPlan[plan.Id]
	if !cached {
		// A plan selecting no nodes has none, as in delivery; the scope
		// query without conditions would list every node.
		nodeIDs, tags, err := plan.NodeScope()
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "plan nodes: %v", err)
		}
		if len(nodeIDs) > 0 || len(tags) > 0 {
			var err error
			nodes, err = s.deps.Nodes.ListEnabledNodesByScope(ctx, nodeIDs, tags)
			if err != nil {
				logger.WithContext(ctx).Errorw("[QueryUserSubscribeNodeList] List plan nodes failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", plan.Id))
				return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list nodes of plan %d", plan.Id)
			}
		}
		nodesByPlan[plan.Id] = nodes
	}

	infos := make([]*dto.UserSubscribeNodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n.Server == nil {
			continue
		}
		credential := userSub.UUID
		if strings.EqualFold(strings.TrimSpace(n.Protocol), "nowhere") {
			var err error
			credential, err = node.NowhereSharedKey(credential)
			if err != nil {
				return nil, xerr.Wrapf(err, xerr.ERROR, "invalid Nowhere credential for subscription %d", userSub.Id)
			}
		}
		listed.add(n)
		infos = append(infos, &dto.UserSubscribeNodeInfo{
			Id:        n.Id,
			Name:      n.Name,
			Uuid:      credential,
			Protocol:  n.Protocol,
			Port:      n.Port,
			Address:   n.Address,
			Tags:      strings.Split(n.Tags, ","),
			Country:   n.Server.Country,
			City:      n.Server.City,
			CreatedAt: n.CreatedAt.Unix(),
		})
	}
	return infos, nil
}

// listedServers records the distinct servers behind the listed nodes and
// which node belongs to which, so their load is read once for the whole
// response.
type listedServers struct {
	servers map[int64]*node.Server
	ofNode  map[int64]int64
}

func newListedServers() *listedServers {
	return &listedServers{servers: make(map[int64]*node.Server), ofNode: make(map[int64]int64)}
}

func (l *listedServers) add(n *node.Node) {
	if n.Server == nil || n.Server.Id <= 0 {
		return
	}
	l.servers[n.Server.Id] = n.Server
	l.ofNode[n.Id] = n.Server.Id
}

// fillServerLoads adds each node's server status and load. A load that
// cannot be read leaves both empty: the node list still serves.
func (s *Service) fillServerLoads(ctx context.Context, list []dto.UserSubscribeInfo, listed *listedServers) {
	if s.deps.Loads == nil || len(listed.servers) == 0 {
		return
	}
	servers := make([]*node.Server, 0, len(listed.servers))
	for _, server := range listed.servers {
		servers = append(servers, server)
	}
	loads, err := s.deps.Loads.ServerLoads(ctx, servers)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryUserSubscribeNodeList] Read server load failed",
			logger.Field("error", err.Error()), logger.Field("servers", len(servers)))
		return
	}
	for i := range list {
		for _, info := range list[i].Nodes {
			load, ok := loads[listed.ofNode[info.Id]]
			if !ok {
				continue
			}
			info.Status, info.Load = load.Status, load.Load
		}
	}
}
