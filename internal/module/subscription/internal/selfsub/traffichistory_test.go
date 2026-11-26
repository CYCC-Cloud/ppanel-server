package selfsub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// storedTraffic answers the traffic history port from memory and records the
// range and granularity it was asked for.
type storedTraffic struct {
	points  []trafficEntity.TrafficPoint
	servers []trafficEntity.ServerTraffic
	owned   int64
	user    int64
	asked   string
}

var _ TrafficHistoryReader = (*storedTraffic)(nil)

func (s *storedTraffic) UserTrafficHistory(_ context.Context, userSubscribeID, userID int64, granularity string, _, _ time.Time) ([]trafficEntity.TrafficPoint, error) {
	s.owned, s.user, s.asked = userSubscribeID, userID, granularity
	return s.points, nil
}

func (s *storedTraffic) UserTrafficServers(_ context.Context, userSubscribeID, userID int64, _, _ time.Time) ([]trafficEntity.ServerTraffic, error) {
	s.owned, s.user, s.asked = userSubscribeID, userID, "servers"
	return s.servers, nil
}

// planNodes answers the plan's node scope from memory.
type planNodes struct {
	ids   []int64
	tags  []string
	nodes []*node.Node
}

var _ NodeLister = (*planNodes)(nil)

func (p *planNodes) ListEnabledNodesByScope(_ context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error) {
	p.ids, p.tags = nodeIDs, tags
	return p.nodes, nil
}

func newTrafficFixture(t *testing.T) (*fixture, *storedTraffic, *planNodes, *usersub.Subscribe) {
	t.Helper()
	f := newFixture(t)
	plan := f.Plan(t, subscribe.Subscribe{Id: 41, Nodes: "1,2", NodeTags: "hk"})
	sub := f.Subscription(t, usersub.Subscribe{
		UserId:      100,
		SubscribeId: plan.Id,
		Status:      usersub.SubscribeStatusActive,
		ExpireTime:  time.Now().Add(24 * time.Hour),
	})
	traffic := &storedTraffic{}
	nodes := &planNodes{nodes: []*node.Node{
		{Id: 1, Name: "Hong Kong 01", ServerId: 10, Protocol: "vmess", Port: 443, Address: "internal-hk-10", Server: &node.Server{Id: 10, Name: "internal-hk-10", Country: "HK", City: "Hong Kong", Address: "10.0.0.10"}},
		{Id: 2, Name: "Tokyo 01", ServerId: 11, Protocol: "vmess", Port: 443, Address: "internal-jp-11", Server: &node.Server{Id: 11, Name: "internal-jp-11", Country: "JP", City: "Tokyo", Address: "10.0.0.11"}},
	}}
	f.svc.deps.Traffic = traffic
	f.svc.deps.Nodes = nodes
	return f, traffic, nodes, sub
}

