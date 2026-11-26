package trafficrollup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
)

// The rollup is dialect-neutral SQL: run it against the server databases the
// release supports, migrated by the real fork migration. The cases skip
// without the test DSNs, like the migration tests.
func TestRollupRunsOnServerDatabases(t *testing.T) {
	for _, dialect := range []struct{ driver, env string }{
		{orm.DriverMySQL, "PPANEL_TEST_MYSQL_DSN"},
		{orm.DriverPostgres, "PPANEL_TEST_POSTGRES_DSN"},
	} {
		t.Run(dialect.driver, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dsn == "" {
				t.Skip("set " + dialect.env + " to run the rollup against a real database")
			}
			logtest.Discard(t)
			previous := timeutil.Location().String()
			// The application zone and the session zone must match, as they
			// do in production; the DSN's own zone is replaced here.
			if err := timeutil.LoadLocation("UTC"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })

			assertServerRollup(t, rollupTestDatabase(t, dialect.driver, dsn))
		})
	}
}

// assertServerRollup rolls two hours of raw log twice and checks the stored
// aggregates and the read paths.
func assertServerRollup(t *testing.T, db *gorm.DB) {
	t.Helper()
	service := NewService(Deps{Rollup: repo.NewTrafficRollupRepo(db), Limits: func() Limits {
		return Limits{HourlyDays: 90}
	}})
	now := time.Date(2026, 11, 26, 12, 34, 0, 0, time.UTC)
	for _, row := range serverRollupLog() {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for run := 1; run <= 2; run++ {
		if err := service.RunAt(context.Background(), now); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	var hourly []traffic.TrafficHourly
	if err := db.Order("hour_start, user_subscribe_id, server_id").Find(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	wantHourly := [][4]int64{{1, 10, 100, 200}, {1, 11, 1, 2}, {2, 10, 5, 6}}
	if len(hourly) != len(wantHourly) {
		t.Fatalf("hourly rows = %+v, want %d", hourly, len(wantHourly))
	}
	for i, want := range wantHourly {
		got := [4]int64{hourly[i].UserSubscribeId, hourly[i].ServerId, hourly[i].Upload, hourly[i].Download}
		if got != want {
			t.Fatalf("hourly row %d = %v, want %v", i, got, want)
		}
	}

	var daily []traffic.TrafficDaily
	if err := db.Order("day, user_subscribe_id, server_id").Find(&daily).Error; err != nil {
		t.Fatal(err)
	}
	if len(daily) != 3 || daily[0].Day != "2026-11-26" || daily[0].Upload != 100 || daily[1].Upload != 1 || daily[2].Upload != 5 {
		t.Fatalf("daily rows = %+v", daily)
	}

	hours, _, err := service.UserHistory(context.Background(), 1, 7, GranularityHour,
		time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 3 || hours[0].Upload != 0 || hours[1].Upload != 101 || hours[2].Upload != 0 {
		t.Fatalf("hour series = %+v", hours)
	}
	days, _, err := service.UserHistory(context.Background(), 2, 8, GranularityDay,
		time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Upload != 0 || days[1].Upload != 5 {
		t.Fatalf("day series = %+v", days)
	}
	servers, err := service.UserServers(context.Background(), 1, 7,
		time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ServerId != 10 || servers[0].Upload != 100 || servers[1].ServerId != 11 {
		t.Fatalf("servers = %+v", servers)
	}
}

func serverRollupLog() []traffic.TrafficLog {
	return []traffic.TrafficLog{
		{ServerId: 10, UserId: 7, SubscribeId: 1, Upload: 100, Download: 200, Timestamp: time.Date(2026, 11, 26, 10, 15, 0, 0, time.UTC)},
		{ServerId: 11, UserId: 7, SubscribeId: 1, Upload: 1, Download: 2, Timestamp: time.Date(2026, 11, 26, 10, 45, 0, 0, time.UTC)},
		{ServerId: 10, UserId: 8, SubscribeId: 2, Upload: 5, Download: 6, Timestamp: time.Date(2026, 11, 26, 11, 5, 0, 0, time.UTC)},
	}
}

// rollupTestDatabase creates a random database with the traffic tables in
// it, and drops it afterwards.
func rollupTestDatabase(t *testing.T, driver, dsn string) *gorm.DB {
	t.Helper()
	params := orm.ParseDSNIn(dsn, "UTC")
	if params == nil {
		t.Fatal("invalid test database DSN")
	}
	params.Driver = orm.NormalizeDriver(driver)
	conn := orm.Mysql{Config: *params, Location: "UTC"}
	admin, err := orm.ConnectDatabase(conn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	var random [8]byte
	_, _ = rand.Read(random[:])
	name := "ppanel_rollup_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error(err)
		}
	})
	conn.Config.Dbname = name
	db, err := orm.ConnectDatabase(conn)
	if err != nil {
		t.Fatal(err)
	}
	rollupPool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollupPool.Close() })
	// The tables come from the entities the rollup queries. Fork migration
	// 00004 creates the same columns, and the migration test checks them on
	// every dialect.
	if err := db.Migrator().CreateTable(&traffic.TrafficLog{}, &traffic.TrafficHourly{}, &traffic.TrafficDaily{}); err != nil {
		t.Fatal(err)
	}
	return db
}
