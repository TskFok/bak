package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
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

func TestMySQLAutoMigrateWidensUploadSize(t *testing.T) {
	db := openMySQLMigrateDB(t)
	prev := global.MysqlPrefix
	global.MysqlPrefix = ""
	t.Cleanup(func() { global.MysqlPrefix = prev })

	if err := db.Exec(`CREATE TABLE upload_history (
		id int unsigned NOT NULL AUTO_INCREMENT,
		path varchar(255) NOT NULL,
		size int NOT NULL,
		fs_id varchar(255) NOT NULL,
		md5 varchar(255) NOT NULL,
		errno int NOT NULL,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		PRIMARY KEY (id)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	prevLog := uploadSizeLogf
	uploadSizeLogf = func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { uploadSizeLogf = prevLog })

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "int -> bigint") {
		t.Fatalf("migration log = %q", joined)
	}
	var dataType string
	if err := db.Raw(`SELECT DATA_TYPE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'upload_history' AND COLUMN_NAME = 'size'`).Scan(&dataType).Error; err != nil {
		t.Fatal(err)
	}
	if dataType != "bigint" {
		t.Fatalf("size type = %s", dataType)
	}
	huge := int64(1 << 32)
	if err := db.Exec(`INSERT INTO upload_history (path, size, fs_id, md5, errno, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NOW(), NOW())`, "/big", huge, "fs", "md", 0).Error; err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := db.Raw(`SELECT size FROM upload_history WHERE path = ?`, "/big").Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != huge {
		t.Fatalf("size = %d", got)
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
	if _, err := admin.ExecContext(ctx, "DROP TABLE IF EXISTS baidubak_migrate_test.users, baidubak_migrate_test.cloud_accounts, baidubak_migrate_test.backup_tasks, baidubak_migrate_test.backup_runs, baidubak_migrate_test.backup_file_results, baidubak_migrate_test.token, baidubak_migrate_test.upload_history"); err != nil {
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
