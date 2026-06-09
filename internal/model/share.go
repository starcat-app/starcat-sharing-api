// Package model 定义分享功能的数据模型
package model

import "time"

// ShareRepoDTO 仓库基本信息 DTO
type ShareRepoDTO struct {
	FullName    string   `json:"fullName"`
	Description *string  `json:"description"`
	Language    *string  `json:"language"`
	StarsCount  int      `json:"starsCount"`
	ForksCount  int      `json:"forksCount"`
	Topics      []string `json:"topics"`
	Homepage    *string  `json:"homepage"`
	URL         string   `json:"url"`
}

// ShareTagDTO AI 推荐的标签 DTO
type ShareTagDTO struct {
	Name       string   `json:"name"`
	Confidence *float64 `json:"confidence"`
}

// ShareAISummaryDTO AI 摘要 DTO
type ShareAISummaryDTO struct {
	OneLiner      string        `json:"oneLiner"`
	Summary       string        `json:"summary"`
	Platforms     []string      `json:"platforms"`
	SuitableFor   []string      `json:"suitableFor"`
	Strengths     []string      `json:"strengths"`
	Risks         []string      `json:"risks"`
	SuggestedTags []ShareTagDTO `json:"suggestedTags"`
}

// ShareRepoRequest 分享请求结构
type ShareRepoRequest struct {
	Repo      ShareRepoDTO      `json:"repo"`
	AISummary ShareAISummaryDTO `json:"aiSummary"`
}

// ShareResponseDTO 分享响应 DTO。
// R-01 v1.2: 扩 shareId + createdAt 字段，expiresAt 改为 nullable 指针。
type ShareResponseDTO struct {
	ShareURL  string     `json:"shareUrl"`
	ShareID   string     `json:"shareId"`
	ExpiresAt *time.Time `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// ShareCreateResponse 是 POST /api/v1/share 的 data 内 payload。
type ShareCreateResponse struct {
	ShareURL  string     `json:"shareUrl"`
	ShareID   string     `json:"shareId"`
	ExpiresAt *time.Time `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// ShareData 分享数据完整结构
type ShareData struct {
	ID        string           `json:"id"`
	Request   ShareRepoRequest `json:"request"`
	CreatedAt time.Time        `json:"createdAt"`
	ExpiresAt time.Time        `json:"expiresAt"`
}
