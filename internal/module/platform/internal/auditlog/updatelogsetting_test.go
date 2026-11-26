package auditlog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// The retention floor: a retention under seven days would let the cleanup
// erase the login, registration and subscription logs before an incident
// noticed days later could be traced.
func TestValidateLogSettingRejectsDestructiveValues(t *testing.T) {
	enabled := true
	for _, tc := range []struct {
		name string
		req  *dto.LogSetting
	}{
		{name: "nil request"},
		{name: "missing auto clear", req: &dto.LogSetting{ClearDays: 7}},
		{name: "zero days", req: &dto.LogSetting{AutoClear: &enabled}},
		{name: "one day", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 1}},
		{name: "six days", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 6}},
		{name: "negative days", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: -7}},
		{name: "unbounded days", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 3651}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateLogSetting(tc.req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	for _, days := range []int64{MinRetentionDays, 30, MaxRetentionDays} {
		if err := validateLogSetting(&dto.LogSetting{AutoClear: &enabled, ClearDays: days}); err != nil {
			t.Fatalf("valid setting of %d days rejected: %v", days, err)
		}
	}
	// The traffic history retentions: hourly never falls under a week,
	// daily is either zero (forever) or at least a month.
	day := func(days int64) *int64 { return &days }
	for _, tc := range []struct {
		name string
		req  *dto.LogSetting
	}{
		{name: "hourly zero", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficHourlyRetentionDays: day(0)}},
		{name: "hourly six days", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficHourlyRetentionDays: day(6)}},
		{name: "hourly unbounded", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficHourlyRetentionDays: day(3651)}},
		{name: "daily one day", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: day(1)}},
		{name: "daily twenty-nine days", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: day(29)}},
		{name: "daily negative", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: day(-1)}},
		{name: "daily unbounded", req: &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: day(3651)}},
	} {
		if err := validateLogSetting(tc.req); err == nil {
			t.Fatalf("%s was accepted", tc.name)
		}
	}
	for _, tc := range []*dto.LogSetting{
		{AutoClear: &enabled, ClearDays: 7, TrafficHourlyRetentionDays: day(7), TrafficDailyRetentionDays: day(0)},
		{AutoClear: &enabled, ClearDays: 7, TrafficHourlyRetentionDays: day(3650), TrafficDailyRetentionDays: day(30)},
		{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: day(3650)},
	} {
		if err := validateLogSetting(tc); err != nil {
			t.Fatalf("valid traffic retention rejected: %v", err)
		}
	}
}

// platformStore is the platform store over one test transaction: the
// settings and the log.
type platformStore struct {
	tx  *gorm.DB
	rds *redis.Client
}

var _ kernel.PlatformStore = platformStore{}

func (s platformStore) System() kernel.SystemRepo {
	return repo.NewSystemRepo(cache.NewConn(s.tx, s.rds))
}
func (s platformStore) Task() kernel.TaskRepo     { return repo.NewTaskRepo(s.tx) }
func (s platformStore) Log() kernel.LogRepo       { return repo.NewLogRepo(s.tx) }
func (s platformStore) Inbox() kernel.InboxRepo   { return repo.NewInboxRepo(s.tx) }
func (s platformStore) Outbox() kernel.OutboxRepo { return repo.NewOutboxRepo(s.tx) }

// storeTx runs platform-scoped transactions on the test database.
type storeTx struct {
	db  *gorm.DB
	rds *redis.Client
}

func (p storeTx) InPlatformTx(ctx context.Context, fn func(kernel.PlatformStore) error) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(platformStore{tx: tx, rds: p.rds})
	})
}

