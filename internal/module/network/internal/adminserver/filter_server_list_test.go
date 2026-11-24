package adminserver

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

type serverPage struct {
	servers []*node.Server
	online  map[string]node.OnlineUserSubscribe // by "serverID/listenerKey"
}

var _ serverListReader = serverPage{}

func (p serverPage) FilterServerList(context.Context, *node.FilterParams) (int64, []*node.Server, error) {
	return int64(len(p.servers)), p.servers, nil
}

func (p serverPage) StatusCache(_ context.Context, serverID int64) (node.Status, error) {
	return node.Status{Cpu: float64(serverID)}, nil
}

func (p serverPage) OnlineUserSubscribe(_ context.Context, serverID int64, listenerKey string) (node.OnlineUserSubscribe, error) {
	return p.online[fmt.Sprintf("%d/%s", serverID, listenerKey)], nil
}

type subscriptionDetails struct {
	rows  map[int64]*usersub.SubscribeDetails
	calls [][]int64
}

var _ OnlineSubscriptionReader = (*subscriptionDetails)(nil)

func (d *subscriptionDetails) SubscriptionDetailsByIDs(_ context.Context, ids []int64) ([]*usersub.SubscribeDetails, error) {
	d.calls = append(d.calls, ids)
	var found []*usersub.SubscribeDetails
	for _, id := range ids {
		if row, ok := d.rows[id]; ok {
			found = append(found, row)
		}
	}
	return found, nil
}

func protocolServer(t *testing.T, id int64, types ...string) *node.Server {
	t.Helper()
	server := &node.Server{Id: id, Name: fmt.Sprintf("server-%d", id)}
	var protocols []node.Protocol
	for index, typ := range types {
		protocols = append(protocols, node.Protocol{ListenerKey: fmt.Sprintf("%d-%s-%d", id, typ, index), Type: typ, Port: uint16(443 + index), Enable: true})
	}
	if err := server.MarshalProtocols(protocols); err != nil {
		t.Fatal(err)
	}
	return server
}

// Online reports from two listeners of one type stay separate in the cache
// and are both included in the server's batched subscription read.
func TestFilterServerListReadsOnlineSubscriptionsInOneQuery(t *testing.T) {
	expire := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	page := serverPage{
		servers: []*node.Server{protocolServer(t, 1, "vless", "vless"), protocolServer(t, 2, "vless")},
		online: map[string]node.OnlineUserSubscribe{
			"1/1-vless-0": {11: {"10.0.0.1"}, 12: {"10.0.0.2"}},
			"1/1-vless-1": {11: {"10.0.0.3"}},
			"2/2-vless-0": {12: {"10.0.0.4"}, 99: {"10.0.0.9"}},
		},
	}
	details := &subscriptionDetails{rows: map[int64]*usersub.SubscribeDetails{
		11: {Id: 11, UserId: 1, Upload: 1, Download: 2, ExpireTime: expire, Subscribe: &subscribe.Subscribe{Name: "gold"}},
		12: {Id: 12, UserId: 2, ExpireTime: expire},
	}}

	resp, err := listServers(context.Background(), page, details, &dto.FilterServerListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.calls) != 1 {
		t.Fatalf("subscription queries = %d, want 1: %v", len(details.calls), details.calls)
	}
	if resp.Total != 2 || len(resp.List) != 2 {
		t.Fatalf("response = %+v", resp)
	}
	first := resp.List[0].Status.Online
	if len(first) != 2 || first[0].SubscribeId != 11 || first[0].Subscribe != "gold" || first[0].Traffic != 3 || first[0].UserId != 1 || len(first[0].IP) != 2 {
		t.Fatalf("server 1 online users = %+v", first)
	}
	if first[0].ExpiredAt != expire.UnixMilli() {
		t.Fatalf("expiry = %d", first[0].ExpiredAt)
	}
	// Subscription 99 no longer exists: it is left out, as before.
	second := resp.List[1].Status.Online
	if got := []int64{second[0].SubscribeId}; len(second) != 1 || !reflect.DeepEqual(got, []int64{12}) {
		t.Fatalf("server 2 online users = %+v", second)
	}
	if resp.List[1].Status.Cpu != 2 || len(resp.List[0].Protocols) != 2 {
		t.Fatalf("server fields lost: %+v", resp.List)
	}
}

func TestFilterServerListSeparatesSameTypeListenerCaches(t *testing.T) {
	expire := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	server := protocolServer(t, 3, "vless", "vless")
	page := serverPage{
		servers: []*node.Server{server},
		online: map[string]node.OnlineUserSubscribe{
			"3/3-vless-0": {11: {"10.0.0.1"}},
			"3/3-vless-1": {11: {"10.0.0.2"}},
		},
	}
	details := &subscriptionDetails{rows: map[int64]*usersub.SubscribeDetails{
		11: {Id: 11, UserId: 1, ExpireTime: expire},
	}}
	resp, err := listServers(context.Background(), page, details, &dto.FilterServerListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.List) != 1 || len(resp.List[0].Status.Online) != 1 {
		t.Fatalf("online response = %+v", resp.List)
	}
	want := []dto.ServerOnlineIP{
		{IP: "10.0.0.1", Protocol: "vless"},
		{IP: "10.0.0.2", Protocol: "vless"},
	}
	if got := resp.List[0].Status.Online[0].IP; !reflect.DeepEqual(got, want) {
		t.Fatalf("online IPs = %+v, want each listener's report: %+v", got, want)
	}
}

// A listed server keeps the stored fields in their API form: the times in
// Unix milliseconds, and a zero report time for a server that never reported.
// The list itself fills in the protocols and the status.
func TestServerDTOMapsTheStoredFields(t *testing.T) {
	reported := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	stored := &node.Server{
		Id: 7, Name: "tokyo", Country: "JP", City: "Tokyo", Address: "203.0.113.7", Sort: 3,
		Protocols: `[{"type":"vless"}]`, LastReportedAt: &reported,
		CreatedAt: reported.Add(-time.Hour), UpdatedAt: reported.Add(-time.Minute),
	}
	want := dto.Server{
		Id: 7, Name: "tokyo", Country: "JP", City: "Tokyo", Address: "203.0.113.7", Sort: 3,
		LastReportedAt: reported.UnixMilli(),
		CreatedAt:      reported.Add(-time.Hour).UnixMilli(), UpdatedAt: reported.Add(-time.Minute).UnixMilli(),
	}
	if got := serverDTO(stored); !reflect.DeepEqual(got, want) {
		t.Fatalf("serverDTO = %+v, want %+v", got, want)
	}
	stored.LastReportedAt = nil
	if got := serverDTO(stored); got.LastReportedAt != 0 {
		t.Fatalf("a server that never reported has report time %d", got.LastReportedAt)
	}
}
