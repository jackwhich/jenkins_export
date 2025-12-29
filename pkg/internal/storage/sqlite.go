package storage

import (
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite" // SQLite driver
)

// NewSQLite creates and initializes a SQLite database connection.
func NewSQLite(path string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// 设置连接池参数（SQLite 推荐单写连接）
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// 设置 PRAGMA 优化
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = MEMORY",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("failed to set PRAGMA %s: %w", pragma, err)
		}
	}

	// 创建表结构
	if err := createTables(db, logger); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// 创建索引
	if err := createIndexes(db, logger); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	logger.Info("SQLite 数据库初始化完成",
		"路径", path,
	)

	return db, nil
}

// createTables creates the required database tables.
func createTables(db *sql.DB, logger *slog.Logger) error {
	// 创建 jobs 表
	jobsTable := `
	CREATE TABLE IF NOT EXISTS jobs (
		job_name        TEXT PRIMARY KEY,
		enabled         INTEGER NOT NULL DEFAULT 1,
		last_seen_build INTEGER NOT NULL DEFAULT 0,
		last_sync_time  INTEGER,
		created_at      INTEGER NOT NULL
	);`

	if _, err := db.Exec(jobsTable); err != nil {
		return fmt.Errorf("failed to create jobs table: %w", err)
	}

	// 创建 job_changes 审计表（可选）
	jobChangesTable := `
	CREATE TABLE IF NOT EXISTS job_changes (
		job_name   TEXT,
		action     TEXT,
		event_time INTEGER
	);`

	if _, err := db.Exec(jobChangesTable); err != nil {
		return fmt.Errorf("failed to create job_changes table: %w", err)
	}

	logger.Debug("数据库表创建完成")
	return nil
}

// createIndexes creates the required database indexes.
func createIndexes(db *sql.DB, logger *slog.Logger) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_jobs_enabled ON jobs(enabled)",
		"CREATE INDEX IF NOT EXISTS idx_jobs_enabled_lastseen ON jobs(enabled, last_seen_build)",
		"CREATE INDEX IF NOT EXISTS idx_jobs_last_sync_time ON jobs(last_sync_time)",
		"CREATE INDEX IF NOT EXISTS idx_job_changes_time ON job_changes(event_time)",
	}

	for _, index := range indexes {
		if _, err := db.Exec(index); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	logger.Debug("数据库索引创建完成")
	return nil
}

