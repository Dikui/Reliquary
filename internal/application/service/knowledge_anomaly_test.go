package service

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

func anomalyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, creator_id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, parse_status TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, seq_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, name TEXT, kind TEXT, color TEXT, sort_order INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE knowledge_tag_relations (knowledge_id TEXT, tag_id TEXT, created_at DATETIME, PRIMARY KEY (knowledge_id, tag_id))`,
		`CREATE TABLE knowledge_tag_manual_decisions (knowledge_id TEXT, tag_id TEXT, selected BOOLEAN, actor_id TEXT, reason TEXT, updated_at DATETIME, PRIMARY KEY (knowledge_id, tag_id))`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, tenant_id INTEGER, tag_id TEXT, deleted_at DATETIME)`,
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
	return context.WithValue(ctx, types.UserIDContextKey, user)
}

func TestKnowledgeAnomalyReviewerCannotApplyOwnerCan(t *testing.T) {
	db := anomalyTestDB(t)
	tagRepo := repository.NewKnowledgeTagRepository(db)
	knowledge := &knowledgeService{repo: repository.NewKnowledgeRepository(db), tagRepo: tagRepo}
	svc := NewKnowledgeAnomalyService(db, knowledge, nil, nil)
	reviewer := anomalyContext("reviewer")
	item, err := svc.Create(reviewer, "kb", KnowledgeAnomalyInput{
		KnowledgeIDs: []string{"message", "second"}, Kind: "suspected_error", Description: "wrong topic",
		ProposalType: "change_tags", ProposedTagIDs: []string{"new"}, ProposedRemoveTagIDs: []string{"old"},
	})
	require.NoError(t, err)
	require.Error(t, func() error { _, err := svc.Resolve(reviewer, "kb", item.ID, "apply", ""); return err }())
	var ids []string
	require.NoError(t, db.Model(&types.KnowledgeTagRelation{}).Where("knowledge_id = ?", "message").Pluck("tag_id", &ids).Error)
	sort.Strings(ids)
	require.Equal(t, []string{"keep", "old"}, ids)

	decided, err := svc.Resolve(anomalyContext("owner"), "kb", item.ID, "apply", "confirmed")
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
	svc := NewKnowledgeAnomalyService(db, nil, tagService, nil)
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
	svc := NewKnowledgeAnomalyService(db, knowledge, nil, nil)
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
