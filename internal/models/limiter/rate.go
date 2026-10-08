package limiter

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
