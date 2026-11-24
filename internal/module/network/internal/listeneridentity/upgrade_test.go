package listeneridentity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/listeneridentity"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// nodeStore narrows the shared store to the only repository the upgrade uses.
// The module may not reach into the composition root, so the network node
// repository is wired directly instead.
type nodeStore struct {
	repository.Store
	nodes repository.NodeRepo
}

func (s nodeStore) Node() repository.NodeRepo { return s.nodes }

func newStore(t *testing.T, name string) (repository.Store, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", name, time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&node.Server{}, &node.Node{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return nodeStore{nodes: repo.NewNodeRepo(db, client)}, db
}

// Protocols and nodes written before listener keys existed carry none. The
// upgrade has to persist an identity for them, because every listener-scoped
// flow resolves by that key.
func TestUpgradeAssignsListenerKeysAndBindsNodes(t *testing.T) {
	store, db := newStore(t, "listener-upgrade")
	legacyProtocols := `[{"type":"shadowsocks","port":1234,"enable":true},{"type":"trojan","port":443,"enable":true}]`
	if err := db.Exec("INSERT INTO servers (id, name, address, sort, protocols) VALUES (?, ?, ?, ?, ?)",
		1, "legacy", "example.com", 1, legacyProtocols).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec("INSERT INTO nodes (id, name, port, address, server_id, protocol, listener_key, enabled, sort) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "ss", 1234, "ss.example.com", 1, "shadowsocks", "", true, 1).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := db.Exec("INSERT INTO nodes (id, name, port, address, server_id, protocol, listener_key, enabled, sort) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		2, "trojan", 443, "trojan.example.com", 1, "trojan", "", true, 2).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	service := listeneridentity.NewService(listeneridentity.Deps{Store: store})
	ctx := context.Background()
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	protocols := readProtocols(t, db)
	if len(protocols) != 2 {
		t.Fatalf("len(protocols) = %d, want 2", len(protocols))
	}
	byType := make(map[string]node.Protocol, len(protocols))
	for _, protocol := range protocols {
		if protocol.ListenerKey == "" {
			t.Fatalf("listener %q has no key", protocol.Type)
		}
		byType[protocol.Type] = protocol
	}
	for id, protocolType := range map[int64]string{1: "shadowsocks", 2: "trojan"} {
		var bound node.Node
		if err := db.First(&bound, id).Error; err != nil {
			t.Fatalf("load node %d: %v", id, err)
		}
		if want := byType[protocolType].ListenerKey; bound.ListenerKey != want {
			t.Fatalf("node %d listener_key = %q, want %q", id, bound.ListenerKey, want)
		}
	}

	// A repeated run must converge: keys and bindings stay identical, so a
	// restart never re-keys listeners the nodes already reference.
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() second error = %v", err)
	}
	if again := readProtocols(t, db); again[0].ListenerKey != protocols[0].ListenerKey || again[1].ListenerKey != protocols[1].ListenerKey {
		t.Fatalf("listener keys changed on the second run: %v -> %v", protocols, again)
	}
}

func TestUpgradePreservesExistingListenerIdentity(t *testing.T) {
	store, db := newStore(t, "listener-upgrade-preserve")
	protocols := `[{"listener_key":"listener-a","type":"trojan","port":443,"enable":true},{"type":"vmess","port":444,"enable":true}]`
	if err := db.Exec("INSERT INTO servers (id, name, address, sort, protocols) VALUES (?, ?, ?, ?, ?)",
		1, "mixed", "example.com", 1, protocols).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec("INSERT INTO nodes (id, name, port, address, server_id, protocol, listener_key, enabled, sort) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "trojan", 443, "trojan.example.com", 1, "trojan", "listener-a", true, 1).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := listeneridentity.NewService(listeneridentity.Deps{Store: store}).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	stored := readProtocols(t, db)
	if stored[0].ListenerKey != "listener-a" {
		t.Fatalf("existing listener key = %q, want listener-a", stored[0].ListenerKey)
	}
	if stored[1].ListenerKey == "" || stored[1].ListenerKey == "listener-a" {
		t.Fatalf("second listener key = %q, want a distinct generated key", stored[1].ListenerKey)
	}
	var bound node.Node
	if err := db.First(&bound, 1).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if bound.ListenerKey != "listener-a" {
		t.Fatalf("node listener_key = %q, want listener-a", bound.ListenerKey)
	}
}

// Two same-type listeners cannot be told apart by a legacy node's protocol
// alone. Guessing would bill its traffic at the other listener's ratio, so the
// upgrade must refuse instead of picking one.
func TestUpgradeRefusesAmbiguousLegacyNodeBinding(t *testing.T) {
	store, db := newStore(t, "listener-upgrade-ambiguous")
	protocols := `[{"type":"trojan","port":443,"enable":true},{"type":"trojan","port":8443,"enable":true}]`
	if err := db.Exec("INSERT INTO servers (id, name, address, sort, protocols) VALUES (?, ?, ?, ?, ?)",
		1, "ambiguous", "example.com", 1, protocols).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec("INSERT INTO nodes (id, name, port, address, server_id, protocol, listener_key, enabled, sort) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "trojan", 443, "trojan.example.com", 1, "trojan", "", true, 1).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	err := listeneridentity.NewService(listeneridentity.Deps{Store: store}).Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want a refusal to guess the node's listener")
	}
	var bound node.Node
	if loadErr := db.First(&bound, 1).Error; loadErr != nil {
		t.Fatalf("load node: %v", loadErr)
	}
	if bound.ListenerKey != "" {
		t.Fatalf("node listener_key = %q, want it left unbound", bound.ListenerKey)
	}
}

// A node whose protocol its server no longer configures already serves
// nothing; it must not block the upgrade of everything else.
func TestUpgradeSkipsNodeWithoutAMatchingListener(t *testing.T) {
	store, db := newStore(t, "listener-upgrade-orphan")
	protocols := `[{"type":"trojan","port":443,"enable":true}]`
	if err := db.Exec("INSERT INTO servers (id, name, address, sort, protocols) VALUES (?, ?, ?, ?, ?)",
		1, "orphaned", "example.com", 1, protocols).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec("INSERT INTO nodes (id, name, port, address, server_id, protocol, listener_key, enabled, sort) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		1, "vmess", 444, "vmess.example.com", 1, "vmess", "", true, 1).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := listeneridentity.NewService(listeneridentity.Deps{Store: store}).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stored := readProtocols(t, db); stored[0].ListenerKey == "" {
		t.Fatal("the server's listener was left without a key")
	}
	var bound node.Node
	if err := db.First(&bound, 1).Error; err != nil {
		t.Fatalf("load node: %v", err)
	}
	if bound.ListenerKey != "" {
		t.Fatalf("node listener_key = %q, want it left unbound", bound.ListenerKey)
	}
}

func readProtocols(t *testing.T, db *gorm.DB) []node.Protocol {
	t.Helper()
	var raw string
	if err := db.Raw("SELECT protocols FROM servers WHERE id = ?", 1).Scan(&raw).Error; err != nil {
		t.Fatalf("read protocols: %v", err)
	}
	var protocols []node.Protocol
	if err := json.Unmarshal([]byte(raw), &protocols); err != nil {
		t.Fatalf("unmarshal protocols %q: %v", raw, err)
	}
	return protocols
}
