package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func boolPtr(v bool) *bool {
	return &v
}

func TestEntryProcessOverridesRoundtrip(t *testing.T) {
	k := &Entry{}
	overrides := &EntryProcessOverrides{
		ChunkingConfig:        &ChunkingConfig{ChunkSize: 1024},
		ParserEngineOverrides: map[string]string{"pdf_force_scanned": "true"},
	}
	require.NoError(t, k.SetProcessOverrides(overrides))
	got, err := k.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 1024, got.ChunkingConfig.ChunkSize)
	require.Equal(t, "true", got.ParserEngineOverrides["pdf_force_scanned"])
}

func TestSetProcessOverridesPreservesOtherMetadata(t *testing.T) {
	k := &Entry{}
	manualMeta := NewManualEntryMetadata("# hello", ManualEntryStatusDraft, 1)
	require.NoError(t, k.SetManualMetadata(manualMeta))

	overrides := &EntryProcessOverrides{ChunkingConfig: &ChunkingConfig{ChunkSize: 256}}
	require.NoError(t, k.SetProcessOverrides(overrides))

	gotManual, err := k.ManualMetadata()
	require.NoError(t, err)
	require.NotNil(t, gotManual)
	require.Equal(t, "# hello", gotManual.Content)
	require.Equal(t, ManualEntryFormatMarkdown, gotManual.Format)
	require.Equal(t, ManualEntryStatusDraft, gotManual.Status)

	gotOverrides, err := k.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, gotOverrides)
	require.Equal(t, 256, gotOverrides.ChunkingConfig.ChunkSize)
}
