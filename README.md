# Starcat Sharing API

Starcat 应用的后端服务，提供 AI 分享页面的生成和托管功能。

> **R-01 v1.2**（2026-06-09）：存储从 JSON 文件迁移到 SQLite，加 Bearer Token 鉴权，API 升级到 `/api/v1/*`。

## 功能特性

- **分享链接生成**：接收 Repo 数据和 AI 摘要，生成唯一短链接（`POST /api/v1/share`，需鉴权）
- **分享页面渲染**：通过短链接访问时，Go `html/template` + Tailwind CSS 渲染分享页面（`GET /s/{id}`，公开）
- **数据持久化**：SQLite（WAL 模式），替代旧的 `data.json` 文件存储

## 快速开始

### 环境要求

- Go 1.25+

### 本地运行

```bash
cp .env.example .env
# 编辑 .env，填入 API_KEYS（用 ../scripts/gen-api-key.sh 生成）
cd starcat-sharing-api
go run ./cmd/server/
```

默认端口 `5001`。

### .env 配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 服务端口 | `5001` |
| `STORE_FILE` | SQLite 数据库路径 | `./sharing.db` |
| `BASE_URL` | 短链基础 URL | `http://localhost:5001` |
| `API_KEYS` | Bearer Token 白名单（逗号分隔） | 必填 |

## API 接口

所有数据接口需要 `Authorization: Bearer <api-key>` 头。

### `POST /api/v1/share`（需鉴权）

创建分享链接。

**请求体**：

```json
{
  "repo": {
    "fullName": "owner/repo",
    "description": "项目描述...",
    "language": "Swift",
    "starsCount": 12345,
    "forksCount": 1234,
    "topics": ["swift", "macos"],
    "homepage": "https://example.com",
    "url": "https://github.com/owner/repo"
  },
  "aiSummary": {
    "oneLiner": "AI 一句话总结",
    "summary": "详细分析...",
    "platforms": ["macOS", "iOS"],
    "suitableFor": ["适合..."],
    "strengths": ["优势..."],
    "risks": ["风险..."],
    "suggestedTags": [{"name": "SwiftUI", "confidence": 0.95}]
  }
}
```

**响应 200**：

```json
{
  "schema_version": 1,
  "data": {
    "shareUrl": "https://starcat.ink/s/aBc1d2eF",
    "shareId": "aBc1d2eF",
    "expiresAt": null,
    "createdAt": "2026-06-09T12:00:00Z"
  }
}
```

### `GET /s/{id}`（公开）

访问分享页面，返回渲染后的 HTML。不存在则返回 404。

### `GET /healthz`（公开）

健康检查，返回 `ok`。

## 鉴权

所有 `/api/v1/*` 端点需要 `Authorization: Bearer <api-key>` 头。API Key 通过 `API_KEYS` 环境变量配置（逗号分隔多个 key）。

生成新 key：

```bash
bash ../scripts/gen-api-key.sh
```

## 部署（Fly.io）

```bash
fly secrets set \
  API_KEYS="sk-starcat-prodKey1,sk-starcat-prodKey2" \
  BASE_URL="https://starcat.ink" \
  STORE_FILE="/data/sharing.db" \
  -a starcat-sharing-api

fly deploy -a starcat-sharing-api
```
