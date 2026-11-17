// 本文件提供匿名、无 cookie 的日报公开页和受控图片路由。
package handler

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
	"github.com/starcat-app/starcat-sharing-api/internal/render"
)

const (
	dailyRadarPageCache      = "public, max-age=0, must-revalidate"
	dailyRadarImmutableCache = "public, max-age=31536000, immutable"
	dailyRadarMissingCache   = "public, max-age=60"
)

// dailyRadarPublicHeaders 对 HTML、图片和错误统一禁用嗅探与来源泄漏。
func dailyRadarPublicHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' https:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

// publicDailyRadar 统一处理缺失、撤销和存储错误，防止图片绕过撤销检查。
func (h *DailyRadarHandler) publicDailyRadar(w http.ResponseWriter, r *http.Request) *model.DailyRadarShare {
	dailyRadarPublicHeaders(w)
	share, err := h.store.GetDailyRadar(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		w.Header().Set("Cache-Control", dailyRadarMissingCache)
		http.NotFound(w, r)
		return nil
	}
	if err != nil {
		log.Printf("[daily-radar] public lookup failed: %v", err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return nil
	}
	if share.RevokedAt != nil {
		w.Header().Set("Cache-Control", dailyRadarMissingCache)
		http.Error(w, "This share is unavailable", http.StatusGone)
		return nil
	}
	return share
}

// HandlePage 渲染完整快照后再设置可缓存响应，避免模板错误形成可缓存的半页。
func (h *DailyRadarHandler) HandlePage(w http.ResponseWriter, r *http.Request) {
	share := h.publicDailyRadar(w, r)
	if share == nil {
		return
	}
	page, err := render.NewDailyRadarPage(share, h.baseURL)
	if err != nil {
		log.Printf("[daily-radar] render failed share=%s: %v", share.ID, err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	var result bytes.Buffer
	if err = h.templates.ExecuteTemplate(&result, "daily_radar.html", page); err != nil {
		log.Printf("[daily-radar] template failed share=%s: %v", share.ID, err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 内容快照不变，但页面模板会更新；HTML 必须重新校验，图片仍可长期缓存。
	w.Header().Set("Cache-Control", dailyRadarPageCache)
	if r.Method != http.MethodHead {
		if err = h.store.RecordDailyRadarVisit(r.Context(), share.ID); err != nil {
			log.Printf("[daily-radar] visit count failed share=%s: %v", share.ID, err)
		}
		_, _ = w.Write(result.Bytes())
	}
}

// HandleHero 只读取数据库中登记且路径符合服务生成规则的资产。
func (h *DailyRadarHandler) HandleHero(w http.ResponseWriter, r *http.Request) {
	share := h.publicDailyRadar(w, r)
	if share == nil {
		return
	}
	kind, filename, contentType := "hero-webp", "hero.webp", "image/webp"
	if r.PathValue("format") == "png" {
		kind, filename, contentType = "hero-png", "hero.png", "image/png"
	}
	var path string
	for _, asset := range share.Assets {
		if asset.Kind == kind && asset.Path == filepath.Join(share.ID, filename) {
			path = filepath.Join(h.assetDir, asset.Path)
			break
		}
	}
	if path == "" {
		w.Header().Set("Cache-Control", dailyRadarMissingCache)
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		w.Header().Set("Cache-Control", dailyRadarMissingCache)
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", dailyRadarImmutableCache)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if r.Method != http.MethodHead {
		written, _ := io.Copy(w, file)
		log.Printf("[daily-radar] asset share=%s kind=%s bytes=%d", share.ID, kind, written)
	}
}
