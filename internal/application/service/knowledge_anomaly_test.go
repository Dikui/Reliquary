package service

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func anomalyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	for _, ddl := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, creator_id TEXT, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, parse_status TEXT, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, seq_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, name TEXT, kind TEXT, color TEXT, sort_order INTEGER, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)`,
		`CREATE TABLE knowledge_tag_relations (knowledge_id TEXT, tag_id TEXT, created_at TIMESTAMPTZ, PRIMARY KEY (knowledge_id, tag_id))`,
		`CREATE TABLE knowledge_tag_manual_decisions (knowledge_id TEXT, tag_id TEXT, selected BOOLEAN, actor_id TEXT, reason TEXT, updated_at TIMESTAMPTZ, PRIMARY KEY (knowledge_id, tag_id))`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, tenant_id INTEGER, tag_id TEXT, deleted_at TIMESTAMPTZ)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.KnowledgeAnomaly{}))
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id,tenant_id,creator_id) VALUES ('kb',1,'owner')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,parse_status) VALUES ('message',1,'kb','completed')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,parse_status) VALUES ('second',1,'kb','completed')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id,tenant_id,knowledge_base_id,name,kind) VALUES ('old',1,'kb','Old','topic'),('new',1,'kb','New','topic'),('keep',1,'kb','Keep','topic')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tag_relations (knowledge_id,tag_id) VALUES ('message','old'),('message','keep')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tag_relations (knowledge_id,tag_id) VALUES ('second','old')`).Error)
	return db
}

func anomalyContext(user string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, user)
	return ctx
}

func TestKnowledgeAnomalyForMessagesReturnsRelevantDecisions(t *testing.T) {
	db := anomalyTestDB(t)
	svc := NewKnowledgeAnomalyService(db, nil, nil)
	ctx := anomalyContext("owner")
	item, err := svc.Create(ctx, "kb", KnowledgeAnomalyInput{KnowledgeIDs: []string{"message", "second"}, Kind: "source_conflict", Description: "conflicting evidence"})
	require.NoError(t, err)
	_, err = svc.Resolve(ctx, "kb", item.ID, "resolved", "")
	require.Error(t, err)
	_, err = svc.Resolve(ctx, "kb", item.ID, "resolved", "Use the later verified source")
	require.NoError(t, err)
	got, err := svc.ForMessages(ctx, []string{"kb"}, []string{"message"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "resolved", got[0].Status)
	require.Equal(t, "Use the later verified source", got[0].DecisionNote)
	got, err = svc.ForMessages(ctx, []string{"kb"}, []string{"unrelated"})
	require.NoError(t, err)
	require.Empty(t, got)
	otherTenant := context.WithValue(ctx, types.TenantIDContextKey, uint64(2))
	got, err = svc.ForMessages(otherTenant, []string{"kb"}, []string{"message"})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestKnowledgeAnomalyRequiresAuthenticatedUserToApply(t *testing.T) {
	db := anomalyTestDB(t)
	tagRepo := repository.NewKnowledgeTagRepository(db)
	knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(db), tagRepo: tagRepo}
	svc := NewKnowledgeAnomalyService(db, knowledge, nil)
	reviewer := anomalyContext("reviewer")
	item, err := svc.Create(reviewer, "kb", KnowledgeAnomalyInput{
		KnowledgeIDs: []string{"message", "second"}, Kind: "suspected_error", Description: "wrong topic",
		ProposalType: "change_tags", ProposedTagIDs: []string{"new"}, ProposedRemoveTagIDs: []string{"old"},
	})
	require.NoError(t, err)
	require.Error(t, func() error {
		_, err := svc.Resolve(context.WithValue(reviewer, types.UserIDContextKey, ""), "kb", item.ID, "apply", "")
		return err
	}())
	var ids []string
	require.NoError(t, db.Model(&types.KnowledgeTagRelation{}).Where("knowledge_id = ?", "message").Pluck("tag_id", &ids).Error)
	sort.Strings(ids)
	require.Equal(t, []string{"keep", "old"}, ids)

	decided, err := svc.Resolve(anomalyContext("other-editor"), "kb", item.ID, "apply", "confirmed")
	require.NoError(t, err)
	require.Equal(t, "applied", decided.Status)
	ids = nil
	require.NoError(t, db.Model(&types.KnowledgeTagRelation{}).Where("knowledge_id = ?", "message").Pluck("tag_id", &ids).Error)
	sort.Strings(ids)
	require.Equal(t, []string{"keep", "new"}, ids)
	ids = nil
	require.NoError(t, db.Model(&types.KnowledgeTagRelation{}).Where("knowledge_id = ?", "second").Pluck("tag_id", &ids).Error)
	require.Equal(t, []string{"new"}, ids)
	var persisted types.KnowledgeAnomaly
	require.NoError(t, db.First(&persisted, "id = ?", item.ID).Error)
	require.Equal(t, types.KnowledgeAnomalyTagChange{Added: []string{"new"}, Removed: []string{"old"}}, persisted.AppliedChanges["message"])
	require.Equal(t, types.KnowledgeAnomalyTagChange{Added: []string{"new"}, Removed: []string{"old"}}, persisted.AppliedChanges["second"])
	var decisions []types.KnowledgeTagManualDecision
	require.NoError(t, db.Find(&decisions).Error)
	require.Len(t, decisions, 4)
	require.Equal(t, "confirmed", decisions[0].Reason)
}

func TestKnowledgeAnomalyTagDeletionKeepsMessage(t *testing.T) {
	db := anomalyTestDB(t)
	tagService := &knowledgeTagService{repo: repository.NewKnowledgeTagRepository(db), kbService: &safeDeleteKBService{}}
	svc := NewKnowledgeAnomalyService(db, nil, tagService)
	item, err := svc.Create(anomalyContext("system-1"), "kb", KnowledgeAnomalyInput{
		KnowledgeIDs: []string{"message"}, Kind: "tag_deletion", Description: "duplicate tag",
		ProposalType: "delete_tag", ProposedDeleteTagID: "old",
	})
	require.NoError(t, err)
	_, err = svc.Resolve(anomalyContext("owner"), "kb", item.ID, "apply", "")
	require.NoError(t, err)
	var tagCount, relationCount, messageCount int64
	require.NoError(t, db.Model(&types.KnowledgeTag{}).Where("id = ?", "old").Count(&tagCount).Error)
	require.NoError(t, db.Model(&types.KnowledgeTagRelation{}).Where("tag_id = ?", "old").Count(&relationCount).Error)
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "message").Count(&messageCount).Error)
	require.Zero(t, tagCount)
	require.Zero(t, relationCount)
	require.EqualValues(t, 1, messageCount)
}

