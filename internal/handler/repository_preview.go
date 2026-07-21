// Package handler 提供基础仓库分享页和 OG 图片的公开 HTTP 路由。
//
// 这两个路由与 AI `/s/{id}` 共用服务，但不读写 sharing.db。所有输入必须先
// 校验，再进入固定 GitHub API origin，避免把分享页变成开放代理或 SSRF 入口。
package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/cache"
	githubclient "github.com/starcat-app/starcat-sharing-api/internal/github"
	"github.com/starcat-app/starcat-sharing-api/internal/model"
	"github.com/starcat-app/starcat-sharing-api/internal/render"
)

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// RepositoryHandler 承担 `/r/*` HTML 和 `/og/repo/*` PNG。
type RepositoryHandler struct {
	source       githubclient.RepositoryClient
	cache        *cache.RepositoryCache
	renderer     render.RepositoryOGRenderer
	templates    *template.Template
	baseURL      *url.URL
	avatarClient *http.Client
}

// NewRepositoryHandler 创建基础仓库分享 handler。
func NewRepositoryHandler(
	source githubclient.RepositoryClient,
	repositoryCache *cache.RepositoryCache,
	renderer render.RepositoryOGRenderer,
	templates *template.Template,
	baseURL string,
) (*RepositoryHandler, error) {
	parsedBaseURL, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("invalid BASE_URL %q", baseURL)
	}
	return &RepositoryHandler{
		source:       source,
		cache:        repositoryCache,
		renderer:     renderer,
		templates:    templates,
		baseURL:      parsedBaseURL,
		avatarClient: &http.Client{Timeout: 4 * time.Second},
	}, nil
}

// HandleRepositoryPage GET /r/{owner}/{repo}，返回首屏即含 OG metadata 的 HTML。
func (h *RepositoryHandler) HandleRepositoryPage(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := repositoryPathValues(r)
	if !ok {
		h.renderUnavailablePage(w, r, http.StatusNotFound, owner, name)
		return
	}

	repository, err := h.resolveRepository(r.Context(), owner, name)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, githubclient.ErrRepositoryUnavailable) {
			status = http.StatusNotFound
		} else if errors.Is(err, githubclient.ErrRateLimited) {
			status = http.StatusServiceUnavailable
		}
		h.renderUnavailablePage(w, r, status, owner, name)
		return
	}

	data := h.makePageData(repository, true, http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300, s-maxage=3600, stale-if-error=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.executeRepositoryTemplate(w, http.StatusOK, data)
}

// HandleRepositoryOG GET /og/repo/{owner}/{repo}.png，始终返回可用 PNG fallback。
func (h *RepositoryHandler) HandleRepositoryOG(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := strings.TrimSuffix(r.PathValue("repo"), ".png")
	if !validRepositoryPath(owner, name) || !strings.HasSuffix(r.PathValue("repo"), ".png") {
		h.writeFallbackOG(w, owner, name)
		return
	}

	repository, err := h.resolveRepository(r.Context(), owner, name)
	if err != nil {
		h.writeFallbackOG(w, owner, name)
		return
	}
	avatar := h.fetchAvatar(r.Context(), repository.AvatarURL)
	pngBytes, err := h.renderer.Render(repository, avatar)
	if err != nil {
		h.writeFallbackOG(w, owner, name)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=86400, stale-if-error=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pngBytes)
}

func (h *RepositoryHandler) resolveRepository(ctx context.Context, owner, name string) (model.RepositoryPreview, error) {
	key := strings.ToLower(owner + "/" + name)
	return h.cache.GetOrLoad(ctx, key, func(loadContext context.Context) (model.RepositoryPreview, error) {
		return h.source.FetchRepository(loadContext, owner, name)
	})
}

