package serverload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

// A level rises at once and falls only once the score is more than 0.05
// below the previous level's lower bound, so a score at a threshold does not
// make the level flap.
func TestGradedKeepsTheLevelNearItsLowerBound(t *testing.T) {
	for _, tc := range []struct {
		score    float64
		previous string
		want     string
	}{
		{0.10, "", LevelIdle},
		{0.30, "", LevelNormal},
		{0.59, "", LevelNormal},
		{0.60, "", LevelBusy},
		{0.84, "", LevelBusy},
		{0.85, "", LevelCrowded},
		// A rise is immediate.
		{0.61, LevelNormal, LevelBusy},
		{0.90, LevelIdle, LevelCrowded},
		// A fall waits until the score is more than 0.05 below the bound.
		{0.26, LevelNormal, LevelNormal},
		{0.25, LevelNormal, LevelNormal},
		{0.249, LevelNormal, LevelIdle},
		{0.55, LevelBusy, LevelBusy},
		{0.549, LevelBusy, LevelNormal},
		{0.70, LevelCrowded, LevelBusy},
		{0.10, LevelCrowded, LevelIdle},
		// A stored level the code does not know is not carried over.
		{0.10, "unknown", LevelIdle},
	} {
		if got := graded(tc.score, tc.previous); got != tc.want {
			t.Errorf("graded(%v, %q) = %q, want %q", tc.score, tc.previous, got, tc.want)
		}
	}
}

// Users grade only when a capacity is configured; CPU and memory always do.
func TestLoadScore(t *testing.T) {
	for _, tc := range []struct {
		name     string
		online   int64
		capacity int64
		status   node.Status
		want     float64
	}{
		{"capacity share wins", 50, 100, node.Status{Cpu: 10, Mem: 20}, 0.50},
		{"cpu wins", 10, 100, node.Status{Cpu: 90, Mem: 20}, 0.90},
		{"memory wins", 10, 100, node.Status{Cpu: 10, Mem: 80}, 0.80},
		{"no capacity leaves users out", 1000, 0, node.Status{Cpu: 10, Mem: 20}, 0.20},
		{"over capacity", 150, 100, node.Status{}, 1.50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := loadScore(tc.online, tc.capacity, tc.status); got != tc.want {
				t.Fatalf("loadScore = %v, want %v", got, tc.want)
			}
		})
	}
}

type fakeStatus struct {
	status node.Status
	err    error
	calls  int
}

func (f *fakeStatus) StatusCache(context.Context, int64) (node.Status, error) {
	f.calls++
	return f.status, f.err
}

type fakeOnline struct {
	byListener map[string]node.OnlineUserSubscribe
	err        error
	calls      int
}

func (f *fakeOnline) OnlineUserSubscribe(_ context.Context, _ int64, listenerKey string) (node.OnlineUserSubscribe, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byListener[listenerKey], nil
}

func newTestService(t *testing.T, status *fakeStatus, online *fakeOnline, defaultCapacity int64) (*Service, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewService(Deps{
		Redis:  client,
		Status: status,
		Online: online,
		DefaultCapacity: func() int64 {
			return defaultCapacity
		},
	}), client
}

func onlineServer(t *testing.T, id, capacity int64, listenerKeys ...string) *node.Server {
	t.Helper()
	server := &node.Server{Id: id, Capacity: capacity, LastReportedAt: new(timeutil.Now())}
	protocols := make([]node.Protocol, 0, len(listenerKeys))
	for i, key := range listenerKeys {
		protocols = append(protocols, node.Protocol{ListenerKey: key, Type: "vless", Port: uint16(443 + i), Enable: true})
	}
	if err := server.MarshalProtocols(protocols); err != nil {
		t.Fatal(err)
	}
	return server
}

