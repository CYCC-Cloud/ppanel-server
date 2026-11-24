package serverapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"uuid"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

func TestPlaceholderServerUserUsesNewUUIDV7(t *testing.T) {
	seen := make(map[uuid.UUID]bool)
	for range 32 {
		user := placeholderServerUser()
		id, err := uuid.Parse(user.UUID)
		if err != nil {
			t.Fatal(err)
		}
		if user.Id != 1 || id[6]>>4 != 7 || id[8]>>6 != 2 {
			t.Fatalf("invalid V7 placeholder: %+v", user)
		}
		if seen[id] {
			t.Fatal("placeholder generation reused a UUID")
		}
		seen[id] = true
	}
}

type staticServerReader struct {
	server *node.Server
}

func (r staticServerReader) FindOneServer(context.Context, int64) (*node.Server, error) {
	return r.server, nil
}

func TestCachedPlaceholderKeepsUUIDAndETag(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	placeholder := placeholderServerUser()
	payload, err := json.Marshal(dto.GetServerUserListResponse{Users: []dto.ServerUser{placeholder}})
	if err != nil {
		t.Fatal(err)
	}
	req := &dto.GetServerUserListRequest{
		ServerCommon: dto.ServerCommon{ServerId: 1, Protocol: "vless", ListenerKey: "listener-vless"},
	}
	if err := server.Set(serverUserListCacheKey(req.ServerId, req.ListenerKey), string(payload)); err != nil {
		t.Fatal(err)
	}
	// The server row is resolved to validate the listener identity; no other
	// repository is needed on a cache hit.
	service := NewService(Deps{
		Redis:   client,
		Servers: staticServerReader{server: &node.Server{Id: 1, Protocols: `[{"listener_key":"listener-vless","type":"vless","enable":true}]`}},
	})
	etag := httpx.GenerateETag(payload)
	for range 2 {
		resp, meta, err := service.GetServerUserList(context.Background(), req, RequestMeta{})
		if err != nil || len(resp.Users) != 1 || resp.Users[0].UUID != placeholder.UUID {
			t.Fatalf("cached user list changed: %+v, %v", resp, err)
		}
		if meta.Headers["ETag"] != etag {
			t.Fatalf("cached user list ETag = %q, want %q", meta.Headers["ETag"], etag)
		}
	}
	if resp, _, err := service.GetServerUserList(context.Background(), req, RequestMeta{IfNoneMatch: etag}); resp != nil || !errors.Is(err, xerr.ErrNotModified) {
		t.Fatalf("cached ETag no longer returns not-modified: %+v, %v", resp, err)
	}
}
