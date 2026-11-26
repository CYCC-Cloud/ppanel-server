package selfsub

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UserTrafficHistory returns the owner's traffic history of one subscription
// as a zero-filled series.
func (s *Service) UserTrafficHistory(ctx context.Context, req *dto.TrafficHistoryRequest) (*dto.TrafficHistoryResponse, error) {
	sub, err := s.ownedSubscriptionDetails(ctx, req.UserSubscribeId)
	if err != nil {
		return nil, err
	}
	points, err := s.deps.Traffic.UserTrafficHistory(ctx, sub.Id, sub.UserId, req.Granularity,
		time.Unix(req.Start, 0), time.Unix(req.End, 0))
	if err != nil {
		return nil, err
	}
	list := make([]dto.TrafficHistoryPoint, 0, len(points))
	for _, point := range points {
		list = append(list, dto.TrafficHistoryPoint{
			Time:     point.Start.Unix(),
			Upload:   point.Upload,
			Download: point.Download,
		})
	}
	return &dto.TrafficHistoryResponse{Timezone: timeutil.LocationName(), List: list}, nil
}

// UserTrafficServers returns the owner's traffic of one subscription per
// server, largest first, each labelled with the plan's node names on that
// server and its location. The server's own name and address never leave the
// network module.
func (s *Service) UserTrafficServers(ctx context.Context, req *dto.TrafficServersRequest) (*dto.TrafficServersResponse, error) {
	sub, err := s.ownedSubscriptionDetails(ctx, req.UserSubscribeId)
	if err != nil {
		return nil, err
	}
	rows, err := s.deps.Traffic.UserTrafficServers(ctx, sub.Id, sub.UserId,
		time.Unix(req.Start, 0), time.Unix(req.End, 0))
	if err != nil {
		return nil, err
	}
	labels, err := s.planServerLabels(ctx, sub.Subscribe)
	if err != nil {
		return nil, err
	}
	list := make([]dto.TrafficServerItem, 0, len(rows))
	for _, row := range rows {
		label := labels[row.ServerId]
		nodes := label.nodes
		if nodes == nil {
			nodes = []string{}
		}
		list = append(list, dto.TrafficServerItem{
			ServerId: row.ServerId,
			Nodes:    nodes,
			Country:  label.country,
			City:     label.city,
			Upload:   row.Upload,
			Download: row.Download,
			Total:    row.Upload + row.Download,
		})
	}
	return &dto.TrafficServersResponse{Timezone: timeutil.LocationName(), List: list}, nil
}

// ownedSubscriptionDetails loads a subscription with its plan and requires
// the caller to own it. Another user's subscription answers exactly like a
// missing one, so the API does not reveal which ids exist.
func (s *Service) ownedSubscriptionDetails(ctx context.Context, id int64) (*usersub.SubscribeDetails, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	sub, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, subscriptionNotFound(id)
		}
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", id)
	}
	if sub.UserId != u.Id {
		return nil, subscriptionNotFound(id)
	}
	return sub, nil
}

// subscriptionNotFound is the repository's not-found answer for the id.
func subscriptionNotFound(id int64) error {
	return xerr.Wrapf(gorm.ErrRecordNotFound, xerr.DatabaseQueryError, "find subscription %d", id)
}

// serverLabel is what a subscription's plan says about one server: the names
// of its nodes on that server, and the server's location.
type serverLabel struct {
	nodes   []string
	country string
	city    string
}

// planServerLabels maps a server id to the plan's enabled nodes on it. A
// plan selecting no nodes has none, as in delivery; a plan that no longer
// exists leaves every server unlabelled.
func (s *Service) planServerLabels(ctx context.Context, plan *subscribe.Subscribe) (map[int64]serverLabel, error) {
	labels := make(map[int64]serverLabel)
	if plan == nil {
		return labels, nil
	}
	nodeIDs, tags, err := plan.NodeScope()
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "plan %d nodes: %v", plan.Id, err)
	}
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return labels, nil
	}
	nodes, err := s.deps.Nodes.ListEnabledNodesByScope(ctx, nodeIDs, tags)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list the nodes of plan %d", plan.Id)
	}
	for _, n := range nodes {
		if n.Server == nil {
			continue
		}
		label := labels[n.ServerId]
		label.nodes = append(label.nodes, n.Name)
		label.country, label.city = n.Server.Country, n.Server.City
		labels[n.ServerId] = label
	}
	return labels, nil
}
