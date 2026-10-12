package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestResetEntryForReparseClearsPreviousAttemptState(t *testing.T) {
	processedAt := time.Now()
	entry := &types.Entry{
		ParseStatus:          types.ParseStatusFailed,
		EnableStatus:         "enabled",
		Description:          "previous description",
		ProcessedAt:          &processedAt,
		ErrorMessage:         "embedding request failed: context deadline exceeded",
		EmbeddingModelID:     "old-model",
		PendingSubtasksCount: 3,
	}
	kb := &types.KnowledgeBase{EmbeddingModelID: "new-model"}

	resetEntryForReparse(entry, kb)

	require.Equal(t, types.ParseStatusPending, entry.ParseStatus)
	require.Equal(t, "disabled", entry.EnableStatus)
	require.Empty(t, entry.Description)
	require.Nil(t, entry.ProcessedAt)
	require.Empty(t, entry.ErrorMessage)
	require.Equal(t, "new-model", entry.EmbeddingModelID)
	require.Zero(t, entry.PendingSubtasksCount)
}
