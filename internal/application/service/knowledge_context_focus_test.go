package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func focusTopic(t *testing.T, db *gorm.DB, id, kind string) types.KnowledgeTag {
	t.Helper()
	tag := types.KnowledgeTag{ID: id, TenantID: 1, KnowledgeBaseID: "p3-kb", Name: id, Kind: kind}
	require.NoError(t, db.Create(&tag).Error)
	return tag
}

func focusWorkflow(t *testing.T, db *gorm.DB, id string, enabled bool, config map[string]any) {
	t.Helper()
	require.NoError(t, db.Create(&types.KnowledgeWorkflow{ID: id, TenantID: 1, KnowledgeBaseID: "p3-kb", Name: id, OwnerID: "p3-reviewer", Enabled: enabled,
		Schedule: types.KnowledgeWorkflowSchedule{Timezone: "UTC"}, Steps: []types.KnowledgeWorkflowStep{{ID: "track", Type: types.WorkflowStepFactQuery, Config: config}}}).Error)
}

func TestIssue06StarChangesPreserveHistoricalStructuredData(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entity := p3Entity("object", "Aurora", "model")
	require.NoError(t, db.Create(&entity).Error)
	evidence := p3Evidence(t, db, "Aurora was released.")
	require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: evidence.ID, EntityID: entity.ID}).Error)
	fact := types.KnowledgeFact{ID: "old-fact", TenantID: 1, KnowledgeBaseID: "p3-kb", SubjectEntityID: entity.ID, Predicate: "license", Value: types.JSON(`"MIT"`), ValueType: "text", EvidenceID: evidence.ID, Quote: "Aurora", Status: "current"}
	event := types.KnowledgeEvent{ID: "old-event", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Released", EvidenceIDs: types.StringArray{evidence.ID}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, db.Create(&fact).Error)
	require.NoError(t, db.Create(&event).Error)
	var before types.KnowledgeEvidence
	require.NoError(t, db.First(&before, "id = ?", evidence.ID).Error)
	topic := focusTopic(t, db, "Models", types.TagKindTopic)
	for _, starred := range []bool{true, true, false, false} {
		require.NoError(t, svc.SetEntityStar(ctx, entity.ID, starred))
		require.NoError(t, svc.SetTopicStar(ctx, topic.ID, starred))
		var after types.KnowledgeEvidence
		require.NoError(t, db.First(&after, "id = ?", evidence.ID).Error)
		require.Equal(t, before, after, "starring must not reextract an existing evidence snapshot")
		var afterFact types.KnowledgeFact
		var afterEvent types.KnowledgeEvent
		require.NoError(t, db.First(&afterFact, "id = ?", fact.ID).Error)
		require.NoError(t, db.First(&afterEvent, "id = ?", event.ID).Error)
		require.Equal(t, fact.Value, afterFact.Value)
		require.Equal(t, event.Title, afterEvent.Title)
		var count int64
		require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
	var count int64
	require.NoError(t, db.Model(&types.UserResourceFavorite{}).Count(&count).Error)
	require.Zero(t, count, "structured focus must not use resource favorites")
}

func TestIssue06FocusMatchesStarsAndIntakeTopics(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entity := p3Entity("object", "Aurora", "model")
	require.NoError(t, db.Create(&entity).Error)
	k := p3Message(t, db, "new-message", "Aurora works.")
	matched, err := svc.MatchesFocus(ctx, k, []string{entity.ID})
	require.NoError(t, err)
	require.False(t, matched)
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, true))
	matched, err = svc.MatchesFocus(ctx, k, []string{entity.ID})
	require.NoError(t, err)
	require.True(t, matched)
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, false))
	for _, kind := range []string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent} {
		topic := focusTopic(t, db, "referent-"+kind, kind)
		require.NoError(t, svc.SetTopicStar(ctx, topic.ID, true))
		require.NoError(t, db.Create(&types.KnowledgeTagRelation{KnowledgeID: k.ID, TagID: topic.ID}).Error)
		matched, err := svc.MatchesFocus(ctx, k, nil)
		require.NoError(t, err)
		require.True(t, matched)
		require.NoError(t, db.Where("knowledge_id = ? AND tag_id = ?", k.ID, topic.ID).Delete(&types.KnowledgeTagRelation{}).Error)
		require.NoError(t, svc.SetTopicStar(ctx, topic.ID, false))
	}
	topic := focusTopic(t, db, "intake-topic", types.TagKindTopic)
	require.NoError(t, svc.SetTopicStar(ctx, topic.ID, true))
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	intake.Classification = []types.KnowledgeIntakeTag{{Kind: topic.Kind, Name: topic.Name}}
	require.NoError(t, k.SetIntakeMetadata(intake))
	matched, err = svc.MatchesFocus(ctx, k, nil)
	require.NoError(t, err)
	require.True(t, matched, "approved intake labels cover tagging-worker scheduling order")
	require.NoError(t, db.Create(&types.KnowledgeTagManualDecision{KnowledgeID: k.ID, TagID: topic.ID, Selected: false, ActorID: "reviewer", UpdatedAt: time.Now()}).Error)
	matched, err = svc.MatchesFocus(ctx, k, nil)
	require.NoError(t, err)
	require.False(t, matched, "explicit label removal overrides cached intake classification")
	nature := focusTopic(t, db, "描述", types.TagKindNature)
	require.ErrorContains(t, svc.SetTopicStar(ctx, nature.ID, true), "concrete referent")
}

