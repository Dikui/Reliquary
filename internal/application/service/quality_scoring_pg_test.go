package service

import (
	"testing"
	"time"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func TestQualityGateRunsOnlyWhenEnabledAndDeletesLowMaterial(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	h.service.config.QualityScoring = nil
	h.processor.output = `{"attentionScore": 10}`

	kept, err := h.service.applyQualityGate(h.ctx, h.library, k, "Fixture body")
	require.NoError(t, err)
	require.True(t, kept, "libraries are not scored unless they opt in")
	require.Empty(t, k.QualityStatus)
	require.Empty(t, h.processor.requests)

	require.NoError(t, h.db.Create(&types.SystemSetting{
		Key: QualityScoringSettingKey(h.library.ID), Value: types.JSON(`true`), ValueType: "bool", Category: "prompts",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	h.processor.output = `{"attentionScore": 80}`
	kept, err = h.service.applyQualityGate(h.ctx, h.library, k, "Fixture body")
	require.NoError(t, err)
	require.True(t, kept)
	var stored types.Entry
	require.NoError(t, h.db.First(&stored, "id=?", k.ID).Error)
	require.Equal(t, types.QualityStatusPassed, stored.QualityStatus)
	require.Equal(t, 160, stored.QualityScore)

	low := &types.Entry{ID: "low-entry", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Title: "Low", Type: "manual",
		ParseStatus: types.ParseStatusCompleted, SourceKind: "article", Source: "https://example.org/low"}
	require.NoError(t, h.db.Create(low).Error)
	h.processor.output = `{"attentionScore": 10}`
	kept, err = h.service.applyQualityGate(h.ctx, h.library, low, "Fixture body")
	require.NoError(t, err)
	require.False(t, kept)
	var remaining int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", low.ID).Count(&remaining).Error)
	require.Zero(t, remaining, "low-quality material is deleted")
	var discard types.IngestDiscard
	require.NoError(t, h.db.Where("entry_id=?", low.ID).First(&discard).Error)
	require.Contains(t, discard.Reason, "质量评分过低")
}
