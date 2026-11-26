package storefront

import (
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
)

// Nodes on one server share its graded state, which is read once for the
// whole list.
func TestQueryUserSubscribeNodeListSharesOneServerLoad(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5,6"})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	server := &node.Server{Id: 3}
	nodes := &planNodes{nodes: []*node.Node{
		{Id: 5, Protocol: "vless", Server: server},
		{Id: 6, Protocol: "vless", Server: server},
	}}
	loads := &planLoads{loads: map[int64]node.ServerLoad{3: {Status: node.ServerStatusOnline, Load: "busy"}}}

	resp, err := newStorefrontWithLoads(f, nodes, loads, 0).QueryUserSubscribeNodeList(ownerContext())
	if err != nil {
		t.Fatal(err)
	}
	if loads.calls != 1 || len(loads.servers) != 1 || loads.servers[0].Id != 3 {
		t.Fatalf("load read %d times for %d servers", loads.calls, len(loads.servers))
	}
	listed := resp.List[0].Nodes
	if len(listed) != 2 {
		t.Fatalf("listed %d nodes, want both", len(listed))
	}
	for _, info := range listed {
		if info.Status != node.ServerStatusOnline || info.Load != "busy" {
			t.Fatalf("node %d has status %q load %q", info.Id, info.Status, info.Load)
		}
	}
}

// A load that cannot be read leaves both fields empty and still lists the
// nodes.
func TestQueryUserSubscribeNodeListKeepsNodesWithoutALoad(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Nodes: "5"})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	nodes := &planNodes{nodes: []*node.Node{{Id: 5, Protocol: "vless", Server: &node.Server{Id: 3}}}}
	loads := &planLoads{err: errors.New("redis unavailable")}

	resp, err := newStorefrontWithLoads(f, nodes, loads, 0).QueryUserSubscribeNodeList(ownerContext())
	if err != nil {
		t.Fatalf("node list failed on an unreadable load: %v", err)
	}
	listed := resp.List[0].Nodes
	if len(listed) != 1 || listed[0].Status != "" || listed[0].Load != "" {
		t.Fatalf("nodes = %+v, want one without a graded state", listed)
	}
}
