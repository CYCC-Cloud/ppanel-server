package serverapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// placeholderServerUserID is the id of the stand-in user an empty list
// carries. It is also a valid subscription id.
const placeholderServerUserID int64 = 1

// placeholderServerUser is the stand-in user of an empty list, with a fresh
// UUIDv7. It is generated only while rebuilding the list: cache hits reuse
// the serialized UUID and ETag until the list expires or changes.
func placeholderServerUser() dto.ServerUser {
	return dto.ServerUser{
		Id:   placeholderServerUserID,
		UUID: uuid.NewV7().String(),
	}
}

// serverUserListCacheKey scopes a node's user list to the listener it serves,
// so two listeners of the same protocol type never share one.
func serverUserListCacheKey(serverID int64, listenerKey string) string {
	return fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, serverID, listenerKey)
}

// serverProtocolUserListCacheKey scopes the type-wide allowlist the traffic
// and online reports validate against, separately from the listener's own
// response cache.
func serverProtocolUserListCacheKey(serverID int64, protocol string) string {
	key := fmt.Sprintf("%s%d:%s", node.ServerProtocolUserListCacheKey, serverID, protocol)
	if protocol == Nowhere {
		return key + ":nw3"
	}
	return key
}

// GetServerUserList returns the users bound to one listener of a server, from
// the listener's response cache when it holds one, rebuilding and caching the
// list otherwise. meta carries the node's If-None-Match: a matching ETag
// returns xerr.ErrNotModified. The returned response metadata holds the
// headers set before any error.
func (s *Service) GetServerUserList(ctx context.Context, req *dto.GetServerUserListRequest, meta RequestMeta) (*dto.GetServerUserListResponse, ResponseMeta, error) {
	log := logger.WithContext(ctx)
	response := NewResponseMeta()
	server, listener, err := s.resolveServerListener(ctx, req.ServerId, req.ServerCommon)
	if err != nil {
		return nil, response, userListWaitError(fmt.Errorf("resolve listener (listener_key=%q, protocol=%q): %w", req.ListenerKey, req.Protocol, err))
	}
	if !listener.Enable {
		return nil, response, fmt.Errorf("listener %s is disabled", listener.ListenerKey)
	}
	cacheKey := serverUserListCacheKey(server.Id, listener.ListenerKey)
	cache, err := s.deps.Redis.Get(ctx, cacheKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		// An unreadable cache must not cost the node its user list: fall
		// through to the database like a miss.
		log.Errorw("[GetServerUserList] read cached user list failed",
			logger.Field("server_id", req.ServerId),
			logger.Field("listener_key", listener.ListenerKey),
			logger.Field("protocol", req.Protocol),
			logger.Field("error", err.Error()))
		cache = ""
	}
	if cache != "" {
		etag := httpx.GenerateETag([]byte(cache))
		if meta.IfNoneMatch == etag {
			return nil, response, xerr.ErrNotModified
		}
		response.SetHeader("ETag", etag)
		resp := &dto.GetServerUserListResponse{}
		if err := json.Unmarshal([]byte(cache), resp); err != nil {
			log.Errorw("[ServerUserListCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, response, err
		}
		return resp, response, nil
	}
	list, err := s.rebuildUserList(ctx, server, listener)
	if err != nil {
		return nil, response, err
	}
	etag := httpx.GenerateETag(list.payload)
	response.SetHeader("ETag", etag)
	if meta.IfNoneMatch == etag {
		return nil, response, xerr.ErrNotModified
	}
	return list.resp, response, nil
}

// userList is a rebuilt node user list with its cached serialization.
type userList struct {
	resp       *dto.GetServerUserListResponse
	payload    []byte
	generation int64
	expiresAt  time.Time
	// A database rebuild already checked existence; a Redis list alone does not.
	serverChecked bool
}

// loadUserList fills one cache scope: the single-flight key and the server
// whose cache generation fences the fill. build runs only on a miss.
func (s *Service) loadUserList(ctx context.Context, key string, serverID int64, build func(context.Context, int64) (*userList, error)) (*userList, error) {
	result := s.userListLoads.DoChan(key, func() (any, error) {
		// One disconnected node must not cancel a fill needed by other nodes.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), userListLoadTimeout)
		defer cancel()
		if err := s.userListBuilds.acquire(ctx); err != nil {
			return nil, err
		}
		defer s.userListBuilds.release()
		generation, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
		if err != nil {
			return nil, err
		}
		// Another caller may have filled Redis since the original miss.
		cached, err := s.readCachedUserList(ctx, key, generation)
		if err == nil && cached != nil {
			return cached, nil
		}
		return build(ctx, generation)
	})
	select {
	case <-ctx.Done():
		return nil, userListWaitError(ctx.Err())
	case result := <-result:
		if result.Err != nil {
			return nil, userListWaitError(result.Err)
		}
		return result.Val.(*userList), nil
	}
}

// rebuildUserList resolves one listener's user list and caches it for the next
// reader. All node endpoints share one fill per listener.
func (s *Service) rebuildUserList(ctx context.Context, server *node.Server, listener node.Protocol) (*userList, error) {
	key := serverUserListCacheKey(server.Id, listener.ListenerKey)
	return s.loadUserList(ctx, key, server.Id, func(ctx context.Context, generation int64) (*userList, error) {
		users, err := s.servedUsers(ctx, server, listener.Type, listener.ListenerKey)
		if err != nil {
			return nil, err
		}
		list, err := s.newUserList(listener.Type, generation, users)
		if err != nil {
			return nil, err
		}
		if err := s.deps.Caches.SetServerCache(ctx, server.Id, key, string(list.payload), generation); err != nil {
			logger.WithContext(ctx).Errorw("[ServerUserListCacheKey] cache set error", logger.Field("error", err.Error()))
		}
		return list, nil
	})
}

// rebuildProtocolUserList resolves the type-wide scope used by report
// validation and caches it separately from listener response caches.
func (s *Service) rebuildProtocolUserList(ctx context.Context, serverID int64, protocol string) (*userList, error) {
	key := serverProtocolUserListCacheKey(serverID, protocol)
	return s.loadUserList(ctx, key, serverID, func(ctx context.Context, generation int64) (*userList, error) {
		server, err := s.deps.Servers.FindOneServer(ctx, serverID)
		if err != nil {
			return nil, err
		}
		users, err := s.servedUsers(ctx, server, protocol, "")
		if err != nil {
			return nil, err
		}
		list, err := s.newUserList(protocol, generation, users)
		if err != nil {
			return nil, err
		}
		if err := s.deps.Caches.SetServerCache(ctx, serverID, key, string(list.payload), generation); err != nil {
			logger.WithContext(ctx).Errorw("[ServerUserListCacheKey] cache set error", logger.Field("error", err.Error()))
		}
		return list, nil
	})
}

// newUserList serializes the users a scope serves, padding an empty list with
// the placeholder and deriving Nowhere's shared key.
func (s *Service) newUserList(protocol string, generation int64, users []dto.ServerUser) (*userList, error) {
	list := &userList{generation: generation, expiresAt: time.Now().Add(node.ServerCacheTTL), serverChecked: true}
	if len(users) == 0 {
		users = []dto.ServerUser{placeholderServerUser()}
	}
	if protocol == Nowhere {
		for index := range users {
			key, err := node.NowhereSharedKey(users[index].UUID)
			if err != nil {
				return nil, fmt.Errorf("nowhere credential for subscription %d: %w", users[index].Id, err)
			}
			users[index].UUID = key
		}
	}
	list.resp = &dto.GetServerUserListResponse{Users: users}
	list.payload, _ = json.Marshal(list.resp)
	return list, nil
}

// servedUsers resolves the servable subscriptions of nodes bound to one
// listener, owned by enabled accounts. An empty listener key scopes by
// protocol type only.
func (s *Service) servedUsers(ctx context.Context, server *node.Server, protocol, listenerKey string) ([]dto.ServerUser, error) {
	nodes, err := s.deps.Nodes.ListNodes(ctx, &node.FilterNodeParams{
		ServerId:    []int64{server.Id},
		Protocol:    protocol,
		ListenerKey: listenerKey,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetServerUserList] list the server's nodes failed", logger.Field("error", err.Error()))
		return nil, err
	}
	var nodeTag []string
	var nodeIds []int64
	for _, n := range nodes {
		nodeIds = append(nodeIds, n.Id)
		if n.Tags != "" {
			nodeTag = append(nodeTag, strings.Split(n.Tags, ",")...)
		}
	}

	// A read endpoint: it selects the servable subscriptions of the plans
	// scoped to the nodes and writes nothing (legacy Pending rows are
	// servable as they are).
	served, err := s.deps.Subscriptions.ServableSubscriptionsByNodeScope(ctx, nodeIds, nodeTag)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetServerUserList] read the servable subscriptions failed", logger.Field("error", err.Error()))
		return nil, err
	}
	if len(served) == 0 {
		return nil, nil
	}
	userIDs := make([]int64, 0, len(served))
	for _, item := range served {
		userIDs = append(userIDs, item.Subscription.UserId)
	}
	enabledIDs, err := s.deps.Accounts.FindEnabledUserIDs(ctx, slicesx.RemoveDuplicateElements(userIDs...))
	if err != nil {
		return nil, err
	}
	enabled := make(map[int64]struct{}, len(enabledIDs))
	for _, id := range enabledIDs {
		enabled[id] = struct{}{}
	}
	users := make([]dto.ServerUser, 0, len(served))
	for _, item := range served {
		if _, ok := enabled[item.Subscription.UserId]; !ok {
			continue
		}
		users = append(users, dto.ServerUser{
			Id: item.Subscription.Id, UUID: item.Subscription.UUID,
			SpeedLimit: item.Plan.SpeedLimit, DeviceLimit: item.Plan.DeviceLimit,
		})
	}
	return users, nil
}

