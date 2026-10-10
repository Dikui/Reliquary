package limiter

import (
	"context"
	"sort"
	"sync"

	"reliquary/internal/types"
)

// The concurrency governor is process-wide, shared by every model-client layer
// that fronts a provider (chat). Keeping the singleton here, rather than
// inside one client package — lets all of them gate against the same limiter
// and per-model limit without importing each other. Wired once at startup (see
// container.registerModelConcurrencyLimiter) via SetGovernor.
var (
	governorMu sync.RWMutex
	governor   ModelConcurrencyLimiter
	governorN  int
)

// SetGovernor installs the process-wide background concurrency governor and the
// default per-model limit. Passing a nil limiter or a non-positive limit
// disables governance (all calls pass through). Safe to call at startup.
func SetGovernor(l ModelConcurrencyLimiter, limit int) {
	governorMu.Lock()
	defer governorMu.Unlock()
	governor = l
	governorN = limit
	if local, ok := l.(*localLimiter); ok {
		local.setDefaultLimit(limit)
	}
}

// SetGlobalLimit updates ONLY the process-wide default per-model limit,
// leaving the installed limiter backend intact. Used by the system-settings
// runtime bridge so an operator can retune model.max_concurrency without a
// restart. A non-positive value disables the default (models that carry their
// own MaxConcurrency still honour it).
func SetGlobalLimit(limit int) {
	governorMu.Lock()
	defer governorMu.Unlock()
	governorN = limit
	if local, ok := governor.(*localLimiter); ok {
		local.setDefaultLimit(limit)
	}
}

// Gate acquires a per-model concurrency slot using the process-wide default
// limit. Equivalent to GateN(ctx, modelID, 0).
func Gate(ctx context.Context, modelID string) func() {
	return GateN(ctx, modelID, 0)
}

// GateN acquires a per-model concurrency slot when the call is a background task
// (see types.IsBackgroundTask) and a governor is installed. modelLimit is the
// model's own configured cap; a value <= 0 means "fall back to the process-wide
// default" (governorN). It returns a release func that is ALWAYS safe to call:
// on the passthrough / fail-open paths it is a cheap no-op. The gate never
// blocks a call permanently: a cancelled context fails open.
func GateN(ctx context.Context, modelID string, modelLimit int) func() {
	return GateNamedN(ctx, modelID, "", modelLimit)
}

func GateNamedN(ctx context.Context, modelID, modelName string, modelLimit int) func() {
	modelID = BudgetKey(ctx, modelID)
	governorMu.RLock()
	l, defaultLimit := governor, governorN
	governorMu.RUnlock()

	limit := modelLimit
	if limit <= 0 {
		limit = defaultLimit
	}
	if l == nil || limit <= 0 || !types.IsBackgroundTask(ctx) {
		return noop
	}
	if named, ok := l.(interface{ SetModelName(string, string) }); ok {
		named.SetModelName(modelID, modelName)
	}
	var release func()
	var err error
	if local, ok := l.(*localLimiter); ok {
		release, err = local.acquire(ctx, modelID, limit, modelLimit <= 0)
	} else {
		release, err = l.Acquire(ctx, modelID, limit)
	}
	if err != nil || release == nil {
		return noop
	}
	return release
}

// RuntimeStats returns the semaphores observed by this process.
func RuntimeStats(ctx context.Context) ([]RuntimeStat, bool, error) {
	governorMu.RLock()
	l := governor
	governorMu.RUnlock()
	inspector, ok := l.(runtimeInspectable)
	if !ok || inspector == nil {
		return []RuntimeStat{}, false, nil
	}
	stats, err := inspector.RuntimeStats(ctx)
	if stats == nil {
		stats = []RuntimeStat{}
	}
	return stats, true, err
}

// localLimiter is an in-process (single-node) counting semaphore keyed by
// model ID shared by background model calls in this process.
type localLimiter struct {
	mu      sync.Mutex
	sems    map[string]*localSemaphore
	tracked map[string]*trackedSemaphore
}

type localSemaphore struct {
	active      int
	limit       int
	usesDefault bool
	changed     chan struct{}
}

// Wake existing waiters while keeping holders attached to the same state.
func (s *localSemaphore) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (l *localLimiter) setDefaultLimit(limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, sem := range l.sems {
		if sem.usesDefault && sem.limit != limit {
			sem.limit = limit
			l.tracked[key].limit.Store(int64(limit))
			sem.notify()
		}
	}
}

// NewLocalLimiter builds an in-process per-key concurrency limiter.
func NewLocalLimiter() ModelConcurrencyLimiter {
	return &localLimiter{sems: make(map[string]*localSemaphore), tracked: make(map[string]*trackedSemaphore)}
}

func (l *localLimiter) Acquire(ctx context.Context, key string, limit int) (func(), error) {
	return l.acquire(ctx, key, limit, false)
}

func (l *localLimiter) acquire(ctx context.Context, key string, limit int, usesDefault bool) (func(), error) {
	if usesDefault {
		// Serialize registration with hot updates so an old Gate snapshot
		// cannot restore the previous default after settings have changed.
		governorMu.RLock()
		limit = governorN
	}
	if l == nil || limit <= 0 || key == "" {
		if usesDefault {
			governorMu.RUnlock()
		}
		return noop, nil
	}

	l.mu.Lock()
	sem := l.sems[key]
	tracked := l.tracked[key]
	if tracked == nil {
		tracked = &trackedSemaphore{}
		l.tracked[key] = tracked
	}
	tracked.limit.Store(int64(limit))
	if sem == nil {
		sem = &localSemaphore{limit: limit, changed: make(chan struct{})}
		l.sems[key] = sem
	} else if sem.limit != limit {
		sem.limit = limit
		sem.notify()
	}
	sem.usesDefault = usesDefault
	if usesDefault {
		governorMu.RUnlock()
	}
	tracked.waiting.Add(1)
	defer tracked.waiting.Add(-1)

	for {
		if ctx.Err() != nil {
			l.mu.Unlock()
			return noop, nil // Preserve the governor's cancellation fail-open contract.
		}
		if sem.limit <= 0 || sem.active < sem.limit {
			sem.active++
			l.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					l.mu.Lock()
					defer l.mu.Unlock()
					sem.active--
					sem.notify()
				})
			}, nil
		}
		changed := sem.changed
		l.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		l.mu.Lock()
	}
}

func (l *localLimiter) RuntimeStats(_ context.Context) ([]RuntimeStat, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	stats := make([]RuntimeStat, 0, len(l.sems))
	for modelID, sem := range l.sems {
		tracked := l.tracked[modelID]
		name, _ := tracked.name.Load().(string)
		stats = append(stats, RuntimeStat{ModelID: modelID, Name: name, Active: int64(sem.active), Waiting: tracked.waiting.Load(), Limit: sem.limit})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].ModelID < stats[j].ModelID })
	return stats, nil
}

func (l *localLimiter) SetModelName(modelID, name string) {
	if modelID == "" || name == "" {
		return
	}
	l.mu.Lock()
	tracked := l.tracked[modelID]
	if tracked == nil {
		tracked = &trackedSemaphore{}
		l.tracked[modelID] = tracked
	}
	l.mu.Unlock()
	tracked.name.Store(name)
}
