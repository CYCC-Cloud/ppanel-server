// Package serverload grades how loaded each server is for the public node
// list: the admin list's report status and a coarse level from the online
// users, the CPU and the memory a node reports. Users see the level, never
// the figures.
package serverload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

// Load levels, from least to most loaded.
const (
	LevelIdle    = "idle"
	LevelNormal  = "normal"
	LevelBusy    = "busy"
	LevelCrowded = "crowded"
)

const (
	// cacheTTL keeps a server's graded load for an hour; a node list read
	// refreshes it as soon as it is due.
	cacheTTL = time.Hour
	// recomputeAfter bounds how often the figures behind a level are read.
	recomputeAfter = 2 * time.Minute
	// hysteresis is how far below a level's lower bound the score must fall
	// before the level follows it down.
	hysteresis = 0.05
)

// StatusReader reads what a server last reported.
type StatusReader interface {
	StatusCache(ctx context.Context, serverId int64) (node.Status, error)
}

// OnlineReader reads the subscriptions a server listener reports online.
type OnlineReader interface {
	OnlineUserSubscribe(ctx context.Context, serverId int64, listenerKey string) (node.OnlineUserSubscribe, error)
}

// Deps declares the subdomain's dependencies.
type Deps struct {
	Redis  *redis.Client
	Status StatusReader
	Online OnlineReader
	// DefaultCapacity returns the node settings' fallback capacity in users;
	// administrators change it at runtime.
	DefaultCapacity func() int64
}

// Service grades and caches the load of servers.
type Service struct {
	deps Deps
}

// NewService returns the grading service over deps.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// cachedLoad is the stored form of a graded load: the level kept for the
// hysteresis check and when it was computed.
type cachedLoad struct {
	Status     string `json:"status"`
	Load       string `json:"load,omitempty"`
	ComputedAt int64  `json:"computed_at"`
}

// ServerLoads returns the graded load of each server, by server id. A
// server's entry is reused for two minutes; a server that never reported is
// offline and has no level.
func (s *Service) ServerLoads(ctx context.Context, servers []*node.Server) (map[int64]node.ServerLoad, error) {
	if s == nil || s.deps.Redis == nil {
		return nil, errors.New("server load is not initialized")
	}
	loads := make(map[int64]node.ServerLoad, len(servers))
	now := timeutil.Now()
	// Read the runtime setting once: every server of the batch falls back to
	// the same default.
	defaultCapacity := s.defaultCapacity()
	for _, server := range servers {
		if server == nil || server.Id <= 0 {
			continue
		}
		if _, done := loads[server.Id]; done {
			continue
		}
		load, err := s.serverLoad(ctx, server, now, defaultCapacity)
		if err != nil {
			return nil, err
		}
		loads[server.Id] = load
	}
	return loads, nil
}

func (s *Service) serverLoad(ctx context.Context, server *node.Server, now time.Time, defaultCapacity int64) (node.ServerLoad, error) {
	key := fmt.Sprintf(node.ServerLoadCacheKey, server.Id)
	previous, cached, err := s.readCache(ctx, key)
	if err != nil {
		return node.ServerLoad{}, err
	}
	if cached && now.Sub(time.UnixMilli(previous.ComputedAt)) < recomputeAfter {
		return node.ServerLoad{Status: previous.Status, Load: previous.Load}, nil
	}

	load := node.ServerLoad{Status: node.ReportStatus(server.LastReportedAt)}
	if load.Status != node.ServerStatusOffline {
		protocols, err := server.UnmarshalProtocols()
		if err != nil {
			return node.ServerLoad{}, fmt.Errorf("server %d protocols: %w", server.Id, err)
		}
		online, err := s.onlineUsers(ctx, server.Id, protocols)
		if err != nil {
			return node.ServerLoad{}, err
		}
		status, err := s.deps.Status.StatusCache(ctx, server.Id)
		if err != nil {
			return node.ServerLoad{}, err
		}
		load.Load = graded(loadScore(online, capacity(server, defaultCapacity), status), previous.Load)
	}

	entry := cachedLoad{Status: load.Status, Load: load.Load, ComputedAt: now.UnixMilli()}
	if err := s.writeCache(ctx, key, entry); err != nil {
		// A cache write that fails does not make the fresh value wrong; the
		// next read recomputes it.
		logger.WithContext(ctx).Errorw("[ServerLoad] the load cache could not be stored",
			logger.Field("error", err.Error()), logger.Field("server_id", server.Id))
	}
	return load, nil
}

