package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestTemplateOnlyCoverageAcrossQueryScopes(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	otherDomain := BuiltinAIKnowledgeDomain()
	otherDomain.ID, otherDomain.TenantID, otherDomain.Name = "other-domain", 1, "Other domain"
	otherDomain.IsBuiltin = false
	require.NoError(t, db.Create(otherDomain).Error)
	for _, kb := range []types.KnowledgeBase{
		{ID: "same-domain-kb", TenantID: 1, DomainID: "p3-domain", Name: "Same domain"},
		{ID: "other-domain-kb", TenantID: 1, DomainID: otherDomain.ID, Name: "Other domain"},
	} {
		require.NoError(t, db.Create(&kb).Error)
	}
	require.NoError(t, db.Create(&[]types.KnowledgeEntity{p3Entity("acme", "Acme", "company"), p3Entity("other", "Other", "company")}).Error)
	for index, kbID := range []string{"p3-kb", "same-domain-kb", "other-domain-kb"} {
		id := fmt.Sprintf("coverage-source-%d", index)
		k := p3Message(t, db, id, "Acme and Other.")
		require.NoError(t, db.Model(k).Update("knowledge_base_id", kbID).Error)
		e := types.KnowledgeEvidence{ID: id, TenantID: 1, KnowledgeBaseID: kbID, KnowledgeID: id, Version: 2,
			BodyHash: strings.Repeat("a", 64), Body: "Acme and Other.", PublishedAt: "2026-10-07", Status: types.KnowledgeEvidenceCurrent,
			ExtractionMode: types.KnowledgeExtractionTemplateOnly, ExtractionStatus: types.KnowledgeExtractionComplete}
		require.NoError(t, db.Create(&e).Error)
		for _, entityID := range []string{"acme", "other"} {
			require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: e.ID, EntityID: entityID}).Error)
		}
	}
	// Historical versions and multiple object associations still count one message.
	history := types.KnowledgeEvidence{ID: "coverage-history", TenantID: 1, KnowledgeBaseID: "p3-kb", KnowledgeID: "coverage-source-0", Version: 1,
		BodyHash: strings.Repeat("b", 64), Body: "Acme.", PublishedAt: "2026-10-07", Status: types.KnowledgeEvidenceSuperseded,
		ExtractionMode: types.KnowledgeExtractionTemplateOnly, ExtractionStatus: types.KnowledgeExtractionComplete}
	require.NoError(t, db.Create(&history).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: history.ID, EntityID: "acme"}).Error)
	for _, test := range []struct {
		q types.KnowledgeContextQuery
		n int
	}{
		{types.KnowledgeContextQuery{Scope: "knowledge_base", KnowledgeBaseID: "p3-kb", EntityID: "acme"}, 1},
		{types.KnowledgeContextQuery{Scope: "domain", DomainID: "p3-domain", EntityID: "acme"}, 2},
		{types.KnowledgeContextQuery{Scope: "all", EntityID: "acme", IncludeHistory: true, Limit: 1, Offset: 10}, 3},
		{types.KnowledgeContextQuery{Scope: "all", Entity: "Acme", Predicate: "unextracted field"}, 3},
		{types.KnowledgeContextQuery{Scope: "all", Entity: "Absent"}, 0},
		{types.KnowledgeContextQuery{Scope: "all", KnowledgeIDs: []string{"coverage-source-0"}}, 1},
		{types.KnowledgeContextQuery{Scope: "all"}, 3},
	} {
		result, err := svc.Query(ctx, "", test.q)
		require.NoError(t, err)
		require.Equal(t, test.n, result.TemplateOnlyMessageCount, "%+v", test.q)
	}
	result, err := svc.EntityDetail(ctx, "acme", false)
	require.NoError(t, err)
	require.Len(t, result.Entities, 1, "objects without facts remain visible")
	require.Len(t, result.Evidence, 3)
	require.Empty(t, result.Facts)
	require.Empty(t, result.Events)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "private-alias", TenantID: 1, EntityID: "acme", EntityType: "company", Alias: "Private identity", NormalizedAlias: "private identity"}).Error)
	// Failed latest evidence is still a timeline gap; disabled sources are hidden.
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "coverage-source-1").Update("enable_status", "disabled").Error)
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("id = ?", "coverage-source-2").Update("extraction_status", types.KnowledgeExtractionFailed).Error)
	result, err = svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "all", EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 2, result.TemplateOnlyMessageCount)
	require.Equal(t, contextEventsCoverageNotice, result.EventsCoverageNotice)
	until := time.Date(2026, 10, 6, 23, 59, 59, 0, time.UTC)
	result, err = svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "all", EntityID: "acme", Until: &until})
	require.NoError(t, err)
	require.Zero(t, result.TemplateOnlyMessageCount)
}

