package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	werrors "reliquary/internal/errors"
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
		kb := &types.KnowledgeBase{ChunkingConfig: types.ChunkingConfig{ChunkOverlap: 30}}
		eff := ResolveProcessConfig(kb, &types.KnowledgeProcessOverrides{ChunkingConfig: &reloaded})
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
		kb := &types.KnowledgeBase{ChunkingConfig: types.ChunkingConfig{ChunkOverlap: 30}}
		eff := ResolveProcessConfig(kb, &types.KnowledgeProcessOverrides{ChunkingConfig: &override})
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

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
	}
	overrides := &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 2048},
	}
	eff := ResolveProcessConfig(kb, overrides)
	require.Equal(t, 2048, eff.ChunkingConfig.ChunkSize)
	require.Equal(t, 50, eff.ChunkingConfig.ChunkOverlap)
}

func TestResolveProcessConfig_OverrideTogglesParentChild(t *testing.T) {
	t.Parallel()

	// KB has parent-child on; override snapshot turns it off.
	kbOn := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, EnableParentChild: true},
	}
	effOff := ResolveProcessConfig(kbOn, &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 512, EnableParentChild: false},
	})
	require.False(t, effOff.ChunkingConfig.EnableParentChild)

	// KB has parent-child off; override snapshot turns it on.
	kbOff := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, EnableParentChild: false},
	}
	effOn := ResolveProcessConfig(kbOff, &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 512, EnableParentChild: true},
	})
	require.True(t, effOn.ChunkingConfig.EnableParentChild)
}

func TestResolveProcessConfig_NilOverridesUsesKBDefaults(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
		VLMConfig:      types.VLMConfig{Enabled: true, ModelID: "vlm-1"},
		QuestionGenerationConfig: &types.QuestionGenerationConfig{
			Enabled:       true,
			QuestionCount: 3,
		},
	}

	eff := ResolveProcessConfig(kb, nil)

	require.Equal(t, 512, eff.ChunkingConfig.ChunkSize)
	require.Equal(t, 50, eff.ChunkingConfig.ChunkOverlap)
	require.True(t, eff.EnableMultimodel)
	require.Equal(t, "vlm-1", eff.VLMConfig.ModelID)
	require.True(t, eff.QuestionGenerationConfig.Enabled)
	require.Equal(t, 3, eff.QuestionGenerationConfig.QuestionCount)
}

func TestBuildSplitterConfigFromChunking_UsesEffectiveChunkingConfig(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50, Strategy: "token"},
	}
	overrides := &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 1500, ChunkOverlap: 120, Strategy: "character"},
	}
	eff := ResolveProcessConfig(kb, overrides)
	cfg := buildSplitterConfigFromChunking(eff.ChunkingConfig)

	require.Equal(t, 1500, cfg.ChunkSize)
	require.Equal(t, 120, cfg.ChunkOverlap)
	require.Equal(t, "character", cfg.Strategy)
}

func TestEffectiveChunkingConfig_ResolveParserEngineFromOverrides(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{
			ParserEngineRules: []types.ParserEngineRule{
				{FileTypes: []string{"pdf"}, Engine: "anydoc"},
			},
		},
	}
	overrides := &types.KnowledgeProcessOverrides{
		ParserEngineRules: []types.ParserEngineRule{
			{FileTypes: []string{"pdf"}, Engine: "mineru"},
			{
				FileTypes: []string{"xlsx", "xls"},
				Engine:    "anydoc",
			},
		},
	}
	eff := ResolveProcessConfig(kb, overrides)
	require.Equal(t, "mineru", eff.ChunkingConfig.ResolveParserEngine("pdf"))
	xlsxRule := eff.ChunkingConfig.ResolveParserEngineRule("xlsx")
	require.NotNil(t, xlsxRule)
	require.Equal(t, "anydoc", xlsxRule.Engine)
}

