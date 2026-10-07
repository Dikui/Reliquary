package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func p2ContextFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService) {
	t.Helper()
	db := knowledgeContextTestDB(t)
	domain := &types.KnowledgeDomain{ID: "energy", TenantID: 9, Name: "Energy", EntityTypes: []string{"company"}, Fields: []types.DomainField{{Key: "power", Label: "Power", Aliases: []string{"rated power"}, ValueType: "number", UnitFamily: "power"}}, UnitFamilies: []types.DomainUnitFamily{{Key: "power", CanonicalUnit: "W", Units: []types.DomainUnit{{Name: "W", Factor: 1}, {Name: "kW", Aliases: []string{"kilowatt"}, Factor: 1000}}}}, ExtractionHints: "Preserve operating conditions."}
	require.NoError(t, validateKnowledgeDomain(domain))
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases(id,tenant_id,name,domain_id) VALUES ('energy-one',9,'Energy one','energy'),('energy-two',9,'Energy two','energy')`).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "reviewer")
	return db, ctx, NewKnowledgeContextService(db, nil)
}

func p2ContextMessage(t *testing.T, db *gorm.DB, id, kbID, body string) types.Knowledge {
	t.Helper()
	k := types.Knowledge{ID: id, TenantID: 9, KnowledgeBaseID: kbID, Title: id, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{Publisher: kbID, OriginalURL: "https://example.com/" + id, PublishedAt: time.Now().UTC().Format(time.RFC3339)}}))
	require.NoError(t, db.Create(&k).Error)
	return k
}

func TestP2GlobalObjectAndEventIdentityAcrossKnowledgeBases(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT 'summary'`).Error)
	model := &knowledgeContextTestChat{reply: func(messages []chat.Message) string {
		if strings.Contains(messages[0].Content, "same specific real-world change") {
			var input struct {
				Candidates []struct {
					ID string `json:"id"`
				} `json:"candidates"`
			}
			require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
			if len(input.Candidates) > 0 {
				return fmt.Sprintf(`{"relation":"SAME_OCCURRENCE","event_id":%q,"confidence":0.99}`, input.Candidates[0].ID)
			}
			return `{"event_id":"","confidence":0}`
		}
		if strings.Contains(messages[0].Content, "same real-world object") {
			return `{"entity_id":"","confidence":0}`
		}
		return `{"entities":[{"mention":"Acme","quote":"Acme released a product.","canonical_name":"Acme","entity_type":"company"}],"facts":[{"kind":"attribute","subject":"Acme","predicate":"workers","value":10,"value_type":"number","quote":"Acme released a product."}],"events":[{"title":"Acme product release","event_type":"release","entity_mentions":["Acme"],"quote":"Acme released a product."}],"attention":{"score":1,"reason":"A product release."}}`
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	for _, kbID := range []string{"kb", "energy-one"} {
		k := p2ContextMessage(t, db, "message-"+kbID, kbID, "Acme released a product.")
		require.NoError(t, svc.ProcessMessage(ctx, &k, "Acme released a product."))
	}
	var entities []types.KnowledgeEntity
	var events []types.KnowledgeEvent
	require.NoError(t, db.Find(&entities).Error)
	require.Len(t, entities, 1)
	require.NoError(t, db.Find(&events).Error)
	require.Len(t, events, 1)
	require.Len(t, events[0].EvidenceIDs, 2)
	team, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: entities[0].ID})
	require.NoError(t, err)
	require.Len(t, team.Entities, 1)
	require.Len(t, team.Facts, 2)
	require.Len(t, team.Events, 1)
	require.Len(t, team.Events[0].Sources, 2)
	for _, fact := range team.Facts {
		require.NotEmpty(t, fact.KnowledgeBaseName)
	}
	// Deleting the original source preserves the global object and shared event.
	require.NoError(t, svc.DeleteKnowledgeBase(ctx, "kb"))
	var remaining types.KnowledgeEntity
	require.NoError(t, db.First(&remaining, "id = ?", entities[0].ID).Error)
	require.Empty(t, remaining.KnowledgeBaseID)
	team, err = svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "team"})
	require.NoError(t, err)
	require.Len(t, team.Facts, 1)
	require.Len(t, team.Events, 1)
	require.Len(t, team.Events[0].EvidenceIDs, 1)
	require.NoError(t, svc.DeleteKnowledgeBase(ctx, "energy-one"))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.KnowledgeEntityAlias{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestP2ObjectAliasesAndCanonicalNamesAreUniqueByTeamAndType(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	model := &knowledgeContextTestChat{response: `{"entity_id":"","confidence":0}`}
	for _, input := range []struct{ kbID, kind string }{{"kb", "company"}, {"energy-one", "company"}, {"energy-two", "product"}} {
		entities, _, err := svc.resolveContextEntities(ctx, model, 9, input.kbID, []contextExtractedEntity{{Mention: "Acme", Canonical: "Acme", EntityType: input.kind, Quote: "Acme"}})
		require.NoError(t, err)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, entities); return err }))
	}
	var entities []types.KnowledgeEntity
	var aliases []types.KnowledgeEntityAlias
	require.NoError(t, db.Find(&entities).Error)
	require.Len(t, entities, 2)
	require.NoError(t, db.Find(&aliases).Error)
	require.Len(t, aliases, 2)
	duplicate := types.KnowledgeEntity{ID: "duplicate", TenantID: 9, KnowledgeBaseID: "energy-two", EntityType: "company", CanonicalName: "Acme", NormalizedName: "acme"}
	require.Error(t, db.Create(&duplicate).Error)
	duplicateAlias := types.KnowledgeEntityAlias{ID: "duplicate-alias", TenantID: 9, KnowledgeBaseID: "energy-two", EntityType: "company", EntityID: entities[0].ID, Alias: "Acme", NormalizedAlias: "acme"}
	require.Error(t, db.Create(&duplicateAlias).Error)
}

