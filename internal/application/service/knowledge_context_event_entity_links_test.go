package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func issue04ContextFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService) {
	t.Helper()
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "issue04-kb-two", TenantID: 1, DomainID: "p3-domain", Name: "Second sources", ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50}}).Error)
	return db, ctx, svc
}

func issue04Source(t *testing.T, db *gorm.DB, id, kbID string) types.KnowledgeEvidence {
	t.Helper()
	body := "Source " + id + " records a change."
	k := types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: kbID, Title: id, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{PublishedAt: "2026-10-07", Publisher: id}}))
	require.NoError(t, db.Create(&k).Error)
	e := types.KnowledgeEvidence{ID: id, TenantID: 1, KnowledgeBaseID: kbID, KnowledgeID: id, Version: 1, BodyHash: strings.Repeat("a", 64), Body: body, Status: types.KnowledgeEvidenceCurrent, CreatedAt: time.Now()}
	require.NoError(t, db.Create(&e).Error)
	return e
}

func issue04CreateEvent(t *testing.T, db *gorm.DB, id, kbID string, ids []string, sources ...types.KnowledgeEvidence) {
	t.Helper()
	event := types.KnowledgeEvent{ID: id, TenantID: 1, KnowledgeBaseID: kbID, Title: id, EventType: "change", EntityIDs: ids, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	for _, source := range sources {
		event.EvidenceIDs = append(event.EvidenceIDs, source.ID)
	}
	require.NoError(t, db.Create(&event).Error)
	for _, entityID := range uniqueContextIDs(ids) {
		require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.KnowledgeEventEntity{TenantID: 1, EventID: event.ID, EntityID: entityID}).Error)
	}
}

func issue04CreateEventEvidence(t *testing.T, db *gorm.DB, id, eventID string, source types.KnowledgeEvidence, ids ...string) {
	t.Helper()
	require.NoError(t, db.Create(&types.KnowledgeEventEvidence{ID: id, TenantID: 1, KnowledgeBaseID: source.KnowledgeBaseID, EventID: eventID, EvidenceID: source.ID, EntityIDs: ids, SourceTitle: eventID, CreatedAt: time.Now()}).Error)
	for _, entityID := range uniqueContextIDs(ids) {
		require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: eventID, EvidenceID: source.ID, EntityID: entityID}).Error)
	}
}

func TestIssue04MergeEntitiesRewritesBothEventReferencesAcrossKnowledgeBases(t *testing.T) {
	db, ctx, svc := issue04ContextFixture(t)
	for _, id := range []string{"keep", "merge", "other"} {
		e := p3Entity(id, "Object "+id, "company")
		require.NoError(t, db.Create(&e).Error)
	}
	sources := []types.KnowledgeEvidence{issue04Source(t, db, "source-one", "p3-kb"), issue04Source(t, db, "source-two", "issue04-kb-two")}
	for i, source := range sources {
		id := fmt.Sprintf("event-%d", i)
		issue04CreateEvent(t, db, id, source.KnowledgeBaseID, []string{"merge", "keep", "merge", "other"}, source)
		issue04CreateEventEvidence(t, db, "link-"+id, id, source, "merge", "keep", "merge", "other")
	}
	require.NoError(t, svc.MergeEntities(ctx, "p3-kb", "keep", "merge"))
	var events []types.KnowledgeEvent
	var links []types.KnowledgeEventEvidence
	require.NoError(t, db.Order("id").Find(&events).Error)
	require.NoError(t, db.Order("id").Find(&links).Error)
	require.NoError(t, loadContextEventEntityIDs(db, 1, events))
	require.NoError(t, loadContextEventEvidenceEntityIDs(db, 1, links))
	require.Len(t, events, 2)
	require.Len(t, links, 2)
	for _, event := range events {
		require.ElementsMatch(t, []string{"keep", "other"}, event.EntityIDs)
		t.Logf("merged event=%s kb=%s entity_ids=%v", event.ID, event.KnowledgeBaseID, event.EntityIDs)
	}
	for _, link := range links {
		require.ElementsMatch(t, []string{"keep", "other"}, link.EntityIDs)
		t.Logf("merged evidence_link=%s kb=%s entity_ids=%v", link.ID, link.KnowledgeBaseID, link.EntityIDs)
	}
	var pairs, sourcePairs int64
	require.NoError(t, db.Model(&types.KnowledgeEventEntity{}).Count(&pairs).Error)
	require.NoError(t, db.Model(&types.KnowledgeEventEvidenceEntity{}).Count(&sourcePairs).Error)
	require.EqualValues(t, 4, pairs)
	require.EqualValues(t, 4, sourcePairs)
	result, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: "keep"})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	merged, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: "merge"})
	require.NoError(t, err)
	require.Empty(t, merged.Events)
}

