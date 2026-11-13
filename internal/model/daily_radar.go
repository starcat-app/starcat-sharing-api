// 本文件定义日报公开快照的白名单 DTO，不接收账号、Stars 状态、笔记或服务凭据。
package model

import "time"

// DailyRadarRequest 只保存点击分享时的报告与公开项目，不在访问页面时刷新内容。
type DailyRadarRequest struct {
	Report   DailyRadarReport    `json:"report"`
	Projects []DailyRadarProject `json:"projects"`
}

// DailyRadarReport 的身份由 ID、Language、Revision 共同确定。
type DailyRadarReport struct {
	ID             string             `json:"id"`
	Date           string             `json:"date"`
	CategoryID     string             `json:"category_id"`
	Language       string             `json:"language"`
	Revision       string             `json:"revision"`
	Title          string             `json:"title"`
	Subtitle       string             `json:"subtitle"`
	Summary        string             `json:"summary"`
	ReadingMinutes int                `json:"reading_minutes"`
	Markdown       string             `json:"markdown"`
	Sources        []DailyRadarSource `json:"sources"`
}

// DailyRadarSource 是报告自带来源，不根据客户端浏览历史补充。
type DailyRadarSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// DailyRadarProject 显式排除用户私有数据及可被用作任意导航地址的字段。
type DailyRadarProject struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Stars       int    `json:"stars"`
	AvatarURL   string `json:"avatar_url"`
}

// DailyRadarShare 与两种资产共同提交；HeroHash 记录上传原图以判定同 revision 冲突。
type DailyRadarShare struct {
	ID          string
	Request     DailyRadarRequest
	ContentHash string
	HeroHash    string
	CreatedAt   time.Time
	RevokedAt   *time.Time
	Assets      []DailyRadarAsset
}

// DailyRadarAsset.Path 是相对受控资产根目录的路径，不接受客户端文件名。
type DailyRadarAsset struct {
	Kind        string
	Path        string
	ContentHash string
	Bytes       int
	Width       int
	Height      int
}

// DailyRadarShareResponse 沿用 v1 envelope 里的 snake_case 字段契约。
type DailyRadarShareResponse struct {
	ShareID       string    `json:"share_id"`
	ShareURL      string    `json:"share_url"`
	AlreadyExists bool      `json:"already_exists"`
	HeroWebPURL   string    `json:"hero_webp_url"`
	HeroPNGURL    string    `json:"hero_png_url"`
	CreatedAt     time.Time `json:"created_at"`
}
