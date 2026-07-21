// Package cache 提供公开仓库 metadata 的进程内缓存。
//
// Nginx 负责跨进程的 stale cache；本包只解决同一 Fly machine 内重复 crawler
// 请求和并发击穿问题。缓存有容量上限，不能随任意 URL 无界增长。
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

type entry struct {
	value     model.RepositoryPreview
	expiresAt time.Time
}

type flight struct {
	done  chan struct{}
	value model.RepositoryPreview
	err   error
}

// RepositoryCache 是带请求合并的有界 TTL cache。
type RepositoryCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]entry
	inflight   map[string]*flight
}

// NewRepositoryCache 创建仓库缓存。
func NewRepositoryCache(ttl time.Duration, maxEntries int) *RepositoryCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if maxEntries <= 0 {
		maxEntries = 512
	}
	return &RepositoryCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]entry),
		inflight:   make(map[string]*flight),
	}
}

// GetOrLoad 返回新鲜缓存，或把同 key 的并发 miss 合并为一次 loader 调用。
func (c *RepositoryCache) GetOrLoad(
	ctx context.Context,
	key string,
	loader func(context.Context) (model.RepositoryPreview, error),
) (model.RepositoryPreview, error) {
	now := time.Now()
	c.mu.Lock()
	if cached, ok := c.entries[key]; ok && now.Before(cached.expiresAt) {
		c.mu.Unlock()
		return cached.value, nil
	}
	if pending, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return model.RepositoryPreview{}, ctx.Err()
		case <-pending.done:
			return pending.value, pending.err
		}
	}

	pending := &flight{done: make(chan struct{})}
	c.inflight[key] = pending
	c.mu.Unlock()

	value, err := loader(ctx)

	c.mu.Lock()
	pending.value = value
	pending.err = err
	if err == nil {
		c.evictExpiredOrOldestLocked(now)
		c.entries[key] = entry{value: value, expiresAt: now.Add(c.ttl)}
	}
	delete(c.inflight, key)
	close(pending.done)
	c.mu.Unlock()
	return value, err
}

func (c *RepositoryCache) evictExpiredOrOldestLocked(now time.Time) {
	for key, value := range c.entries {
		if !now.Before(value.expiresAt) {
			delete(c.entries, key)
		}
	}
	if len(c.entries) < c.maxEntries {
		return
	}

	// entry 不保存访问时间；达到上限时淘汰最早过期项，保证实现简单且有界。
	var oldestKey string
	var oldestExpiry time.Time
	for key, value := range c.entries {
		if oldestKey == "" || value.expiresAt.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = value.expiresAt
		}
	}
	delete(c.entries, oldestKey)
}
