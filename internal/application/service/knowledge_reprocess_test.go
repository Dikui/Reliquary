package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type reprocessKnowledge struct {
	interfaces.KnowledgeService
	repo  interfaces.KnowledgeRepository
	badID string
}

func (s reprocessKnowledge) GetKnowledgeBody(_ context.Context, id string) (io.ReadCloser, string, error) {
	if id == s.badID {
		return nil, "", errors.New("source read failed")
	}
	return io.NopCloser(strings.NewReader("Original body with evidence for " + id)), "original.md", nil
}
func (s reprocessKnowledge) GetKnowledgeTags(ctx context.Context, ids []string) (map[string][]*types.KnowledgeTag, error) {
	return s.repo.GetKnowledgeTags(ctx, ids)
}
func (s reprocessKnowledge) SetKnowledgeTags(ctx context.Context, id string, ids []string) error {
	return s.repo.SetKnowledgeTags(ctx, id, ids)
}

func TestKnowledgeReprocessKeepsManualChoicesAndScope(t *testing.T) {
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Routing test team"}).Error)
	for _, ddl := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, creator_id TEXT, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, channel TEXT, parse_status TEXT, metadata JSONB, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, seq_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, name TEXT, kind TEXT, color TEXT, sort_order INTEGER, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledge_tag_relations (knowledge_id TEXT, tag_id TEXT, created_at TIMESTAMPTZ, PRIMARY KEY (knowledge_id, tag_id))`,
		`CREATE TABLE knowledge_tag_manual_decisions (knowledge_id TEXT, tag_id TEXT, selected BOOLEAN, actor_id TEXT, reason TEXT, updated_at TIMESTAMPTZ, PRIMARY KEY (knowledge_id, tag_id))`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.KnowledgeStrategyVersion{}, &types.KnowledgeAnomaly{}))
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id,tenant_id,creator_id) VALUES ('kb',1,'owner')`).Error)
	for _, id := range []string{"selected", "outside"} {
		message := &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "kb", Title: "Report", ParseStatus: types.ParseStatusCompleted}
		require.NoError(t, message.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.org/" + id, PublishedAt: "2026-09-01"}}))
		require.NoError(t, db.Exec(`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,title,parse_status,metadata) VALUES (?,?,?,?,?,?)`, id, 1, "kb", "Report", types.ParseStatusCompleted, string(message.Metadata)).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id,tenant_id,knowledge_base_id,name,kind) VALUES
		('nature',1,'kb','描述','nature'),('predicted',1,'kb','Specific object','object'),
		('keep',1,'kb','Keep','topic'),('stale',1,'kb','Stale','topic')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tag_relations (knowledge_id,tag_id) VALUES
		('selected','keep'),('selected','stale'),('outside','stale')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tag_manual_decisions (knowledge_id,tag_id,selected,actor_id,reason) VALUES
		('selected','keep',true,'owner','keep it'),('selected','predicted',false,'owner','wrong object')`).Error)
	require.NoError(t, db.Create(&types.KnowledgeStrategyVersion{ID: "active", TenantID: 1, KnowledgeBaseID: "kb", Status: "active",
		Rules: types.JSON(`{"tags":"Prefer specific object names"}`), PreviousRules: types.JSON(`{}`), SampleResults: types.JSON(`[]`), Reason: "reviewed", CreatedBy: "owner"}).Error)
	repo := repository.NewKnowledgeRepository(db)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner", SummaryModelID: "model"}
	svc := NewKnowledgeStrategyService(db, strategyKB{kb: kb}, reprocessKnowledge{repo: repo}, intakeModelService{chat: strategyChat{}}, repository.NewKnowledgeTagRepository(db))
	_, err := svc.Reprocess(anomalyContext(""), "kb", []string{"selected"}, "")
	require.Error(t, err)
	_, err = svc.Reprocess(anomalyContext("owner"), "kb", []string{"selected", "missing"}, "")
	require.Error(t, err)
	_, err = svc.Reprocess(anomalyContext("owner"), "kb", nil, "nature")
	require.Error(t, err)
	result, err := svc.Reprocess(anomalyContext("owner"), "kb", []string{"selected"}, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Updated)
	require.Equal(t, 1, result.Conflicts)
	require.Zero(t, result.Failed)
	got, err := repo.GetKnowledgeTags(anomalyContext("owner"), []string{"selected", "outside"})
	require.NoError(t, err)
	selected := map[string]bool{}
	for _, tag := range got["selected"] {
		selected[tag.ID] = true
	}
	require.Equal(t, map[string]bool{"keep": true, "nature": true}, selected)
	require.Len(t, got["outside"], 1)
	require.Equal(t, "stale", got["outside"][0].ID)
	var decisions []types.KnowledgeTagManualDecision
	require.NoError(t, db.Where("knowledge_id = ?", "selected").Find(&decisions).Error)
	require.Len(t, decisions, 2)
	var anomalies []types.KnowledgeAnomaly
	require.NoError(t, db.Where("knowledge_base_id = ?", "kb").Find(&anomalies).Error)
	require.Len(t, anomalies, 1)
	require.Equal(t, "manual_conflict", anomalies[0].Kind)

	// The same active strategy can be explicitly scoped to a concrete topic.
	result, err = svc.Reprocess(anomalyContext("owner"), "kb", nil, "keep")
	require.NoError(t, err)
	require.Equal(t, 1, result.Processed)

	failedService := NewKnowledgeStrategyService(db, strategyKB{kb: kb}, reprocessKnowledge{repo: repo, badID: "outside"},
		intakeModelService{chat: strategyChat{}}, repository.NewKnowledgeTagRepository(db))
	failed, err := failedService.Reprocess(anomalyContext("owner"), "kb", []string{"outside"}, "")
	require.NoError(t, err)
	require.Equal(t, 1, failed.Failed)
	require.Zero(t, failed.Flagged)
	unchanged, err := repo.GetKnowledgeTags(anomalyContext("owner"), []string{"outside"})
	require.NoError(t, err)
	require.Len(t, unchanged["outside"], 1)
	require.Equal(t, "stale", unchanged["outside"][0].ID)
}
