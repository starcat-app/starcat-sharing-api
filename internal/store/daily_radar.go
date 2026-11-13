// 本文件提供日报快照的事务存取；不复用会刷新指标的 Repo 分享 Store 接口。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

// FindDailyRadar 按不可变三元组查找；查询本身不计 PV。
func (s *SQLiteStore) FindDailyRadar(ctx context.Context, reportID, language, revision string) (*model.DailyRadarShare, error) {
	return s.readDailyRadar(ctx, "report_id = ? AND language = ? AND revision = ?", reportID, language, revision)
}

// GetDailyRadar 供公开 HTML 与图片共同检查存在和撤销状态。
func (s *SQLiteStore) GetDailyRadar(ctx context.Context, id string) (*model.DailyRadarShare, error) {
	return s.readDailyRadar(ctx, "id = ?", id)
}

// readDailyRadar 的 where 只能由上面两个固定查询提供，用户输入全部使用参数绑定。
func (s *SQLiteStore) readDailyRadar(ctx context.Context, where string, arguments ...any) (*model.DailyRadarShare, error) {
	var result model.DailyRadarShare
	var projects, sources, created string
	var revoked sql.NullString
	report := &result.Request.Report
	err := s.db.QueryRowContext(ctx, `SELECT id, report_id, report_date, category_id,
		language, revision, title, subtitle, summary, reading_minutes, markdown,
		projects_json, sources_json, content_hash, hero_hash, created_at, revoked_at
		FROM daily_radar_shares WHERE `+where, arguments...).Scan(
		&result.ID, &report.ID, &report.Date, &report.CategoryID, &report.Language,
		&report.Revision, &report.Title, &report.Subtitle, &report.Summary,
		&report.ReadingMinutes, &report.Markdown, &projects, &sources,
		&result.ContentHash, &result.HeroHash, &created, &revoked)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(projects), &result.Request.Projects); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(sources), &report.Sources); err != nil {
		return nil, err
	}
	if result.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return nil, err
	}
	if revoked.Valid {
		value, err := time.Parse(time.RFC3339Nano, revoked.String)
		if err != nil {
			return nil, err
		}
		result.RevokedAt = &value
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, path, content_hash, bytes, width, height
		FROM daily_radar_share_assets WHERE share_id = ? ORDER BY kind`, result.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var asset model.DailyRadarAsset
		if err = rows.Scan(&asset.Kind, &asset.Path, &asset.ContentHash, &asset.Bytes, &asset.Width, &asset.Height); err != nil {
			return nil, err
		}
		result.Assets = append(result.Assets, asset)
	}
	return &result, rows.Err()
}

// InsertDailyRadar 一次提交快照和两种资产；唯一索引负责并发创建的最终裁决。
func (s *SQLiteStore) InsertDailyRadar(ctx context.Context, share model.DailyRadarShare) error {
	projects, err := json.Marshal(share.Request.Projects)
	if err != nil {
		return err
	}
	sources, err := json.Marshal(share.Request.Report.Sources)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	report := share.Request.Report
	created := share.CreatedAt.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO daily_radar_shares (
		id, report_id, report_date, category_id, language, revision, title, subtitle,
		summary, reading_minutes, markdown, projects_json, sources_json, content_hash,
		hero_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, report.ID, report.Date, report.CategoryID, report.Language, report.Revision,
		report.Title, report.Subtitle, report.Summary, report.ReadingMinutes, report.Markdown,
		string(projects), string(sources), share.ContentHash, share.HeroHash, created)
	if err != nil {
		return err
	}
	for _, asset := range share.Assets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO daily_radar_share_assets
			(share_id, kind, path, content_hash, bytes, width, height, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, share.ID, asset.Kind, asset.Path,
			asset.ContentHash, asset.Bytes, asset.Width, asset.Height, created); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecordDailyRadarVisit 仅在 HTML 成功渲染后计 PV；图片和 HEAD 请求不重复计数。
func (s *SQLiteStore) RecordDailyRadarVisit(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE daily_radar_shares SET
		visit_count = visit_count + 1, last_visited_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}
