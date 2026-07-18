# Starcat Sharing API

<!-- starcat-promo:start -->
<div align="center">
<a href="https://starcat.ink"><img src="https://raw.githubusercontent.com/dong4j/starcat-pro/main/banner.webp" width="100%" alt="Starcat" /></a>

<p><strong>Self-hostable support API for Starcat share page generation and hosting.</strong></p>
<p>Starcat is a native macOS app that turns GitHub Stars into a searchable, organized and AI-assisted knowledge base. It supports README rendering, tags, private notes, release tracking, repository health signals, AI summaries, semantic search, browser plugin workflows and self-hostable support APIs.</p>

<a href="https://github.com/dong4j/homebrew-starcat"><img src="https://img.shields.io/badge/Install%20with-Homebrew-FBBF24?style=for-the-badge&logo=homebrew&logoColor=white" width="220" alt="Install with Homebrew"/></a>
<br/>
<sub><a href="./README-ZH.md">中文说明</a></sub>
</div>

<div align="center">
<a href="https://starcat.ink"><img src="https://img.shields.io/badge/website-starcat.ink-38BDF8?style=flat&color=blue" alt="website"/></a>
<a href="https://github.com/dong4j/starcat-pro"><img src="https://img.shields.io/badge/support-starcat--pro-lightgrey.svg?style=flat&color=blue" alt="support"/></a>
<a href="https://github.com/dong4j/homebrew-starcat"><img src="https://img.shields.io/badge/install-homebrew-lightgrey.svg?style=flat&color=blue" alt="homebrew"/></a>
<a href="https://github.com/dong4j/starcat-localization"><img src="https://img.shields.io/badge/localization-open-lightgrey.svg?style=flat&color=blue" alt="localization"/></a>
</div>

<div align="center">
<img width="900" src="https://raw.githubusercontent.com/dong4j/starcat-pro/main/main.webp" alt="Starcat main window"/>
</div>

**Preferred install method:**

```bash
brew tap dong4j/starcat
brew trust dong4j/starcat
brew install --cask starcat
```

**Useful links:**

- Home: https://starcat.ink
- Download: https://starcat.ink/downloads/Starcat-1.1.0-arm64.dmg
- Public support and release notes: https://github.com/dong4j/starcat-pro
- Homebrew tap: https://github.com/dong4j/homebrew-starcat
- Browser plugins: [Chrome](https://github.com/dong4j/starcat-chrome-plugin) / [Safari](https://github.com/dong4j/starcat-safari-plugin)
- Localization: https://github.com/dong4j/starcat-localization

**Starcat ecosystem:**

- [starcat-sharing-api](https://github.com/dong4j/starcat-sharing-api)
- [starcat-trending-api](https://github.com/dong4j/starcat-trending-api)
- [starcat-weekly-api](https://github.com/dong4j/starcat-weekly-api)
- [starcat-wiki-api](https://github.com/dong4j/starcat-wiki-api)
- [starcat-recommend-api](https://github.com/dong4j/starcat-recommend-api)
- [starcat-discovery-api](https://github.com/dong4j/starcat-discovery-api)
- [starcat-license-api](https://github.com/dong4j/starcat-license-api)

> Starcat provides hosted defaults for normal users. This API is open source so advanced users can inspect it, run it locally, or deploy their own instance.
<!-- starcat-promo:end -->

Backend service for the Starcat app that generates and hosts AI-powered share pages.

> **R-01 v1.2** (2026-06-09): Migrated storage from JSON files to SQLite, added Bearer Token authentication, and upgraded the API to `/api/v1/*`.

## Features

- **Share link generation**: Accepts repository data and AI summaries and generates unique short links (`POST /api/v1/share`, authentication required)
- **Share page rendering**: Renders share pages with Go `html/template` and Tailwind CSS when a short link is opened (`GET /s/{id}`, public)
- **Data persistence**: Uses SQLite in WAL mode instead of the legacy `data.json` file

## Quick Start

### Requirements

- Go 1.25+

### Run Locally

```bash
cp .env.example .env
# Edit .env and set API_KEYS (generate keys with ../scripts/gen-api-key.sh)
cd starcat-sharing-api
go run ./cmd/server/
```

The default port is `5001`.

### .env Configuration

| Variable | Description | Default |
|------|------|--------|
| `PORT` | Server port | `5001` |
| `STORE_FILE` | SQLite database path | `./sharing.db` |
| `BASE_URL` | Base URL for short links | `http://localhost:5001` |
| `API_KEYS` | Bearer Token allowlist (comma-separated) | Required |

## API Endpoints

All data endpoints require the `Authorization: Bearer <api-key>` header.

### `POST /api/v1/share` (Authentication Required)

Creates a share link.

**Request body**:

> R-01 P1-3b (2026-06-10): All JSON field names use snake_case, consistent with trending-api and weekly-api.

```json
{
  "repo": {
    "full_name": "owner/repo",
    "description": "Project description...",
    "language": "Swift",
    "stars_count": 12345,
    "forks_count": 1234,
    "topics": ["swift", "macos"],
    "homepage": "https://example.com",
    "url": "https://github.com/owner/repo"
  },
  "ai_summary": {
    "one_liner": "One-sentence AI summary",
    "summary": "Detailed analysis...",
    "platforms": ["macOS", "iOS"],
    "suitable_for": ["Suitable for..."],
    "strengths": ["Strength..."],
    "risks": ["Risk..."],
    "suggested_tags": [{"name": "SwiftUI", "confidence": 0.95}]
  }
}
```

**Response 200**:

```json
{
  "schema_version": 1,
  "data": {
    "share_url": "https://starcat.ink/s/aBc1d2eF",
    "share_id": "aBc1d2eF",
    "expires_at": null,
    "created_at": "2026-06-09T12:00:00Z"
  }
}
```

### `GET /s/{id}` (Public)

Returns the rendered HTML share page. Returns 404 if the share does not exist.

### `GET /healthz` (Public)

Health check that returns `ok`.

## Authentication

All `/api/v1/*` endpoints require the `Authorization: Bearer <api-key>` header. Configure API keys with the `API_KEYS` environment variable as a comma-separated list.

Generate a new key:

```bash
bash ../scripts/gen-api-key.sh
```

## Deployment (Fly.io)

```bash
fly secrets set \
  API_KEYS="sk-starcat-prodKey1,sk-starcat-prodKey2" \
  BASE_URL="https://starcat.ink" \
  STORE_FILE="/data/sharing.db" \
  -a starcat-sharing-api

fly deploy -a starcat-sharing-api
```
