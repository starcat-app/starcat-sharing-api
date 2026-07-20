// Package handler 中的 stats.go 提供本地 admin 面板使用的分享统计。
package handler

import (
	"net/http"

	"github.com/starcat-app/starcat-sharing-api/internal/store"
)

// SharingStatsResponse 是 GET /internal/stats 的 data 结构。
type SharingStatsResponse struct {
	TotalShares int `json:"total_shares"`
}

// HandleStats 返回 sharing-api 的真实分享记录总数。
func HandleStats(s store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		count, err := s.CountShares()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
				"failed to count shares: "+err.Error(), nil)
			return
		}
		writeJSON(w, SharingStatsResponse{TotalShares: count})
	}
}
