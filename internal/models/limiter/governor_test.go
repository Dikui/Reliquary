package limiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func withBackground() context.Context {
	return types.WithBackgroundTask(context.Background())
}

// TestGateOnlyGovernsBackground verifies interactive calls always pass through
// (no-op release) and only background calls consult the installed limiter.
func TestGateOnlyGovernsBackground(t *testing.T) {
	t.Cleanup(func() { SetGovernor(nil, 0) })
	SetGovernor(NewLocalLimiter(), 1)

	// Interactive (non-background) ctx: never gated, even at limit 1.
	rel1 := Gate(context.Background(), "m")
	rel2 := Gate(context.Background(), "m")
	rel1()
	rel2()

	// Background ctx: gated. First slot taken; second would block, so a
	// cancelled second acquire must fail open (return a usable release).
	held := Gate(withBackground(), "m")
	defer held()

	ctx, cancel := context.WithTimeout(withBackground(), 30*time.Millisecond)
	defer cancel()
	rel := Gate(ctx, "m")
	if rel == nil {
		t.Fatal("cancelled background gate must fail open with a usable release")
	}
	rel()
}

// TestGateDisabledWhenNoGovernor verifies Gate is a passthrough when no
// governor is installed.
func TestGateDisabledWhenNoGovernor(t *testing.T) {
	SetGovernor(nil, 0)
	if rel := Gate(withBackground(), "m"); rel == nil {
		t.Fatal("gate with no governor must return a usable release")
	} else {
		rel()
	}
}

// TestLocalLimiterCaps verifies the in-process limiter enforces the cap and
// releases slots.
func TestLocalLimiterCaps(t *testing.T) {
	l := NewLocalLimiter()
	ctx := context.Background()

	r1, _ := l.Acquire(ctx, "k", 2)
	r2, _ := l.Acquire(ctx, "k", 2)

	// Third acquire must block while full.
	done := make(chan func(), 1)
	go func() {
		r3, _ := l.Acquire(ctx, "k", 2)
		done <- r3
	}()
	select {
	case <-done:
		t.Fatal("third acquire should block while at limit")
	case <-time.After(50 * time.Millisecond):
	}

	r1() // free a slot; the waiter proceeds
	select {
	case r3 := <-done:
		r3()
	case <-time.After(time.Second):
		t.Fatal("waiter should proceed after a slot frees")
	}
	r2()
}

// TestLocalLimiterIndependentKeys verifies per-key budgets are independent.
func TestLocalLimiterIndependentKeys(t *testing.T) {
	l := NewLocalLimiter()
	ctx := context.Background()
	ra, _ := l.Acquire(ctx, "a", 1)
	rb, _ := l.Acquire(ctx, "b", 1) // different key: must not block
	ra()
	rb()
}

// TestLocalLimiterFailOpen verifies degraded inputs pass through.
func TestLocalLimiterFailOpen(t *testing.T) {
	l := NewLocalLimiter()
	ctx := context.Background()
	if r, err := l.Acquire(ctx, "", 4); err != nil || r == nil {
		t.Fatal("empty key should fail open")
	} else {
		r()
	}
	if r, err := l.Acquire(ctx, "k", 0); err != nil || r == nil {
		t.Fatal("limit<=0 should fail open")
	} else {
		r()
	}
}

// TestLocalLimiterReleaseIdempotent verifies double release doesn't
// over-free a slot.
func TestLocalLimiterReleaseIdempotent(t *testing.T) {
	l := NewLocalLimiter()
	ctx := context.Background()
	r1, _ := l.Acquire(ctx, "k", 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r1() }()
	go func() { defer wg.Done(); r1() }()
	wg.Wait()

	// After idempotent release the single slot is free again.
	r2, _ := l.Acquire(ctx, "k", 1)
	done := make(chan struct{})
	go func() { r3, _ := l.Acquire(ctx, "k", 1); r3(); close(done) }()
	select {
	case <-done:
		t.Fatal("a double-release must not leave the slot permanently free")
	case <-time.After(50 * time.Millisecond):
	}
	r2()
	<-done
}