func TestIssue04DeletingSourcesRecomputesEventEntitiesAndPrunesLastReferences(t *testing.T) {
	for _, mode := range []string{"message", "knowledge_base"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx, svc := issue04ContextFixture(t)
			for _, id := range []string{"first-only", "shared", "second-only"} {
				e := p3Entity(id, "Object "+id, "company")
				require.NoError(t, db.Create(&e).Error)
			}
			first := issue04Source(t, db, "first", "p3-kb")
			second := issue04Source(t, db, "second", "issue04-kb-two")
			issue04CreateEvent(t, db, "shared-event", first.KnowledgeBaseID, []string{"first-only", "shared", "second-only"}, first, second)
			issue04CreateEventEvidence(t, db, "first-link", "shared-event", first, "first-only", "shared")
			issue04CreateEventEvidence(t, db, "second-link", "shared-event", second, "second-only", "shared")
			before, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: "shared"})
			require.NoError(t, err)
			require.Len(t, before.Events, 1)
			require.Len(t, before.Events[0].EvidenceIDs, 2)
			deleteSource := func(source types.KnowledgeEvidence) {
				if mode == "message" {
					require.NoError(t, svc.DeleteMessages(ctx, source.KnowledgeBaseID, []string{source.KnowledgeID}))
				} else {
					require.NoError(t, svc.DeleteKnowledgeBase(ctx, source.KnowledgeBaseID))
				}
			}
			deleteSource(first)
			var event types.KnowledgeEvent
			var links []types.KnowledgeEventEvidence
			require.NoError(t, db.First(&event, "id = ?", "shared-event").Error)
			events := []types.KnowledgeEvent{event}
			require.NoError(t, loadContextEventEntityIDs(db, 1, events))
			event = events[0]
			require.ElementsMatch(t, []string{"second-only", "shared"}, event.EntityIDs)
			require.ElementsMatch(t, []string{second.ID}, event.EvidenceIDs)
			require.NoError(t, db.Find(&links).Error)
			require.NoError(t, loadContextEventEvidenceEntityIDs(db, 1, links))
			require.Len(t, links, 1)
			require.ElementsMatch(t, []string{"second-only", "shared"}, links[0].EntityIDs)
			var objects []types.KnowledgeEntity
			require.NoError(t, db.Order("id").Find(&objects).Error)
			require.Len(t, objects, 2)
			if mode == "knowledge_base" {
				require.Equal(t, second.KnowledgeBaseID, event.KnowledgeBaseID)
			}
			t.Logf("after first source deletion event entity_ids=%v evidence_link entity_ids=%v objects=%d", event.EntityIDs, links[0].EntityIDs, len(objects))
			after, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: "shared"})
			require.NoError(t, err)
			require.Len(t, after.Events, 1)
			require.Equal(t, before.Events[0].ID, after.Events[0].ID)
			require.ElementsMatch(t, []string{second.ID}, after.Events[0].EvidenceIDs)
			deleteSource(second)
			for _, model := range []any{&types.KnowledgeEvent{}, &types.KnowledgeEventEvidence{}, &types.KnowledgeEventEntity{}, &types.KnowledgeEventEvidenceEntity{}, &types.KnowledgeEntity{}} {
				var count int64
				require.NoError(t, db.Model(model).Count(&count).Error)
				require.Zero(t, count)
			}
			t.Log("after last source deletion events=0 evidence_links=0 objects=0")
		})
	}
}

