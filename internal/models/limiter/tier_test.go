package limiter

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestModelTierConcurrencyAndRateAreIndependent(t *testing.T) {
	SetGovernor(NewLocalLimiter(), 1)
	t.Cleanup(func() { SetGovernor(nil, 0) })
	extraction := types.WithModelTier(withBackground(), types.ModelTierExtraction)
	light := types.WithModelTier(withBackground(), types.ModelTierLight)
	held := Gate(extraction, "shared-model")
	defer held()
	ready := make(chan func(), 1)
	go func() { ready <- Gate(light, "shared-model") }()
	select {
	case release := <-ready:
		release()
	case <-time.After(time.Second):
		t.Fatal("extraction must not block light calls")
	}
	require.NoError(t, WaitRate(extraction, "tier-rate-test", 60))
	require.NoError(t, WaitRate(light, "tier-rate-test", 60))
	ctx, cancel := context.WithTimeout(extraction, 10*time.Millisecond)
	defer cancel()
	require.Error(t, WaitRate(ctx, "tier-rate-test", 60), "a second extraction call must consume extraction rate budget")
}