// onlineUsers counts the distinct subscriptions online across all of the
// server's listeners.
func (s *Service) onlineUsers(ctx context.Context, serverID int64, protocols []node.Protocol) (int64, error) {
	ids := make(map[int64]struct{})
	for _, protocol := range protocols {
		data, err := s.deps.Online.OnlineUserSubscribe(ctx, serverID, protocol.ListenerKey)
		if err != nil {
			return 0, err
		}
		for id := range data {
			ids[id] = struct{}{}
		}
	}
	return int64(len(ids)), nil
}

// capacity is the server's own capacity, or the node settings' default when
// it has none.
func capacity(server *node.Server, defaultCapacity int64) int64 {
	if server.Capacity > 0 {
		return server.Capacity
	}
	return defaultCapacity
}

func (s *Service) defaultCapacity() int64 {
	if s.deps.DefaultCapacity == nil {
		return 0
	}
	return s.deps.DefaultCapacity()
}

func (s *Service) readCache(ctx context.Context, key string) (cachedLoad, bool, error) {
	data, err := s.deps.Redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return cachedLoad{}, false, nil
	}
	if err != nil {
		return cachedLoad{}, false, err
	}
	var entry cachedLoad
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		// A corrupt entry is recomputed rather than reported: a cache value
		// must not fail a user's node list.
		logger.WithContext(ctx).Errorw("[ServerLoad] discarding a corrupt load cache entry",
			logger.Field("error", err.Error()), logger.Field("key", key))
		return cachedLoad{}, false, nil
	}
	return entry, true, nil
}

func (s *Service) writeCache(ctx context.Context, key string, entry cachedLoad) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.deps.Redis.Set(ctx, key, data, cacheTTL).Err()
}

// loadScore is the worst of the three figures the level is graded on: the
// share of the capacity in use, and the CPU and memory percentages. Users are
// left out when no capacity is configured.
func loadScore(onlineUsers, capacity int64, status node.Status) float64 {
	score := math.Max(status.Cpu, status.Mem) / 100
	if capacity > 0 {
		score = math.Max(score, float64(onlineUsers)/float64(capacity))
	}
	return score
}

// graded returns the level a score falls in. A level rises at once but falls
// only once the score is more than hysteresis below the previous level's
// lower bound, so a figure hovering at a threshold does not make the level
// flap; falling far enough drops several levels at once.
func graded(score float64, previous string) string {
	level := grade(score)
	previousRank := rank(previous)
	if previousRank < 0 || rank(level) >= previousRank || score < lowerBound(previous)-hysteresis {
		return level
	}
	return previous
}

func grade(score float64) string {
	switch {
	case score >= 0.85:
		return LevelCrowded
	case score >= 0.60:
		return LevelBusy
	case score >= 0.30:
		return LevelNormal
	default:
		return LevelIdle
	}
}

func rank(level string) int {
	switch level {
	case LevelIdle:
		return 0
	case LevelNormal:
		return 1
	case LevelBusy:
		return 2
	case LevelCrowded:
		return 3
	default:
		return -1
	}
}

func lowerBound(level string) float64 {
	switch level {
	case LevelNormal:
		return 0.30
	case LevelBusy:
		return 0.60
	case LevelCrowded:
		return 0.85
	default:
		return 0
	}
}