// The retention change is stored with its audit row, filed under the
// administrator and naming the new values, and propagated to the runtime.
func TestUpdateLogSettingRecordsTheChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:log-setting-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&system.System{}, &log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	var propagated []string
	svc := NewService(Deps{
		System: repo.NewSystemRepo(cache.NewConn(db, rds)),
		Store:  storeTx{db: db, rds: rds},
		OnLogSettingChanged: func(retention config.Log) {
			propagated = append(propagated, fmt.Sprintf("%t/%d/%d/%d", retention.AutoClear, retention.ClearDays,
				retention.HourlyTrafficRetentionDays(), retention.DailyTrafficRetentionDays()))
		},
	})
	ctx := requestmeta.WithActor(requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "AdminPanel/1.0")), 9)
	enabled := true
	days := func(value int64) *int64 { return &value }

	if err := svc.UpdateLogSetting(ctx, &dto.LogSetting{AutoClear: &enabled, ClearDays: 14}); err != nil {
		t.Fatal(err)
	}
	// An omitted traffic retention keeps its stored value: the first save
	// stores the defaults, an older admin UI does not reset them.
	got, err := svc.GetLogSetting(ctx)
	if err != nil || got.AutoClear == nil || !*got.AutoClear || got.ClearDays != 14 ||
		got.TrafficHourlyRetentionDays == nil || *got.TrafficHourlyRetentionDays != config.DefaultTrafficHourlyRetentionDays ||
		got.TrafficDailyRetentionDays == nil || *got.TrafficDailyRetentionDays != 0 {
		t.Fatalf("setting = %+v (err %v)", got, err)
	}
	if len(propagated) != 1 || propagated[0] != "true/14/90/0" {
		t.Fatalf("propagated = %v, want the committed setting", propagated)
	}

	if err := svc.UpdateLogSetting(ctx, &dto.LogSetting{
		AutoClear:                  &enabled,
		ClearDays:                  14,
		TrafficHourlyRetentionDays: days(30),
		TrafficDailyRetentionDays:  days(365),
	}); err != nil {
		t.Fatal(err)
	}
	// Omitting them again keeps what was just stored.
	if err := svc.UpdateLogSetting(ctx, &dto.LogSetting{AutoClear: &enabled, ClearDays: 14}); err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetLogSetting(ctx)
	if err != nil || got.TrafficHourlyRetentionDays == nil || *got.TrafficHourlyRetentionDays != 30 ||
		got.TrafficDailyRetentionDays == nil || *got.TrafficDailyRetentionDays != 365 {
		t.Fatalf("setting after omitted fields = %+v (err %v)", got, err)
	}
	if len(propagated) != 3 || propagated[1] != "true/14/30/365" || propagated[2] != "true/14/30/365" {
		t.Fatalf("propagated = %v, want the kept compound retention", propagated)
	}

	var rows []log.SystemLog
	if err := db.Where("type = ?", log.TypeAdminAction).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].ObjectID != 9 {
		t.Fatalf("audit rows = %+v, want three filed under administrator 9", rows)
	}
	var action log.AdminAction
	if err := action.Unmarshal([]byte(rows[0].Content)); err != nil {
		t.Fatal(err)
	}
	if action.Action != "settings.update" || action.Object != "log" || action.ActorID != 9 || action.ClientIP != "203.0.113.9" ||
		action.Detail != "keys: AutoClear, ClearDays, TrafficHourlyRetentionDays, TrafficDailyRetentionDays; auto_clear=true clear_days=14 traffic_hourly_retention_days=90 traffic_daily_retention_days=0" {
		t.Fatalf("action = %+v", action)
	}

	// A refused setting changes and records nothing.
	if err := svc.UpdateLogSetting(ctx, &dto.LogSetting{AutoClear: &enabled, ClearDays: 3}); err == nil {
		t.Fatal("a three-day retention was accepted")
	}
	if err := svc.UpdateLogSetting(ctx, &dto.LogSetting{AutoClear: &enabled, ClearDays: 7, TrafficDailyRetentionDays: days(7)}); err == nil {
		t.Fatal("a seven-day traffic daily retention was accepted")
	}
	var n int64
	if err := db.Model(&log.SystemLog{}).Count(&n).Error; err != nil || n != 3 || len(propagated) != 3 {
		t.Fatalf("after the refusal: %d audit rows, propagated %v", n, propagated)
	}
}