func TestP2QueryScopesAndOpenFieldMappingPreserveFacts(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "acme", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company"}).Error)
	for i, kbID := range []string{"kb", "energy-one", "energy-two"} {
		id := fmt.Sprintf("source-%d", i)
		body := "Acme rated power is 2 kW."
		p2ContextMessage(t, db, id, kbID, body)
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: id, TenantID: 9, KnowledgeBaseID: kbID, KnowledgeID: id, Version: 1, BodyHash: strings.Repeat(fmt.Sprint(i), 64), Body: body, Status: types.KnowledgeEvidenceCurrent}).Error)
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: id, TenantID: 9, KnowledgeBaseID: kbID, SubjectEntityID: "acme", Kind: types.KnowledgeFactAttribute, Predicate: "maximum output", OriginalPredicate: "maximum output", Value: types.JSON(`2`), ValueType: "number", Unit: "kW", EvidenceID: id, Quote: body, Status: types.KnowledgeFactCurrent}).Error)
	}
	for _, test := range []struct {
		query types.KnowledgeContextQuery
		count int
	}{{types.KnowledgeContextQuery{Scope: "knowledge_base", KnowledgeBaseID: "kb"}, 1}, {types.KnowledgeContextQuery{Scope: "domain", DomainID: "energy"}, 2}, {types.KnowledgeContextQuery{Scope: "team"}, 3}} {
		result, err := svc.Query(ctx, "", test.query)
		require.NoError(t, err)
		require.Len(t, result.Facts, test.count)
		require.Len(t, result.Entities, 1)
	}
	_, err := svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "domain"})
	require.Error(t, err)
	_, err = svc.Query(ctx, "", types.KnowledgeContextQuery{Scope: "invalid"})
	require.Error(t, err)
	var domain types.KnowledgeDomain
	require.NoError(t, db.First(&domain, "id = ?", "energy").Error)
	threshold := 1500.0
	query := types.KnowledgeContextQuery{Scope: "team", Predicate: "power", MinValue: &threshold, Unit: "W"}
	result, err := svc.Query(ctx, "", query)
	require.NoError(t, err)
	require.Empty(t, result.Facts)
	domain.Fields[0].Aliases = append(domain.Fields[0].Aliases, "maximum output")
	require.NoError(t, db.Save(&domain).Error)
	result, err = svc.Query(ctx, "", query)
	require.NoError(t, err)
	require.Len(t, result.Facts, 2)
	for _, fact := range result.Facts {
		require.Equal(t, "power", fact.Predicate)
		require.Equal(t, "maximum output", fact.OriginalPredicate)
		require.NotEmpty(t, fact.KnowledgeBaseName)
	}
	var original types.KnowledgeFact
	require.NoError(t, db.First(&original, "id = ?", "source-1").Error)
	require.Equal(t, "maximum output", original.Predicate)
	// Workflow fact_query consumes the same scope contract.
	workflow := &KnowledgeWorkflowService{context: svc}
	output, err := workflow.executeFactQuery(ctx, &types.KnowledgeWorkflow{KnowledgeBaseID: "kb"}, types.KnowledgeWorkflowStep{Config: types.JSONMap{"scope": "domain", "domain_id": "energy", "predicate": "power"}})
	require.NoError(t, err)
	require.Len(t, output.Data.(*types.KnowledgeContextResult).Facts, 2)
	extras := make([]types.KnowledgeFact, 251)
	for i := range extras {
		extras[i] = types.KnowledgeFact{ID: fmt.Sprintf("extra-%d", i), TenantID: 9, KnowledgeBaseID: "energy-one", SubjectEntityID: "acme", Kind: "attribute", Predicate: "maximum output", Value: types.JSON(`2`), ValueType: "number", Unit: "kW", EvidenceID: "source-1", Quote: "Acme rated power is 2 kW.", Status: "current"}
	}
	require.NoError(t, db.Create(&extras).Error)
	detail, err := svc.EntityDetail(ctx, "acme")
	require.NoError(t, err)
	require.Len(t, detail.Facts, 254, "object detail aggregates every knowledge base without list pagination")
	require.Len(t, detail.Evidence, 3)
}

