// Package store 定义分享数据持久化接口。
//
// R-01 v1.2: 从内存 + JSON 文件升级到 SQLite。
// Store 接口用于解耦 handler 与具体存储实现，便于单测 mock。
package store

import (
	"math/rand"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

// Store 定义分享数据访问接口。
type Store interface {
	// Upsert 创建或更新分享记录（按 id 幂等）。
	Upsert(data model.ShareData) error

	// Get 按 id 获取分享数据。未找到返回 nil。
	Get(id string) (*model.ShareData, error)

	// CountShares 返回当前 shares 表中的分享记录总数。
	//
	// 本地 admin 面板只需要真实总量，不读取 payload 明细，避免为了统计把
	// repo_json / ai_summary_json 拉出来反序列化。
	CountShares() (int, error)

	// Close 关闭数据库连接。
	Close() error
}

// NewID 生成指定长度的随机 base62 短链 ID。
func NewID(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
