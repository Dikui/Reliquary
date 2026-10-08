// Package limiter provides process-local concurrency limits for outbound model calls.
package limiter

import (
	"context"
	"sync/atomic"
)

// ModelConcurrencyLimiter caps the number of concurrent in-flight calls per
// key (typically a model ID) within the backend process.
type ModelConcurrencyLimiter interface {
	// Acquire blocks until a slot for key is available or ctx is done. It
	// returns a release func that MUST be invoked to free the slot. On any
	// ctx cancellation it fails open: release is a no-op and
	// err is nil, so callers proceed without a slot rather than dropping the
	// call.
	Acquire(ctx context.Context, key string, limit int) (release func(), err error)
}

// RuntimeStat is a point-in-time view of a process-local model semaphore.
type RuntimeStat struct {
	ModelID string `json:"model_id"`
	Name    string `json:"name"`
	Active  int64  `json:"active"`
	Waiting int64  `json:"waiting"`
	Limit   int    `json:"limit"`
}

type runtimeInspectable interface {
	RuntimeStats(context.Context) ([]RuntimeStat, error)
}

type trackedSemaphore struct {
	limit   atomic.Int64
	waiting atomic.Int64
	name    atomic.Value // string
}

// noop is the release returned on the fail-open / passthrough paths.
func noop() {}
