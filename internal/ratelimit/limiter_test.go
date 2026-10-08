package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalLimiterAllowsWithinBudget(t *testing.T) {
	l := New(time.Minute)
	ctx := context.Background()
	const max = 3
	for i := 0; i < max; i++ {
		if !l.Allow(ctx, "k1", max) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow(ctx, "k1", max) {
		t.Fatal("request over budget should be denied")
	}
}

func TestLocalLimiterConcurrentBudgetAndExpiration(t *testing.T) {
	l := New(20 * time.Millisecond)
	var accepted atomic.Int64
	var callers sync.WaitGroup
	for i := 0; i < 100; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if l.Allow(context.Background(), "same-key", 7) {
				accepted.Add(1)
			}
		}()
	}
	callers.Wait()
	if got := accepted.Load(); got != 7 {
		t.Fatalf("concurrent callers exceeded shared budget: %d", got)
	}
	time.Sleep(25 * time.Millisecond)
	if !l.Allow(context.Background(), "same-key", 7) {
		t.Fatal("expired requests must release the budget")
	}
}

func TestLocalLimiterIndependentKeys(t *testing.T) {
	l := New(time.Minute)
	ctx := context.Background()
	if !l.Allow(ctx, "a", 1) {
		t.Fatal("first key should be allowed")
	}
	if !l.Allow(ctx, "b", 1) {
		t.Fatal("second key should be allowed")
	}
}

func TestAllowSkipsWhenMaxZero(t *testing.T) {
	l := New(time.Minute)
	for i := 0; i < 100; i++ {
		if !l.Allow(context.Background(), "k", 0) {
			t.Fatal("max<=0 should always allow")
		}
	}
}
