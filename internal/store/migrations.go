package store

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// createSchema 保留已发布的 Repo 分享建表入口，再追加有账本的增量迁移。
//
// 历史 shares 已有线上数据；不能删库、回写其字段或将新日报数据塞入旧表。
func createSchema(db *sql.DB) error {
	log.Println("[migrate] createSchema: shares table")
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS shares (
			id              TEXT PRIMARY KEY,
			repo_json       TEXT NOT NULL,
			ai_summary_json TEXT NOT NULL,
			created_at      TEXT NOT NULL,
			expires_at      TEXT,
			visit_count     INTEGER NOT NULL DEFAULT 0,
			last_visited_at TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_shares_created_at ON shares(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at) WHERE expires_at IS NOT NULL;
	`)
	if err != nil {
		return err
	}
	return migrateDailyRadar(db)
}

// migrateDailyRadar 将 SQL 与版本记录放在同一事务；失败时阻止服务带半套表启动。
func migrateDailyRadar(db *sql.DB) error {
	const version = "v2__daily_radar_shares.sql"
	script, err := migrationFiles.ReadFile("migrations/" + version)
	if err != nil {
		return err
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(script))
	started := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL, duration_ms INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	var existing string
	err = tx.QueryRow("SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&existing)
	switch {
	case err == nil:
		if existing != checksum {
			return fmt.Errorf("migration checksum mismatch: %s", version)
		}
		return tx.Commit()
	case err != sql.ErrNoRows:
		return err
	}
	if _, err = tx.Exec(string(script)); err != nil {
		return fmt.Errorf("apply %s: %w", version, err)
	}
	elapsed := time.Since(started).Milliseconds()
	if _, err = tx.Exec("INSERT INTO schema_migrations VALUES (?, ?, ?, ?)",
		version, checksum, time.Now().UTC().Format(time.RFC3339Nano), elapsed); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	log.Printf("[migrate] version=%s duration_ms=%d checksum=%s", version, elapsed, checksum)
	return nil
}