func TestP2DynamicTemplatePromptAndUnitFamilies(t *testing.T) {
	domain := &types.KnowledgeDomain{Name: "Energy", EntityTypes: []string{"power plant"}, Fields: []types.DomainField{{Key: "power", Label: "Power", Aliases: []string{"rated power"}, ValueType: "number", UnitFamily: "power", EntityTypes: []string{"power plant"}}}, Relations: []types.DomainRelation{{Key: "operated_by", Label: "Operator", Aliases: []string{"operator"}, SubjectTypes: []string{"power plant"}, ObjectTypes: []string{"company"}}}, UnitFamilies: []types.DomainUnitFamily{{Key: "power", CanonicalUnit: "W", Units: []types.DomainUnit{{Name: "W", Factor: 1}, {Name: "kW", Aliases: []string{"kilowatt"}, Factor: 1000}}}}, ExtractionHints: "Preserve operating conditions."}
	prompt := contextDomainPrompt(domain)
	for _, text := range []string{"Energy", "power plant", "rated power", "number", "operated_by", "kilowatt", "Preserve operating conditions.", "open fields"} {
		require.Contains(t, prompt, text)
	}
	require.NotContains(t, prompt, "context_length")
	require.Equal(t, "power", canonicalContextField(domain, "  RATED   POWER  "))
	threshold := 1500.0
	fact := types.KnowledgeFact{Kind: types.KnowledgeFactAttribute, Predicate: "power", Value: types.JSON(`2`), ValueType: "number", Unit: "kilowatt"}
	require.True(t, contextFactMatchesValue(domain, fact, types.KnowledgeContextQuery{MinValue: &threshold, Unit: "W"}))
	for _, unit := range []string{"", "tokens", "unknown"} {
		require.False(t, contextFactMatchesValue(domain, fact, types.KnowledgeContextQuery{MinValue: &threshold, Unit: unit}))
	}
	fact.Unit = "unknown"
	require.False(t, contextFactMatchesValue(domain, fact, types.KnowledgeContextQuery{MinValue: &threshold, Unit: "unknown"}))
	fact.Unit = ""
	require.False(t, contextFactMatchesValue(BuiltinAIKnowledgeDomain(), fact, types.KnowledgeContextQuery{MinValue: &threshold, Unit: "tokens"}))
	threshold = 1
	require.True(t, contextFactMatchesValue(domain, fact, types.KnowledgeContextQuery{MinValue: &threshold}))
}

func TestP2GlobalMergeAuditsAndUpdatesAllKnowledgeBases(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	for _, item := range []types.KnowledgeEntity{{ID: "keep", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company"}, {ID: "merge", TenantID: 9, KnowledgeBaseID: "energy-one", CanonicalName: "Acme Holdings", NormalizedName: "acme holdings", EntityType: "company"}} {
		require.NoError(t, db.Create(&item).Error)
	}
	for _, kbID := range []string{"kb", "energy-one"} {
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: kbID, TenantID: 9, KnowledgeBaseID: kbID, SubjectEntityID: "merge", Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: kbID, Quote: "Acme has staff.", Status: "current"}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "alias", TenantID: 9, KnowledgeBaseID: "energy-one", EntityID: "merge", EntityType: "company", Alias: "Acme Holdings", NormalizedAlias: "acme holdings"}).Error)
	require.NoError(t, svc.MergeEntities(ctx, "kb", "keep", "merge"))
	var facts []types.KnowledgeFact
	require.NoError(t, db.Find(&facts).Error)
	require.Len(t, facts, 2)
	for _, fact := range facts {
		require.Equal(t, "keep", fact.SubjectEntityID)
	}
	var audit types.AuditLog
	require.NoError(t, db.First(&audit).Error)
	require.Equal(t, types.AuditAction("knowledge_context.entities_merged"), audit.Action)
	require.Equal(t, "team", audit.ScopeType)
	require.Contains(t, string(audit.Details), "energy-one")
	require.Contains(t, string(audit.Details), "facts_before")
	require.Contains(t, string(audit.Details), "merge")
}