func TestIssue04ConcurrentMessagesKeepOneEventEntityAssociation(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := issue04ContextFixture(t)
	e := p3Entity("acme", "Acme", "company")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, []types.KnowledgeEntity{e}); return err }))
	original := issue04Source(t, db, "original", "p3-kb")
	issue04CreateEvent(t, db, "shared-event", "p3-kb", []string{e.ID}, original)
	issue04CreateEventEvidence(t, db, "original-link", "shared-event", original, e.ID)
	first := p3Message(t, db, "incoming-one", "Acme released a product.")
	second := p3Message(t, db, "incoming-two", "Acme released a product.")
	reachedJudgment := make(chan struct{}, 2)
	releaseJudgment := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseJudgment) }) }
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	model := &p3CountingChat{reply: func(callCtx context.Context, messages []chat.Message) string {
		if strings.Contains(messages[0].Content, "same specific real-world change") {
			reachedJudgment <- struct{}{}
			select {
			case <-releaseJudgment:
			case <-callCtx.Done():
			}
			return `{"relation":"SAME_OCCURRENCE","event_id":"shared-event","confidence":0.99}`
		}
		if strings.Contains(messages[0].Content, "same real-world object") {
			return `{"decision":"match","entity_id":"acme","confidence":0.99}`
		}
		return `{"entities":[{"mention":"Acme","canonical_name":"Acme","entity_type":"company","quote":"Acme released a product."}],"events":[{"title":"Acme product release","event_type":"release","entity_mentions":["Acme"],"quote":"Acme released a product."}],"attention":{"score":1,"reason":"A release."}}`
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	processed := make(chan error, 2)
	go func() { processed <- svc.ProcessMessage(ctx, first, "Acme released a product.") }()
	go func() { processed <- svc.ProcessMessage(ctx, second, "Acme released a product.") }()
	for range 2 {
		select {
		case <-reachedJudgment:
		case <-ctx.Done():
			t.Fatal("both messages did not reach event judgment concurrently")
		}
	}
	release()
	for range 2 {
		require.NoError(t, <-processed)
	}
	var events, pairs, sourcePairs, duplicates int64
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
	require.NoError(t, db.Model(&types.KnowledgeEventEntity{}).Where("tenant_id = 1 AND event_id = ? AND entity_id = ?", "shared-event", "acme").Count(&pairs).Error)
	require.NoError(t, db.Model(&types.KnowledgeEventEvidenceEntity{}).Where("tenant_id = 1 AND event_id = ? AND entity_id = ?", "shared-event", "acme").Count(&sourcePairs).Error)
	require.NoError(t, db.Raw(`SELECT count(*) FROM (SELECT tenant_id, event_id, entity_id FROM knowledge_context_event_entities GROUP BY tenant_id, event_id, entity_id HAVING count(*) > 1) duplicate_pairs`).Scan(&duplicates).Error)
	require.EqualValues(t, 1, events)
	require.EqualValues(t, 1, pairs)
	require.EqualValues(t, 3, sourcePairs)
	require.Zero(t, duplicates)
	result, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: e.ID})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Len(t, result.Events[0].EvidenceIDs, 3)
	t.Logf("concurrent messages=%d event_pairs=%d source_pairs=%d duplicate_pairs=%d", 2, pairs, sourcePairs, duplicates)
}

func TestIssue04MigratedAssociationConstraints(t *testing.T) {
	db, _, _ := issue04ContextFixture(t)
	var legacyColumns int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name IN ('knowledge_context_events', 'knowledge_context_event_evidence') AND column_name = 'entity_ids'`).Scan(&legacyColumns).Error)
	require.Zero(t, legacyColumns, "both stored JSONB object arrays must be removed")
	for _, id := range []string{"acme", "unassociated"} {
		e := p3Entity(id, id, "company")
		require.NoError(t, db.Create(&e).Error)
	}
	otherTeamEntity := p3Entity("foreign-entity", "Foreign entity", "company")
	otherTeamEntity.TenantID = 2
	require.NoError(t, db.Create(&otherTeamEntity).Error)
	source := issue04Source(t, db, "constraint-source", "p3-kb")
	issue04CreateEvent(t, db, "constraint-event", "p3-kb", []string{"acme"}, source)
	issue04CreateEventEvidence(t, db, "constraint-link", "constraint-event", source, "acme")
	require.NoError(t, db.Create(&types.KnowledgeEvent{ID: "foreign-event", TenantID: 2, KnowledgeBaseID: "p3-kb", Title: "Foreign event"}).Error)
	for _, test := range []struct {
		name string
		row  any
		code string
	}{
		{"duplicate_event_object", &types.KnowledgeEventEntity{TenantID: 1, EventID: "constraint-event", EntityID: "acme"}, "23505"},
		{"duplicate_source_object", &types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: "constraint-event", EvidenceID: source.ID, EntityID: "acme"}, "23505"},
		{"missing_event", &types.KnowledgeEventEntity{TenantID: 1, EventID: "missing-event", EntityID: "acme"}, "23503"},
		{"missing_object", &types.KnowledgeEventEntity{TenantID: 1, EventID: "constraint-event", EntityID: "missing-object"}, "23503"},
		{"cross_team_event", &types.KnowledgeEventEntity{TenantID: 1, EventID: "foreign-event", EntityID: "acme"}, "23503"},
		{"cross_team_object", &types.KnowledgeEventEntity{TenantID: 1, EventID: "constraint-event", EntityID: "foreign-entity"}, "23503"},
		{"missing_event_object_pair", &types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: "constraint-event", EvidenceID: source.ID, EntityID: "unassociated"}, "23503"},
		{"missing_event_evidence_pair", &types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: "constraint-event", EvidenceID: "missing-evidence", EntityID: "acme"}, "23503"},
		{"cross_team_source_pair", &types.KnowledgeEventEvidenceEntity{TenantID: 2, EventID: "constraint-event", EvidenceID: source.ID, EntityID: "acme"}, "23503"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := db.Create(test.row).Error
			var postgresError *pgconn.PgError
			require.ErrorAs(t, err, &postgresError)
			require.Equal(t, test.code, postgresError.Code)
		})
	}
	t.Log("legacy entity_ids columns=0; duplicate pairs rejected; missing and cross-team parents rejected")
}