// The owner reads their subscription's history; the caller's id is the one
// the stored traffic is filtered by, and the bucket start reaches the client
// as Unix seconds.
func TestUserTrafficHistoryReadsTheOwnersSubscription(t *testing.T) {
	f, traffic, _, sub := newTrafficFixture(t)
	start := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	traffic.points = []trafficEntity.TrafficPoint{
		{Start: start, Upload: 10, Download: 20},
		{Start: start.Add(time.Hour), Upload: 0, Download: 0},
	}

	resp, err := f.svc.UserTrafficHistory(as(100), &dto.TrafficHistoryRequest{
		UserSubscribeId: sub.Id,
		Granularity:     "hour",
		Start:           start.Add(-time.Hour).Unix(),
		End:             start.Add(2 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if traffic.owned != sub.Id || traffic.user != 100 || traffic.asked != "hour" {
		t.Fatalf("asked for subscription %d of user %d at %q", traffic.owned, traffic.user, traffic.asked)
	}
	if resp.Timezone != timeutil.LocationName() {
		t.Fatalf("timezone = %q, want %q", resp.Timezone, timeutil.LocationName())
	}
	if len(resp.List) != 2 || resp.List[0].Time != start.Unix() || resp.List[0].Upload != 10 ||
		resp.List[1].Time != start.Add(time.Hour).Unix() || resp.List[1].Download != 0 {
		t.Fatalf("history = %+v", resp.List)
	}
}

// Another user's subscription answers exactly like a missing one, and an
// anonymous call is refused.
func TestUserTrafficHistoryHidesAnotherUsersSubscription(t *testing.T) {
	f, traffic, _, sub := newTrafficFixture(t)
	request := &dto.TrafficHistoryRequest{UserSubscribeId: sub.Id, Granularity: "day", Start: 1, End: 2}

	missing := &dto.TrafficHistoryRequest{UserSubscribeId: sub.Id + 1000, Granularity: "day", Start: 1, End: 2}
	_, missingErr := f.svc.UserTrafficHistory(as(100), missing)
	_, foreignErr := f.svc.UserTrafficHistory(as(101), request)
	if missingErr == nil || foreignErr == nil {
		t.Fatalf("missing id = %v, foreign id = %v, want both refused", missingErr, foreignErr)
	}
	if xerr.CodeOf(missingErr) != xerr.CodeOf(foreignErr) || !errors.Is(foreignErr, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign id = %v (%d), want the missing id's answer (%d)", foreignErr, xerr.CodeOf(foreignErr), xerr.CodeOf(missingErr))
	}
	if _, err := f.svc.UserTrafficHistory(context.Background(), request); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("anonymous call = %v, want InvalidAccess", err)
	}
	if traffic.owned != 0 {
		t.Fatalf("a refused call read the traffic of subscription %d", traffic.owned)
	}
}

// The servers endpoint labels each server with the plan's node names and its
// location, never with the server's own name or address, and sorts nothing
// itself: the network port returns the traffic largest first.
func TestUserTrafficServersLabelsThePlansNodes(t *testing.T) {
	f, traffic, nodes, sub := newTrafficFixture(t)
	traffic.servers = []trafficEntity.ServerTraffic{
		{ServerId: 10, Upload: 100, Download: 200},
		{ServerId: 11, Upload: 10, Download: 20},
		{ServerId: 12, Upload: 1, Download: 2},
	}

	resp, err := f.svc.UserTrafficServers(as(100), &dto.TrafficServersRequest{UserSubscribeId: sub.Id, Start: 1, End: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes.ids) != 2 || nodes.ids[0] != 1 || nodes.ids[1] != 2 || len(nodes.tags) != 1 || nodes.tags[0] != "hk" {
		t.Fatalf("listed nodes for ids %v tags %v, want the plan's scope", nodes.ids, nodes.tags)
	}
	if len(resp.List) != 3 {
		t.Fatalf("servers = %+v", resp.List)
	}
	if resp.List[0].ServerId != 10 || resp.List[0].Nodes[0] != "Hong Kong 01" || resp.List[0].Country != "HK" ||
		resp.List[0].City != "Hong Kong" || resp.List[0].Total != 300 {
		t.Fatalf("the first server = %+v", resp.List[0])
	}
	if resp.List[1].ServerId != 11 || resp.List[1].Nodes[0] != "Tokyo 01" || resp.List[1].Country != "JP" {
		t.Fatalf("the second server = %+v", resp.List[1])
	}
	if resp.List[2].ServerId != 12 || resp.List[2].Nodes == nil || len(resp.List[2].Nodes) != 0 ||
		resp.List[2].Country != "" || resp.List[2].City != "" {
		t.Fatalf("a server without a plan node = %+v", resp.List[2])
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	for _, internal := range []string{"internal-hk-10", "internal-jp-11", "10.0.0.10", "10.0.0.11"} {
		if strings.Contains(string(encoded), internal) {
			t.Fatalf("the response exposes the server's internal %q: %s", internal, encoded)
		}
	}
}

// A subscription whose plan was deleted lists its traffic without labels.
func TestUserTrafficServersWithoutItsPlan(t *testing.T) {
	f := newFixture(t)
	sub := f.Subscription(t, usersub.Subscribe{UserId: 100, SubscribeId: 999, Status: usersub.SubscribeStatusActive, ExpireTime: time.Now().Add(time.Hour)})
	traffic := &storedTraffic{servers: []trafficEntity.ServerTraffic{{ServerId: 10, Upload: 1, Download: 2}}}
	f.svc.deps.Traffic = traffic
	f.svc.deps.Nodes = &planNodes{}

	resp, err := f.svc.UserTrafficServers(as(100), &dto.TrafficServersRequest{UserSubscribeId: sub.Id, Start: 1, End: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.List) != 1 || resp.List[0].ServerId != 10 || len(resp.List[0].Nodes) != 0 || resp.List[0].Total != 3 {
		t.Fatalf("servers = %+v", resp.List)
	}
}
