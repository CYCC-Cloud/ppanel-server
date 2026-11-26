package auditlog

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// cleanupTraffic records the traffic-log prune the cleanup asked for.
type cleanupTraffic struct {
	before time.Time
	calls  int
	err    error
}

var _ TrafficLogPruner = (*cleanupTraffic)(nil)

func (r *cleanupTraffic) PruneTrafficLogs(_ context.Context, before time.Time) (int64, error) {
	r.before = before
	r.calls++
	return 2, r.err
}

// cleanupDelete is one type-set batch delete the cleanup asked for.
type cleanupDelete struct {
	before time.Time
	types  []int
}

// cleanupLogRows records the system-log batch deletes, deleting one row per
// batch.
type cleanupLogRows struct {
	deletes []cleanupDelete
}

func (r *cleanupLogRows) DeleteTypesBeforeBatch(_ context.Context, before time.Time, types []int, _ int) (int64, error) {
	r.deletes = append(r.deletes, cleanupDelete{before: before, types: types})
	return 1, nil
}

// The traffic logs are pruned and the traffic system logs deleted at the
// ClearDays threshold; the audit trail is deleted at its own threshold, which
// is never the same one.
func TestCleanupLogsDeletesEachTypeSetAtItsOwnRetention(t *testing.T) {
	traffic, logs := &cleanupTraffic{}, &cleanupLogRows{}
	if err := cleanupLogs(context.Background(), true, 7, 365, traffic, logs); err != nil {
		t.Fatal(err)
	}
	if traffic.calls != 1 || traffic.before.IsZero() {
		t.Fatalf("traffic prunes = %d, threshold %v", traffic.calls, traffic.before)
	}
	if h, m, s := traffic.before.Clock(); h != 0 || m != 0 || s != 0 {
		t.Fatalf("threshold %v is not the start of a day", traffic.before)
	}
	if len(logs.deletes) != 2 {
		t.Fatalf("system log deletes = %+v, want the traffic and the audit set", logs.deletes)
	}
	if !slices.Equal(logs.deletes[0].types, log.ExpirableTypes()) || !logs.deletes[0].before.Equal(traffic.before) {
		t.Fatalf("first delete = %+v, want the expirable types at the ClearDays threshold", logs.deletes[0])
	}
	if !slices.Equal(logs.deletes[1].types, log.AuditTypes()) || !logs.deletes[1].before.Equal(traffic.before.AddDate(0, 0, -358)) {
		t.Fatalf("second delete = %+v, want the audit types 365 days back", logs.deletes[1])
	}
}

// A zero audit retention keeps the audit trail forever; automatic clearing
// off is the master switch and deletes nothing at all.
func TestCleanupLogsSkipsWhatItsSettingsKeep(t *testing.T) {
	traffic, logs := &cleanupTraffic{}, &cleanupLogRows{}
	if err := cleanupLogs(context.Background(), true, 7, 0, traffic, logs); err != nil {
		t.Fatal(err)
	}
	if len(logs.deletes) != 1 || !slices.Equal(logs.deletes[0].types, log.ExpirableTypes()) {
		t.Fatalf("deletes = %+v, want only the expirable types", logs.deletes)
	}

	traffic, logs = &cleanupTraffic{}, &cleanupLogRows{}
	if err := cleanupLogs(context.Background(), false, 7, 365, traffic, logs); err != nil || traffic.calls != 0 || len(logs.deletes) != 0 {
		t.Fatalf("cleanup with automatic clearing off: err %v, prunes %d, deletes %+v", err, traffic.calls, logs.deletes)
	}
}

func TestCleanupLogsRejectsUnsafeRetention(t *testing.T) {
	for _, days := range []int64{0, -1, maxRetentionDays + 1} {
		traffic, logs := &cleanupTraffic{}, &cleanupLogRows{}
		if err := cleanupLogs(context.Background(), true, days, 365, traffic, logs); err == nil {
			t.Fatalf("retention of %d days was accepted", days)
		}
		if traffic.calls != 0 || len(logs.deletes) != 0 {
			t.Fatalf("an unsafe retention of %d days deleted: %d prunes, %+v", days, traffic.calls, logs.deletes)
		}
	}
	for _, days := range []int64{-1, maxRetentionDays + 1} {
		traffic, logs := &cleanupTraffic{}, &cleanupLogRows{}
		if err := cleanupLogs(context.Background(), true, 7, days, traffic, logs); err == nil {
			t.Fatalf("audit retention of %d days was accepted", days)
		}
		if traffic.calls != 0 || len(logs.deletes) != 0 {
			t.Fatalf("an unsafe audit retention of %d days deleted: %d prunes, %+v", days, traffic.calls, logs.deletes)
		}
	}

	traffic, logs := &cleanupTraffic{err: errors.New("delete failed")}, &cleanupLogRows{}
	if err := cleanupLogs(context.Background(), true, 7, 365, traffic, logs); !errors.Is(err, traffic.err) {
		t.Fatalf("cleanup error = %v, want %v", err, traffic.err)
	}
}