func (h *RepositoryHandler) makePageData(repository model.RepositoryPreview, available bool, statusCode int) model.RepositoryPageData {
	canonical := *h.baseURL
	canonical.Path = "/r/" + repository.Owner + "/" + repository.Name
	canonical.RawQuery = url.Values{
		"v":   {"1"},
		"rid": {strconv.FormatInt(repository.ID, 10)},
	}.Encode()

	ogImage := *h.baseURL
	ogImage.Path = "/og/repo/" + repository.Owner + "/" + repository.Name + ".png"
	revision := strconv.FormatInt(repository.ID, 10)
	if !repository.UpdatedAt.IsZero() {
		revision += "-" + strconv.FormatInt(repository.UpdatedAt.Unix(), 10)
	}
	ogImage.RawQuery = url.Values{"rev": {revision}}.Encode()

	openApp := &url.URL{
		Scheme: "starcat",
		Host:   "repo",
		Path:   "/" + repository.Owner + "/" + repository.Name,
		RawQuery: url.Values{
			"v":   {"1"},
			"rid": {strconv.FormatInt(repository.ID, 10)},
		}.Encode(),
	}
	description := repository.Description
	if description == "" {
		description = "Open this GitHub repository in Starcat."
	}
	return model.RepositoryPageData{
		Repository:   repository,
		Available:    available,
		StatusCode:   statusCode,
		CanonicalURL: canonical.String(),
		OGImageURL:   ogImage.String(),
		GitHubURL:    repository.HTMLURL,
		OpenAppURL:   template.URL(openApp.String()), // #nosec G203 -- URL 仅由已验证 path segment 构造。
		DownloadURL:  h.baseURL.ResolveReference(&url.URL{Fragment: "download"}).String(),
		Title:        repository.FullName + " · Starcat",
		Description:  description,
		StarsText:    compactCount(repository.Stars),
		ForksText:    compactCount(repository.Forks),
	}
}

func (h *RepositoryHandler) renderUnavailablePage(w http.ResponseWriter, r *http.Request, status int, owner, name string) {
	fullName := strings.Trim(strings.TrimSpace(owner)+"/"+strings.TrimSpace(name), "/")
	if fullName == "" {
		fullName = "GitHub repository"
	}
	repository := model.RepositoryPreview{
		Owner:       owner,
		Name:        name,
		FullName:    fullName,
		Description: "This repository does not exist or is not publicly available.",
		HTMLURL:     "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(name),
	}
	data := h.makePageData(repository, false, status)
	data.Title = "Repository unavailable · Starcat"
	data.Description = "This repository does not exist or is not publicly available."
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=300")
	h.executeRepositoryTemplate(w, status, data)
}

func (h *RepositoryHandler) executeRepositoryTemplate(w http.ResponseWriter, status int, data model.RepositoryPageData) {
	var output bytes.Buffer
	if err := h.templates.ExecuteTemplate(&output, "repository.html", data); err != nil {
		http.Error(w, "repository preview unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(output.Bytes())
}

func (h *RepositoryHandler) writeFallbackOG(w http.ResponseWriter, owner, name string) {
	fullName := strings.Trim(strings.TrimSpace(owner)+"/"+strings.TrimSpace(name), "/")
	if fullName == "" {
		fullName = "GitHub repository"
	}
	pngBytes, err := h.renderer.Render(model.RepositoryPreview{
		Owner:       owner,
		Name:        name,
		FullName:    fullName,
		Description: "Open this repository in Starcat.",
	}, nil)
	if err != nil {
		http.Error(w, "repository preview unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300, s-maxage=900")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pngBytes)
}

func (h *RepositoryHandler) fetchAvatar(ctx context.Context, rawURL string) image.Image {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".githubusercontent.com") {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil
	}
	resp, err := h.avatarClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	decoded, _, err := image.Decode(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil
	}
	return decoded
}

func repositoryPathValues(r *http.Request) (string, string, bool) {
	owner := r.PathValue("owner")
	name := r.PathValue("repo")
	return owner, name, validRepositoryPath(owner, name)
}

func validRepositoryPath(owner, name string) bool {
	return ownerPattern.MatchString(owner) &&
		!strings.HasSuffix(owner, "-") &&
		repoPattern.MatchString(name) &&
		name != "." && name != ".."
}

func compactCount(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return strconv.Itoa(value)
	}
}
