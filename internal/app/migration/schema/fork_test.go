package schema

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
)

func TestWithMigrationsTableReplacesUpstreamTable(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"mysql://u:p@tcp(db:3306)/ppanel", "mysql://u:p@tcp(db:3306)/ppanel?x-migrations-table=fork"},
		{"mysql://u:p@tcp(db:3306)/ppanel?multiStatements=true&loc=Asia%2FShanghai",
			"mysql://u:p@tcp(db:3306)/ppanel?multiStatements=true&loc=Asia%2FShanghai&x-migrations-table=fork"},
		{"pgx5://u:p@db/ppanel?x-migrations-table=custom&sslmode=disable",
			"pgx5://u:p@db/ppanel?sslmode=disable&x-migrations-table=fork"},
	} {
		if got := withMigrationsTable(tc.in, "fork"); got != tc.want {
			t.Errorf("withMigrationsTable(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestForkDialectMigrationVersionsStayAligned(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		mysql := forkMigrationVersions(t, "mysql", direction)
		postgres := forkMigrationVersions(t, "postgres", direction)
		if !reflect.DeepEqual(mysql, postgres) {
			t.Fatalf("%s fork migration versions differ:\nmysql:    %v\npostgres: %v", direction, mysql, postgres)
		}
	}
}

func forkMigrationVersions(t *testing.T, dialect, direction string) []string {
	t.Helper()
	entries, err := forkSQLFiles.ReadDir("cycc/" + dialect)
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), "."+direction+".sql"); ok {
			version, _, _ := strings.Cut(name, "_")
			versions = append(versions, version)
		}
	}
	sort.Strings(versions)
	return versions
}

// The database tests need a login that may create databases. Each case runs in
// its own random database, dropped afterwards.
func TestForkMigrations(t *testing.T) {
	for _, dialect := range []struct{ driver, env string }{
		{orm.DriverMySQL, "PPANEL_TEST_MYSQL_DSN"},
		{orm.DriverPostgres, "PPANEL_TEST_POSTGRES_DSN"},
	} {
		t.Run(dialect.driver, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dsn == "" {
				t.Skip("set " + dialect.env + " to run fork migrations against a real database")
			}
			logtest.Discard(t)

			t.Run("fresh database runs both lines once", func(t *testing.T) {
				conn, db := forkTestDatabase(t, dialect.driver, dsn)
				if err := Up(conn.Driver(), conn.MigrationDsn()); err != nil {
					t.Fatal(err)
				}
				assertForkSchema(t, db, dialect.driver)
				if err := Up(conn.Driver(), conn.MigrationDsn()); !errors.Is(err, NoChange) {
					t.Fatalf("second Up = %v, want NoChange", err)
				}
			})

			t.Run("retired fork checkpoint moves to the fork table", func(t *testing.T) {
				conn, db := forkTestDatabase(t, dialect.driver, dsn)
				simulateRetiredFork(t, conn, db, dialect.driver, false)
				if err := Up(conn.Driver(), conn.MigrationDsn()); err != nil {
					t.Fatal(err)
				}
				assertForkSchema(t, db, dialect.driver)
				if db.Migrator().HasTable("traffic_report_receipt") {
					t.Fatal("retired traffic_report_receipt survived the fork migrations")
				}
				if !db.Migrator().HasTable("node_traffic_report") {
					t.Fatal("upstream migrations above the retired base did not run")
				}
			})

			t.Run("dirty retired checkpoint is refused", func(t *testing.T) {
				conn, db := forkTestDatabase(t, dialect.driver, dsn)
				simulateRetiredFork(t, conn, db, dialect.driver, true)
				err := Up(conn.Driver(), conn.MigrationDsn())
				if err == nil || !strings.Contains(err.Error(), "dirty") {
					t.Fatalf("Up = %v, want a dirty checkpoint error", err)
				}
			})
		})
	}
}

func forkTestDatabase(t *testing.T, driver, dsn string) (orm.Mysql, *gorm.DB) {
	t.Helper()
	params := orm.ParseDSN(dsn)
	if params == nil {
		t.Fatal("invalid test database DSN")
	}
	params.Driver = orm.NormalizeDriver(driver)
	conn := orm.Mysql{Config: *params}
	admin := openForkTestDB(t, conn)
	var random [8]byte
	_, _ = rand.Read(random[:])
	name := "ppanel_fork_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatal(err)
	}
	// Cleanups run last-in first-out: register the drop before opening db so
	// db's pool is closed first.
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error(err)
		}
	})
	conn.Config.Dbname = name
	db := openForkTestDB(t, conn)
	return conn, db
}

func openForkTestDB(t *testing.T, conn orm.Mysql) *gorm.DB {
	t.Helper()
	db, err := orm.ConnectDatabase(conn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

// simulateRetiredFork builds what a retired fork release left behind:
// upstream through 03023, the listener column and receipt table, and 4002
// recorded in upstream's table.
func simulateRetiredFork(t *testing.T, conn orm.Mysql, db *gorm.DB, driver string, dirty bool) {
	t.Helper()
	client, err := Migrate(conn.Driver(), conn.MigrationDsn())
	if err != nil {
		t.Fatal(err)
	}
	migrateErr := client.Migrate(retiredForkUpstreamVersion)
	forceErr := client.Force(4002)
	sourceErr, databaseErr := client.Close()
	if err := errors.Join(migrateErr, forceErr, sourceErr, databaseErr); err != nil {
		t.Fatal(err)
	}
	q := func(name string) string {
		if driver == orm.DriverMySQL {
			return "`" + name + "`"
		}
		return `"` + name + `"`
	}
	for _, statement := range []string{
		"ALTER TABLE " + q("nodes") + " ADD COLUMN " + q("listener_key") + " varchar(100) NOT NULL DEFAULT ''",
		"CREATE TABLE " + q("traffic_report_receipt") + " (" + q("report_id") + " varchar(128) NOT NULL)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if dirty {
		if err := db.Exec("UPDATE schema_migrations SET dirty = true").Error; err != nil {
			t.Fatal(err)
		}
	}
}

func assertForkSchema(t *testing.T, db *gorm.DB, driver string) {
	t.Helper()
	if !db.Migrator().HasColumn("nodes", "listener_key") {
		t.Fatal("nodes.listener_key is missing")
	}
	for _, column := range []string{"hour_start", "user_subscribe_id", "user_id", "server_id", "upload", "download"} {
		if !db.Migrator().HasColumn("traffic_hourly", column) {
			t.Fatalf("traffic_hourly.%s is missing", column)
		}
	}
	for _, column := range []string{"day", "user_subscribe_id", "user_id", "server_id", "upload", "download"} {
		if !db.Migrator().HasColumn("traffic_daily", column) {
			t.Fatalf("traffic_daily.%s is missing", column)
		}
	}
	assertMigrationVersion(t, db, "schema_migrations", latestVersion(t, migrationNames(t, "database/"+driver, "up")))
	assertMigrationVersion(t, db, forkMigrationsTable, latestVersion(t, forkMigrationVersions(t, driver, "up")))
}

func latestVersion(t *testing.T, versions []string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(versions[len(versions)-1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func assertMigrationVersion(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	var row struct {
		Version int64
		Dirty   bool
	}
	if err := db.Raw("SELECT version, dirty FROM " + table).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Version != want || row.Dirty {
		t.Fatalf("%s = (%d, dirty=%v), want (%d, clean)", table, row.Version, row.Dirty, want)
	}
}
