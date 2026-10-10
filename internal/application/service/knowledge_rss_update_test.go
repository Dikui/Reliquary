package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type rssCandidateSequenceRepo struct {
	interfaces.KnowledgeRepository
	reads int
}

func (r *rssCandidateSequenceRepo) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	r.reads++
	if r.reads == 1 {
		return &types.Knowledge{ParseStatus: types.ParseStatusFinalizing, EnableStatus: "disabled"}, nil
	}
	return &types.Knowledge{ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled",
		Description: "completed summary", Metadata: types.JSON(`{"rss_update_candidate":"true"}`)}, nil
}

func TestWaitRSSUpdateCandidateUsesFinalizedRow(t *testing.T) {
	repo := &rssCandidateSequenceRepo{}
	svc := &knowledgeService{repo: repo}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	candidate, err := svc.waitRSSUpdateCandidate(ctx, 1, "candidate")
	require.NoError(t, err)
	require.Equal(t, 2, repo.reads)
	require.Equal(t, "completed summary", candidate.Description)
	require.Equal(t, types.JSON(`{"rss_update_candidate":"true"}`), candidate.Metadata)
}

func TestSourceUpdateCandidatePromotesAfterOriginalIsReady(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeTag{}, &types.KnowledgeTagRelation{}, &types.KnowledgeTagManualDecision{}))
	repo := repository.NewKnowledgeRepository(db)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	svc := &knowledgeService{db: db, repo: repo}

	oldBody := "Approved original source: the company opened its first factory."
	old := types.Knowledge{
		ID: "approved-original", TenantID: 9, KnowledgeBaseID: "kb", Type: types.KnowledgeTypeManual,
		Title: "Factory opening", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, old.SetManualMetadata(types.NewManualKnowledgeMetadata(oldBody, types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, old.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, mergeStringMetadataIntoKnowledge(&old, map[string]string{"external_id": "external-1", "datasource_id": "datasource-1"}))
	require.NoError(t, db.Create(&old).Error)
	tags := []types.KnowledgeTag{
		{ID: "candidate-auto", TenantID: 9, KnowledgeBaseID: "kb", Kind: types.TagKindTopic, Name: "candidate auto", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "manual-add", TenantID: 9, KnowledgeBaseID: "kb", Kind: types.TagKindTopic, Name: "manual add", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "manual-remove", TenantID: 9, KnowledgeBaseID: "kb", Kind: types.TagKindTopic, Name: "manual remove", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "legacy-custom", TenantID: 9, KnowledgeBaseID: "kb", Kind: "custom", Name: "legacy custom", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	for i := range tags {
		require.NoError(t, db.Create(&tags[i]).Error)
	}
	require.NoError(t, repo.SetKnowledgeTags(ctx, old.ID, []string{"manual-remove", "legacy-custom"}))
	require.NoError(t, db.Create(&types.KnowledgeTagManualDecision{
		KnowledgeID: old.ID, TagID: "manual-add", Selected: true, ActorID: "owner", Reason: "add explicit label", UpdatedAt: time.Now(),
	}).Error)
	require.NoError(t, db.Create(&types.KnowledgeTagManualDecision{
		KnowledgeID: old.ID, TagID: "manual-remove", Selected: false, ActorID: "owner", Reason: "remove explicit label", UpdatedAt: time.Now(),
	}).Error)
	var storedOld types.Knowledge
	require.NoError(t, db.Where("id = ?", old.ID).First(&storedOld).Error)
	storedIntake, err := storedOld.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", storedIntake.Decision)
	require.Equal(t, types.ParseStatusCompleted, storedOld.ParseStatus)
	require.Equal(t, "enabled", storedOld.EnableStatus)
	require.False(t, storedOld.IsSourceUpdateCandidate())
	require.False(t, storedOld.IsArchivedSource())
	storedBody, err := storedOld.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, oldBody, storedBody.Content, "the approved original remains readable while its replacement is pending")

	candidateBody := "Approved updated source: the company opened two factories."
	candidate := types.Knowledge{
		ID: "replacement-candidate", TenantID: 9, KnowledgeBaseID: "kb", Type: types.KnowledgeTypeManual,
		Title: "Factory opening updated", ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled",
		Metadata:  types.JSON(`{"source_update_candidate":"true","source_update_replaces":"approved-original","source_update_external_id":"external-1","source_update_datasource_id":"datasource-1"}`),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, candidate.SetManualMetadata(types.NewManualKnowledgeMetadata(candidateBody, types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, candidate.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, repo.SetKnowledgeTags(ctx, candidate.ID, []string{"candidate-auto", "manual-remove"}))
	require.NoError(t, svc.promoteSourceUpdateCandidate(ctx, candidate.ID))

	updatedOld, err := repo.GetKnowledgeByID(ctx, 9, old.ID)
	require.NoError(t, err)
	updatedCandidate, err := repo.GetKnowledgeByID(ctx, 9, candidate.ID)
	require.NoError(t, err)
	require.True(t, updatedOld.IsArchivedSource())
	require.Equal(t, "disabled", updatedOld.EnableStatus)
	require.NotContains(t, updatedOld.GetMetadata(), "external_id")
	require.Equal(t, candidate.ID, updatedOld.GetMetadata()["superseded_by"])
	require.Equal(t, "enabled", updatedCandidate.EnableStatus)
	require.False(t, updatedCandidate.IsSourceUpdateCandidate())
	require.Equal(t, "external-1", updatedCandidate.GetMetadata()["external_id"])
	retainedBody, err := updatedOld.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, oldBody, retainedBody.Content)
	replacementBody, err := updatedCandidate.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, candidateBody, replacementBody.Content)
	updatedTags, err := repo.GetKnowledgeTags(ctx, []string{candidate.ID})
	require.NoError(t, err)
	updatedTagIDs := make(map[string]bool)
	for _, tag := range updatedTags[candidate.ID] {
		updatedTagIDs[tag.ID] = true
	}
	require.True(t, updatedTagIDs["candidate-auto"])
	require.True(t, updatedTagIDs["manual-add"])
	require.True(t, updatedTagIDs["legacy-custom"])
	require.False(t, updatedTagIDs["manual-remove"])
	var copiedDecisions []types.KnowledgeTagManualDecision
	require.NoError(t, db.Where("knowledge_id = ?", candidate.ID).Find(&copiedDecisions).Error)
	require.Len(t, copiedDecisions, 2)
}

func TestSourceUpdateCandidateFailureLeavesApprovedOriginalCurrent(t *testing.T) {
	db := pgtest.Open(t)
	repo := repository.NewKnowledgeRepository(db)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	svc := &knowledgeService{db: db, repo: repo}

	old := types.Knowledge{
		ID: "approved-original", TenantID: 9, KnowledgeBaseID: "kb", Type: types.KnowledgeTypeManual,
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, old.SetManualMetadata(types.NewManualKnowledgeMetadata("Original retained", types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, old.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Create(&old).Error)
	candidate := types.Knowledge{
		ID: "replacement-candidate", TenantID: 9, KnowledgeBaseID: "kb", Type: types.KnowledgeTypeManual,
		ParseStatus: types.ParseStatusFailed, EnableStatus: "disabled",
		Metadata:  types.JSON(`{"source_update_candidate":true,"source_update_replaces":"approved-original"}`),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, candidate.SetManualMetadata(types.NewManualKnowledgeMetadata("Unparsed replacement", types.ManualKnowledgeStatusPublish, 1)))
	require.NoError(t, candidate.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Create(&candidate).Error)

	require.Error(t, svc.promoteSourceUpdateCandidate(ctx, candidate.ID), "an unparsed replacement must not archive the original")
	candidate.ParseStatus = types.ParseStatusCompleted
	require.NoError(t, candidate.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "rejected", Reason: "source rejected"}))
	require.NoError(t, db.Save(&candidate).Error)
	require.Error(t, svc.promoteSourceUpdateCandidate(ctx, candidate.ID), "a rejected replacement must not archive the original")

	current, err := repo.GetKnowledgeByID(ctx, 9, old.ID)
	require.NoError(t, err)
	require.False(t, current.IsArchivedSource())
	require.Equal(t, "enabled", current.EnableStatus)
	failedCandidate, err := repo.GetKnowledgeByID(ctx, 9, candidate.ID)
	require.NoError(t, err)
	require.NotEmpty(t, failedCandidate.GetMetadata()["source_update_error"], "promotion failure must be visible on the retained candidate")
	require.NotEmpty(t, failedCandidate.ErrorMessage)
	original, err := current.ManualMetadata()
	require.NoError(t, err)
	require.Equal(t, "Original retained", original.Content)
}