func waitForLocalWaiting(t *testing.T, l *localLimiter, waiting int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		stats, _ := l.RuntimeStats(context.Background())
		return len(stats) == 1 && stats[0].Waiting == waiting
	}, time.Second, time.Millisecond)
}

func receiveRelease(t *testing.T, ready <-chan func()) func() {
	t.Helper()
	select {
	case release := <-ready:
		return release
	case <-time.After(time.Second):
		t.Fatal("waiting acquire did not resume")
		return nil
	}
}

func TestLocalLimiterHotLimitsWakeExistingWaiters(t *testing.T) {
	l := NewLocalLimiter().(*localLimiter)
	SetGovernor(l, 1)
	t.Cleanup(func() { SetGovernor(nil, 0) })
	r1 := Gate(withBackground(), "m")
	t.Cleanup(r1)
	ready := make(chan func(), 1)
	go func() { ready <- Gate(withBackground(), "m") }()
	waitForLocalWaiting(t, l, 1)
	SetGlobalLimit(2) // No new Acquire is needed to wake this waiter.
	r2 := receiveRelease(t, ready)
	t.Cleanup(r2)
	go func() { ready <- Gate(withBackground(), "m") }()
	waitForLocalWaiting(t, l, 1)
	SetGlobalLimit(1)
	r1()
	r1() // Old holders still release once after both limit changes.
	select {
	case release := <-ready:
		release()
		t.Fatal("lowered limit must wait until active falls below one")
	case <-time.After(30 * time.Millisecond):
	}
	stats, _ := l.RuntimeStats(context.Background())
	require.Equal(t, int64(1), stats[0].Active)
	require.Equal(t, 1, stats[0].Limit)
	r2()
	r3 := receiveRelease(t, ready)
	t.Cleanup(r3)
	go func() { ready <- Gate(withBackground(), "m") }()
	waitForLocalWaiting(t, l, 1)
	SetGlobalLimit(0) // Disabling the default must release existing waiters too.
	r4 := receiveRelease(t, ready)
	r3()
	r4()
	stats, _ = l.RuntimeStats(context.Background())
	require.Zero(t, stats[0].Active)
	require.Zero(t, stats[0].Waiting)
	require.Zero(t, stats[0].Limit)

	// A model's explicit cap is independent of the global default.
	explicit := GateNamedN(withBackground(), "m", "", 1)
	t.Cleanup(explicit)
	go func() { ready <- GateNamedN(withBackground(), "m", "", 1) }()
	waitForLocalWaiting(t, l, 1)
	SetGlobalLimit(3)
	stats, _ = l.RuntimeStats(context.Background())
	require.Equal(t, 1, stats[0].Limit)
	select {
	case release := <-ready:
		release()
		t.Fatal("global limit must not override the model's explicit cap")
	case <-time.After(30 * time.Millisecond):
	}
	explicit()
	receiveRelease(t, ready)()
}

func TestLocalLimiterCancelledWaitDoesNotTakeOrReleaseSlot(t *testing.T) {
	l := NewLocalLimiter().(*localLimiter)
	held, err := l.Acquire(context.Background(), "m", 1)
	require.NoError(t, err)
	t.Cleanup(held)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan func(), 1)
	go func() {
		release, err := l.Acquire(ctx, "m", 1)
		require.NoError(t, err)
		ready <- release
	}()
	waitForLocalWaiting(t, l, 1)
	cancel()
	release := receiveRelease(t, ready)
	release()
	release()
	stats, _ := l.RuntimeStats(context.Background())
	require.Equal(t, int64(1), stats[0].Active)
	require.Zero(t, stats[0].Waiting)
	held()
	release, err = l.Acquire(ctx, "m", 1) // Already cancelled with a free slot.
	require.NoError(t, err)
	release()
	stats, _ = l.RuntimeStats(context.Background())
	require.Zero(t, stats[0].Active)
}