// servedSubscriptionIDs returns an immutable, shared set. Callers must not
// mutate it: reports and device admission use the same generation of users.
func (s *Service) servedSubscriptionIDs(ctx context.Context, serverID int64, protocol string) (map[int64]struct{}, error) {
	index, err := s.servedUserIndex(ctx, serverID, protocol)
	if err != nil {
		return nil, err
	}
	return index.ids, nil
}

// readCachedUserList is used only during a shared fill. The local index must
// expire no later than the source list, including lists filled by older servers.
func (s *Service) readCachedUserList(ctx context.Context, key string, generation int64) (*userList, error) {
	started := time.Now()
	pipe := s.deps.Redis.TxPipeline()
	data := pipe.Get(ctx, key)
	ttl := pipe.PTTL(ctx, key)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if data.Val() == "" {
		return nil, nil
	}
	lifetime := ttl.Val()
	if lifetime < 0 || lifetime > node.ServerCacheTTL {
		lifetime = node.ServerCacheTTL
	}
	list := &userList{
		resp: &dto.GetServerUserListResponse{}, payload: []byte(data.Val()),
		generation: generation, expiresAt: started.Add(lifetime),
	}
	if err := json.Unmarshal(list.payload, list.resp); err != nil {
		return nil, err
	}
	return list, nil
}

// withoutPlaceholder drops the stand-in user of a cached empty list. Its id
// is a valid subscription id, so a lone entry with that id is kept only when
// its UUID is that subscription's UUID.
func (s *Service) withoutPlaceholder(ctx context.Context, protocol string, users []dto.ServerUser) ([]dto.ServerUser, error) {
	if len(users) != 1 || users[0].Id != placeholderServerUserID {
		return users, nil
	}
	sub, err := s.deps.Subscriptions.SubscriptionByID(ctx, placeholderServerUserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected := sub.UUID
	if protocol == Nowhere {
		expected, err = node.NowhereSharedKey(expected)
		if err != nil {
			return nil, err
		}
	}
	if expected != users[0].UUID {
		return nil, nil
	}
	return users, nil
}
