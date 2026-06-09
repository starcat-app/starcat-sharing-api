# R-01 改造遗留问题

> 创建：2026-06-09 | 改造方案：`supports/docs/R-01-sharing-api-改造方案.md` v1.2

## 阻断（编译/部署前必须解决）

- [ ] **go.mod 缺依赖**：`modernc.org/sqlite` 未加入 go.mod/go.sum。
  ```bash
  cd supports/starcat-sharing-api
  go get modernc.org/sqlite && go mod tidy
  go build ./... && go vet ./...
  ```

## 高优（赶在首次部署前）

- [ ] **单元测试**：方案 §9.1 要求 7 个 store/middleware/handler 测试，当前为 0。
  - `internal/store/sqlite.go`：TestUpsertShare / TestGetShare_NotFound / TestVisitCount_Increment
  - `internal/middleware/auth.go`：4 个 Bearer Token 验证测试
  - `internal/handler/share.go`：envelope 形态 / BadRequest / Persists / RenderShare
- [ ] **本地 smoke 测试**：启动服务 → healthz → 鉴权创建分享 → 无 key 401 → HTML 渲染。

## 中优

- [ ] **fly.toml [env] 清理**：当前保留 `GOGC='100'` + `GOMAXPROCS='1'`，方案要求仅 `PORT`。评估是否保留（Fly.io Go 优化惯例）或移除。
- [ ] **version 包冗余**：`internal/version/version.go` 定义了 `Version = "1.0.0"`，但无代码引用。考虑在 `main.go` 启动日志中打印版本号或删除。

## 低优

- [ ] **部署阶段 1 周验收**（方案 §9.2）：`fly logs` 无 panic、SQLite 增长 < 1MB/周、鉴权失败 < 5%、旧 `data.json` 不被读取。
