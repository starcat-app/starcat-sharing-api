// 本文件覆盖真实上传、幂等、冲突、公开读取与事务失败清理，避免只测试 DTO。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/assets"
	"github.com/starcat-app/starcat-sharing-api/internal/middleware"
	"github.com/starcat-app/starcat-sharing-api/internal/model"
	"github.com/starcat-app/starcat-sharing-api/internal/render"
	"github.com/starcat-app/starcat-sharing-api/internal/store"
	"golang.org/x/image/webp"
)

type dailyRadarTestStore struct {
	DailyRadarStore
	failInsert bool
	revoked    bool
}

func (s *dailyRadarTestStore) InsertDailyRadar(ctx context.Context, share model.DailyRadarShare) error {
	if s.failInsert {
		return errors.New("simulated storage failure with internal path")
	}
	return s.DailyRadarStore.InsertDailyRadar(ctx, share)
}

func (s *dailyRadarTestStore) GetDailyRadar(ctx context.Context, id string) (*model.DailyRadarShare, error) {
	share, err := s.DailyRadarStore.GetDailyRadar(ctx, id)
	if err == nil && s.revoked {
		now := time.Now()
		share.RevokedAt = &now
	}
	return share, err
}

func newDailyRadarTestHandler(t *testing.T) (*DailyRadarHandler, http.Handler, *dailyRadarTestStore, string) {
	t.Helper()
	root := t.TempDir()
	storage, err := store.NewSQLiteStore(filepath.Join(root, "sharing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	// ParseFS 同时解析旧仓库模板，测试与生产一样显式提供它们需要的 helper。
	templates, err := assets.ParseHTML(template.FuncMap{
		"repoOwner": func(fullName string) string {
			owner, _, _ := strings.Cut(fullName, "/")
			return owner
		},
		"repoName": func(fullName string) string {
			_, name, _ := strings.Cut(fullName, "/")
			return name
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &dailyRadarTestStore{DailyRadarStore: storage}
	handler, err := NewDailyRadarHandler(wrapped, templates, "https://share.example.com", filepath.Join(root, "daily-radar"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/daily-radar/shares",
		middleware.NewBearerAuth([]string{"test-token", "second-token"}).Wrap(http.HandlerFunc(handler.HandleCreate)))
	mux.HandleFunc("GET /d/{id}", handler.HandlePage)
	mux.HandleFunc("GET /d/{id}/hero.webp", handler.HandleHero)
	mux.HandleFunc("GET /d/{id}/hero.png", func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("format", "png")
		handler.HandleHero(w, r)
	})
	return handler, mux, wrapped, handler.assetDir
}

func dailyRadarTestSnapshot() model.DailyRadarRequest {
	return model.DailyRadarRequest{
		Report: model.DailyRadarReport{
			ID: "report-2026-11-12", Date: "2026-11-12", CategoryID: "openclaw",
			Language: "en", Revision: "r1", Title: `A report <script>alert(1)</script>`,
			Summary: "Daily report", ReadingMinutes: 8,
			Markdown: "# Report\n\n[Source](http://example.com/news)\n\n<script>alert(1)</script>\n\n[bad](javascript:alert%281%29)",
			Sources:  []model.DailyRadarSource{{Title: "News", URL: "http://example.com/news"}},
		},
		Projects: []model.DailyRadarProject{{Owner: "starcat-app", Repo: "Starcat", Name: "Starcat", Stars: 100}},
	}
}

func dailyRadarTestPNG(t *testing.T) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := png.Encode(&result, image.NewRGBA(image.Rect(0, 0, 1600, 840))); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func dailyRadarMultipart(t *testing.T, snapshot model.DailyRadarRequest, hero []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormField("request")
	_ = json.NewEncoder(part).Encode(snapshot)
	imagePart, _ := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="hero"; filename="hero.png"`},
		"Content-Type":        {"image/png"},
	})
	_, _ = imagePart.Write(hero)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/daily-radar/shares", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer test-token")
	return request
}

func TestDailyRadarCreateAndPublicRoundTrip(t *testing.T) {
	_, router, storage, root := newDailyRadarTestHandler(t)
	snapshot, uploaded := dailyRadarTestSnapshot(), dailyRadarTestPNG(t)
	first := httptest.NewRecorder()
	router.ServeHTTP(first, dailyRadarMultipart(t, snapshot, uploaded))
	if first.Code != 200 {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	var response struct {
		SchemaVersion int                           `json:"schema_version"`
		Data          model.DailyRadarShareResponse `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != 1 || response.Data.AlreadyExists || response.Data.ShareID == "" {
		t.Fatalf("invalid response: %+v", response)
	}
	again := httptest.NewRecorder()
	router.ServeHTTP(again, dailyRadarMultipart(t, snapshot, uploaded))
	var repeated struct {
		Data model.DailyRadarShareResponse `json:"data"`
	}
	_ = json.Unmarshal(again.Body.Bytes(), &repeated)
	if again.Code != 200 || !repeated.Data.AlreadyExists || repeated.Data.ShareID != response.Data.ShareID {
		t.Fatalf("idempotence failed: %d %s", again.Code, again.Body.String())
	}
	for _, mutate := range []func(*model.DailyRadarRequest, *[]byte){
		func(request *model.DailyRadarRequest, _ *[]byte) { request.Report.Summary = "different content" },
		func(_ *model.DailyRadarRequest, hero *[]byte) { *hero = append(*hero, 0) },
	} {
		changed, hero := dailyRadarTestSnapshot(), bytes.Clone(uploaded)
		mutate(&changed, &hero)
		conflict := httptest.NewRecorder()
		router.ServeHTTP(conflict, dailyRadarMultipart(t, changed, hero))
		if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), "SHARE_CONFLICT") {
			t.Fatalf("snapshot was overwritten: %d %s", conflict.Code, conflict.Body.String())
		}
	}
	for _, route := range []string{"", "/hero.webp", "/hero.png"} {
		public := httptest.NewRecorder()
		router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/d/"+response.Data.ShareID+route, nil))
		expectedCache := dailyRadarImmutableCache
		if route == "" {
			expectedCache = "public, max-age=0, must-revalidate"
		}
		if public.Code != 200 || public.Header().Get("Cache-Control") != expectedCache ||
			public.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("public route %s: %d %v", route, public.Code, public.Header())
		}
		if route == "" {
			text := public.Body.String()
			for _, expected := range []string{"<picture>", response.Data.HeroPNGURL, "noindex, nofollow",
				`class="card" href="https://github.com/starcat-app/Starcat" target="_blank" rel="noopener noreferrer"`,
				"https://example.com/news", "&lt;script&gt;"} {
				if !strings.Contains(text, expected) {
					t.Errorf("missing %q", expected)
				}
			}
			if logos := strings.Count(text, `src="/r/starcat-logo.png"`); logos != 2 {
				t.Errorf("expected official logo in header and footer, got %d", logos)
			}
			if strings.Contains(text, "<script>") || strings.Contains(text, "javascript:") || public.Header().Get("Set-Cookie") != "" {
				t.Fatal("unsafe HTML or tracking cookie in public snapshot")
			}
		} else if route == "/hero.webp" {
			if _, err := webp.DecodeConfig(bytes.NewReader(public.Body.Bytes())); err != nil {
				t.Fatalf("invalid public WebP: %v", err)
			}
		}
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("duplicate or temporary assets remain: %d", len(entries))
	}
	storage.revoked = true
	for _, route := range []string{"", "/hero.webp", "/hero.png"} {
		gone := httptest.NewRecorder()
		router.ServeHTTP(gone, httptest.NewRequest("GET", "/d/"+response.Data.ShareID+route, nil))
		if gone.Code != 410 || gone.Header().Get("Cache-Control") != dailyRadarMissingCache {
			t.Fatalf("revoked route %s was served: %d", route, gone.Code)
		}
	}
}

func TestDailyRadarFailureCleansAssets(t *testing.T) {
	handler, router, storage, root := newDailyRadarTestHandler(t)
	storage.failInsert = true
	handler.encode = func(context.Context, []byte) (render.DailyRadarHero, error) {
		return render.DailyRadarHero{WebP: []byte("webp"), PNG: []byte("png")}, nil
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, dailyRadarMultipart(t, dailyRadarTestSnapshot(), dailyRadarTestPNG(t)))
	entries, _ := os.ReadDir(root)
	if result.Code != 500 || len(entries) != 0 || strings.Contains(result.Body.String(), "internal path") {
		t.Fatalf("failed commit leaked assets or details: %d assets=%d %s", result.Code, len(entries), result.Body.String())
	}
	handler.encode = func(context.Context, []byte) (render.DailyRadarHero, error) {
		return render.DailyRadarHero{}, errors.New("encoder failed")
	}
	result = httptest.NewRecorder()
	router.ServeHTTP(result, dailyRadarMultipart(t, dailyRadarTestSnapshot(), dailyRadarTestPNG(t)))
	entries, _ = os.ReadDir(root)
	if result.Code != 500 || len(entries) != 0 {
		t.Fatal("failed encoding published assets")
	}
}

func TestDailyRadarAuthRateLimitsAndMissingPage(t *testing.T) {
	handler, router, _, _ := newDailyRadarTestHandler(t)
	unauthorized := dailyRadarMultipart(t, dailyRadarTestSnapshot(), dailyRadarTestPNG(t))
	unauthorized.Header.Del("Authorization")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, unauthorized)
	if result.Code != 401 {
		t.Fatalf("missing auth accepted: %d", result.Code)
	}
	now := time.Now()
	for range 10 {
		if !handler.allowCreate("Bearer isolated-token", now) {
			t.Fatal("valid quota rejected")
		}
	}
	if handler.allowCreate("Bearer isolated-token", now) ||
		!handler.allowCreate("Bearer different-token", now) ||
		!handler.allowCreate("Bearer isolated-token", now.Add(time.Minute)) {
		t.Fatal("per-token isolation or reset failed")
	}
	for range 10 {
		request := httptest.NewRequest("POST", "/api/v1/daily-radar/shares", nil)
		request.Header.Set("Authorization", "Bearer test-token")
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	request := httptest.NewRequest("POST", "/api/v1/daily-radar/shares", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	result = httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != 429 || result.Header().Get("Retry-After") == "" {
		t.Fatalf("missing rate limit: %d", result.Code)
	}
	result = httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest("GET", "/d/missing", nil))
	if result.Code != 404 || result.Header().Get("Cache-Control") != dailyRadarMissingCache {
		t.Fatalf("missing share: %d", result.Code)
	}
}

func TestDailyRadarConcurrentDuplicateLeavesOneAssetDirectory(t *testing.T) {
	handler, router, _, root := newDailyRadarTestHandler(t)
	handler.encode = func(context.Context, []byte) (render.DailyRadarHero, error) {
		return render.DailyRadarHero{WebP: []byte("webp"), PNG: []byte("png")}, nil
	}
	uploaded := dailyRadarTestPNG(t)
	requests := []*http.Request{
		dailyRadarMultipart(t, dailyRadarTestSnapshot(), uploaded),
		dailyRadarMultipart(t, dailyRadarTestSnapshot(), uploaded),
	}
	results := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	var workers sync.WaitGroup
	for index := range requests {
		workers.Go(func() { router.ServeHTTP(results[index], requests[index]) })
	}
	workers.Wait()
	for _, result := range results {
		if result.Code != 200 {
			t.Fatalf("duplicate failed: %d %s", result.Code, result.Body.String())
		}
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("concurrent share leaked %d directories", len(entries))
	}
}

func TestDailyRadarRequestValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		mutate func(*model.DailyRadarRequest)
	}{
		{"missing identity", 400, func(request *model.DailyRadarRequest) { request.Report.Revision = "" }},
		{"markdown limit", 413, func(request *model.DailyRadarRequest) { request.Report.Markdown = strings.Repeat("x", (1<<20)+1) }},
		{"project limit", 400, func(request *model.DailyRadarRequest) { request.Projects = make([]model.DailyRadarProject, 101) }},
		{"source limit", 400, func(request *model.DailyRadarRequest) { request.Report.Sources = make([]model.DailyRadarSource, 201) }},
		{"unsafe source", 400, func(request *model.DailyRadarRequest) { request.Report.Sources[0].URL = "javascript:alert(1)" }},
		{"invalid repo", 400, func(request *model.DailyRadarRequest) { request.Projects[0].Repo = "../../secret" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := dailyRadarTestSnapshot()
			test.mutate(&snapshot)
			if status, err := validateDailyRadarRequest(snapshot); status != test.status || err == nil {
				t.Fatalf("expected %d rejection, got %d %v", test.status, status, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		hero   []byte
		status int
	}{
		{"invalid image", []byte("not PNG"), 415},
		{"large image", make([]byte, render.DailyRadarPNGMax+1), 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, router, _, _ := newDailyRadarTestHandler(t)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, dailyRadarMultipart(t, dailyRadarTestSnapshot(), test.hero))
			if result.Code != test.status {
				t.Fatalf("got %d: %s", result.Code, result.Body.String())
			}
		})
	}
}
