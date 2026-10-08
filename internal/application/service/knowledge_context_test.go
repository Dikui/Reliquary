package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func knowledgeContextTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(`CREATE TABLE tenants (id BIGINT PRIMARY KEY, model_tiers JSONB NOT NULL DEFAULT '{}', deleted_at TIMESTAMPTZ)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tenants(id) VALUES (9)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL DEFAULT 'Test knowledge base', domain_id TEXT NOT NULL DEFAULT 'ai', model_tiers JSONB NOT NULL DEFAULT '{}', embedding_model_id TEXT DEFAULT '', auto_tag_config JSONB, deleted_at TIMESTAMPTZ)`).Error)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeEntity{}, &types.KnowledgeEntityAlias{}, &types.KnowledgeEvidence{},
		&types.KnowledgeEvidenceEntity{}, &types.KnowledgeEntityStar{}, &types.KnowledgeTopicStar{}, &types.KnowledgeTag{}, &types.KnowledgeTagRelation{}, &types.KnowledgeTagManualDecision{}, &types.KnowledgeWorkflow{},
		&types.KnowledgeEntityMergeSuggestion{}, &types.KnowledgeFact{}, &types.KnowledgeEvent{}, &types.KnowledgeEventEvidence{},
		&types.KnowledgeEventEntity{}, &types.KnowledgeEventEvidenceEntity{}, &types.KnowledgeAttention{}, &types.KnowledgeDomain{}))
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_tags ADD COLUMN deleted_at TIMESTAMPTZ`).Error)
	domain := BuiltinAIKnowledgeDomain()
	domain.ID = "ai"
	domain.TenantID = 9
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases(id,tenant_id) VALUES ('kb',9)`).Error)
	return db
}

// Full-extraction regressions explicitly opt into focus with a starred topic.
func focusContextTestMessage(t *testing.T, db *gorm.DB, k *types.Knowledge) {
	t.Helper()
	tag := types.KnowledgeTag{ID: uuid.NewString(), TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID,
		Name: "Full extraction " + k.ID, Kind: types.TagKindTopic}
	require.NoError(t, db.Create(&tag).Error)
	require.NoError(t, db.Create(&types.KnowledgeTopicStar{TenantID: k.TenantID, TagID: tag.ID}).Error)
	require.NoError(t, db.Create(&types.KnowledgeTagRelation{KnowledgeID: k.ID, TagID: tag.ID}).Error)
}

func seedContextEventEntities(t *testing.T, db *gorm.DB, events ...types.KnowledgeEvent) {
	t.Helper()
	var rows []types.KnowledgeEventEntity
	for _, event := range events {
		for _, entityID := range event.EntityIDs {
			rows = append(rows, types.KnowledgeEventEntity{TenantID: event.TenantID, EventID: event.ID, EntityID: entityID})
		}
	}
	if len(rows) > 0 {
		require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error)
	}
}

func seedContextEventEvidenceEntities(t *testing.T, db *gorm.DB, links ...types.KnowledgeEventEvidence) {
	t.Helper()
	var rows []types.KnowledgeEventEvidenceEntity
	for _, link := range links {
		seedContextEventEntities(t, db, types.KnowledgeEvent{TenantID: link.TenantID, ID: link.EventID, EntityIDs: link.EntityIDs})
		for _, entityID := range link.EntityIDs {
			rows = append(rows, types.KnowledgeEventEvidenceEntity{TenantID: link.TenantID, EventID: link.EventID, EvidenceID: link.EvidenceID, EntityID: entityID})
		}
	}
	if len(rows) > 0 {
		require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error)
	}
}

type knowledgeContextTestModelService struct {
	interfaces.ModelService
	model chat.Chat
}

func (s knowledgeContextTestModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.model, nil
}

type knowledgeContextChatInterface interface{ chat.Chat }

type knowledgeContextTestChat struct {
	knowledgeContextChatInterface
	response string
	reply    func([]chat.Message) string
	calls    int
}

func (c *knowledgeContextTestChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	if c.reply != nil {
		return &types.ChatResponse{Content: c.reply(messages)}, nil
	}
	if len(messages) > 0 && strings.Contains(messages[0].Content, "same real-world object") {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":0}`}, nil
	}
	if len(messages) > 0 && strings.Contains(messages[0].Content, "same specific real-world change") {
		return &types.ChatResponse{Content: `{"relation":"UNRELATED","confidence":0}`}, nil
	}
	return &types.ChatResponse{Content: c.response}, nil
}
func (c *knowledgeContextTestChat) GetModelName() string { return "context-test" }
func (c *knowledgeContextTestChat) GetModelID() string   { return "context-test" }

