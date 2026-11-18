# 每日热点快照分享

2026-11-13 已实现并随 `starcat-api` 聚合部署，dong4j 已确认功能完成。
四仓代码均已合回各自本地 `dev`；后续公开页展示样式在本仓嵌入的 HTML/CSS
模板中优化，仅调整页面样式不需要修改或重新发布 Starcat 客户端。

## 接口

`GET /api/v1/daily-radar/shares?report_id=...&language=...&revision=...` 使用同一 Bearer
鉴权，按报告 ID、内容语言和 revision 查询。命中返回 `200` 和首发的链接、封面 URL、
创建时间，`already_exists` 为 `true`。尚未发布返回 `404 DAILY_RADAR_SHARE_NOT_FOUND`，
撤销返回 `409 SHARE_REVOKED`，输入错误返回 `400`，读取异常返回 `500`。客户端只在收到
明确的未发布响应时创建快照。查询响应使用 `Cache-Control: no-store`。

创建使用 `POST /api/v1/daily-radar/shares`，复用 Bearer 鉴权，每个有效 token 每分钟
最多 10 次。multipart 两个 part：`request` 为 JSON，`hero` 为 `image/png`。
创建与幂等命中均返回 200，通过 `already_exists` 区分；使用现有 `schema_version: 1` envelope 与
snake_case。不要照产品方案示例直接发送 camelCase。

```json
{
  "report": {
    "id": "report-id",
    "date": "2026-11-13",
    "category_id": "ai-cli",
    "language": "en",
    "revision": "revision-id",
    "title": "Report title",
    "subtitle": "",
    "summary": "Report summary",
    "reading_minutes": 5,
    "markdown": "## News\n\nReport body.",
    "sources": [{"title": "Source", "url": "https://example.com/news"}]
  },
  "projects": [{
    "owner": "example", "repo": "project", "name": "project",
    "description": "Description", "language": "Swift", "stars": 10, "avatar_url": ""
  }]
}
```

响应 `data` 包含 `share_id`、`share_url`、`already_exists`、`hero_webp_url`、
`hero_png_url`、`created_at`。同 `(id, language, revision)` 共享首发快照，重复上传时
返回原链接、封面 URL 和创建时间。正文与原 PNG 的 SHA-256 随首发记录保存。
未知 JSON 字段直接拒绝。记录仅包含报告白名单数据。

限制：正文 1 MiB、原 PNG 3 MiB、multipart 5 MiB、100 个项目、200 个来源，
图片固定 1600×840。转码使用纯 Go `github.com/gen2brain/vpx v0.2.2`，质量 88，
超过 1 MiB 只重试质量 84；PNG fallback 重新编码且不超过 3 MiB。单槽转码避免并发
工作集成倍膨胀；30 秒 context 在编码阶段之间检查，并非可抢占的编码 CPU 超时。

## 公开页与运行配置

`GET /d/{id}`、`GET /d/{id}/hero.webp`、`GET /d/{id}/hero.png` 匿名可读，
不写 cookie。正文禁用原始 HTML，链接仅保留 HTTP(S) 并提升为 HTTPS，增加外链 rel；
OG 使用绝对 PNG URL。公开 HTML 回源重新验证，封面响应为一年 immutable 缓存，
撤销后回源返回 410 / 60 秒缓存。
浏览器和平台已经缓存的响应不能被主动收回。

`DAILY_RADAR_ASSET_DIR` 默认位于 SQLite 同目录的 `daily-radar/`；聚合部署显式设置
`SHARING_DAILY_RADAR_ASSET_DIR=/data/daily-radar`。目录与数据库必须在同一持久卷，
不能使用容器临时文件系统。服务启动时追加 `v2__daily_radar_shares.sql`，记录 checksum
与耗时；迁移失败停止启动，既有 `shares` 不改动。

复用直接读取既有 SQLite 快照。升级可保留 `sharing.db`、`daily-radar/` 和公开页缓存，
既有 `/d/{id}` 与封面 URL 继续有效。部署时先更新分享服务，再切换使用查询接口的客户端。

公开域名必须转发 `/d/` 到聚合服务并注入 `X-SC-Svc: sharing`，可审查 Nginx 片段
位于官网仓 `direct/nginx/daily-radar-sharing.conf.example`。查询和创建接口均通过客户端
配置的 API host 访问，Bearer token 放在请求头。

