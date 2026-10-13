package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"bak/global"
)

// TestInitCreatesMissingTablesBeforeDependencies 复现容器启动：
// JWT 未设置或 Redis 连不上时，进程会退出，但缺的库和表必须已经建好。
func TestInitCreatesMissingTablesBeforeDependencies(t *testing.T) {
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

	const name = "baidubak_boot_test"
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
	})

	prevDB, prevRedis := global.DataBase, global.RedisClient
	prevDSN, prevJWT := global.MysqlDsn, global.JwtSecret
	prevRedisHost, prevPrefix := global.RedisHost, global.MysqlPrefix
	t.Cleanup(func() {
		global.DataBase, global.RedisClient = prevDB, prevRedis
		global.MysqlDsn, global.JwtSecret = prevDSN, prevJWT
		global.RedisHost, global.MysqlPrefix = prevRedisHost, prevPrefix
	})

	t.Setenv("BAIDUBAK_JWT_SECRET", "")
	t.Setenv("BAIDUBAK_CONFIG", "")
	t.Setenv("BAIDUBAK_MYSQL_DSN", "root:@tcp(127.0.0.1:3306)/"+name+"?charset=utf8mb4&parseTime=True&loc=Local")
	t.Setenv("BAIDUBAK_REDIS_HOST", "127.0.0.1:1")

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		Init()
	}()
	if !panicked {
		t.Fatal("startup continued without jwt and redis")
	}

	for _, table := range []string{
		"users", "cloud_accounts", "backup_tasks", "backup_runs", "backup_file_results", "token", "upload_history",
	} {
		var n int
		if err := admin.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?`, name, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("missing table %s", table)
		}
	}
}