func readCache(t *testing.T, client *redis.Client, serverID int64) cachedLoad {
	t.Helper()
	data, err := client.Get(context.Background(), fmt.Sprintf(node.ServerLoadCacheKey, serverID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	var entry cachedLoad
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

// Distinct subscriptions across all listeners of a server count once, the
// server's own capacity wins over the default, and the result is cached.
func TestServerLoadsCountsDistinctSubscriptionsAcrossListeners(t *testing.T) {
	status := &fakeStatus{status: node.Status{Cpu: 5, Mem: 5}}
	online := &fakeOnline{byListener: map[string]node.OnlineUserSubscribe{
		"a": {11: {"10.0.0.1"}, 12: {"10.0.0.2"}},
		"b": {12: {"10.0.0.3"}, 13: {"10.0.0.4"}},
	}}
	service, client := newTestService(t, status, online, 10)
	servers := []*node.Server{onlineServer(t, 7, 4, "a", "b")}

	loads, err := service.ServerLoads(context.Background(), servers)
	if err != nil {
		t.Fatal(err)
	}
	// 3 distinct users of a capacity of 4 is 0.75: busy.
	if got := loads[7]; got.Status != node.ServerStatusOnline || got.Load != LevelBusy {
		t.Fatalf("load = %+v, want online busy", got)
	}
	if online.calls != 2 {
		t.Fatalf("online cache reads = %d, want one per listener", online.calls)
	}

	entry := readCache(t, client, 7)
	if entry.Status != node.ServerStatusOnline || entry.Load != LevelBusy {
		t.Fatalf("cached entry = %+v", entry)
	}
	if age := time.Since(time.UnixMilli(entry.ComputedAt)); age < 0 || age > time.Minute {
		t.Fatalf("cached entry was computed %v ago, want now", age)
	}
}

// A cached entry younger than two minutes is returned as it is; an older one
// is recomputed, and the stored level gates the fall.
func TestServerLoadsReusesAndRefreshesTheCache(t *testing.T) {
	status := &fakeStatus{status: node.Status{Cpu: 70}}
	online := &fakeOnline{byListener: map[string]node.OnlineUserSubscribe{"a": {}}}
	service, client := newTestService(t, status, online, 0)
	server := onlineServer(t, 9, 0, "a")
	key := fmt.Sprintf(node.ServerLoadCacheKey, 9)
	ctx := context.Background()

	stale := timeutil.Now().Add(-3 * time.Minute).UnixMilli()
	entry, err := json.Marshal(cachedLoad{Status: node.ServerStatusOnline, Load: LevelBusy, ComputedAt: stale})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, key, entry, cacheTTL).Err(); err != nil {
		t.Fatal(err)
	}

	// 0.70 is busy by the thresholds, and 0.70 is not more than 0.05 below
	// busy's 0.60 bound: the stored busy level is kept.
	loads, err := service.ServerLoads(ctx, []*node.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	if got := loads[9]; got.Load != LevelBusy {
		t.Fatalf("stale entry graded %+v, want the hysteresis to keep busy", got)
	}
	if refreshed := readCache(t, client, 9); refreshed.ComputedAt <= stale {
		t.Fatalf("entry was not recomputed: computed_at %d", refreshed.ComputedAt)
	}

	// A fresh entry is reused without reading the figures.
	status.calls, online.calls = 0, 0
	status.status = node.Status{Cpu: 99}
	if _, err := service.ServerLoads(ctx, []*node.Server{server}); err != nil {
		t.Fatal(err)
	}
	if status.calls != 0 || online.calls != 0 {
		t.Fatalf("a fresh entry was recomputed: status %d, online %d", status.calls, online.calls)
	}
}

// A fall far enough below the stored level's bound drops several levels at
// once.
func TestServerLoadsDropsSeveralLevelsWhenFarBelow(t *testing.T) {
	status := &fakeStatus{status: node.Status{Cpu: 10}}
	online := &fakeOnline{byListener: map[string]node.OnlineUserSubscribe{"a": {}}}
	service, client := newTestService(t, status, online, 0)
	server := onlineServer(t, 3, 0, "a")
	key := fmt.Sprintf(node.ServerLoadCacheKey, 3)

	stale := timeutil.Now().Add(-3 * time.Minute).UnixMilli()
	entry, err := json.Marshal(cachedLoad{Status: node.ServerStatusOnline, Load: LevelCrowded, ComputedAt: stale})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(context.Background(), key, entry, cacheTTL).Err(); err != nil {
		t.Fatal(err)
	}

	loads, err := service.ServerLoads(context.Background(), []*node.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	if got := loads[3]; got.Load != LevelIdle {
		t.Fatalf("load = %+v, want idle", got)
	}
}

// The node settings' default is the capacity of a server without one, and
// neither drops users from the grade.
func TestServerLoadsFallsBackToTheDefaultCapacity(t *testing.T) {
	for _, tc := range []struct {
		name            string
		serverCapacity  int64
		defaultCapacity int64
		online          int64
		want            string
	}{
		{"default fills in", 0, 10, 6, LevelBusy},
		{"server capacity wins", 2, 10, 3, LevelCrowded},
		{"no capacity anywhere leaves users out", 0, 0, 1000, LevelIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &fakeStatus{}
			online := &fakeOnline{byListener: map[string]node.OnlineUserSubscribe{"a": users(tc.online)}}
			service, _ := newTestService(t, status, online, tc.defaultCapacity)
			loads, err := service.ServerLoads(context.Background(), []*node.Server{onlineServer(t, 1, tc.serverCapacity, "a")})
			if err != nil {
				t.Fatal(err)
			}
			if got := loads[1]; got.Load != tc.want {
				t.Fatalf("load = %+v, want %s", got, tc.want)
			}
		})
	}
}

func users(count int64) node.OnlineUserSubscribe {
	online := make(node.OnlineUserSubscribe, count)
	for id := int64(1); id <= count; id++ {
		online[id] = []string{"10.0.0.1"}
	}
	return online
}

// A server that has not reported for five minutes is offline and has no
// level; its figures are not read at all.
func TestServerLoadsLeavesAnOfflineServerUngraded(t *testing.T) {
	reported := timeutil.Now().Add(-6 * time.Minute)
	server := &node.Server{Id: 5, LastReportedAt: &reported}
	status := &fakeStatus{err: errors.New("status must not be read")}
	online := &fakeOnline{err: errors.New("online users must not be read")}
	service, _ := newTestService(t, status, online, 10)

	loads, err := service.ServerLoads(context.Background(), []*node.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	if got := loads[5]; got.Status != node.ServerStatusOffline || got.Load != "" {
		t.Fatalf("load = %+v, want offline without a level", got)
	}
	if status.calls != 0 || online.calls != 0 {
		t.Fatalf("an offline server had its figures read")
	}
}

// A corrupt cache entry is recomputed, not reported.
func TestServerLoadsIgnoresACorruptCacheEntry(t *testing.T) {
	status := &fakeStatus{status: node.Status{Cpu: 40}}
	online := &fakeOnline{byListener: map[string]node.OnlineUserSubscribe{"a": {}}}
	service, client := newTestService(t, status, online, 0)
	key := fmt.Sprintf(node.ServerLoadCacheKey, 8)
	if err := client.Set(context.Background(), key, "not json", cacheTTL).Err(); err != nil {
		t.Fatal(err)
	}

	loads, err := service.ServerLoads(context.Background(), []*node.Server{onlineServer(t, 8, 0, "a")})
	if err != nil {
		t.Fatal(err)
	}
	if got := loads[8]; got.Load != LevelNormal {
		t.Fatalf("load = %+v, want normal", got)
	}
}
