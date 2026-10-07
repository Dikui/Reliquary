package limiter

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// BudgetKey isolates provider concurrency and request rate by processing tier.
func BudgetKey(ctx context.Context, modelID string) string {
	if tier := types.ModelTierFromContext(ctx); tier != "" {
		return modelID + ":" + string(tier)
	}
	return modelID
}

var rates = struct {
	sync.Mutex
	entries map[string]*rate.Limiter
}{entries: map[string]*rate.Limiter{}}

// WaitRate applies an optional requests-per-minute limit to each model tier.
func WaitRate(ctx context.Context, modelID string, requestsPerMinute int) error {
	if requestsPerMinute <= 0 || !types.IsBackgroundTask(ctx) {
		return nil
	}
	key := BudgetKey(ctx, modelID)
	governorMu.RLock()
	backend, _ := governor.(*redisLimiter)
	governorMu.RUnlock()
	if backend != nil && backend.rdb != nil {
		wait, err := reserveRateSlot.Run(ctx, backend.rdb, []string{"reliquary:modelrate:" + key}, (60000+requestsPerMinute-1)/requestsPerMinute).Int64()
		if err == nil {
			if wait <= 0 {
				return nil
			}
			timer := time.NewTimer(time.Duration(wait) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		logger.Warnf(ctx, "[ModelLimiter] request rate backend failed for %s: %v", key, err)
	}
	rates.Lock()
	entry := rates.entries[key]
	limit := rate.Limit(float64(requestsPerMinute) / 60)
	if entry == nil {
		entry = rate.NewLimiter(limit, 1)
		rates.entries[key] = entry
	} else if entry.Limit() != limit {
		entry.SetLimitAt(time.Now(), limit)
	}
	rates.Unlock()
	return entry.Wait(ctx)
}

// Each replica reserves a start time from the same model and tier schedule.
var reserveRateSlot = redis.NewScript(`
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local interval = math.max(1, tonumber(ARGV[1]))
local next_slot = math.max(now, tonumber(redis.call('GET', KEYS[1]) or '0'))
redis.call('SET', KEYS[1], next_slot + interval, 'PX', next_slot - now + interval * 2)
return next_slot - now
`)
