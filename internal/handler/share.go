// Package handler 提供分享相关的 HTTP 请求处理。
//
// R-01 v1.2: POST /api/v1/share 包 envelope + 错误响应统一形态;
// GET /s/{id} HTML 渲染不动，改为读 SQLite。
package handler

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
	"github.com/starcat-app/starcat-sharing-api/internal/store"
)

// ShareHandler 处理分享相关的 HTTP 请求。
type ShareHandler struct {
	store     store.Store
	templates *template.Template
	baseURL   string
}

// NewShareHandler 创建分享处理器。
func NewShareHandler(s store.Store, t *template.Template, baseURL string) *ShareHandler {
	return &ShareHandler{
		store:     s,
		templates: t,
		baseURL:   baseURL,
	}
}

// HandleCreateShareV1 POST /api/v1/share - 创建分享链接（v1 envelope 响应）。
//
// 过期策略（R-01 v1.2）：
//   - data.ExpiresAt 留零值 → store.Upsert 写 NULL → 响应 ExpiresAt=nil → 永不过期
//   - 与设计文档 supports/docs/R-01-sharing-api-改造方案.md §3.1 一致
//     （schema 列 `expires_at TEXT NULL`，null 表示永不过期，nullable 设计）
//   - 当前业务无主动过期需求；如未来需要，应在请求体接受 expires_in 参数后再赋值。
func (h *ShareHandler) HandleCreateShareV1(w http.ResponseWriter, r *http.Request) {
	var req model.ShareRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST",
			"request body decode failed: "+err.Error(), nil)
		return
	}

	id := store.NewID(8)
	now := time.Now()

	// 留 ExpiresAt 零值 → 落库为 NULL，响应中为 nil
	data := model.ShareData{
		ID:        id,
		Request:   req,
		CreatedAt: now,
	}

	if err := h.store.Upsert(data); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to save share: "+err.Error(), nil)
		return
	}

	resp := model.ShareCreateResponse{
		ShareURL:  fmt.Sprintf("%s/s/%s", h.baseURL, id),
		ShareID:   id,
		ExpiresAt: nil, // 永不过期
		CreatedAt: now,
	}

	writeJSON(w, resp)
}

// HandleRenderShare GET /s/{id} - 查看分享页面（HTML 渲染，不鉴权）。
func (h *ShareHandler) HandleRenderShare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	data, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	err = h.templates.ExecuteTemplate(w, "share.html", data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"template render failed: "+err.Error(), nil)
	}
}
