package trafficrollup

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newRollupFixture opens a SQLite database with the traffic tables in the
// application zone set to UTC, so the assertions can use plain UTC times.
func newRollupFixture(t *testing.T, limits Limits) (*Service, repository.TrafficRollupRepo, *gorm.DB) {
	t.Helper()
	logtest.Discard(t)
	previous := timeutil.Location().String()
	if err := timeutil.LoadLocation("UTC"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec(`CREATE TABLE traffic_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		subscribe_id INTEGER NOT NULL,
		download INTEGER NOT NULL DEFAULT 0,
		upload INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE traffic_hourly (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hour_start DATETIME NOT NULL,
			user_subscribe_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL DEFAULT 0,
			server_id INTEGER NOT NULL DEFAULT 0,
			upload INTEGER NOT NULL DEFAULT 0,
			download INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL,
			UNIQUE (hour_start, user_subscribe_id, server_id)
		)`,
		`CREATE TABLE traffic_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			day VARCHAR(10) NOT NULL,
			user_subscribe_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL DEFAULT 0,
			server_id INTEGER NOT NULL DEFAULT 0,
			upload INTEGER NOT NULL DEFAULT 0,
			download INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL,
			UNIQUE (day, user_subscribe_id, server_id)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	rollup := repo.NewTrafficRollupRepo(db)
	service := NewService(Deps{Rollup: rollup, Limits: func() Limits { return limits }})
	return service, rollup, db
}

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(time.RFC3339, value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func addTrafficLog(t *testing.T, db *gorm.DB, row traffic.TrafficLog) {
	t.Helper()
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

// storedHourly returns the hourly rows of an hour, as user subscribe, user,
// server and amounts.
func storedHourly(t *testing.T, db *gorm.DB, hour time.Time) [][4]int64 {
	t.Helper()
	var rows []traffic.TrafficHourly
	if err := db.Where("hour_start = ?", hour).Order("user_subscribe_id, server_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make([][4]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, [4]int64{row.UserSubscribeId, row.ServerId, row.Upload, row.Download})
	}
	return out
}

func storedDaily(t *testing.T, db *gorm.DB, day string) [][4]int64 {
	t.Helper()
	var rows []traffic.TrafficDaily
	if err := db.Where("day = ?", day).Order("user_subscribe_id, server_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make([][4]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, [4]int64{row.UserSubscribeId, row.ServerId, row.Upload, row.Download})
	}
	return out
}

func equalRows(t *testing.T, what string, got, want [][4]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

// A run rebuilds each local hour from the raw log and each day from the
// hours, and a second run replaces the same rows instead of adding to them.
func TestRunRebuildsHoursAndDaysIdempotently(t *testing.T) {
	service, _, db := newRollupFixture(t, Limits{HourlyDays: 90})
	now := at(t, "2026-11-26T12:34:00Z")
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 100, Download: 200, Timestamp: at(t, "2026-11-25T23:10:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 1, Download: 2, Timestamp: at(t, "2026-11-25T23:50:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 11, UserId: 7, SubscribeId: 1, Upload: 10, Download: 20, Timestamp: at(t, "2026-11-26T00:20:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 8, SubscribeId: 2, Upload: 5, Download: 5, Timestamp: at(t, "2026-11-26T11:05:00Z")})

	for run := 1; run <= 2; run++ {
		if err := service.RunAt(context.Background(), now); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		equalRows(t, "the first hour", storedHourly(t, db, at(t, "2026-11-25T23:00:00Z")), [][4]int64{{1, 10, 101, 202}})
		equalRows(t, "a second hour", storedHourly(t, db, at(t, "2026-11-26T00:00:00Z")), [][4]int64{{1, 11, 10, 20}})
		equalRows(t, "a third hour", storedHourly(t, db, at(t, "2026-11-26T11:00:00Z")), [][4]int64{{2, 10, 5, 5}})
		equalRows(t, "an empty hour", storedHourly(t, db, at(t, "2026-11-26T05:00:00Z")), nil)
		equalRows(t, "the first day", storedDaily(t, db, "2026-11-25"), [][4]int64{{1, 10, 101, 202}})
		equalRows(t, "the second day", storedDaily(t, db, "2026-11-26"), [][4]int64{{1, 11, 10, 20}, {2, 10, 5, 5}})
	}
}

// The one-hour overlap picks up a report that reached the raw log after its
// hour was rolled, and rows that disappeared from the log go with the
// rebuild.
func TestRunOverwritesAnHourWhoseSourceChanged(t *testing.T) {
	service, _, db := newRollupFixture(t, Limits{HourlyDays: 90})
	now := at(t, "2026-11-26T12:34:00Z")
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 5, Download: 5, Timestamp: at(t, "2026-11-26T11:05:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 8, SubscribeId: 2, Upload: 9, Download: 9, Timestamp: at(t, "2026-11-26T11:40:00Z")})
	if err := service.RunAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	equalRows(t, "the hour", storedHourly(t, db, at(t, "2026-11-26T11:00:00Z")), [][4]int64{{1, 10, 5, 5}, {2, 10, 9, 9}})

	// A late report of the same hour, and one of its earlier rows gone.
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 1, Download: 1, Timestamp: at(t, "2026-11-26T11:59:00Z")})
	if err := db.Where("subscribe_id = ?", 2).Delete(&traffic.TrafficLog{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.RunAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	equalRows(t, "the rebuilt hour", storedHourly(t, db, at(t, "2026-11-26T11:00:00Z")), [][4]int64{{1, 10, 6, 6}})
	equalRows(t, "the rebuilt day", storedDaily(t, db, "2026-11-26"), [][4]int64{{1, 10, 6, 6}})
}

// The hour in progress is rebuilt too, so a user sees today's traffic within
// one tick.
func TestRunIncludesTheHourInProgress(t *testing.T) {
	service, _, db := newRollupFixture(t, Limits{HourlyDays: 90})
	now := at(t, "2026-11-26T12:34:00Z")
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 3, Download: 4, Timestamp: at(t, "2026-11-26T12:20:00Z")})

	if err := service.RunAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	equalRows(t, "the current hour", storedHourly(t, db, at(t, "2026-11-26T12:00:00Z")), [][4]int64{{1, 10, 3, 4}})
	equalRows(t, "today", storedDaily(t, db, "2026-11-26"), [][4]int64{{1, 10, 3, 4}})
}

// Retention deletes the expired hourly rows, and the daily rows only when a
// daily limit is configured: zero keeps them forever.
func TestRunAppliesRetention(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limits    Limits
		keepDaily bool
	}{
		{name: "hourly and daily limits", limits: Limits{HourlyDays: 7, DailyDays: 30}},
		{name: "daily kept forever", limits: Limits{HourlyDays: 7, DailyDays: 0}, keepDaily: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, rollup, db := newRollupFixture(t, tc.limits)
			now := at(t, "2026-11-26T12:34:00Z")
			addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 1, Download: 1, Timestamp: at(t, "2026-11-26T11:05:00Z")})
			if err := service.RunAt(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			// Expired rows of both tables, as a long-running installation
			// would hold them.
			if err := db.Create(&traffic.TrafficHourly{HourStart: at(t, "2026-10-01T00:00:00Z"), UserSubscribeId: 1, UserId: 7, ServerId: 10, Upload: 1, UpdatedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&traffic.TrafficDaily{Day: "2026-09-01", UserSubscribeId: 1, UserId: 7, ServerId: 10, Upload: 1, UpdatedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
			if err := service.RunAt(context.Background(), now); err != nil {
				t.Fatal(err)
			}

			if _, found, err := rollup.OldestHourlyStart(context.Background()); err != nil || !found {
				t.Fatalf("hourly rows after retention: found=%v err=%v", found, err)
			} else if oldest, _, _ := rollup.OldestHourlyStart(context.Background()); oldest.Before(at(t, "2026-11-19T12:00:00Z")) {
				t.Fatalf("an hourly row before the seven-day limit survived: %s", oldest)
			}
			var dailyDays []string
			if err := db.Model(&traffic.TrafficDaily{}).Order("day").Pluck("day", &dailyDays).Error; err != nil {
				t.Fatal(err)
			}
			want := []string{"2026-11-26"}
			if tc.keepDaily {
				want = []string{"2026-09-01", "2026-11-26"}
			}
			if len(dailyDays) != len(want) || dailyDays[0] != want[0] || dailyDays[len(dailyDays)-1] != want[len(want)-1] {
				t.Fatalf("daily rows after retention = %v, want %v", dailyDays, want)
			}
		})
	}
}

// The history reads zero-fill their buckets from the stored rollup, and the
// server reads sum per server largest first.
func TestHistoryAndServersReadTheRollup(t *testing.T) {
	service, _, db := newRollupFixture(t, Limits{HourlyDays: 90})
	now := at(t, "2026-11-26T12:34:00Z")
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 10, Download: 20, Timestamp: at(t, "2026-11-26T10:10:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 11, UserId: 7, SubscribeId: 1, Upload: 1, Download: 1, Timestamp: at(t, "2026-11-26T10:20:00Z")})
	addTrafficLog(t, db, traffic.TrafficLog{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 2, Download: 3, Timestamp: at(t, "2026-11-26T11:30:00Z")})
	if err := service.RunAt(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	start := at(t, "2026-11-26T09:00:00Z")
	end := at(t, "2026-11-26T12:00:00Z")
	points, err := service.UserHistory(context.Background(), 1, 7, GranularityHour, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 {
		t.Fatalf("hour series = %d buckets, want 3", len(points))
	}
	if points[0].Upload != 0 || points[0].Download != 0 {
		t.Fatalf("the empty hour is not zero-filled: %+v", points[0])
	}
	if points[1].Upload != 11 || points[1].Download != 21 || points[2].Upload != 2 || points[2].Download != 3 {
		t.Fatalf("hour series = %+v", points)
	}

	servers, err := service.UserServers(context.Background(), 1, 7, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ServerId != 10 || servers[1].ServerId != 11 {
		t.Fatalf("servers = %+v, want server 10 first", servers)
	}
	if servers[0].Upload != 12 || servers[0].Download != 23 || servers[1].Upload != 1 || servers[1].Download != 1 {
		t.Fatalf("servers = %+v", servers)
	}

	// A range older than the hourly table falls back to the daily rows,
	// which resolve its edge days as whole days.
	oldServers, err := service.UserServers(context.Background(), 1, 7, at(t, "2026-11-20T06:00:00Z"), at(t, "2026-11-26T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(oldServers) != 2 || oldServers[0].ServerId != 10 || oldServers[0].Upload != 12 {
		t.Fatalf("daily fallback servers = %+v", oldServers)
	}
}

// The granularity and the span limits are enforced before any read.
func TestHistoryValidatesItsRange(t *testing.T) {
	service, _, _ := newRollupFixture(t, Limits{HourlyDays: 90})
	ctx := context.Background()
	start := at(t, "2026-11-26T00:00:00Z")

	cases := []struct {
		name        string
		granularity string
		start, end  time.Time
	}{
		{name: "unknown granularity", granularity: "week", start: start, end: start.Add(time.Hour)},
		{name: "reversed range", granularity: GranularityHour, start: start.Add(time.Hour), end: start},
		{name: "minute over a day", granularity: GranularityMinute, start: start, end: start.Add(25 * time.Hour)},
		{name: "hour over a month", granularity: GranularityHour, start: start, end: start.Add(32 * 24 * time.Hour)},
		{name: "day over a year", granularity: GranularityDay, start: start, end: start.Add(367 * 24 * time.Hour)},
		{name: "month over ten years", granularity: GranularityMonth, start: start, end: start.AddDate(0, 121, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UserHistory(ctx, 1, 7, tc.granularity, tc.start, tc.end); err == nil {
				t.Fatal("the range was accepted")
			}
		})
	}
	if _, err := service.UserServers(ctx, 1, 7, start, start.Add(367*24*time.Hour)); err == nil {
		t.Fatal("a server range over a year was accepted")
	}

	// A minute series zero-fills its minutes from the raw log.
	if _, err := service.UserHistory(ctx, 1, 7, GranularityMinute, start, start.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
}