func TestResolveProcessConfig_ParserEngineRulesReplaced(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{
			ParserEngineRules: []types.ParserEngineRule{
				{FileTypes: []string{"pdf"}, Engine: "anydoc"},
			},
		},
	}
	overrides := &types.KnowledgeProcessOverrides{
		ParserEngineRules: []types.ParserEngineRule{
			{FileTypes: []string{"docx"}, Engine: "custom"},
		},
	}
	eff := ResolveProcessConfig(kb, overrides)
	require.Len(t, eff.ChunkingConfig.ParserEngineRules, 1)
	require.Equal(t, []string{"docx"}, eff.ChunkingConfig.ParserEngineRules[0].FileTypes)
	require.Equal(t, "custom", eff.ChunkingConfig.ParserEngineRules[0].Engine)
}

func TestResolveProcessConfig_EnableMultimodelOverride(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: true, ModelID: "vlm-1"},
	}
	overrides := &types.KnowledgeProcessOverrides{
		EnableMultimodel: processConfigBoolPtr(false),
	}
	eff := ResolveProcessConfig(kb, overrides)
	require.False(t, eff.EnableMultimodel)
}

func TestResolveProcessConfig_PreservesKnowledgeBasePromptInstructions(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		ChunkingConfig: types.ChunkingConfig{TableMetadataInstructions: "table context"},
		VLMConfig: types.VLMConfig{
			Enabled: true, ModelID: "vlm-1", DescriptionLanguage: "English", CustomInstructions: "read labels",
		},
		QuestionGenerationConfig: &types.QuestionGenerationConfig{
			Enabled: true, QuestionCount: 3, CustomInstructions: "customer questions",
		},
	}
	overrides := &types.KnowledgeProcessOverrides{
		ChunkingConfig:           &types.ChunkingConfig{ChunkSize: 256},
		VLMConfig:                &types.VLMConfig{Enabled: true, ModelID: "vlm-2"},
		QuestionGenerationConfig: &types.QuestionGenerationConfig{Enabled: true, QuestionCount: 5},
	}

	eff := ResolveProcessConfig(kb, overrides)
	require.Equal(t, "table context", eff.ChunkingConfig.TableMetadataInstructions)
	require.Equal(t, "English", eff.VLMConfig.DescriptionLanguage)
	require.Equal(t, "read labels", eff.VLMConfig.CustomInstructions)
	require.Equal(t, "customer questions", eff.QuestionGenerationConfig.CustomInstructions)
}

func TestValidateProcessOverrides_RejectsOversizedInstructions(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{}
	overrides := &types.KnowledgeProcessOverrides{
		VLMConfig: &types.VLMConfig{
			CustomInstructions: strings.Repeat("x", types.MaxCustomPromptInstructionsLength+1),
		},
	}
	err := ValidateProcessOverrides(context.Background(), kb, overrides, []string{"txt"})
	require.Error(t, err)
	var badReq *werrors.AppError
	require.ErrorAs(t, err, &badReq)
}

func TestValidateProcessOverrides_NilOverrides(t *testing.T) {
	t.Parallel()

	err := ValidateProcessOverrides(context.Background(), &types.KnowledgeBase{}, nil, []string{"png"})
	require.NoError(t, err)
}

func TestValidateProcessOverrides_ImageWithoutVLMAllowed(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: false},
	}
	err := ValidateProcessOverrides(context.Background(), kb, &types.KnowledgeProcessOverrides{}, []string{"png"})
	require.NoError(t, err)
}

func TestValidateProcessOverrides_ImageWithEffectiveVLM(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: false},
	}
	overrides := &types.KnowledgeProcessOverrides{
		VLMConfig: &types.VLMConfig{Enabled: true, ModelID: "vlm-1"},
	}
	err := ValidateProcessOverrides(context.Background(), kb, overrides, []string{"jpg"})
	require.NoError(t, err)
}