func TestP2MergeAliasCollisionRollsBackGlobalChanges(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	entities := []types.KnowledgeEntity{{ID: "keep", TenantID: 9, EntityType: "company", CanonicalName: "Keep", NormalizedName: "keep"}, {ID: "merge", TenantID: 9, EntityType: "product", CanonicalName: "Merged", NormalizedName: "merged"}, {ID: "third", TenantID: 9, EntityType: "company", CanonicalName: "Third", NormalizedName: "third"}}
	require.NoError(t, db.Create(&entities).Error)
	aliases := []types.KnowledgeEntityAlias{{ID: "source-alias", TenantID: 9, EntityType: "product", EntityID: "merge", Alias: "Shared", NormalizedAlias: "shared"}, {ID: "third-alias", TenantID: 9, EntityType: "company", EntityID: "third", Alias: "Shared", NormalizedAlias: "shared"}}
	require.NoError(t, db.Create(&aliases).Error)
	err := svc.MergeEntities(ctx, "", "keep", "merge")
	require.ErrorContains(t, err, "conflicts")
	var merged types.KnowledgeEntity
	require.NoError(t, db.First(&merged, "id = ?", "merge").Error)
	require.Empty(t, merged.MergedIntoID)
	var alias types.KnowledgeEntityAlias
	require.NoError(t, db.First(&alias, "id = ?", "source-alias").Error)
	require.Equal(t, "merge", alias.EntityID)
	require.Equal(t, "product", alias.EntityType)
	var count int64
	require.NoError(t, db.Model(&types.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestP2ObjectDetailHistoryTogglePreservesIdentity(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "acme", TenantID: 9, CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company"}).Error)
	for _, status := range []string{types.KnowledgeEvidenceCurrent, types.KnowledgeEvidenceSuperseded} {
		p2ContextMessage(t, db, status, "kb", "Acme has 10 staff.")
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: status, TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: status, Version: 1, BodyHash: strings.Repeat("a", 64), Body: "Acme has 10 staff.", Status: status}).Error)
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: status, TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "acme", Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: status, Quote: "Acme has 10 staff.", Status: types.KnowledgeFactCurrent}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "corrected", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "acme", Kind: "attribute", Predicate: "staff", Value: types.JSON(`20`), ValueType: "number", EvidenceID: types.KnowledgeEvidenceCurrent, Quote: "Acme has 10 staff.", Status: types.KnowledgeFactCorrected}).Error)
	history, err := svc.EntityDetail(ctx, "acme")
	require.NoError(t, err)
	require.Len(t, history.Facts, 3)
	require.Len(t, history.Evidence, 2)
	current, err := svc.EntityDetail(ctx, "acme", false)
	require.NoError(t, err)
	require.Len(t, current.Facts, 1)
	require.Len(t, current.Evidence, 1)
	require.Len(t, current.Entities, 1)
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("id = ?", types.KnowledgeEvidenceCurrent).Update("status", types.KnowledgeFactCorrected).Error)
	current, err = svc.EntityDetail(ctx, "acme", false)
	require.NoError(t, err)
	require.Empty(t, current.Facts)
	require.Empty(t, current.Evidence)
	require.Len(t, current.Entities, 1)
	_, err = svc.EntityDetail(ctx, "missing", false)
	require.Error(t, err)
}

