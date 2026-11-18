// 本文件实现日报快照查询与上传。鉴权复用 server 的 Bearer middleware，创建限额按 token hash 隔离。
package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
	"github.com/starcat-app/starcat-sharing-api/internal/render"
)

const dailyRadarMultipartMax = 5 << 20

var githubComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// DailyRadarStore 只依赖日报事务，测试可在失败路径注入存储错误。
type DailyRadarStore interface {
	FindDailyRadar(context.Context, string, string, string) (*model.DailyRadarShare, error)
	GetDailyRadar(context.Context, string) (*model.DailyRadarShare, error)
	InsertDailyRadar(context.Context, model.DailyRadarShare) error
	RecordDailyRadarVisit(context.Context, string) error
}

type dailyRadarRate struct {
	since time.Time
	count int
}

// DailyRadarHandler 持有单槽转码器和每 token 的分钟窗口，不保存原始凭据或访问者身份。
type DailyRadarHandler struct {
	store     DailyRadarStore
	templates *template.Template
	baseURL   string
	assetDir  string
	encode    func(context.Context, []byte) (render.DailyRadarHero, error)
	slots     chan struct{}
	rateMu    sync.Mutex
	rates     map[[32]byte]dailyRadarRate
}

// NewDailyRadarHandler 将资产置于数据库旁的受控目录；初始化失败必须阻止服务启动。
func NewDailyRadarHandler(storage DailyRadarStore, templates *template.Template, baseURL, assetDir string) (*DailyRadarHandler, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return nil, errors.New("invalid public share base URL")
	}
	if err = os.MkdirAll(assetDir, 0700); err != nil {
		return nil, err
	}
	return &DailyRadarHandler{
		store: storage, templates: templates, baseURL: strings.TrimRight(baseURL, "/"),
		assetDir: assetDir, encode: render.EncodeDailyRadarHero,
		slots: make(chan struct{}, 1), rates: make(map[[32]byte]dailyRadarRate),
	}, nil
}

// allowCreate 的 key 来自已通过鉴权的 token；到期桶按请求顺便清理，不启动后台 goroutine。
func (h *DailyRadarHandler) allowCreate(token string, now time.Time) bool {
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	for key, window := range h.rates {
		if now.Sub(window.since) >= time.Minute {
			delete(h.rates, key)
		}
	}
	key := sha256.Sum256([]byte(token))
	window := h.rates[key]
	if window.count >= 10 {
		return false
	}
	if window.count == 0 {
		window.since = now
	}
	window.count++
	h.rates[key] = window
	return true
}

// HandleLookup 按公开报告身份复用首发快照，客户端无需上传正文或封面即可获取链接。
func (h *DailyRadarHandler) HandleLookup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	reportID, language, revision := query.Get("report_id"), query.Get("language"), query.Get("revision")
	if !validDailyRadarIdentity(reportID, language, revision) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid report identity.", nil)
		return
	}
	share, err := h.store.FindDailyRadar(r.Context(), reportID, language, revision)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "DAILY_RADAR_SHARE_NOT_FOUND", "This report revision has not been shared.", nil)
	case err != nil:
		log.Printf("[daily-radar] lookup failed: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to look up this report share. Please try again.", nil)
	default:
		h.respondExisting(w, share)
	}
}

