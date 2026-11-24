package schema

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/perfect-panel/backend/pkg/orm"
)

// The CYCC fork keeps its own schema line beside upstream's. golang-migrate
// records a single version per table and only applies higher numbers, so fork
// migrations numbered inside upstream's table would make every later upstream
// migration unreachable. Fork migrations therefore live in cycc/<dialect>,
// are recorded in forkMigrationsTable, and always run after upstream's.
//
//go:embed cycc/mysql/*.sql cycc/postgres/*.sql
var forkSQLFiles embed.FS

const forkMigrationsTable = "schema_migrations_cycc"

// Retired fork releases recorded 04000-04002 in upstream's table on top of
// upstream 03023 (their last upstream base). retiredForkVersions maps each of
// those checkpoints to the fork-line version that holds the same schema.
const retiredForkUpstreamVersion = 3023

var retiredForkVersions = map[uint]int{4000: 0, 4001: 1, 4002: 1}

// upWithFork applies upstream's migrations, then the fork's. It returns
// NoChange only when neither line had anything to apply.
func upWithFork(driver, dsn string, upstream *migrate.Migrate) error {
	if err := moveRetiredForkCheckpoint(driver, dsn, upstream); err != nil {
		sourceErr, databaseErr := upstream.Close()
		return errors.Join(err, sourceErr, databaseErr)
	}
	upstreamErr := upAndClose(upstream)
	if upstreamErr != nil && !errors.Is(upstreamErr, NoChange) {
		return upstreamErr
	}
	fork, err := migrateFork(driver, dsn)
	if err != nil {
		return err
	}
	forkErr := upAndClose(fork)
	if forkErr != nil && !errors.Is(forkErr, NoChange) {
		return fmt.Errorf("fork migrations: %w", forkErr)
	}
	if upstreamErr == nil || forkErr == nil {
		return nil
	}
	return NoChange
}

// moveRetiredForkCheckpoint rewrites a clean retired fork checkpoint once:
// the fork table takes the fork's part, upstream's table returns to the
// upstream version that database last ran, and upstream's newer migrations
// then apply normally. The fork table is written first, so an interrupted
// move is repeated rather than lost. A dirty checkpoint is never rewritten.
func moveRetiredForkCheckpoint(driver, dsn string, upstream *migrate.Migrate) error {
	version, dirty, err := upstream.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read migration checkpoint: %w", err)
	}
	forkVersion, retired := retiredForkVersions[version]
	if !retired {
		return nil
	}
	if dirty {
		return fmt.Errorf("retired fork migration %d is dirty; restore a backup or rebuild the database", version)
	}
	if forkVersion > 0 {
		fork, err := migrateFork(driver, dsn)
		if err != nil {
			return err
		}
		forceErr := fork.Force(forkVersion)
		sourceErr, databaseErr := fork.Close()
		if err := errors.Join(forceErr, sourceErr, databaseErr); err != nil {
			return fmt.Errorf("record retired fork checkpoint %d: %w", version, err)
		}
	}
	if err := upstream.Force(retiredForkUpstreamVersion); err != nil {
		return fmt.Errorf("return retired fork checkpoint %d to upstream: %w", version, err)
	}
	return nil
}

// migrateFork opens a migration client for the fork's embedded migrations,
// recorded in forkMigrationsTable.
func migrateFork(driver, dsn string) (*migrate.Migrate, error) {
	driver = orm.NormalizeDriver(driver)
	var databaseURL string
	switch driver {
	case orm.DriverMySQL:
		databaseURL = ensureScheme(orm.DriverMySQL, dsn)
	case orm.DriverPostgres:
		databaseURL = postgresMigrationURL(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
	d, err := iofs.New(forkSQLFiles, "cycc/"+driver)
	if err != nil {
		return nil, fmt.Errorf("open embedded %s fork migrations: %w", driver, err)
	}
	client, err := migrate.NewWithSourceInstance("iofs", d, withMigrationsTable(databaseURL, forkMigrationsTable))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open %s fork migration database: %w", driver, err), d.Close())
	}
	return client, nil
}

// withMigrationsTable points a migration URL at table. An x-migrations-table
// the DSN already carries names upstream's table and is replaced, never
// shared: both drivers read the first value of a repeated parameter.
func withMigrationsTable(databaseURL, table string) string {
	base, query, _ := strings.Cut(databaseURL, "?")
	params := []string{}
	for _, param := range strings.Split(query, "&") {
		if param != "" && !strings.HasPrefix(param, "x-migrations-table=") {
			params = append(params, param)
		}
	}
	return base + "?" + strings.Join(append(params, "x-migrations-table="+table), "&")
}
