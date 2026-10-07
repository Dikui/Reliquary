package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

type contextConcurrencyMarker struct{}

func TestP2TeamPruneSerializesConcurrentExtractionPersistence(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	p2ContextMessage(t, db, "incoming", "energy-one", "NewCo has 10 staff.")
	paused := make(chan struct{})
	release := make(chan struct{})
	var pauseOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	pruneCtx, cancel := context.WithTimeout(context.WithValue(ctx, contextConcurrencyMarker{}, true), 10*time.Second)
	defer cancel()
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("pause_team_prune", func(tx *gorm.DB) {
		if marker, _ := tx.Statement.Context.Value(contextConcurrencyMarker{}).(bool); marker && tx.Statement.Table == "knowledge_context_facts" {
			pauseOnce.Do(func() {
				close(paused)
				select {
				case <-release:
				case <-pruneCtx.Done():
					tx.AddError(pruneCtx.Err())
				}
			})
		}
	}))
	defer db.Callback().Query().Remove("pause_team_prune")
	cleaned := make(chan error, 1)
	go func() { cleaned <- svc.DeleteKnowledgeBase(pruneCtx, "kb") }()
	select {
	case <-paused:
	case <-pruneCtx.Done():
		t.Fatal("cleanup did not reach its reference snapshot")
	}
	writerPID := make(chan int, 1)
	written := make(chan error, 1)
	go func() {
		written <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var pid int
			if err := tx.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				return err
			}
			writerPID <- pid
			ids, err := persistContextEntities(tx, []types.KnowledgeEntity{{ID: "newco", TenantID: 9, KnowledgeBaseID: "energy-one", CanonicalName: "NewCo", NormalizedName: "newco", EntityType: "company"}})
			if err != nil {
				return err
			}
			if err := tx.Create(&types.KnowledgeEvidence{ID: "incoming", TenantID: 9, KnowledgeBaseID: "energy-one", KnowledgeID: "incoming", Version: 1, BodyHash: strings.Repeat("a", 64), Body: "NewCo has 10 staff.", Status: types.KnowledgeEvidenceCurrent}).Error; err != nil {
				return err
			}
			return tx.Create(&types.KnowledgeFact{ID: "incoming", TenantID: 9, KnowledgeBaseID: "energy-one", SubjectEntityID: ids["newco"], Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: "incoming", Quote: "NewCo has 10 staff.", Status: types.KnowledgeFactCurrent}).Error
		})
	}()
	var pid int
	select {
	case pid = <-writerPID:
	case <-pruneCtx.Done():
		t.Fatal("persistence transaction did not start")
	}
	require.Eventually(t, func() bool {
		var waiting int64
		return db.Raw("SELECT count(*) FROM pg_locks WHERE pid = ? AND locktype = 'advisory' AND NOT granted", pid).Scan(&waiting).Error == nil && waiting == 1
	}, 3*time.Second, 10*time.Millisecond, "new extraction persistence must wait for the team's prune transaction")
	unblock()
	require.NoError(t, <-cleaned)
	require.NoError(t, <-written)
	var object types.KnowledgeEntity
	require.NoError(t, db.First(&object, "id = ?", "newco").Error)
	var fact types.KnowledgeFact
	require.NoError(t, db.First(&fact, "id = ?", "incoming").Error)
	require.Equal(t, object.ID, fact.SubjectEntityID)
	var orphans int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM knowledge_context_facts f LEFT JOIN knowledge_context_entities e ON e.tenant_id=f.tenant_id AND e.id=f.subject_entity_id WHERE f.tenant_id=9 AND e.id IS NULL`).Scan(&orphans).Error)
	require.Zero(t, orphans)
}

func TestP2ExtractionRechecksGlobalMergeAfterModelResolution(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT 'summary'`).Error)
	entities := []types.KnowledgeEntity{{ID: "keep", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "NewCo", NormalizedName: "newco", EntityType: "company"}, {ID: "merge", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "OldCo", NormalizedName: "oldco", EntityType: "company"}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, entities); return err }))
	p2ContextMessage(t, db, "old", "kb", "OldCo has 10 staff.")
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "old", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "old", Version: 1, BodyHash: strings.Repeat("a", 64), Body: "OldCo has 10 staff.", Status: types.KnowledgeEvidenceCurrent}).Error)
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "old", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "merge", Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: "old", Quote: "OldCo has 10 staff.", Status: types.KnowledgeFactCurrent}).Error)
	incoming := p2ContextMessage(t, db, "incoming", "energy-one", "OldCo has 10 staff.")
	svc.models = knowledgeContextTestModelService{model: &knowledgeContextTestChat{response: `{"entities":[{"mention":"OldCo","canonical_name":"OldCo","entity_type":"company","quote":"OldCo has 10 staff."}],"facts":[{"kind":"attribute","subject":"OldCo","predicate":"staff","value":10,"value_type":"number","quote":"OldCo has 10 staff."}],"events":[{"title":"OldCo staff count changed","event_type":"change","entity_mentions":["OldCo"],"quote":"OldCo has 10 staff."}],"attention":{"score":1,"reason":"Staff count."}}`}}
	paused := make(chan struct{})
	release := make(chan struct{})
	var pauseOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	extractionCtx, cancel := context.WithTimeout(context.WithValue(ctx, contextConcurrencyMarker{}, true), 10*time.Second)
	defer cancel()
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("pause_extraction_after_entities", func(tx *gorm.DB) {
		if marker, _ := tx.Statement.Context.Value(contextConcurrencyMarker{}).(bool); marker && tx.Statement.Table == "knowledge_context_events" {
			pauseOnce.Do(func() {
				close(paused)
				select {
				case <-release:
				case <-extractionCtx.Done():
					tx.AddError(extractionCtx.Err())
				}
			})
		}
	}))
	defer db.Callback().Query().Remove("pause_extraction_after_entities")
	processed := make(chan error, 1)
	go func() { processed <- svc.ProcessMessage(extractionCtx, &incoming, "OldCo has 10 staff.") }()
	select {
	case <-paused:
	case <-extractionCtx.Done():
		t.Fatal("extraction did not finish its identity resolution")
	}
	// Model work holds no team lock, so these mutations can complete immediately.
	require.NoError(t, svc.MergeEntities(ctx, "", "keep", "merge"))
	require.NoError(t, svc.DeleteKnowledgeBase(ctx, "energy-two"))
	var tombstones int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Where("id = ?", "merge").Count(&tombstones).Error)
	require.Zero(t, tombstones, "cleanup has removed the merged tombstone while the current identity stays referenced")
	unblock()
	require.NoError(t, <-processed)
	var facts []types.KnowledgeFact
	require.NoError(t, db.Order("id").Find(&facts).Error)
	require.Len(t, facts, 2)
	for _, fact := range facts {
		require.Equal(t, "keep", fact.SubjectEntityID)
	}
	var objects int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Count(&objects).Error)
	require.EqualValues(t, 1, objects, "stale model output must use the current global alias owner")
}