func TestIssue06TrackingDerivesObjectConditionsAndEffectiveQueryScope(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entities := []types.KnowledgeEntity{p3Entity("object", "Aurora Model", "model"), p3Entity("other", "Other Model", "model")}
	require.NoError(t, db.Create(&entities).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "alias", TenantID: 1, EntityID: "object", Alias: "北极星", NormalizedAlias: "北极星", EntityType: "model"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "second-kb", TenantID: 1, DomainID: "p3-domain", Name: "Second"}).Error)
	focusWorkflow(t, db, "disabled", false, map[string]any{"entity_id": "other"})
	focusWorkflow(t, db, "subject", true, map[string]any{"entity_id": "object"})
	focusWorkflow(t, db, "relation-object", true, map[string]any{"predicate": "license", "object_entity_id": "object"})
	focusWorkflow(t, db, "alias", true, map[string]any{"entity": "北极"})
	focusWorkflow(t, db, "name", true, map[string]any{"entity": "AURORA", "scope": "all"})
	focusWorkflow(t, db, "domain", true, map[string]any{"entity_id": "object", "scope": "domain", "domain_id": "p3-domain"})
	focusWorkflow(t, db, "foreign-base", true, map[string]any{"entity_id": "other", "knowledge_base_id": "second-kb"})
	focusWorkflow(t, db, "field-only", true, map[string]any{"predicate": "license"})
	k := p3Message(t, db, "track-message", "Aurora works.")
	matched, err := svc.MatchesFocus(ctx, k, []string{"object"})
	require.NoError(t, err)
	require.True(t, matched)
	matched, err = svc.MatchesFocus(ctx, k, []string{"other"})
	require.NoError(t, err)
	require.False(t, matched)
	focus, err := svc.FocusScope(ctx, "p3-kb", types.KnowledgeContextQuery{})
	require.NoError(t, err)
	require.Len(t, focus.TrackedEntities, 1)
	require.Equal(t, "object", focus.TrackedEntities[0].ID)
	require.ElementsMatch(t, []string{"subject", "relation-object", "alias", "name", "domain"}, focus.TrackedEntities[0].WorkflowIDs)
	require.Equal(t, []string{"p3-kb"}, focus.TrackedEntities[0].KnowledgeBaseIDs)
	require.Empty(t, focus.TrackedTopics)
	second := *k
	second.KnowledgeBaseID = "second-kb"
	matched, err = svc.MatchesFocus(ctx, &second, []string{"other"})
	require.NoError(t, err)
	require.True(t, matched)
	matched, err = svc.MatchesFocus(ctx, &second, []string{"object"})
	require.NoError(t, err)
	require.True(t, matched, "team/domain queries include objects across their selected source bases")
}

