package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"BaiduYunPanBak/global"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// TestMySQLAutoMigrateTwice 复现启动时的 Error 1091：
// users.username 上已有唯一索引时，再次 AutoMigrate 不能去删不存在的外键 uni_users_username。
func TestMySQLAutoMigrateTwice(t *testing.T) {
	db := openMySQLMigrateDB(t)

	prev := global.MysqlPrefix
	global.MysqlPrefix = ""
	t.Cleanup(func() { global.MysqlPrefix = prev })

	// 与现有 yy.users 相同：username 的唯一索引名叫 idx_users_username，不是 uni_users_username。
	if err := db.Exec(`CREATE TABLE users (
		id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
		email varchar(255) NOT NULL,
		password_hash varchar(255) NOT NULL,
		nickname varchar(100) DEFAULT NULL,
		avatar_url varchar(500) DEFAULT NULL,
		feishu_id varchar(100) DEFAULT NULL,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		deleted_at datetime(3) DEFAULT NULL,
		username varchar(32) NOT NULL,
		PRIMARY KEY (id),
		UNIQUE KEY idx_users_email (email),
		UNIQUE KEY idx_users_username (username),
		KEY idx_users_feishu_id (feishu_id),
		KEY idx_users_deleted_at (deleted_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("first AutoMigrate: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second AutoMigrate: %v", err)
	}

	var count int
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users'
		AND INDEX_NAME = 'idx_users_username' AND NON_UNIQUE = 0 AND COLUMN_NAME = 'username'`).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("idx_users_username count = %d", count)
	}
}

func openMySQLMigrateDB(t *testing.T) *gorm.DB {
	t.Helper()
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
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS baidubak_migrate_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "DROP TABLE IF EXISTS baidubak_migrate_test.users, baidubak_migrate_test.cloud_accounts, baidubak_migrate_test.backup_tasks, baidubak_migrate_test.backup_runs, baidubak_migrate_test.backup_file_results"); err != nil {
		t.Fatal(err)
	}

	dsn := os.Getenv("BAIDUBAK_TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = "root:@tcp(127.0.0.1:3306)/baidubak_migrate_test?charset=utf8mb4&parseTime=True&loc=Local"
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
