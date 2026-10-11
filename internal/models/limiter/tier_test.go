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
	entityResolution := types.WithModelTier(withBackground(), types.ModelTierEntityResolution)
	light := types.WithModelTier(withBackground(), types.ModelTierLight)
	held := Gate(entityResolution, "shared-model")
	defer held()
	ready := make(chan func(), 1)
	go func() { ready <- Gate(light, "shared-model") }()
	select {
	case release := <-ready:
		release()
	case <-time.After(time.Second):
		t.Fatal("entity resolution must not block light calls")
	}
	require.NoError(t, WaitRate(entityResolution, "tier-rate-test", 60))
	require.NoError(t, WaitRate(light, "tier-rate-test", 60))
	ctx, cancel := context.WithTimeout(entityResolution, 10*time.Millisecond)
	defer cancel()
	require.Error(t, WaitRate(ctx, "tier-rate-test", 60), "a second entity resolution call must consume its rate budget")
}