func TestIssue06FocusScopeIsolatesTenantAndConcreteTopics(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entity := p3Entity("object", "Aurora", "model")
	require.NoError(t, db.Create(&entity).Error)
	evidence := p3Evidence(t, db, "Aurora works.")
	require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: evidence.ID, EntityID: entity.ID}).Error)
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, true))
	topic := focusTopic(t, db, "Models", types.TagKindTopic)
	focusTopic(t, db, "描述", types.TagKindNature)
	require.NoError(t, svc.SetTopicStar(ctx, topic.ID, true))
	for _, q := range []types.KnowledgeContextQuery{{Scope: "all"}, {Scope: "knowledge_base", KnowledgeBaseID: "p3-kb"}, {Scope: "domain", DomainID: "p3-domain"}} {
		focus, err := svc.FocusScope(ctx, "", q)
		require.NoError(t, err)
		require.Len(t, focus.StarredEntities, 1)
		require.Len(t, focus.StarredTopics, 1)
		require.Len(t, focus.AvailableTopics, 1)
		require.Equal(t, "P3 sources", focus.StarredTopics[0].KnowledgeBaseName)
	}
	foreign := context.WithValue(ctx, types.TenantIDContextKey, uint64(2))
	require.Error(t, svc.SetEntityStar(foreign, entity.ID, true))
	require.Error(t, svc.SetTopicStar(foreign, topic.ID, true))
	focus, err := svc.FocusScope(foreign, "", types.KnowledgeContextQuery{Scope: "all"})
	require.NoError(t, err)
	require.Empty(t, focus.StarredEntities)
	require.Empty(t, focus.StarredTopics)
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, false))
	entity.Description = "Global details"
	require.NoError(t, db.Model(&entity).Update("description", entity.Description).Error)
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, true))
	private := p3Entity("private", "Private object", "model")
	require.NoError(t, db.Create(&private).Error)
	require.NoError(t, svc.SetEntityStar(ctx, private.ID, true))
	focusWorkflow(t, db, "private-tracking", true, map[string]any{"entity_id": private.ID, "scope": "all"})
	focus, err = svc.FocusScope(ctx, "", types.KnowledgeContextQuery{Scope: "all"})
	require.NoError(t, err)
	require.Len(t, focus.StarredEntities, 2)
	require.Len(t, focus.TrackedEntities, 1)

}

func TestIssue06TrackingNameMatchesFirstObjectAppearance(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	// The task exists before its named object has ever been recognized. Its
	// source spelling differs from the canonical name returned by extraction.
	focusWorkflow(t, db, "first-appearance", true, map[string]any{"entity": "Acme Inc"})
	model := &p3SegmentChat{reply: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_entity_judgment" {
			return `{"decision":"new","confidence":1}`, nil
		}
		if stage == "context_event_judgment" {
			return `{"relation":"unrelated","confidence":1}`, nil
		}
		return onDemandOutput, nil
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	k := p3Message(t, db, "first-object-message", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	evidence := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, evidence.ExtractionMode)
	var facts, events int64
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("evidence_id = ?", evidence.ID).Count(&facts).Error)
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
	require.EqualValues(t, 3, facts)
	require.EqualValues(t, 1, events)
	require.Equal(t, 1, model.calls["context_full_extraction"])
	require.Equal(t, 2, model.calls["context_entity_judgment"], "reuse initial P3 decisions after full extraction")
}

func TestIssue06GlobalObjectStarsRemainVisibleAcrossFocusScopes(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	domain := BuiltinAIKnowledgeDomain()
	domain.ID, domain.TenantID, domain.Name = "another-domain", 1, "Another"
	domain.IsBuiltin = false
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "new-kb", TenantID: 1, DomainID: domain.ID, Name: "New sources"}).Error)
	linked := p3Entity("linked-star", "Linked object", "model")
	orphan := p3Entity("star-without-links", "Future object", "model")
	require.NoError(t, db.Create(&[]types.KnowledgeEntity{linked, orphan}).Error)
	evidence := p3Evidence(t, db, "Linked object.")
	require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: evidence.ID, EntityID: linked.ID}).Error)
	for _, entity := range []types.KnowledgeEntity{linked, orphan} {
		require.NoError(t, svc.SetEntityStar(ctx, entity.ID, true))
	}
	for _, q := range []types.KnowledgeContextQuery{
		{Scope: "knowledge_base", KnowledgeBaseID: "new-kb"},
		{Scope: "domain", DomainID: domain.ID},
		{Scope: "all"},
	} {
		focus, err := svc.FocusScope(ctx, "", q)
		require.NoError(t, err)
		require.Len(t, focus.StarredEntities, 2, "global stars apply even before objects appear in the selected source scope")
	}
	message := p3Message(t, db, "new-scope-message", "Linked object.")
	message.KnowledgeBaseID = "new-kb"
	matched, err := svc.MatchesFocus(ctx, message, []string{linked.ID})
	require.NoError(t, err)
	require.True(t, matched)
}
