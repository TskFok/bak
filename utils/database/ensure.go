package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func splitAdminDSN(dsn string) (adminDSN, dbName string, err error) {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return "", "", fmt.Errorf("解析数据库连接失败")
	}
	dbName = cfg.DBName
	cfg.DBName = ""
	return cfg.FormatDSN(), dbName, nil
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// EnsureDatabase 在连到业务库之前创建 DSN 里的数据库。已存在时不做修改。
func EnsureDatabase(dsn string) error {
	adminDSN, name, err := splitAdminDSN(dsn)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	db, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return fmt.Errorf("连接 MySQL 失败")
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	query := "CREATE DATABASE IF NOT EXISTS " + quoteIdent(name) + " CHARACTER SET utf8mb4"
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("创建数据库失败: %w", err)
	}
	return nil
}
