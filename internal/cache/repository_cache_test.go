package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starcat-app/starcat-sharing-api/internal/model"
)

func TestRepositoryCacheCoalescesConcurrentLoads(t *testing.T) {
	cache := NewRepositoryCache(time.Minute, 8)
	var loads atomic.Int32
	loader := func(context.Context) (model.RepositoryPreview, error) {
		loads.Add(1)
		time.Sleep(20 * time.Millisecond)
		return model.RepositoryPreview{ID: 42, FullName: "owner/repo"}, nil
	}

	var waitGroup sync.WaitGroup
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			value, err := cache.GetOrLoad(context.Background(), "owner/repo", loader)
			if err != nil || value.ID != 42 {
				t.Errorf("unexpected cache result: value=%+v err=%v", value, err)
			}
		}()
	}
	waitGroup.Wait()

	if got := loads.Load(); got != 1 {
		t.Fatalf("loader should run once, got %d", got)
	}
}

func TestRepositoryCacheDoesNotCacheErrors(t *testing.T) {
	cache := NewRepositoryCache(time.Minute, 8)
	loads := 0
	loader := func(context.Context) (model.RepositoryPreview, error) {
		loads++
		if loads == 1 {
			return model.RepositoryPreview{}, context.DeadlineExceeded
		}
		return model.RepositoryPreview{ID: 7}, nil
	}

	if _, err := cache.GetOrLoad(context.Background(), "owner/repo", loader); err == nil {
		t.Fatal("first load should fail")
	}
	value, err := cache.GetOrLoad(context.Background(), "owner/repo", loader)
	if err != nil || value.ID != 7 || loads != 2 {
		t.Fatalf("error must not be cached: value=%+v loads=%d err=%v", value, loads, err)
	}
}
