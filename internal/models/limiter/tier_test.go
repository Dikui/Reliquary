package limiter

import (
	"context"
	"reliquary/internal/types"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelTierConcurrencyAndRateAreIndependent(t *testing.T) {
	SetGovernor(NewLocalLimiter(), 1)
	t.Cleanup(func() { SetGovernor(nil, 0) })
	generation := types.WithModelTier(withBackground(), types.ModelTierGeneration)
	light := types.WithModelTier(withBackground(), types.ModelTierLight)
	held := Gate(generation, "shared-model")
	defer held()
	ready := make(chan func(), 1)
	go func() { ready <- Gate(light, "shared-model") }()
	select {
	case release := <-ready:
		release()
	case <-time.After(time.Second):
		t.Fatal("generation must not block light calls")
	}
	require.NoError(t, WaitRate(generation, "tier-rate-test", 60))
	require.NoError(t, WaitRate(light, "tier-rate-test", 60))
	ctx, cancel := context.WithTimeout(generation, 10*time.Millisecond)
	defer cancel()
	require.Error(t, WaitRate(ctx, "tier-rate-test", 60), "a second generation call must consume its rate budget")
}