func TestKnowledgeAnomalyApplyingCanRecoverAfterOutcomeWriteFailure(t *testing.T) {
	db := anomalyTestDB(t)
	tagRepo := repository.NewKnowledgeTagRepository(db)
	knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(db), tagRepo: tagRepo}
	svc := NewKnowledgeAnomalyService(db, knowledge, nil)
	item, err := svc.Create(anomalyContext("reviewer"), "kb", KnowledgeAnomalyInput{
		KnowledgeIDs: []string{"message"}, Kind: "suspected_error", Description: "wrong topic",
		ProposalType: "change_tags", ProposedTagIDs: []string{"new"}, ProposedRemoveTagIDs: []string{"old"},
	})
	require.NoError(t, err)
	// Simulate a crash after the tag mutation and before the final decision write.
	require.NoError(t, db.Exec(`DELETE FROM knowledge_tag_relations WHERE knowledge_id = 'message' AND tag_id = 'old'`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tag_relations (knowledge_id,tag_id) VALUES ('message','new')`).Error)
	planned, err := json.Marshal(map[string]types.KnowledgeAnomalyTagChange{"message": {Added: []string{"new"}, Removed: []string{"old"}}})
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.KnowledgeAnomaly{}).Where("id = ?", item.ID).Updates(map[string]any{
		"status": "applying", "applied_changes": string(planned), "updated_at": time.Now().Add(-3 * time.Minute),
	}).Error)
	decided, err := svc.Resolve(anomalyContext("owner"), "kb", item.ID, "apply", "")
	require.NoError(t, err)
	require.Equal(t, "applied", decided.Status)
	require.Equal(t, []string{"new"}, decided.AppliedChanges["message"].Added)
}
