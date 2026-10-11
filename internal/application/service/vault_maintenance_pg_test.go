package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestVaultReplicaRebuildPreservesEveryPersistentTableAndUserFiles(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = articleProcessingOutput(t, "research", []types.EntityCandidate{{Name: "Company", EntityType: "company"}}, []string{})
	full := h.upload(t, "Research preservation", "# Original research\n\nImmutable original measurements and reasoning.")
	h.processor.output = articleProcessingOutput(t, "reference", nil, []string{})
	summary := h.upload(t, "Reference preservation", "# Summary reference\n\nSource is kept as a summary and link.")
	require.NoError(t, h.db.Model(full).Updates(map[string]any{"content_type_corrected": true, "processing_review_status": "pending", "processing_review_content_type": "research", "processing_review_reason": "model uncertainty", "collect_reason": "User reason"}).Error)
	var entity types.Entity
	require.NoError(t, h.db.Where("canonical_name='Company'").First(&entity).Error)
	require.NoError(t, h.db.Model(&entity).Update("description", "User entity description").Error)
	require.NoError(t, h.db.Create(&types.Topic{ID: "maintenance-topic", SpaceID: 1, Name: "Maintenance", Description: "User authored description", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
	require.NoError(t, h.db.Create(&types.KnowledgeTopic{SpaceID: 1, KnowledgeID: full.ID, TopicID: "maintenance-topic", Source: types.TopicSourceUser, IsHistorical: true, CreatedAt: time.Now()}).Error)
	require.NoError(t, h.db.Exec("INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity',?),(1,'topic','maintenance-topic')", entity.ID).Error)
	require.NoError(t, h.db.Exec("INSERT INTO follow_feed_states(space_id,last_viewed_at) VALUES(1,'2026-10-10T09:00:00Z')").Error)
	for path, body := range map[string]string{"mine/note.md": "# User original\n\nOnly the file and backup can recover this text.", "reports/report.md": "# Report original\n\nThis report remains user owned."} {
		require.NoError(t, os.WriteFile(filepath.Join(v.root, path), []byte(body), 0600))
	}
	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, v.root, time.Millisecond, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, documents.Close()) })
	// Build keyword chunks without calling the configured vector fixture.
	indexing := *h.kb
	indexing.IndexingStrategy.VectorEnabled = false
	for _, kind := range []string{"mine", "report"} {
		require.NoError(t, documents.scanDirectory(h.ctx, &indexing, kind, true))
	}
	require.NoError(t, v.Flush(h.ctx))
	before, err := maintenance.BuildManifest(context.Background(), h.db, v.root, "")
	require.NoError(t, err)
	fresh := pgtest.Open(t)
	rebuilder, err := NewVaultDocumentService(fresh, nil, nil, v.root, time.Millisecond, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rebuilder.Close()) })
	result, err := rebuilder.RebuildReplica(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, result.Articles)
	require.EqualValues(t, 1, result.Mine)
	require.EqualValues(t, 1, result.Reports)
	after, err := maintenance.BuildManifest(context.Background(), fresh, v.root, "")
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(before, after))
	for _, original := range []*types.Knowledge{full, summary} {
		var rebuilt types.Knowledge
		require.NoError(t, fresh.Where("id=?", original.ID).First(&rebuilt).Error)
		require.Equal(t, original.VaultPath, rebuilt.VaultPath)
		require.Equal(t, original.VaultBody, rebuilt.VaultBody)
	}
	var personal []types.Knowledge
	require.NoError(t, fresh.Where("source_kind IN ('mine','report')").Find(&personal).Error)
	require.Len(t, personal, 2)
	for _, document := range personal {
		require.Empty(t, document.VaultBody)
	}
}

