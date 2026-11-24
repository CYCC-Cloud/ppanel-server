package adminserver

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/redis/go-redis/v9"
)

// stubUserSubscriptions supplies the batched subscription read used by the list.
type stubUserSubscriptions struct {
	details map[int64]*usersub.SubscribeDetails
}

func (s stubUserSubscriptions) SubscriptionDetailsByIDs(_ context.Context, ids []int64) ([]*usersub.SubscribeDetails, error) {
	var result []*usersub.SubscribeDetails
	for _, id := range ids {
		if detail, ok := s.details[id]; ok {
			result = append(result, detail)
		}
	}
	return result, nil
}

// A server running two listeners of the same protocol type must report which
// listener each address came from. Reporting only the type collapses the two
// listeners, and one address online on both produces two indistinguishable
// entries.
func TestOnlineUsersIdentifyTheReportingListener(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	nodes := repo.NewNodeRepo(nil, client)

	const subscribeID int64 = 7
	if err := nodes.UpdateOnlineUserSubscribe(ctx, 1, "listener-a", node.OnlineUserSubscribe{
		subscribeID: []string{"198.51.100.5"},
	}); err != nil {
		t.Fatalf("seed listener-a online state: %v", err)
	}
	if err := nodes.UpdateOnlineUserSubscribe(ctx, 1, "listener-b", node.OnlineUserSubscribe{
		subscribeID: []string{"198.51.100.5"},
	}); err != nil {
		t.Fatalf("seed listener-b online state: %v", err)
	}

	expire := time.Now().Add(time.Hour)
	subscriptions := stubUserSubscriptions{details: map[int64]*usersub.SubscribeDetails{
		subscribeID: {
			Id: subscribeID, UserId: 42, Download: 30, Upload: 12, ExpireTime: expire,
		},
	}}
	online := onlineUsers(onlineIPs(ctx, nodes, 1, []dto.Protocol{
		{ListenerKey: "listener-a", ListenerName: "Primary", Type: "trojan", Port: 443},
		{ListenerKey: "listener-b", ListenerName: "Fallback", Type: "trojan", Port: 8443},
	}), onlineSubscriptions(ctx, subscriptions, []int64{subscribeID}))

	if len(online) != 1 {
		t.Fatalf("expected the two listeners to merge into one subscriber, got %d", len(online))
	}
	if online[0].UserId != 42 {
		t.Fatalf("user id = %d, want 42", online[0].UserId)
	}
	if online[0].Traffic != 42 {
		t.Fatalf("traffic = %d, want 42", online[0].Traffic)
	}

	addresses := online[0].IP
	if len(addresses) != 2 {
		t.Fatalf("expected one entry per listener, got %d", len(addresses))
	}
	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].ListenerKey < addresses[j].ListenerKey
	})

	want := []dto.ServerOnlineIP{
		{IP: "198.51.100.5", Protocol: "trojan", ListenerKey: "listener-a", ListenerName: "Primary"},
		{IP: "198.51.100.5", Protocol: "trojan", ListenerKey: "listener-b", ListenerName: "Fallback"},
	}
	for i, expected := range want {
		if addresses[i] != expected {
			t.Fatalf("address %d = %+v, want %+v", i, addresses[i], expected)
		}
	}
}

// A listener that reported nothing must not appear, so an unused listener does
// not invent an empty online entry.
func TestOnlineUsersSkipListenersWithoutState(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	nodes := repo.NewNodeRepo(nil, client)

	const subscribeID int64 = 9
	if err := nodes.UpdateOnlineUserSubscribe(ctx, 1, "listener-b", node.OnlineUserSubscribe{
		subscribeID: []string{"203.0.113.9"},
	}); err != nil {
		t.Fatalf("seed listener-b online state: %v", err)
	}

	subscriptions := stubUserSubscriptions{details: map[int64]*usersub.SubscribeDetails{
		subscribeID: {Id: subscribeID, UserId: 11, ExpireTime: time.Now().Add(time.Hour)},
	}}
	online := onlineUsers(onlineIPs(ctx, nodes, 1, []dto.Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443},
	}), onlineSubscriptions(ctx, subscriptions, []int64{subscribeID}))

	if len(online) != 1 {
		t.Fatalf("expected one subscriber, got %d", len(online))
	}
	if len(online[0].IP) != 1 {
		t.Fatalf("expected one address, got %d", len(online[0].IP))
	}
	if online[0].IP[0].ListenerKey != "listener-b" {
		t.Fatalf("listener key = %q, want listener-b", online[0].IP[0].ListenerKey)
	}
}
