package orm

import (
	"testing"
)

func TestParseDSN(t *testing.T) {
	dsn := "ppanel:secret@tcp(localhost:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"
	config := ParseDSN(dsn)
	if config == nil {
		t.Fatal("config is nil")
	}
	t.Log(config)
}
