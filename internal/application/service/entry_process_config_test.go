package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/types"
)

func processConfigBoolPtr(v bool) *bool {
	return &v
}

func TestChunkOverlapExplicitZeroAndOmittedDefaults(t *testing.T) {
	t.Run("explicit zero", func(t *testing.T) {
		var override types.ChunkingConfig
		require.NoError(t, json.Unmarshal([]byte(`{"chunk_size":100,"chunk_overlap":0,"strategy":"legacy"}`), &override))
		stored, err := override.Value()
		require.NoError(t, err)
		var reloaded types.ChunkingConfig
		require.NoError(t, reloaded.Scan(stored))
		require.True(t, reloaded.ChunkOverlapSet)
		library := &types.Library{ChunkingConfig: types.ChunkingConfig{ChunkOverlap: 30}}
		eff := ResolveProcessConfig(library, &types.EntryProcessOverrides{ChunkingConfig: &reloaded})
		cfg := buildSplitterConfigFromChunking(eff.ChunkingConfig)
		require.Zero(t, cfg.ChunkOverlap)
		text := strings.Repeat("An individual structured record.\n\n", 20)
		chunks := chunker.Split(text, cfg)
		require.Greater(t, len(chunks), 1)
		for i := 1; i < len(chunks); i++ {
			require.GreaterOrEqual(t, chunks[i].Start, chunks[i-1].End)
		}
		parent, child := buildParentChildConfigs(eff.ChunkingConfig, cfg)
		require.Zero(t, parent.ChunkOverlap)
		require.Zero(t, child.ChunkOverlap)
		require.True(t, parent.ChunkOverlapSet)
		require.True(t, child.ChunkOverlapSet)
	})
	t.Run("omitted", func(t *testing.T) {
		var override types.ChunkingConfig
		require.NoError(t, json.Unmarshal([]byte(`{"chunk_size":100}`), &override))
		library := &types.Library{ChunkingConfig: types.ChunkingConfig{ChunkOverlap: 30}}
		eff := ResolveProcessConfig(library, &types.EntryProcessOverrides{ChunkingConfig: &override})
		require.Equal(t, 30, buildSplitterConfigFromChunking(eff.ChunkingConfig).ChunkOverlap)
		stored, err := (types.ChunkingConfig{}).Value()
		require.NoError(t, err)
		require.NotContains(t, string(stored.([]byte)), `"chunk_overlap"`)
		var reloaded types.ChunkingConfig
		require.NoError(t, reloaded.Scan(stored))
		require.False(t, reloaded.ChunkOverlapSet)
		cfg := buildSplitterConfigFromChunking(reloaded)
		require.Equal(t, chunker.DefaultChunkOverlap, cfg.ChunkOverlap)
		parent, child := buildParentChildConfigs(reloaded, cfg)
		require.Equal(t, chunker.DefaultChunkOverlap, parent.ChunkOverlap)
		require.Equal(t, child.ChunkSize/5, child.ChunkOverlap)
	})
}

func TestResolveProcessConfig_OverridesChunkSize(t *testing.T) {
	t.Parallel()

	library := &types.Library{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
	}
	overrides := &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 2048},
	}
	eff := ResolveProcessConfig(library, overrides)
	require.Equal(t, 2048, eff.ChunkingConfig.ChunkSize)
	require.Equal(t, 50, eff.ChunkingConfig.ChunkOverlap)
}

func TestResolveProcessConfig_OverrideTogglesParentChild(t *testing.T) {
	t.Parallel()

	// Library has parent-child on; override snapshot turns it off.
	libraryOn := &types.Library{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, EnableParentChild: true},
	}
	effOff := ResolveProcessConfig(libraryOn, &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 512, EnableParentChild: false},
	})
	require.False(t, effOff.ChunkingConfig.EnableParentChild)

	// Library has parent-child off; override snapshot turns it on.
	libraryOff := &types.Library{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, EnableParentChild: false},
	}
	effOn := ResolveProcessConfig(libraryOff, &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 512, EnableParentChild: true},
	})
	require.True(t, effOn.ChunkingConfig.EnableParentChild)
}

func TestResolveProcessConfig_NilOverridesUsesLibraryDefaults(t *testing.T) {
	t.Parallel()

	library := &types.Library{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
	}

	eff := ResolveProcessConfig(library, nil)

	require.Equal(t, 512, eff.ChunkingConfig.ChunkSize)
	require.Equal(t, 50, eff.ChunkingConfig.ChunkOverlap)
}

func TestBuildSplitterConfigFromChunking_UsesEffectiveChunkingConfig(t *testing.T) {
	t.Parallel()

	library := &types.Library{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50, Strategy: "token"},
	}
	overrides := &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 1500, ChunkOverlap: 120, Strategy: "character"},
	}
	eff := ResolveProcessConfig(library, overrides)
	cfg := buildSplitterConfigFromChunking(eff.ChunkingConfig)

	require.Equal(t, 1500, cfg.ChunkSize)
	require.Equal(t, 120, cfg.ChunkOverlap)
	require.Equal(t, "character", cfg.Strategy)
}

func TestBuildParentChildConfigs_PropagatesStrategy(t *testing.T) {
	t.Parallel()

	base := chunker.SplitterConfig{
		ChunkSize:    1000,
		ChunkOverlap: 100,
		Separators:   []string{"\n\n", "\n"},
		Strategy:     chunker.StrategyAuto,
	}
	cc := types.ChunkingConfig{
		EnableParentChild: true,
		ParentChunkSize:   4096,
		ChildChunkSize:    512,
	}

	parent, child := buildParentChildConfigs(cc, base)

	require.Equal(t, chunker.StrategyAuto, parent.Strategy,
		"parent splitting must honour the configured strategy; empty resolves to the legacy tier")
	require.Equal(t, chunker.StrategyAuto, child.Strategy,
		"child splitting must honour the configured strategy; empty resolves to the legacy tier")
	require.Equal(t, 4096, parent.ChunkSize)
	require.Equal(t, 512, child.ChunkSize)
	require.Equal(t, base.ChunkOverlap, parent.ChunkOverlap)
	require.Equal(t, 512/5, child.ChunkOverlap)
	require.Equal(t, base.Separators, parent.Separators)
	require.Equal(t, base.Separators, child.Separators)
}
