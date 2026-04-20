//go:build integration

package tool

import (
	"os"
	"testing"
)

func testRedisURI(t *testing.T) string {
	t.Helper()

	uri := os.Getenv("PPANEL_TEST_REDIS_URI")
	if uri == "" {
		t.Skip("PPANEL_TEST_REDIS_URI is not set")
	}

	return uri
}

func TestRedisPing(t *testing.T) {
	addr, password, database, err := ParseRedisURI(testRedisURI(t))
	if err != nil {
		t.Fatal(err)
	}

	err = RedisPing(addr, password, database)
	if err != nil {
		t.Fatal(err)
	}
}
