package adminserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type nodeBindingStore struct {
	repository.Store
	nodes repository.NodeRepo
}

func (s nodeBindingStore) Node() repository.NodeRepo { return s.nodes }

func newNodeBindingService(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", name, time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&node.Server{}, &node.Node{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	server := &node.Server{Id: 1, Name: "server", Address: "203.0.113.10"}
	if err := server.MarshalProtocols([]node.Protocol{
		{ListenerKey: "listener-a", Type: "trojan", Port: 443, Enable: true, Security: "tls", SNI: "a.example.com", CertMode: "self", Transport: "tcp"},
		{ListenerKey: "listener-b", Type: "trojan", Port: 8443, Enable: true, Security: "tls", SNI: "b.example.com", CertMode: "self", Transport: "ws", Path: "/b"},
	}); err != nil {
		t.Fatalf("marshal protocols: %v", err)
	}
	if err := db.Create(server).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}

	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return NewService(Deps{
		Store:  nodeBindingStore{nodes: repo.NewNodeRepo(db, client)},
		Config: func() Snapshot { return Snapshot{} },
	}), db
}

func loadNode(t *testing.T, db *gorm.DB, id int64) node.Node {
	t.Helper()
	var stored node.Node
	if err := db.First(&stored, id).Error; err != nil {
		t.Fatalf("load node %d: %v", id, err)
	}
	return stored
}

// Creating a node persists the listener it is bound to; the port comes from
// that listener when the request does not override it.
func TestCreateNodePersistsTheListenerBinding(t *testing.T) {
	service, db := newNodeBindingService(t, "node-binding-create")
	enabled := true
	if err := service.CreateNode(context.Background(), &dto.CreateNodeRequest{
		Name:        "node-b",
		Address:     "b.example.com",
		ServerId:    1,
		ListenerKey: "listener-b",
		Enabled:     &enabled,
	}); err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	stored := loadNode(t, db, 1)
	if stored.ListenerKey != "listener-b" || stored.Protocol != "trojan" || stored.Port != 8443 {
		t.Fatalf("stored node = %+v, want it bound to listener-b on port 8443", stored)
	}
}

// Rebinding a node must move its listener key, protocol and port together; a
// stale binding would keep sending its traffic to the previous listener's
// ratio and user list.
func TestUpdateNodeRebindsTheListener(t *testing.T) {
	service, db := newNodeBindingService(t, "node-binding-update")
	enabled := true
	if err := service.CreateNode(context.Background(), &dto.CreateNodeRequest{
		Name: "node", Address: "a.example.com", ServerId: 1, ListenerKey: "listener-a", Enabled: &enabled,
	}); err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if err := service.UpdateNode(context.Background(), &dto.UpdateNodeRequest{
		Id: 1, Name: "node", Address: "a.example.com", ServerId: 1, ListenerKey: "listener-b", Enabled: &enabled,
	}); err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}
	stored := loadNode(t, db, 1)
	if stored.ListenerKey != "listener-b" || stored.Port != 8443 {
		t.Fatalf("stored node = %+v, want it rebound to listener-b on port 8443", stored)
	}
}

// The node list is how an administrator sees which listener a node serves, so
// the binding has to be reported.
func TestFilterNodeListReportsTheListenerBinding(t *testing.T) {
	service, _ := newNodeBindingService(t, "node-binding-list")
	enabled := true
	if err := service.CreateNode(context.Background(), &dto.CreateNodeRequest{
		Name: "node-b", Address: "b.example.com", ServerId: 1, ListenerKey: "listener-b", Enabled: &enabled,
	}); err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	resp, err := service.FilterNodeList(context.Background(), &dto.FilterNodeListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("FilterNodeList() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(resp.List))
	}
	if resp.List[0].ListenerKey != "listener-b" {
		t.Fatalf("listed listener key = %q, want listener-b", resp.List[0].ListenerKey)
	}
}

// A request that names only a protocol type cannot pick between two listeners
// of that type.
func TestUpdateNodeRejectsAmbiguousRebinding(t *testing.T) {
	service, db := newNodeBindingService(t, "node-binding-ambiguous")
	enabled := true
	if err := service.CreateNode(context.Background(), &dto.CreateNodeRequest{
		Name: "node", Address: "a.example.com", ServerId: 1, ListenerKey: "listener-a", Enabled: &enabled,
	}); err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if err := service.UpdateNode(context.Background(), &dto.UpdateNodeRequest{
		Id: 1, Name: "node", Address: "a.example.com", ServerId: 1, Protocol: "trojan", Enabled: &enabled,
	}); err == nil {
		t.Fatal("UpdateNode() error = nil, want the ambiguous rebinding rejected")
	}
	if stored := loadNode(t, db, 1); stored.ListenerKey != "listener-a" {
		t.Fatalf("stored listener key = %q, want the original binding kept", stored.ListenerKey)
	}
}
