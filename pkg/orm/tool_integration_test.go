//go:build integration

package orm

import (
	"os"
	"testing"

	"github.com/perfect-panel/server/internal/model/task"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func testMySQLDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("PPANEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("PPANEL_TEST_MYSQL_DSN is not set")
	}

	return dsn
}

func TestPing(t *testing.T) {
	status := Ping(testMySQLDSN(t))
	if !status {
		t.Fatal("expected mysql ping to succeed")
	}
}

func TestMysql(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: testMySQLDSN(t)}))
	if err != nil {
		t.Fatalf("Failed to connect to MySQL: %v", err)
	}

	err = db.Migrator().AutoMigrate(&task.Task{})
	if err != nil {
		t.Fatalf("Failed to auto migrate: %v", err)
	}

	t.Log("MySQL connection and migration successful")
}
