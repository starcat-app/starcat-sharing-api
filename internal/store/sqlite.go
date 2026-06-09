package store

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dong4j/starcat-sharing-api/internal/model"
)

// SQLiteStore 基于 SQLite 的分享数据持久化实现。
//
// R-01 v1.2: 替代旧的 MemoryStore（内存 + JSON 文件）。
// 连接策略与 trending / weekly 一致: WAL + busy_timeout=5000 + MaxOpenConns(1)。
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 打开 SQLite 数据库并执行 migration。
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	log.Printf("[store] sqlite opened at %s", dsn)
	return &SQLiteStore{db: db}, nil
}

// Upsert 按 id 幂等创建或更新分享数据。
func (s *SQLiteStore) Upsert(data model.ShareData) error {
	repoJSON, err := json.Marshal(data.Request.Repo)
	if err != nil {
		return err
	}
	aiSummaryJSON, err := json.Marshal(data.Request.AISummary)
	if err != nil {
		return err
	}

	var expiresAt interface{}
	if !data.ExpiresAt.IsZero() {
		expiresAt = data.ExpiresAt.Format(time.RFC3339)
	}

	_, err = s.db.Exec(`
		INSERT INTO shares (id, repo_json, ai_summary_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repo_json = excluded.repo_json,
			ai_summary_json = excluded.ai_summary_json,
			expires_at = excluded.expires_at
	`, data.ID, string(repoJSON), string(aiSummaryJSON), data.CreatedAt.Format(time.RFC3339), expiresAt)
	return err
}

// Get 按 id 查询分享数据, 同时递增访问计数。
func (s *SQLiteStore) Get(id string) (*model.ShareData, error) {
	var (
		repoJSON, aiSummaryJSON, createdAtStr string
		expiresAtStr                          sql.NullString
	)

	err := s.db.QueryRow(
		`SELECT repo_json, ai_summary_json, created_at, expires_at FROM shares WHERE id = ?`, id,
	).Scan(&repoJSON, &aiSummaryJSON, &createdAtStr, &expiresAtStr)

	if err != nil {
		return nil, err
	}

	// 反序列化业务数据
	var repo model.ShareRepoDTO
	if err := json.Unmarshal([]byte(repoJSON), &repo); err != nil {
		return nil, err
	}
	var aiSummary model.ShareAISummaryDTO
	if err := json.Unmarshal([]byte(aiSummaryJSON), &aiSummary); err != nil {
		return nil, err
	}

	createdAt, _ := time.Parse(time.RFC3339, createdAtStr)
	var expiresAt time.Time
	if expiresAtStr.Valid {
		expiresAt, _ = time.Parse(time.RFC3339, expiresAtStr.String)
	}

	// 异步更新访问计数（不阻塞 HTML 渲染）
	go func() {
		s.db.Exec(
			"UPDATE shares SET visit_count = visit_count + 1, last_visited_at = ? WHERE id = ?",
			time.Now().Format(time.RFC3339), id,
		)
	}()

	return &model.ShareData{
		ID: id,
		Request: model.ShareRepoRequest{
			Repo:      repo,
			AISummary: aiSummary,
		},
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
	}, nil
}

// Close 关闭数据库连接。
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