func TestKnowledgeContextCanonicalCollisionKeepsFactAndEventReferences(t *testing.T) {
	db := knowledgeContextTestDB(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT 'summary'`).Error)
	body := "Alpha Inc and A Corp are names of Alpha. Alpha Inc has 10 staff. A Corp has 20 clients. Alpha Inc is also A Corp."
	k := types.Knowledge{ID: "aliases-message", TenantID: 9, KnowledgeBaseID: "kb", Title: "Aliases",
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved",
		SourceInfo: types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.com/aliases", PublishedAt: "2026-10-01"}}))
	require.NoError(t, db.Create(&k).Error)
	focusContextTestMessage(t, db, &k)
	model := &knowledgeContextTestChat{response: `{
		"entities":[
			{"mention":"Alpha Inc","quote":"Alpha Inc and A Corp are names of Alpha.","canonical_name":"Alpha","entity_type":"organization","aliases":[]},
			{"mention":"A Corp","quote":"Alpha Inc and A Corp are names of Alpha.","canonical_name":"Alpha","entity_type":"organization","aliases":[]}],
		"facts":[
			{"kind":"attribute","subject":"Alpha Inc","predicate":"staff","value":10,"value_type":"number","quote":"Alpha Inc has 10 staff."},
			{"kind":"attribute","subject":"A Corp","predicate":"clients","value":20,"value_type":"number","quote":"A Corp has 20 clients."},
			{"kind":"relation","subject":"Alpha Inc","predicate":"also_named","object":"A Corp","value_type":"entity","quote":"Alpha Inc is also A Corp."}],
		"events":[{"title":"Alpha aliases recorded","event_type":"record","entity_mentions":["Alpha Inc","A Corp"],"quote":"Alpha Inc and A Corp are names of Alpha."}],
		"attention":{"score":1,"reason":"recorded"}}`}
	svc := NewKnowledgeContextService(db, knowledgeContextTestModelService{model: model})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	require.NoError(t, svc.ProcessMessage(ctx, &k, body))
	var entities []types.KnowledgeEntity
	var facts []types.KnowledgeFact
	var events []types.KnowledgeEvent
	var links []types.KnowledgeEventEvidence
	require.NoError(t, db.Find(&entities).Error)
	require.NoError(t, db.Find(&facts).Error)
	require.NoError(t, db.Find(&events).Error)
	require.NoError(t, db.Find(&links).Error)
	require.NoError(t, loadContextEventEntityIDs(db, 9, events))
	require.NoError(t, loadContextEventEvidenceEntityIDs(db, 9, links))
	require.Len(t, entities, 1)
	require.Len(t, facts, 3)
	for _, fact := range facts {
		require.Equal(t, entities[0].ID, fact.SubjectEntityID)
		if fact.Kind == types.KnowledgeFactRelation {
			require.Equal(t, entities[0].ID, fact.ObjectEntityID)
		}
	}
	require.Len(t, events, 1)
	require.Equal(t, types.StringArray{entities[0].ID}, events[0].EntityIDs)
	require.Len(t, links, 1)
	require.Equal(t, types.StringArray{entities[0].ID}, links[0].EntityIDs, "source entity references must use the persisted identity")
	result, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{EntityID: entities[0].ID})
	require.NoError(t, err)
	require.Len(t, result.Facts, 3)
	require.Len(t, result.Events, 1)
}

func TestKnowledgeContextProcessSnapshotCorrectionAndTenantScope(t *testing.T) {
	db := knowledgeContextTestDB(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT ''`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledge_bases SET summary_model_id = 'summary' WHERE id = 'kb'`).Error)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	body := "ACME Inc. acquired WidgetCo on 2026-09-25."
	k := types.Knowledge{ID: "message", TenantID: 9, KnowledgeBaseID: "kb", Title: "Acquisition", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	firstPublishedAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Daily News", OriginalURL: "https://news.example/story", PublishedAt: firstPublishedAt}}))
	require.NoError(t, db.Create(&k).Error)
	focusContextTestMessage(t, db, &k)
	firstResponse := `{"entities":[{"mention":"ACME Inc.","quote":"ACME Inc. acquired WidgetCo","canonical_name":"Acme Holdings","entity_type":"organization","description":"A business organization","aliases":["ACME Inc."]},{"mention":"WidgetCo","quote":"ACME Inc. acquired WidgetCo","canonical_name":"WidgetCo","entity_type":"organization","description":"WidgetCo","aliases":[]}],"facts":[{"kind":"relation","subject":"ACME Inc.","predicate":"acquired","value":null,"value_type":"entity","unit":"","object":"WidgetCo","effective_from":"2026-09-25","effective_to":"","quote":"ACME Inc. acquired WidgetCo on 2026-09-25."},{"kind":"attribute","subject":"ACME Inc.","predicate":"transaction_status","value":"complete","value_type":"string","unit":"","object":"","effective_from":"2026-09-25","effective_to":"","quote":"ACME Inc. acquired WidgetCo on 2026-09-25."}],"events":[{"title":"Acme Holdings acquired WidgetCo","event_type":"acquisition","description":"Acme Holdings acquired WidgetCo","occurred_at":"2026-09-25","entity_mentions":["ACME Inc.","WidgetCo"],"quote":"ACME Inc. acquired WidgetCo on 2026-09-25."}],"attention":{"novelty":60,"impact":80,"credibility":70,"relevance":75,"actionability":40,"score":68,"reason":"A verified acquisition changes ownership."}}`
	secondBody := "ACME Inc. acquired WidgetCo on 2026-09-25. An independent report confirms the same transaction."
	secondResponse := `{"entities":[{"mention":"ACME Inc.","quote":"ACME Inc. acquired WidgetCo","canonical_name":"Acme Holdings","entity_type":"organization","description":"A business organization","aliases":["ACME Inc."]},{"mention":"WidgetCo","quote":"ACME Inc. acquired WidgetCo","canonical_name":"WidgetCo","entity_type":"organization","description":"WidgetCo","aliases":[]}],"facts":[{"kind":"relation","subject":"ACME Inc.","predicate":"acquired","value":null,"value_type":"entity","unit":"","object":"WidgetCo","effective_from":"2026-09-25","effective_to":"","quote":"ACME Inc. acquired WidgetCo on 2026-09-25."},{"kind":"attribute","subject":"ACME Inc.","predicate":"transaction_status","value":"confirmed","value_type":"string","unit":"","object":"","effective_from":"2026-09-25","effective_to":"","quote":"ACME Inc. acquired WidgetCo on 2026-09-25."}],"events":[{"title":"Independent report confirms Acme Holdings acquisition","event_type":"acquisition","description":"A separate outlet confirms this acquisition","occurred_at":"2026-09-25","entity_mentions":["ACME Inc.","WidgetCo"],"quote":"ACME Inc. acquired WidgetCo on 2026-09-25."}],"attention":{"novelty":45,"impact":80,"credibility":85,"relevance":75,"actionability":40,"score":70,"reason":"An independent report corroborates the acquisition."}}`
	model := &knowledgeContextTestChat{reply: func(messages []chat.Message) string {
		if strings.Contains(messages[0].Content, "same specific real-world change") {
			var input struct {
				Candidates []struct {
					ID string `json:"id"`
				} `json:"candidates"`
			}
			if len(messages) > 1 {
				_ = json.Unmarshal([]byte(messages[1].Content), &input)
			}
			if len(input.Candidates) > 0 {
				return fmt.Sprintf(`{"relation":"SAME_OCCURRENCE","event_id":%q,"confidence":0.99}`, input.Candidates[0].ID)
			}
			return `{"event_id":"","confidence":0}`
		}
		if len(messages) > 1 && strings.Contains(messages[1].Content, "An independent report") {
			return secondResponse
		}
		return firstResponse
	}}
	svc := NewKnowledgeContextService(db, knowledgeContextTestModelService{model: model})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "reviewer")
	require.NoError(t, svc.ProcessMessage(ctx, &k, body))
	require.NoError(t, svc.ProcessMessage(ctx, &k, body))
	require.Equal(t, 6, model.calls, "a focused source adds one full extraction pass; completed snapshots remain idempotent")

	result, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}})
	require.NoError(t, err)
	require.Len(t, result.Entities, 2)
	require.Len(t, result.Facts, 2)
	var correctionTarget string
	for _, fact := range result.Facts {
		if fact.Kind == types.KnowledgeFactRelation {
			require.Equal(t, "acquired", fact.Predicate)
		}
		if fact.Kind == types.KnowledgeFactAttribute {
			correctionTarget = fact.ID
		}
		require.Equal(t, body, fact.Quote)
	}
	require.Equal(t, body, result.Evidence[0].Body)
	require.Len(t, result.Events, 1)
	require.Len(t, result.Attention, 1)
	require.Equal(t, "WidgetCo", result.Entities[1].CanonicalName)
	firstEventID := result.Events[0].ID

	// A second approved source repeats the same organization under an existing
	// alias and describes the acquisition as a corroborating report. Entity
	// resolution should reuse the canonical object; event resolution must
	// explicitly confirm this is the same real-world change.
	second := types.Knowledge{ID: "second-message", TenantID: 9, KnowledgeBaseID: "kb", Title: "Independent confirmation", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	secondPublishedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	require.NoError(t, second.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Second Gazette", OriginalURL: "https://second.example/story", PublishedAt: secondPublishedAt}}))
	require.NoError(t, db.Create(&second).Error)
	focusContextTestMessage(t, db, &second)
	require.NoError(t, svc.ProcessMessage(ctx, &second, secondBody))
	require.GreaterOrEqual(t, model.calls, 3, "the second source must be extracted and its event compared to existing events")
	combined, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID, second.ID}})
	require.NoError(t, err)
	require.Len(t, combined.Entities, 2, "the repeated alias should resolve to the existing canonical organization")
	require.Len(t, combined.Facts, 4)
	require.Len(t, combined.Events, 1, "model-confirmed same-change reports should share one event")
	require.Equal(t, firstEventID, combined.Events[0].ID)
	require.Len(t, combined.Events[0].EvidenceIDs, 2)
	require.Equal(t, 2, combined.Events[0].SourceCount)
	require.Contains(t, combined.Entities[0].Aliases, "ACME Inc.")

	_, anonymousCorrectionErr := svc.CorrectFact(context.WithValue(ctx, types.UserIDContextKey, ""), "kb", correctionTarget, types.KnowledgeFactCorrection{
		Value: types.JSON(`"confirmed"`), ValueType: "string", Quote: body,
		CorrectionNote: "No authenticated actor.",
	})
	require.Error(t, anonymousCorrectionErr, "a correction cannot be stored without an authenticated actor")

	_, invalidCorrectionErr := svc.CorrectFact(ctx, "kb", correctionTarget, types.KnowledgeFactCorrection{
		Value: types.JSON(`"not-a-number"`), ValueType: "number", Quote: body,
		CorrectionNote: "This invalid typed value must not be stored.",
	})
	require.Error(t, invalidCorrectionErr, "a string must not be accepted as a numeric fact value")
	beforeValidCorrection, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}})
	require.NoError(t, err)
	require.Len(t, beforeValidCorrection.Facts, 2, "a rejected correction must not create a current fact")

	corrected, err := svc.CorrectFact(ctx, "kb", correctionTarget, types.KnowledgeFactCorrection{
		Value: types.JSON(`"confirmed"`), ValueType: "string", Quote: body,
		CorrectionNote: "Reviewed against the original source.",
	})
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeFactCurrent, corrected.Status)
	require.Equal(t, "web_user:reviewer", corrected.CorrectionActor)
	var storedCorrection types.KnowledgeFact
	require.NoError(t, db.First(&storedCorrection, "id = ?", corrected.ID).Error)
	require.Equal(t, corrected.CorrectionActor, storedCorrection.CorrectionActor)
	require.Equal(t, correctionTarget, storedCorrection.SupersedesID)
	current, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}})
	require.NoError(t, err)
	require.Len(t, current.Facts, 2)
	require.Contains(t, []string{current.Facts[0].ID, current.Facts[1].ID}, corrected.ID)
	history, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}, IncludeHistory: true})
	require.NoError(t, err)
	require.Len(t, history.Facts, 3)
	for _, fact := range history.Facts {
		if fact.ID == correctionTarget {
			require.Equal(t, corrected.ID, fact.CorrectedBy)
			require.Empty(t, fact.CorrectionActor, "the original version must not be attributed to its corrector")
		}
		if fact.ID == corrected.ID {
			require.Equal(t, "web_user:reviewer", fact.CorrectionActor)
		}
	}

	updatedBody := body + " The closing is complete."
	updatedEvidence, err := svc.SnapshotMessage(ctx, &k, updatedBody)
	require.NoError(t, err)
	oldEvidence, err := svc.GetEvidence(ctx, "kb", result.Evidence[0].ID)
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeEvidenceSuperseded, oldEvidence.Status)
	require.Equal(t, updatedEvidence.ID, oldEvidence.SupersededByID)
	require.Equal(t, types.KnowledgeEvidenceCurrent, updatedEvidence.Status)
	updatedHistory, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}, IncludeHistory: true})
	require.NoError(t, err)
	require.Len(t, updatedHistory.Facts, 3)
	for _, fact := range updatedHistory.Facts {
		require.Equal(t, types.KnowledgeFactSuperseded, fact.Status)
	}

	candidate := types.Knowledge{ID: "candidate-message", TenantID: 9, KnowledgeBaseID: "kb", Title: "Unpromoted replacement", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"source_update_candidate":true,"intake":{"decision":"approved"}}`), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, db.Create(&candidate).Error)
	callsBeforeCandidate := model.calls
	require.NoError(t, svc.ProcessMessage(ctx, &candidate, body))
	require.Equal(t, callsBeforeCandidate, model.calls, "an unpromoted source replacement must not enter extraction")
	var candidateEvidenceCount int64
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("knowledge_id = ?", candidate.ID).Count(&candidateEvidenceCount).Error)
	require.Zero(t, candidateEvidenceCount, "processing an unpromoted replacement must not create evidence or facts")
	result, err = svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{candidate.ID}})
	require.NoError(t, err)
	require.Empty(t, result.Evidence)
	require.Empty(t, result.Facts)

	// Existing derived rows must also stay hidden if a candidate was indexed
	// before its update was marked for review.
	candidateEvidence := types.KnowledgeEvidence{ID: "candidate-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: candidate.ID, Version: 1, BodyHash: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Body: body, Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: time.Now()}
	require.NoError(t, db.Create(&candidateEvidence).Error)
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "candidate-fact", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: combined.Entities[0].ID, Kind: types.KnowledgeFactAttribute, Predicate: "status", Value: types.JSON(`"candidate"`), ValueType: "string", EvidenceID: candidateEvidence.ID, Quote: body, Status: types.KnowledgeFactCurrent, CreatedAt: time.Now()}).Error)
	result, err = svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{candidate.ID}})
	require.NoError(t, err)
	require.Empty(t, result.Evidence, "candidate evidence must remain unqueryable until promotion")
	require.Empty(t, result.Facts, "candidate facts must remain unqueryable until promotion")

	wrongTenant := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(10))
	require.Error(t, svc.ProcessMessage(wrongTenant, &k, body))
}