func TestP2RestrictedAPIKeyScopesGlobalReadsAndEdits(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "shared", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company", Description: "Private source description"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "private", TenantID: 9, KnowledgeBaseID: "energy-two", CanonicalName: "Private", NormalizedName: "private", EntityType: "company"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "secret-alias", TenantID: 9, KnowledgeBaseID: "kb", EntityID: "shared", EntityType: "company", Alias: "Secret Acme", NormalizedAlias: "secret acme"}).Error)
	for _, kbID := range []string{"kb", "energy-one", "energy-two"} {
		body := "Acme has 10 staff."
		p2ContextMessage(t, db, kbID, kbID, body)
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: kbID, TenantID: 9, KnowledgeBaseID: kbID, KnowledgeID: kbID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: body, Status: types.KnowledgeEvidenceCurrent}).Error)
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: kbID, TenantID: 9, KnowledgeBaseID: kbID, SubjectEntityID: "shared", Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: kbID, Quote: body, Status: types.KnowledgeFactCurrent}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "private", TenantID: 9, KnowledgeBaseID: "energy-two", SubjectEntityID: "private", Kind: "attribute", Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: "energy-two", Quote: "Acme has 10 staff.", Status: types.KnowledgeFactCurrent}).Error)
	event := types.KnowledgeEvent{ID: "shared-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Private aggregate title", Description: "Private aggregate description", OccurredAt: parseContextTime("2025-01-01"), EntityIDs: types.StringArray{"shared"}}
	require.NoError(t, db.Create(&event).Error)
	for _, kbID := range []string{"kb", "energy-one"} {
		require.NoError(t, db.Create(&types.KnowledgeEventEvidence{ID: "link-" + kbID, TenantID: 9, KnowledgeBaseID: kbID, EventID: event.ID, EvidenceID: kbID, EntityIDs: types.StringArray{"shared"}, SourceTitle: "Allowed source title", SourceDescription: "Allowed source description"}).Error)
	}
	for _, fullAccess := range []bool{false, true} {
		keyCtx := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: fullAccess, KnowledgeBaseIDs: types.StringArray{"energy-one"}, Capabilities: types.StringArray{"retrieve", "ingest"}})
		for _, query := range []types.KnowledgeContextQuery{{Scope: "team"}, {Scope: "domain", DomainID: "energy"}} {
			result, err := svc.Query(keyCtx, "", query)
			require.NoError(t, err)
			require.Len(t, result.Facts, 1)
			require.Equal(t, "energy-one", result.Facts[0].KnowledgeBaseID)
			require.Len(t, result.Entities, 1)
			require.Empty(t, result.Entities[0].Description)
			require.Empty(t, result.Entities[0].KnowledgeBaseID)
			require.Empty(t, result.Entities[0].Aliases)
			require.Len(t, result.Events, 1)
			require.Equal(t, "energy-one", result.Events[0].KnowledgeBaseID)
			require.Len(t, result.Events[0].Sources, 1)
			require.Equal(t, "Allowed source title", result.Events[0].Title)
			require.Nil(t, result.Events[0].OccurredAt)
		}
		excluded, err := svc.Query(keyCtx, "", types.KnowledgeContextQuery{Scope: "domain", DomainID: "ai"})
		require.NoError(t, err)
		require.Empty(t, excluded.Facts)
		_, err = svc.Query(keyCtx, "", types.KnowledgeContextQuery{Scope: "knowledge_base", KnowledgeBaseID: "kb"})
		require.Error(t, err)
		_, err = svc.Query(keyCtx, "kb", types.KnowledgeContextQuery{})
		require.Error(t, err)
		evidence, err := svc.ListEvidence(keyCtx, "", types.KnowledgeContextQuery{Scope: "team"})
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		require.Equal(t, "energy-one", evidence[0].KnowledgeBaseID)
		_, err = svc.GetEvidence(keyCtx, "", "kb")
		require.Error(t, err)
		detail, err := svc.EntityDetail(keyCtx, "shared")
		require.NoError(t, err)
		require.Len(t, detail.Facts, 1)
		require.Len(t, detail.Evidence, 1)
		_, err = svc.EntityDetail(keyCtx, "private")
		require.Error(t, err)
		correction := types.KnowledgeFactCorrection{Value: types.JSON(`10`), ValueType: "number", Quote: "Acme has 10 staff.", CorrectionNote: "Correct extraction"}
		_, err = svc.CorrectFact(keyCtx, "", "kb", correction)
		require.Error(t, err)
		require.Error(t, svc.MergeEntities(keyCtx, "", "shared", "private"))
		require.Error(t, svc.MergeEntities(keyCtx, "energy-one", "shared", "private"))
	}
	keyCtx := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, FullAccess: true, KnowledgeBaseIDs: types.StringArray{"energy-one"}})
	corrected, err := svc.CorrectFact(keyCtx, "", "energy-one", types.KnowledgeFactCorrection{Value: types.JSON(`10`), ValueType: "number", Quote: "Acme has 10 staff.", CorrectionNote: "Correct extraction"})
	require.NoError(t, err)
	require.Equal(t, "energy-one", corrected.KnowledgeBaseID)
	var privateFact types.KnowledgeFact
	require.NoError(t, db.First(&privateFact, "id = ?", "kb").Error)
	require.Equal(t, types.KnowledgeFactCurrent, privateFact.Status)
}
