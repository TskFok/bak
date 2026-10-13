package database

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestEnsureDatabaseCreatesMissingDatabase(t *testing.T) {
	adminDSN := os.Getenv("BAIDUBAK_TEST_MYSQL_ADMIN_DSN")
	if adminDSN == "" {
		adminDSN = "root:@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=True&loc=Local"
	}
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { admin.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}

	const name = "baidubak_ensure_test"
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(name)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name))
	})

	dsn := "root:@tcp(127.0.0.1:3306)/" + name + "?charset=utf8mb4&parseTime=True&loc=Local"
	if err := EnsureDatabase(dsn); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDatabase(dsn); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	var charset string
	if err := admin.QueryRowContext(ctx, `SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?`, name).Scan(&charset); err != nil {
		t.Fatal(err)
	}
	if charset != "utf8mb4" {
		t.Fatalf("charset = %s", charset)
	}
}