func TestValidateProcessOverrides_NonMediaFileTypes(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{}
	err := ValidateProcessOverrides(context.Background(), kb, &types.KnowledgeProcessOverrides{}, []string{"pdf", "txt"})
	require.NoError(t, err)
}

func TestValidateProcessOverrides_ImageAllowsStorageFallback(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: true, ModelID: "vlm-1"},
	}

	err := ValidateProcessOverrides(context.Background(), kb, &types.KnowledgeProcessOverrides{}, []string{"png"})
	require.NoError(t, err)
}

func TestResolveFileImportProcessConfig_ImageWithoutVLMAllowed(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: false},
	}
	_, err := resolveFileImportProcessConfig(context.Background(), kb, "png", nil, nil)
	require.NoError(t, err)
}

// The regression behind #2447: spreadsheets must clear the shared import gate
// even when the caller sends no per-import overrides.
func TestResolveFileImportProcessConfig_SpreadsheetAllowedWithoutOverrides(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{ChunkingConfig: types.ChunkingConfig{ChunkSize: 512}}
	for _, ext := range []string{"xlsx", "xls", "csv"} {
		eff, err := resolveFileImportProcessConfig(context.Background(), kb, ext, nil, nil)
		require.NoErrorf(t, err, "ext=%s", ext)
		require.Equal(t, 512, eff.ChunkingConfig.ChunkSize)
	}
}

func TestResolveFileImportProcessConfig_RejectsUnsupportedAndUndeterminable(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{}
	for _, ext := range []string{"exe", "mp4", "", unknownFileType} {
		_, err := resolveFileImportProcessConfig(context.Background(), kb, ext, nil, nil)
		require.Errorf(t, err, "ext=%s should be rejected", ext)
	}
}

// ApplyKnowledgeProcessOverrides stays scoped to overrides: import-time file
// type gating belongs to resolveFileImportProcessConfig, so callers that pass
// no overrides (reparse, connector sync) keep their existing behaviour.
func TestApplyKnowledgeProcessOverrides_NoOverridesSkipsImportGate(t *testing.T) {
	t.Parallel()

	kb := &types.KnowledgeBase{
		VLMConfig: types.VLMConfig{Enabled: false},
	}
	knowledge := &types.Knowledge{}
	_, err := ApplyKnowledgeProcessOverrides(context.Background(), kb, knowledge, nil, []string{"png"}, nil)
	require.NoError(t, err)
}

func TestMergeParserEngineOverrides(t *testing.T) {
	t.Parallel()

	// 1. Both nil/empty
	merged := MergeParserEngineOverrides(nil, nil)
	require.Empty(t, merged)

	// 2. Tenant only
	merged = MergeParserEngineOverrides(map[string]string{"k1": "v1"}, nil)
	require.Equal(t, map[string]string{"k1": "v1"}, merged)

	// 3. Upload only
	merged = MergeParserEngineOverrides(nil, map[string]string{"k2": "v2"})
	require.Equal(t, map[string]string{"k2": "v2"}, merged)

	// 4. Overlap priority (upload override should take priority over tenant config)
	tenant := map[string]string{"k1": "tenant_val", "k2": "v2"}
	upload := map[string]string{"k1": "upload_val", "k3": "v3"}
	merged = MergeParserEngineOverrides(tenant, upload)
	require.Equal(t, map[string]string{
		"k1": "upload_val",
		"k2": "v2",
		"k3": "v3",
	}, merged)
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

func TestImageWithoutVisionIgnoresMultimodalRequest(t *testing.T) {
	enabled := true
	for _, overrides := range []*types.KnowledgeProcessOverrides{nil, {EnableMultimodel: &enabled}} {
		eff, err := resolveFileImportProcessConfig(context.Background(), &types.KnowledgeBase{}, "png", overrides, &enabled)
		require.NoError(t, err)
		require.False(t, eff.EnableMultimodel)
	}
}