func TestVaultVerifyRepairsArticlesAndReplicaButReportsUserFileLoss(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	article := h.upload(t, "Verify recovery", "# Verified article\n\nDatabase can restore this original.")
	require.NoError(t, os.WriteFile(filepath.Join(v.root, "mine/note.md"), []byte("# Private note\n\nBackup only original."), 0600))
	documents, err := NewVaultDocumentService(h.db, nil, h.service.retrieveEngine, v.root, time.Millisecond, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, documents.Close()) })
	indexing := *h.kb
	indexing.IndexingStrategy.VectorEnabled = false
	require.NoError(t, documents.scanDirectory(h.ctx, &indexing, "mine", true))
	require.NoError(t, v.Flush(h.ctx))
	require.NoError(t, os.WriteFile(filepath.Join(v.root, article.VaultPath), []byte("damaged article"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(v.root, ".reliquary/data/topics.json"), []byte("invalid"), 0600))
	require.NoError(t, os.Remove(filepath.Join(v.root, "mine/note.md")))
	check, err := v.Verify(h.ctx, false)
	require.NoError(t, err)
	require.False(t, check.OK)
	check, err = v.Verify(h.ctx, true)
	require.NoError(t, err)
	require.False(t, check.OK)
	require.Len(t, check.Differences, 1)
	require.Contains(t, check.Differences[0], "mine/note.md")
	require.Contains(t, check.Repaired, article.VaultPath)
	_, body, err := SplitVaultArticle(mustVaultRead(t, filepath.Join(v.root, article.VaultPath)))
	require.NoError(t, err)
	require.Equal(t, article.VaultBody, string(body))
	_, err = os.Stat(filepath.Join(v.root, "mine/note.md"))
	require.True(t, os.IsNotExist(err))
	// Broken JSONL is quarantined rather than overwritten in place.
	require.NoError(t, os.WriteFile(filepath.Join(v.root, ".reliquary/operation_logs.jsonl"), []byte("broken log"), 0600))
	_, err = v.Verify(h.ctx, true)
	require.NoError(t, err)
	quarantined, err := filepath.Glob(filepath.Join(v.root, ".reliquary/quarantine/*.corrupt.jsonl"))
	require.NoError(t, err)
	require.Len(t, quarantined, 1)
	require.Equal(t, "broken log", string(mustVaultRead(t, quarantined[0])))
}

func TestVaultVerifyMarksSameDimensionOldEntityModelPendingAndExcludesMergedEntities(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	require.NoError(t, h.db.Exec(`INSERT INTO models(id,space_id,name,type,source,parameters,status) VALUES('old-entity-model',1,'Old entity model','Embedding','remote','{"embedding_parameters":{"dimension":3}}','active')`).Error)
	require.NoError(t, h.db.Exec(`INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,name_embedding,embedding_model_id,embedding_dimension,embedding_needs_rebuild,merged_into_id,created_at,updated_at) VALUES
 ('current-entity',1,'Current entity','current entity','company','[1,0,0]','embedding',3,false,'',now(),now()),
 ('stale-entity',1,'Stale entity','stale entity','company','[1,0,0]','old-entity-model',3,false,'',now(),now()),
 ('merged-entity',1,'Merged entity','merged entity','company','[1,0,0]','old-entity-model',3,true,'current-entity',now(),now())`).Error)
	dimensions, err := maintenance.CheckVectorDimensions(h.ctx, h.db)
	require.NoError(t, err)
	require.Empty(t, dimensions, "the obsolete entity vector remains dimensionally valid")
	checkPending := func(expected int64) {
		t.Helper()
		require.NoError(t, v.Flush(h.ctx))
		result, err := v.Verify(h.ctx, false)
		require.NoError(t, err)
		require.True(t, result.OK)
		require.Equal(t, expected, result.PendingVectors["entities"])
	}
	checkPending(1)
	require.NoError(t, h.db.Exec(`UPDATE entities SET embedding_model_id='embedding' WHERE id='stale-entity'`).Error)
	checkPending(0)
	require.NoError(t, h.db.Exec(`UPDATE entities SET embedding_needs_rebuild=true WHERE id='stale-entity'`).Error)
	checkPending(1)
	require.NoError(t, h.db.Exec(`UPDATE entities SET embedding_needs_rebuild=false,embedding_dimension=4 WHERE id='stale-entity'`).Error)
	checkPending(1)
	require.NoError(t, h.db.Exec(`UPDATE entities SET embedding_dimension=3 WHERE id='stale-entity'`).Error)
	require.NoError(t, h.db.Exec(`UPDATE models SET parameters=jsonb_set(parameters,'{embedding_parameters,dimension}','4'::jsonb) WHERE id='embedding'`).Error)
	checkPending(2)
}

func mustVaultRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