func TestKnowledgeContextQueryRestrictsAggregatesToAuthorizedMessages(t *testing.T) {
	db := knowledgeContextTestDB(t)
	now := time.Now().UTC()
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	sources := []types.Knowledge{
		{ID: "allowed-message", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"approved"}}`), CreatedAt: now, UpdatedAt: now},
		{ID: "allowed-newer-message", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"approved"}}`), CreatedAt: now, UpdatedAt: now},
		{ID: "denied-message", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"approved"}}`), CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&sources).Error)
	entities := []types.KnowledgeEntity{
		{ID: "allowed-entity", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "AllowedCo", NormalizedName: "allowedco", Description: "PRIVATE SOURCE DESCRIPTION", CreatedAt: now, UpdatedAt: now},
		{ID: "denied-entity", TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: "HiddenCo", NormalizedName: "hiddenco", Description: "PRIVATE HIDDEN DESCRIPTION", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&entities).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "private-alias", TenantID: 9, KnowledgeBaseID: "kb", EntityID: "allowed-entity", Alias: "Private Alias", NormalizedAlias: "private alias", CreatedAt: now}).Error)
	evidence := []types.KnowledgeEvidence{
		{ID: "allowed-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "allowed-message", Version: 1, BodyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Body: "Allowed source", PublishedAt: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339), Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
		{ID: "allowed-newer-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "allowed-newer-message", Version: 1, BodyHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Body: "Allowed newer source", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339), Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
		{ID: "denied-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "denied-message", Version: 1, BodyHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Body: "Private source", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
	}
	require.NoError(t, db.Create(&evidence).Error)
	facts := []types.KnowledgeFact{
		{ID: "allowed-fact", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "allowed-entity", Kind: types.KnowledgeFactAttribute, Predicate: "status", Value: types.JSON(`"open"`), ValueType: "string", EvidenceID: "allowed-evidence", Quote: "AllowedCo is open", Status: types.KnowledgeFactCurrent, CreatedAt: now},
		{ID: "denied-fact", TenantID: 9, KnowledgeBaseID: "kb", SubjectEntityID: "denied-entity", Kind: types.KnowledgeFactAttribute, Predicate: "status", Value: types.JSON(`"private"`), ValueType: "string", EvidenceID: "denied-evidence", Quote: "HiddenCo is private", Status: types.KnowledgeFactCurrent, CreatedAt: now},
	}
	require.NoError(t, db.Create(&facts).Error)
	privateEventTime := now.Add(24 * time.Hour)
	visibleEventTime := now.Add(-10 * 24 * time.Hour)
	staleEventTime := now.Add(-20 * 24 * time.Hour)
	event := types.KnowledgeEvent{ID: "shared-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "PRIVATE AGGREGATE TITLE", Description: "PRIVATE AGGREGATE DESCRIPTION", OccurredAt: &privateEventTime, EntityIDs: types.StringArray{"allowed-entity", "denied-entity"}, EvidenceIDs: types.StringArray{"allowed-evidence", "denied-evidence"}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&event).Error)
	newVisibleEventTime := now.Add(-5 * 24 * time.Hour)
	newVisibleEvent := types.KnowledgeEvent{ID: "newer-visible-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "PRIVATE SORTING TITLE", Description: "PRIVATE SORTING DESCRIPTION", OccurredAt: &staleEventTime, EntityIDs: types.StringArray{"allowed-entity"}, EvidenceIDs: types.StringArray{"allowed-newer-evidence"}, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&newVisibleEvent).Error)
	links := []types.KnowledgeEventEvidence{
		{ID: "allowed-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: event.ID, EvidenceID: "allowed-evidence", EntityIDs: types.StringArray{"allowed-entity"}, SourceTitle: "Allowed source event", SourceEventType: "allowed type", SourceDescription: "Supported by the permitted message", SourceOccurredAt: &visibleEventTime, CreatedAt: now},
		{ID: "allowed-newer-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: event.ID, EvidenceID: "allowed-newer-evidence", EntityIDs: types.StringArray{"allowed-entity"}, SourceTitle: "Newest permitted report", SourceEventType: "allowed type", SourceDescription: "Newest permitted report about the event", CreatedAt: now},
		{ID: "newer-visible-event-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: newVisibleEvent.ID, EvidenceID: "allowed-newer-evidence", EntityIDs: types.StringArray{"allowed-entity"}, SourceTitle: "Permitted event with a visible date", SourceEventType: "allowed type", SourceDescription: "A newer visible event", SourceOccurredAt: &newVisibleEventTime, CreatedAt: now},
		{ID: "denied-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: event.ID, EvidenceID: "denied-evidence", EntityIDs: types.StringArray{"denied-entity"}, SourceTitle: "PRIVATE HIDDEN EVENT", SourceEventType: "private type", SourceDescription: "PRIVATE HIDDEN DESCRIPTION", SourceOccurredAt: &privateEventTime, CreatedAt: now},
	}
	require.NoError(t, db.Create(&links).Error)
	seedContextEventEvidenceEntities(t, db, links...)
	attention := []types.KnowledgeAttention{
		{ID: "allowed-attention", TenantID: 9, KnowledgeBaseID: "kb", EvidenceID: "allowed-evidence", Reason: "authorized", CreatedAt: now},
		{ID: "allowed-newer-attention", TenantID: 9, KnowledgeBaseID: "kb", EvidenceID: "allowed-newer-evidence", Reason: "newer authorized", CreatedAt: now},
		{ID: "denied-attention", TenantID: 9, KnowledgeBaseID: "kb", EvidenceID: "denied-evidence", Reason: "private", CreatedAt: now},
	}
	require.NoError(t, db.Create(&attention).Error)

	svc := NewKnowledgeContextService(db, nil)
	result, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{"allowed-message", "allowed-newer-message"}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	require.Equal(t, "allowed-fact", result.Facts[0].ID)
	require.Len(t, result.Events, 2)
	require.Equal(t, "newer-visible-event", result.Events[0].ID, "restricted pagination must use visible event dates, not aggregate timestamps")
	var sharedEvent types.KnowledgeEvent
	for _, item := range result.Events {
		if item.ID == event.ID {
			sharedEvent = item
		}
	}
	require.NotNil(t, sharedEvent.OccurredAt)
	require.Equal(t, "Newest permitted report", sharedEvent.Title)
	require.Equal(t, "Newest permitted report about the event", sharedEvent.Description)
	require.Equal(t, visibleEventTime, *sharedEvent.OccurredAt, "a newer permitted source without a date must not erase an older permitted date")
	require.Equal(t, types.StringArray{"allowed-entity"}, sharedEvent.EntityIDs)
	require.ElementsMatch(t, []string{"allowed-evidence", "allowed-newer-evidence"}, []string(sharedEvent.EvidenceIDs))
	pagedEvents, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{"allowed-message", "allowed-newer-message"}, Limit: 1})
	require.NoError(t, err)
	require.Len(t, pagedEvents.Events, 1)
	require.Equal(t, "newer-visible-event", pagedEvents.Events[0].ID)
	require.Len(t, result.Entities, 1)
	require.Equal(t, "allowed-entity", result.Entities[0].ID)
	require.Empty(t, result.Entities[0].Description)
	require.Empty(t, result.Entities[0].Aliases)
	require.Len(t, result.Evidence, 2)
	require.Contains(t, []string{result.Evidence[0].ID, result.Evidence[1].ID}, "allowed-evidence")
	require.Contains(t, []string{result.Evidence[0].ID, result.Evidence[1].ID}, "allowed-newer-evidence")
	require.Len(t, result.Attention, 2)

	entityPage, err := svc.ListEntities(ctx, "kb", types.KnowledgeContextQuery{Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, entityPage, 1)
	require.Equal(t, "denied-entity", entityPage[0].ID)
	entityQueryPage, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, entityQueryPage.Entities, 1)
	require.Equal(t, "denied-entity", entityQueryPage.Entities[0].ID)
	entityEvidenceIDs := make([]string, 0, len(entityQueryPage.Evidence))
	for _, item := range entityQueryPage.Evidence {
		entityEvidenceIDs = append(entityEvidenceIDs, item.ID)
	}
	require.Contains(t, entityEvidenceIDs, "denied-evidence", "the entity page keeps its backing evidence even when its fact is on another page")

	require.NoError(t, svc.DeleteMessages(ctx, "kb", []string{"allowed-message"}))
	_, err = svc.GetEvidence(ctx, "kb", "allowed-evidence")
	require.Error(t, err, "explicit deletion removes the deleted source's evidence")
	remaining, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{"denied-message"}})
	require.NoError(t, err)
	require.Len(t, remaining.Facts, 1)
	require.Len(t, remaining.Events, 1)
	require.Equal(t, types.StringArray{"denied-entity"}, remaining.Events[0].EntityIDs)
	var evidenceCount int64
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("knowledge_base_id = ? AND knowledge_id = ?", "kb", "allowed-message").Count(&evidenceCount).Error)
	require.Zero(t, evidenceCount)
	require.NoError(t, svc.DeleteKnowledgeBase(ctx, "kb"))
	var eventCount int64
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Where("knowledge_base_id = ?", "kb").Count(&eventCount).Error)
	require.Zero(t, eventCount)
}

func TestContextHeatUsesPublicationTimeAndDeduplicatesPublisher(t *testing.T) {
	now := time.Now().UTC()
	items := []types.KnowledgeEvidence{
		{Publisher: "Daily News", OriginalURL: "https://one.example/a", PublishedAt: now.Add(-25 * time.Hour).Format(time.RFC3339)},
		{Publisher: "Daily News", OriginalURL: "https://two.example/a", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339)},
		{Publisher: "Wire Service", OriginalURL: "https://wire.example/a", PublishedAt: now.Add(-47 * time.Hour).Format(time.RFC3339)},
		{Publisher: "Old Outlet", OriginalURL: "https://old.example/a", PublishedAt: now.Add(-49 * time.Hour).Format(time.RFC3339)},
	}
	count, heat := contextHeat(items, contextHeatWindow, contextHeatHalfLife)
	require.Equal(t, 2, count)
	require.InDelta(t, 1.0/2.0+1.0/3.9, heat, 0.03)
}
