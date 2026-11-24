// Package configevent broadcasts server configuration invalidations to the
// node control plane. The gateway that fans these events out to node agents
// subscribes to a Redis pub/sub channel directly, so this deliberately does
// not travel through the durable domain event bus: the event carries no state
// a late subscriber could use, and a node that misses one still converges
// through its configuration poll.
package configevent

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"uuid"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/redis/go-redis/v9"
)

// Channel is the Redis pub/sub channel the node control gateway subscribes to.
// Subscribers must apply the same Redis key prefix as the publishing client.
const Channel = "ppanel:events:config"

// Publisher announces that a server's node-facing configuration changed.
type Publisher interface {
	// ConfigChanged is best effort by contract: it is called after the
	// mutation is committed and its caches are cleared, so it must never fail
	// the mutation. changedProtocols may be empty when the change is not
	// protocol-scoped.
	ConfigChanged(ctx context.Context, serverID int64, changedProtocols []string)
}

type changedEvent struct {
	EventID          string   `json:"event_id"`
	ServerID         int64    `json:"server_id"`
	OccurredAtUnixMs int64    `json:"occurred_at_unix_ms"`
	ChangedProtocols []string `json:"changed_protocols,omitempty"`
}

type redisPublisher struct {
	client redis.Cmdable
}

// NewRedisPublisher publishes onto Channel. A nil client yields a publisher
// that does nothing, so a composition without Redis stays usable.
func NewRedisPublisher(client redis.Cmdable) Publisher {
	if client == nil {
		return NopPublisher{}
	}
	return &redisPublisher{client: client}
}

func (p *redisPublisher) ConfigChanged(ctx context.Context, serverID int64, changedProtocols []string) {
	eventID := uuid.NewV7().String()
	payload, err := json.Marshal(changedEvent{
		EventID:          eventID,
		ServerID:         serverID,
		OccurredAtUnixMs: time.Now().UnixMilli(),
		ChangedProtocols: normalizeProtocols(changedProtocols),
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[ConfigEvent] marshal failed",
			logger.Field("server_id", serverID),
			logger.Field("event_id", eventID),
			logger.Field("error", err.Error()),
		)
		return
	}
	if err := p.client.Publish(ctx, Channel, payload).Err(); err != nil {
		logger.WithContext(ctx).Errorw("[ConfigEvent] publish failed",
			logger.Field("server_id", serverID),
			logger.Field("event_id", eventID),
			logger.Field("error", err.Error()),
		)
	}
}

// NopPublisher discards events. It keeps tests and Redis-less compositions
// from having to special-case the publisher.
type NopPublisher struct{}

func (NopPublisher) ConfigChanged(context.Context, int64, []string) {}

// normalizeProtocols sorts and deduplicates the protocol types so subscribers
// receive one canonical shape regardless of how the admin request was ordered.
func normalizeProtocols(protocols []string) []string {
	if len(protocols) == 0 {
		return nil
	}
	unique := make(map[string]struct{}, len(protocols))
	for _, protocol := range protocols {
		if protocol == "" {
			continue
		}
		unique[protocol] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}
	result := make([]string, 0, len(unique))
	for protocol := range unique {
		result = append(result, protocol)
	}
	sort.Strings(result)
	return result
}