// HandleCreate 校验上传；同一报告身份以首发为准，封面或正文差异不改变已发布快照。
func (h *DailyRadarHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.allowCreate(r.Header.Get("Authorization"), time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please try again in a minute.", nil)
		return
	}
	request, uploaded, status, err := readDailyRadarMultipart(w, r)
	if err != nil {
		h.writeFailure(w, status, err)
		return
	}
	existing, err := h.store.FindDailyRadar(r.Context(), request.Report.ID, request.Report.Language, request.Report.Revision)
	if err == nil {
		h.respondExisting(w, existing)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		h.writeFailure(w, http.StatusInternalServerError, err)
		return
	}
	canonical, err := json.Marshal(request)
	if err != nil {
		h.writeFailure(w, http.StatusBadRequest, err)
		return
	}
	contentHash, heroHash := dailyRadarHash(canonical), dailyRadarHash(uploaded)

	// 严格尺寸已限制单次工作量；单槽进一步避免多 token 同时编码将常驻服务内存成倍放大。
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		h.writeFailure(w, http.StatusServiceUnavailable, ctx.Err())
		return
	}
	started := time.Now()
	hero, err := h.encode(ctx, uploaded)
	if err != nil {
		switch {
		case errors.Is(err, render.ErrHeroFormat):
			status = http.StatusUnsupportedMediaType
		case errors.Is(err, render.ErrHeroTooLarge):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			status = http.StatusServiceUnavailable
		default:
			status = http.StatusInternalServerError
		}
		h.writeFailure(w, status, err)
		return
	}
	share := model.DailyRadarShare{
		ID: hex.EncodeToString(randomShareBytes()), Request: request,
		ContentHash: contentHash, HeroHash: heroHash, CreatedAt: time.Now().UTC(),
	}
	if err = h.publishAssets(&share, hero); err != nil {
		h.writeFailure(w, http.StatusInternalServerError, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			if err := os.RemoveAll(filepath.Join(h.assetDir, share.ID)); err != nil {
				log.Printf("[daily-radar] orphan cleanup failed share=%s: %v", share.ID, err)
			}
		}
	}()
	if err = h.store.InsertDailyRadar(ctx, share); err != nil {
		// 两个并发首发可能都先查到空；唯一索引只准一个成功，失败方删除自己的独立目录。
		existing, lookupErr := h.store.FindDailyRadar(r.Context(), request.Report.ID, request.Report.Language, request.Report.Revision)
		if lookupErr == nil {
			h.respondExisting(w, existing)
		} else {
			h.writeFailure(w, http.StatusInternalServerError, err)
		}
		return
	}
	committed = true
	log.Printf("[daily-radar] created report=%s language=%s revision=%s encode_ms=%d webp_bytes=%d png_bytes=%d",
		request.Report.ID, request.Report.Language, request.Report.Revision,
		time.Since(started).Milliseconds(), len(hero.WebP), len(hero.PNG))
	writeJSON(w, h.response(&share, false))
}

// randomShareBytes 使用密码学随机数生成不可预测的公开 ID，不携带报告或用户身份。
func randomShareBytes() []byte {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		panic("crypto/rand unavailable")
	}
	return value
}

