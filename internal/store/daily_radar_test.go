// 本文件从已有 shares 数据库升级，防止只在空库通过而破坏正式用户数据。
package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

func TestDailyRadarMigrationPreservesLegacyAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sharing.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE shares (
		id TEXT PRIMARY KEY, repo_json TEXT NOT NULL, ai_summary_json TEXT NOT NULL,
		created_at TEXT NOT NULL, expires_at TEXT, visit_count INTEGER NOT NULL DEFAULT 0,
		last_visited_at TEXT);
		INSERT INTO shares VALUES ('old', '{}', '{}', '2026-11-12T00:00:00Z', NULL, 7, NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	for range 2 {
		storage, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		var count, visits, migrations int
		err = storage.db.QueryRow("SELECT COUNT(*), visit_count FROM shares").Scan(&count, &visits)
		if err != nil || count != 1 || visits != 7 {
			t.Fatalf("legacy share changed: count=%d visits=%d err=%v", count, visits, err)
		}
		_ = storage.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations)
		if migrations != 1 {
			t.Fatalf("migration must apply once, got %d", migrations)
		}
		storage.Close()
	}
}

func TestDailyRadarTransactionAndImmutableIdentity(t *testing.T) {
	storage, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sharing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	share := model.DailyRadarShare{
		ID: "first", Request: model.DailyRadarRequest{
			Report: model.DailyRadarReport{ID: "report", Language: "en", Revision: "r1"},
		}, CreatedAt: time.Now().UTC(), ContentHash: "snapshot", HeroHash: "upload",
		Assets: []model.DailyRadarAsset{
			{Kind: "hero-webp", Path: "first/hero.webp", Width: 1600, Height: 840},
			{Kind: "hero-png", Path: "first/hero.png", Width: 1600, Height: 840},
		},
	}
	if err = storage.InsertDailyRadar(ctx, share); err != nil {
		t.Fatal(err)
	}
	share.ID = "second"
	if err = storage.InsertDailyRadar(ctx, share); err == nil {
		t.Fatal("same identity must not create a second share")
	}
	found, err := storage.FindDailyRadar(ctx, "report", "en", "r1")
	if err != nil || found.ID != "first" || len(found.Assets) != 2 {
		t.Fatalf("snapshot mismatch: %+v %v", found, err)
	}
	share.ID = "invalid-assets"
	share.Request.Report.Revision = "r2"
	share.Assets[1].Kind = "invalid-kind"
	if err = storage.InsertDailyRadar(ctx, share); err == nil {
		t.Fatal("invalid asset must roll back its report")
	}
	if _, err = storage.GetDailyRadar(ctx, share.ID); err != sql.ErrNoRows {
		t.Fatalf("half snapshot survived: %v", err)
	}
	if err = storage.RecordDailyRadarVisit(ctx, found.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = storage.db.Exec("UPDATE daily_radar_shares SET revoked_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339Nano), found.ID)
	found, err = storage.GetDailyRadar(ctx, found.ID)
	if err != nil || found.RevokedAt == nil {
		t.Fatalf("revoked state missing: %+v %v", found, err)
	}
}

func TestDailyRadarMigrationRejectsChecksumDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sharing.db")
	storage, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = storage.db.Exec("UPDATE schema_migrations SET checksum = 'modified'")
	storage.Close()
	if reopened, err := NewSQLiteStore(path); err == nil {
		reopened.Close()
		t.Fatal("modified historical migration must stop startup")
	}
}
