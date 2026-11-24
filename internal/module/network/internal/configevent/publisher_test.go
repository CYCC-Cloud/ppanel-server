package configevent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestConfigChangedPublishesNormalizedEvent(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	subscription := client.Subscribe(ctx, Channel)
	t.Cleanup(func() { _ = subscription.Close() })
	if _, err := subscription.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	before := time.Now().UnixMilli()
	NewRedisPublisher(client).ConfigChanged(ctx, 7, []string{"vmess", "trojan", "vmess", ""})

	select {
	case message := <-subscription.Channel():
		var event changedEvent
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if event.ServerID != 7 {
			t.Fatalf("server_id = %d, want 7", event.ServerID)
		}
		if event.EventID == "" {
			t.Fatal("event_id is empty; subscribers cannot deduplicate")
		}
		if event.OccurredAtUnixMs < before {
			t.Fatalf("occurred_at_unix_ms = %d, want >= %d", event.OccurredAtUnixMs, before)
		}
		// Sorted and deduplicated, with the empty entry dropped, so
		// subscribers see one canonical shape.
		want := []string{"trojan", "vmess"}
		if len(event.ChangedProtocols) != len(want) {
			t.Fatalf("changed_protocols = %v, want %v", event.ChangedProtocols, want)
		}
		for i := range want {
			if event.ChangedProtocols[i] != want[i] {
				t.Fatalf("changed_protocols = %v, want %v", event.ChangedProtocols, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no config event was published")
	}
}

func TestConfigChangedOmitsProtocolsForNonProtocolChanges(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	subscription := client.Subscribe(ctx, Channel)
	t.Cleanup(func() { _ = subscription.Close() })
	if _, err := subscription.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	NewRedisPublisher(client).ConfigChanged(ctx, 9, nil)

	select {
	case message := <-subscription.Channel():
		var raw map[string]any
		if err := json.Unmarshal([]byte(message.Payload), &raw); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if _, ok := raw["changed_protocols"]; ok {
			t.Fatalf("changed_protocols is present for a non-protocol change: %s", message.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no config event was published")
	}
}

// The mutation is already committed when the event is published, so a broker
// failure must be swallowed instead of failing the admin request.
func TestConfigChangedSwallowsPublishFailures(t *testing.T) {
	NewRedisPublisher(failingCmdable{}).ConfigChanged(context.Background(), 7, []string{"vmess"})
}

func TestNewRedisPublisherWithoutClientIsANoOp(t *testing.T) {
	if _, ok := NewRedisPublisher(nil).(NopPublisher); !ok {
		t.Fatal("NewRedisPublisher(nil) must yield a publisher that does nothing")
	}
	NewRedisPublisher(nil).ConfigChanged(context.Background(), 7, nil)
}

type failingCmdable struct {
	redis.Cmdable
}

func (failingCmdable) Publish(ctx context.Context, channel string, message any) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "publish", channel, message)
	cmd.SetErr(errors.New("broker is unreachable"))
	return cmd
}