func TestTemplateOnlyEventCoverageUsesVisibleSourceObjects(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(&[]types.KnowledgeEntity{p3Entity("public", "Public", "company"), p3Entity("hidden", "Hidden", "company")}).Error)
	for _, id := range []string{"event-public", "event-hidden", "template-public", "template-hidden"} {
		k := p3Message(t, db, id, id)
		mode := types.KnowledgeExtractionFull
		if strings.HasPrefix(id, "template") {
			mode = types.KnowledgeExtractionTemplateOnly
		}
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: id, TenantID: 1, KnowledgeBaseID: "p3-kb", KnowledgeID: k.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: id, PublishedAt: "2026-10-07", Status: types.KnowledgeEvidenceCurrent, ExtractionMode: mode, ExtractionStatus: types.KnowledgeExtractionComplete}).Error)
		entityID := "public"
		if strings.HasSuffix(id, "hidden") {
			entityID = "hidden"
		}
		require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: id, EntityID: entityID}).Error)
	}
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", "event-hidden").Update("enable_status", "disabled").Error)
	require.NoError(t, db.Create(&types.KnowledgeEvent{ID: "coverage-event", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Public change", EvidenceIDs: types.StringArray{"event-public", "event-hidden"}}).Error)
	for _, entityID := range []string{"public", "hidden"} {
		evidenceID := "event-" + entityID
		require.NoError(t, db.Create(&types.KnowledgeEventEntity{TenantID: 1, EventID: "coverage-event", EntityID: entityID}).Error)
		require.NoError(t, db.Create(&types.KnowledgeEventEvidence{ID: evidenceID, TenantID: 1, KnowledgeBaseID: "p3-kb", EventID: "coverage-event", EvidenceID: evidenceID}).Error)
		require.NoError(t, db.Create(&types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: "coverage-event", EvidenceID: evidenceID, EntityID: entityID}).Error)
	}
	result, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "all", EventID: "coverage-event"})
	require.NoError(t, err)
	require.Equal(t, 1, result.TemplateOnlyMessageCount, "the disabled event source must not add its objects to relevance")
}

func TestEvidenceObjectLinksAndStarsSurviveMergeAndSourceDeletion(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(&[]types.KnowledgeEntity{p3Entity("keep", "Keep", "company"), p3Entity("merge", "Merge", "company")}).Error)
	e := p3Evidence(t, db, "Keep and Merge.")
	for _, id := range []string{"keep", "merge"} {
		require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: e.ID, EntityID: id}).Error)
	}
	require.NoError(t, svc.SetEntityStar(ctx, "merge", true))
	require.NoError(t, svc.MergeEntities(ctx, "", "keep", "merge"))
	var links []types.KnowledgeEvidenceEntity
	require.NoError(t, db.Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, "keep", links[0].EntityID)
	var stars []types.KnowledgeEntityStar
	require.NoError(t, db.Find(&stars).Error)
	require.Len(t, stars, 1)
	require.Equal(t, "keep", stars[0].EntityID)
	require.NoError(t, svc.DeleteMessages(ctx, "p3-kb", []string{e.KnowledgeID}))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeEvidenceEntity{}).Count(&count).Error)
	require.Zero(t, count)
	var entity types.KnowledgeEntity
	require.NoError(t, db.First(&entity, "id = ?", "keep").Error, "starred identities remain available for future intake")
}