新编码器的许可见 `THIRD_PARTY_NOTICES.md`。该依赖只进入服务端，客户端继续用系统
ImageIO，不引入编码库。固定版本的 Go directive 为 1.26.4，满足本仓 1.26.11 基线；
具体质量、大小与内存以本仓验收测试为准，不用上游基准替代产品封面实测。
2026-11-13 核对上游 tags 与默认分支：`v0.2.2` 指向
`5022b0c7dc7a2d7fc4ed83889af5f0cf9ca523e3`，也是当时最新提交，
提交日期为 2026-09-18；本次固定该版本，不跟随浮动分支。

## 成套备份与恢复

先创建平台卷快照，保存快照 ID。需要可校验的离线备份时，在能读取挂载卷且装有
Python 3 的受控环境执行以下工具；不把它用于复制正在写入的裸 `.db/-wal/-shm`。

```bash
python3 scripts/backup_daily_radar.py \
  --database /data/sharing.db \
  --assets /data/daily-radar \
  --output /backup/new-snapshot-directory
```

工具先用 SQLite backup API 固定一致性视图，再仅复制该视图引用的不可变资产，
逐个核对大小和 SHA-256，生成 manifest。缺文件、hash 错误或库损坏均失败并清理本次
新目录；已有备份目录绝不覆盖。备份不包含凭据，其它服务库与 metrics 仍按各自流程备份。
聚合镜像不要求安装 Python，可在平台卷快照的恢复副本上运行此工具。

恢复时先停止对应服务写入，在空目录检查 `sharing.db` 的 `PRAGMA integrity_check`
和 manifest，再成套恢复数据库与 `daily-radar/`，设置服务用户独占写权限后启动。
禁止只恢复数据库而遗漏封面。不得在在线库上直接覆盖文件，也不要删除仍被快照引用的
资产；孤儿临时目录可另行审计，不属于在线自动清理职责。

## 本地验证与发布顺序

```bash
go test -race ./...
go build ./...
go vet ./...
python3 -m unittest discover -s scripts -p 'test_backup_daily_radar.py'
```

2026-11-13 的本机实测使用客户端测试附件中的真实 1600×840 `nebula` 封面：
原 PNG 1,232,004 bytes，质量 88 的 WebP 11,352 bytes，fallback PNG 939,024 bytes。
Apple M2 Ultra / darwin arm64 下编码基准为 3 次平均约 3.33 秒、
22,544,141 B/op；单次测试进程最大 RSS 为 49,823,744 bytes。这是本机数据，
不能代替生产容器的延迟与内存验收。

独立 VP8 解码后按 BT.601 studio range 换算的 PSNR 为 47.61 dB，
与 macOS ImageIO 的 sRGB 解码交叉校验一致。Go `image.YCbCr.RGBA` 使用 JPEG
full range，不能直接用于该质量比较。重现时设置
`DAILY_RADAR_HERO_FIXTURE` 为从 xcresult 导出的客户端 PNG，再运行：

```bash
go test ./internal/render -run '^TestDailyRadarHeroClientFixture$' -v -count=1
go test ./internal/render -run '^$' -bench '^BenchmarkDailyRadarHero$' -benchtime=3x -benchmem
```

实现期客户端相关自动测试共 45 项通过（Swift Testing 38 项、XCTest 构图测试 7 项）。
`make build-direct` 与 `make build-appstore` 均通过构建和渠道产物校验，未启动 App。
17 种语言已补齐配置并核对编译资源；除英文与简中外保留 `needs_review`，
母语审核和真实 GUI 长文案布局仍待验收。

2026-11-13 后续已按授权以聚合提交 `db6800e5` 部署生产，`dependencies.json`
固定本仓 `e98d51a5912426a00031698c343c342decb8e0fb`。真实报告创建、幂等、
冲突、匿名页面与 WebP/PNG 响应冒烟通过，官网 `/d/` 转发已生效；
合入 `dev` 后客户端 10 项定向测试通过，dong4j 确认功能完成。
移动浏览器、WebP 不支持环境的回退和社交平台 OG debugger 仍未单独实测。
部署前的一致性备份已从专项 worktree 保留到主工作区，并通过 SHA-256
及 SQLite 完整性校验。本轮文档与分支收尾不重新部署。

聚合服务使用 sibling replace 验证本地源码。后续变更仍必须获准提交后再更新
`dependencies.json` 的完整 SHA；工作区未提交时不得伪造发布锁或宣称已部署。
后续授权部署按以下顺序执行：备份 → 固定提交构建 → staging → 真实报告与浏览器/OG 验收 →
production 与 `/d/` 转发。旧 `/api/v1/share`、`/s/{id}`、`/r/` 回归也须通过。