// publishAssets 先完成同目录临时文件，再 rename 整个目录，数据库只会引用完整资产组。
func (h *DailyRadarHandler) publishAssets(share *model.DailyRadarShare, hero render.DailyRadarHero) error {
	temporary, err := os.MkdirTemp(h.assetDir, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	for _, asset := range []struct {
		kind, filename string
		data           []byte
	}{{"hero-webp", "hero.webp", hero.WebP}, {"hero-png", "hero.png", hero.PNG}} {
		if err = os.WriteFile(filepath.Join(temporary, asset.filename), asset.data, 0600); err != nil {
			return err
		}
		share.Assets = append(share.Assets, model.DailyRadarAsset{
			Kind: asset.kind, Path: filepath.Join(share.ID, asset.filename),
			ContentHash: dailyRadarHash(asset.data), Bytes: len(asset.data),
			Width: render.DailyRadarHeroWidth, Height: render.DailyRadarHeroHeight,
		})
	}
	return os.Rename(temporary, filepath.Join(h.assetDir, share.ID))
}

func (h *DailyRadarHandler) respondExisting(w http.ResponseWriter, share *model.DailyRadarShare) {
	if share.RevokedAt != nil {
		writeError(w, http.StatusConflict, "SHARE_REVOKED", "The share for this report revision has been revoked.", nil)
		return
	}
	writeJSON(w, h.response(share, true))
}

func (h *DailyRadarHandler) response(share *model.DailyRadarShare, exists bool) model.DailyRadarShareResponse {
	page := h.baseURL + "/d/" + share.ID
	return model.DailyRadarShareResponse{
		ShareID: share.ID, ShareURL: page, AlreadyExists: exists,
		HeroWebPURL: page + "/hero.webp", HeroPNGURL: page + "/hero.png", CreatedAt: share.CreatedAt,
	}
}

// writeFailure 只将稳定错误码和可恢复提示发给客户端，内部错误仅留服务端日志。
func (h *DailyRadarHandler) writeFailure(w http.ResponseWriter, status int, err error) {
	code := map[int]string{
		400: "BAD_REQUEST", 413: "PAYLOAD_TOO_LARGE", 415: "UNSUPPORTED_MEDIA_TYPE",
		500: "INTERNAL_ERROR", 503: "SERVICE_UNAVAILABLE",
	}[status]
	log.Printf("[daily-radar] create failed code=%s: %v", code, err)
	writeError(w, status, code, "Unable to create this report share. Please try again.", nil)
}

func dailyRadarHash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// readDailyRadarMultipart 用 MaxBytesReader 约束整个网络请求，不向系统临时目录落用户上传。
func readDailyRadarMultipart(w http.ResponseWriter, r *http.Request) (model.DailyRadarRequest, []byte, int, error) {
	var request model.DailyRadarRequest
	r.Body = http.MaxBytesReader(w, r.Body, dailyRadarMultipartMax)
	if r.ContentLength > dailyRadarMultipartMax {
		return request, nil, 413, errors.New("multipart body too large")
	}
	reader, err := r.MultipartReader()
	if err != nil {
		return request, nil, 400, errors.New("multipart required")
	}
	var raw, hero []byte
	seen := make(map[string]bool)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return request, nil, multipartErrorStatus(err), errors.New("invalid multipart body")
		}
		name := part.FormName()
		if seen[name] || (name != "request" && name != "hero") {
			part.Close()
			return request, nil, 400, errors.New("unexpected or duplicate multipart field")
		}
		seen[name] = true
		content, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			return request, nil, multipartErrorStatus(err), errors.New("unable to read multipart part")
		}
		switch name {
		case "request":
			raw = content
		case "hero":
			if len(content) > render.DailyRadarPNGMax {
				return request, nil, 413, render.ErrHeroTooLarge
			}
			mediaType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if mediaType != "image/png" {
				return request, nil, 415, render.ErrHeroFormat
			}
			hero = content
		}
	}
	if len(raw) == 0 || len(hero) == 0 {
		return request, nil, 400, errors.New("request and hero are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return request, nil, 400, errors.New("invalid report snapshot JSON")
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return request, nil, 400, errors.New("multiple JSON values")
	}
	if status, err := validateDailyRadarRequest(request); err != nil {
		return request, nil, status, err
	}
	return request, hero, 200, nil
}

func multipartErrorStatus(err error) int {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// validateDailyRadarRequest 只校验公开快照，不读取 GitHub、用户仓库库或任何个人设置。
func validateDailyRadarRequest(request model.DailyRadarRequest) (int, error) {
	report := request.Report
	if !validDailyRadarIdentity(report.ID, report.Language, report.Revision) {
		return 400, errors.New("invalid report identity")
	}
	if strings.TrimSpace(report.Title) == "" || strings.TrimSpace(report.Markdown) == "" {
		return 400, errors.New("missing required report field")
	}
	if len(report.Markdown) > 1<<20 {
		return 413, errors.New("markdown exceeds 1 MB")
	}
	if _, err := time.Parse("2006-01-02", report.Date); err != nil || report.ReadingMinutes < 0 {
		return 400, errors.New("invalid report date or reading time")
	}
	if len(request.Projects) > 100 || len(report.Sources) > 200 {
		return 400, errors.New("too many projects or sources")
	}
	for _, project := range request.Projects {
		if !githubComponent.MatchString(project.Owner) || !githubComponent.MatchString(project.Repo) ||
			project.Repo == "." || project.Repo == ".." || project.Stars < 0 {
			return 400, errors.New("invalid GitHub project")
		}
		if project.AvatarURL != "" && render.DailyRadarSafeURL(project.AvatarURL) == "" {
			return 400, errors.New("invalid project avatar URL")
		}
	}
	for _, source := range report.Sources {
		if render.DailyRadarSafeURL(source.URL) == "" {
			return 400, errors.New("invalid source URL")
		}
	}
	return 200, nil
}

// 查询与创建接受相同的身份，保留原始值以匹配已经落库的键。
func validDailyRadarIdentity(reportID, language, revision string) bool {
	return len(reportID) <= 512 && len(language) <= 64 && len(revision) <= 256 &&
		strings.TrimSpace(reportID) != "" && strings.TrimSpace(language) != "" && strings.TrimSpace(revision) != ""
}
