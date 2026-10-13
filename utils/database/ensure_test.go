package database

import (
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestSplitAdminDSNRemovesDatabase(t *testing.T) {
	admin, name, err := splitAdminDSN("user:secret@tcp(127.0.0.1:3306)/bak?charset=utf8mb4&parseTime=True&loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	if name != "bak" {
		t.Fatalf("name = %q", name)
	}
	cfg, err := mysqldriver.ParseDSN(admin)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBName != "" || cfg.User != "user" || cfg.Passwd != "secret" || cfg.Addr != "127.0.0.1:3306" {
		t.Fatalf("%+v", cfg)
	}
	if strings.Contains(admin, "/bak") {
		t.Fatalf("admin dsn still selects database: %s", admin)
	}
}

func TestSplitAdminDSNWithoutDatabase(t *testing.T) {
	_, name, err := splitAdminDSN("user:@tcp(127.0.0.1:3306)/?charset=utf8mb4")
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("name = %q", name)
	}
}

func TestQuoteIdentEscapesBackticks(t *testing.T) {
	if got := quoteIdent("ba`k"); got != "`ba``k`" {
		t.Fatalf("got %s", got)
	}
}
