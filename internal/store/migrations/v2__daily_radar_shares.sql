CREATE TABLE IF NOT EXISTS daily_radar_shares (
    id TEXT PRIMARY KEY,
    report_id TEXT NOT NULL,
    report_date TEXT NOT NULL,
    category_id TEXT NOT NULL,
    language TEXT NOT NULL,
    revision TEXT NOT NULL,
    title TEXT NOT NULL,
    subtitle TEXT NOT NULL,
    summary TEXT NOT NULL,
    reading_minutes INTEGER NOT NULL,
    markdown TEXT NOT NULL,
    projects_json TEXT NOT NULL,
    sources_json TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    hero_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    visit_count INTEGER NOT NULL DEFAULT 0,
    last_visited_at TEXT,
    revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_radar_share_identity
ON daily_radar_shares(report_id, language, revision);

CREATE TABLE IF NOT EXISTS daily_radar_share_assets (
    share_id TEXT NOT NULL REFERENCES daily_radar_shares(id),
    kind TEXT NOT NULL CHECK(kind IN ('hero-webp', 'hero-png')),
    path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    bytes INTEGER NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(share_id, kind)
);
