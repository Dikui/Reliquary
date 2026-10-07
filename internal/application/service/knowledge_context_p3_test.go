package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func p3ContextFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService) {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "P3 team"}).Error)
	domain := BuiltinAIKnowledgeDomain()
	domain.ID = "p3-domain"
	domain.TenantID = 1
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "p3-kb", TenantID: 1, DomainID: domain.ID, Name: "P3 sources", SummaryModelID: "p3-chat", ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50}}).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "p3-reviewer")
	return db, ctx, NewKnowledgeContextService(db, nil)
}
func p3Message(t *testing.T, db *gorm.DB, id, body string) *types.Knowledge {
	t.Helper()
	k := &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "p3-kb", Title: id, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{PublishedAt: "2026-10-07", Publisher: "P3 source"}}))
	require.NoError(t, db.Create(k).Error)
	return k
}
func p3Entity(id, name, kind string) types.KnowledgeEntity {
	return types.KnowledgeEntity{ID: id, TenantID: 1, KnowledgeBaseID: "p3-kb", CanonicalName: name, NormalizedName: normalizeContextName(name), EntityType: kind, CreatedAt: time.Now(), UpdatedAt: time.Now()}
}
func p3Evidence(t *testing.T, db *gorm.DB, body string) *types.KnowledgeEvidence {
	t.Helper()
	k := p3Message(t, db, uuid.NewString(), body)
	e := &types.KnowledgeEvidence{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: body, Status: "current", CreatedAt: time.Now()}
	require.NoError(t, db.Create(e).Error)
	return e
}

type p3ChatInterface interface{ chat.Chat }
type p3CountingChat struct {
	p3ChatInterface
	mu    sync.Mutex
	calls map[string]int
	reply func(context.Context, []chat.Message) string
}

func (m *p3CountingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage := "extraction"
	if strings.Contains(messages[0].Content, "same real-world object") {
		stage = "entity"
	}
	if strings.Contains(messages[0].Content, "same specific real-world change") {
		stage = "event"
	}
	if strings.Contains(messages[0].Content, "Score attention") {
		stage = "attention"
	}
	m.mu.Lock()
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	m.calls[stage]++
	m.mu.Unlock()
	return &types.ChatResponse{Content: m.reply(ctx, messages)}, nil
}
func (m *p3CountingChat) GetModelID() string   { return "p3-chat" }
func (m *p3CountingChat) GetModelName() string { return "P3 test chat" }
func (m *p3CountingChat) count(stage string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[stage]
}

func TestP3ObjectJudgmentBudgetIsIndependentOfRegistrySize(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	results := map[int]int{}
	for _, size := range []int{50, 5000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db, ctx, svc := p3ContextFixture(t)
			seeded := make([]types.KnowledgeEntity, size)
			for i := range seeded {
				seeded[i] = p3Entity(fmt.Sprintf("registered-%d", i), fmt.Sprintf("Registered Object %04d", i), "model")
			}
			require.NoError(t, db.CreateInBatches(&seeded, 500).Error)
			require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "registered-alias", TenantID: 1, KnowledgeBaseID: "p3-kb", EntityType: "model", EntityID: seeded[2].ID, Alias: "已有别名", NormalizedAlias: "已有别名", CreatedAt: time.Now()}).Error)
			names := []string{"Registered Object 0000", "REGISTERED OBJECT 0001", "已有别名", "New Mention Alpha", "New Mention Beta", "New Mention Gamma", "New Mention Delta", "New Mention Epsilon", "New Mention Zeta", "New Mention Eta"}
			extraction := contextExtraction{Attention: contextExtractedAttention{Reason: "Source-supported test."}}
			for _, name := range names {
				extraction.Entities = append(extraction.Entities, contextExtractedEntity{Mention: name, Canonical: name, EntityType: "model", Quote: name})
			}
			encoded, err := json.Marshal(extraction)
			require.NoError(t, err)
			model := &p3CountingChat{reply: func(_ context.Context, messages []chat.Message) string {
				if strings.Contains(messages[0].Content, "same real-world object") {
					var payload struct {
						Candidates []types.KnowledgeEntity `json:"candidates"`
					}
					require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
					require.LessOrEqual(t, len(payload.Candidates), 20)
					return `{"decision":"new","confidence":1}`
				}
				return string(encoded)
			}}
			svc.models = knowledgeContextTestModelService{model: model}
			body := strings.Join(names, ". ")
			k := p3Message(t, db, "scale-message", body)
			require.NoError(t, svc.ProcessMessage(ctx, k, body))
			require.Equal(t, 7, model.count("entity"), "each of the seven non-exact mentions needs one judgment")
			results[size] = model.count("entity")
			t.Logf("registry=%d, mentions=%d, exact=3, entity_judgments=%d", size, len(names), model.count("entity"))
		})
	}
	require.Equal(t, results[50], results[5000])
}

func TestP3ExactNamesRecallAliasesAndVersions(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entities := []types.KnowledgeEntity{p3Entity("moonshot", "Moonshot AI", "company"), p3Entity("kimi-k2", "Kimi K2", "model"), p3Entity("different-type", "Same Name", "product")}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, entities); return err }))
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "moonshot-cn", TenantID: 1, KnowledgeBaseID: "p3-kb", EntityType: "company", EntityID: "moonshot", Alias: "月之暗面", NormalizedAlias: "月之暗面", CreatedAt: time.Now()}).Error)
	model := &p3CountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		var payload struct {
			Mention    string `json:"mention"`
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		if payload.Mention == "MoonshotAI" {
			require.NotEmpty(t, payload.Candidates)
			return `{"decision":"match","entity_id":"moonshot","confidence":0.99}`
		}
		return `{"decision":"new","confidence":1}`
	}}
	input := []contextExtractedEntity{{Mention: "MOONSHOT AI", Canonical: "Moonshot AI", EntityType: "organization", Quote: "MOONSHOT AI"}, {Mention: "月之暗面", Canonical: "Moonshot AI", EntityType: "公司", Quote: "月之暗面"}, {Mention: "MoonshotAI", Canonical: "MoonshotAI", EntityType: "company", Quote: "MoonshotAI"}, {Mention: "Kimi K2.5", Canonical: "Kimi K2", EntityType: "model", Quote: "Kimi K2.5"}, {Mention: "Same Name", Canonical: "Same Name", EntityType: "company", Quote: "Same Name"}}
	evidence := p3Evidence(t, db, "MOONSHOT AI 月之暗面 MoonshotAI Kimi K2.5 Same Name")
	resolved, mentions, err := svc.resolveContextEntities(withContextResolutionEvidence(ctx, evidence), model, 1, "p3-kb", input)
	require.NoError(t, err)
	require.Equal(t, 3, model.count("entity"))
	require.Equal(t, "moonshot", mentions[normalizeContextName("MoonshotAI")].ID)
	require.NotEqual(t, "kimi-k2", mentions[normalizeContextName("Kimi K2.5")].ID)
	require.Equal(t, "Kimi K2.5", mentions[normalizeContextName("Kimi K2.5")].CanonicalName)
	require.NotEqual(t, "different-type", mentions[normalizeContextName("Same Name")].ID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, resolved); return err }))
	var alias types.KnowledgeEntityAlias
	require.NoError(t, db.First(&alias, "normalized_alias=?", "moonshotai").Error)
	require.Equal(t, "model", alias.ResolutionMethod)
	require.Equal(t, evidence.ID, alias.EvidenceID)
	require.Equal(t, "MoonshotAI", alias.Quote)
	before := model.count("entity")
	_, _, err = svc.resolveContextEntities(ctx, model, 1, "p3-kb", []contextExtractedEntity{input[2]})
	require.NoError(t, err)
	require.Equal(t, before, model.count("entity"))
}

func TestP3MergeSuggestionsCreateAcceptAndReject(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(ptrP3Entity(p3Entity("acme", "Acme Systems", "company"))).Error)
	evidence := p3Evidence(t, db, "Acme Systems Ltd is growing.")
	model := &p3CountingChat{reply: func(context.Context, []chat.Message) string {
		return `{"decision":"uncertain","candidate_ids":["acme"],"reason":"The legal suffix is unclear.","confidence":0.5}`
	}}
	resolved, _, err := svc.resolveContextEntities(withContextResolutionEvidence(ctx, evidence), model, 1, "p3-kb", []contextExtractedEntity{{Mention: "Acme Systems Ltd", Canonical: "Acme Systems", EntityType: "company", Quote: evidence.Body}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.NotEqual(t, "acme", resolved[0].ID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, resolved); return err }))
	items, err := svc.ListMergeSuggestions(ctx, "", types.KnowledgeContextQuery{Scope: "team"}, "pending")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "Acme Systems Ltd", items[0].NewEntityName)
	require.Equal(t, "Acme Systems", items[0].CandidateEntityName)
	accepted, err := svc.DecideMergeSuggestion(ctx, items[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "accepted", accepted.Status)
	var merged types.KnowledgeEntity
	require.NoError(t, db.First(&merged, "id=?", items[0].NewEntityID).Error)
	require.Equal(t, "acme", merged.MergedIntoID)
	var auditCount int64
	require.NoError(t, db.Model(&types.AuditLog{}).Where("action=?", "knowledge_context.entities_merged").Count(&auditCount).Error)
	require.EqualValues(t, 1, auditCount)
	other := p3Entity("other-acme", "Acme Regional", "company")
	require.NoError(t, db.Create(&other).Error)
	rejected := types.KnowledgeEntityMergeSuggestion{ID: "reject-suggestion", TenantID: 1, KnowledgeBaseID: "p3-kb", NewEntityID: other.ID, CandidateEntityID: "acme", EvidenceID: evidence.ID, Reason: "Unclear", Quote: evidence.Body, Status: "pending", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&rejected).Error)
	decision, err := svc.DecideMergeSuggestion(ctx, rejected.ID, false)
	require.NoError(t, err)
	require.Equal(t, "rejected", decision.Status)
	require.NoError(t, db.First(&other, "id=?", other.ID).Error)
	require.Empty(t, other.MergedIntoID)
	_, err = svc.DecideMergeSuggestion(ctx, rejected.ID, true)
	require.Error(t, err)
}
func ptrP3Entity(entity types.KnowledgeEntity) *types.KnowledgeEntity { return &entity }

type p3EmbeddingInterface interface{ embedding.Embedder }
type p3Embedder struct {
	p3EmbeddingInterface
	dimension int
	calls     int
}

func (m *p3Embedder) Embed(context.Context, string) ([]float32, error) {
	m.calls++
	v := make([]float32, m.dimension)
	v[0] = 1
	return v, nil
}
func (m *p3Embedder) GetDimensions() int { return m.dimension }
func (m *p3Embedder) GetModelID() string { return "p3-embedding" }

type p3EmbeddingModels struct {
	interfaces.ModelService
	model embedding.Embedder
}

func (m p3EmbeddingModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.model, nil
}

func TestP3VectorRecallSkipsStaleModelsAndMismatchedDimensions(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Model(&types.Tenant{}).Where("id=1").Update("model_tiers", types.ModelTierConfig{Embedding: "p3-embedding"}).Error)
	records := []types.KnowledgeEntity{p3Entity("vector-good", "Unique unrelated canonical", "model"), p3Entity("vector-wrong-dimension", "Foreign vector dimension", "model"), p3Entity("vector-stale-model", "Obsolete model vector", "model")}
	records[0].NameEmbedding = types.ContextNameEmbedding{1, 0}
	records[0].EmbeddingModelID = "p3-embedding"
	records[0].EmbeddingDimension = 2
	records[1].NameEmbedding = types.ContextNameEmbedding{1, 0, 0}
	records[1].EmbeddingModelID = "p3-embedding"
	records[1].EmbeddingDimension = 3
	records[2].NameEmbedding = types.ContextNameEmbedding{1, 0}
	records[2].EmbeddingModelID = "previous-embedding"
	records[2].EmbeddingDimension = 2
	require.NoError(t, db.Create(&records).Error)
	embedder := &p3Embedder{dimension: 2}
	svc.models = p3EmbeddingModels{model: embedder}
	judgment := &p3CountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		var payload struct {
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		require.Len(t, payload.Candidates, 1)
		require.Equal(t, "vector-good", payload.Candidates[0].ID)
		return `{"decision":"match","entity_id":"vector-good","confidence":0.99}`
	}}
	_, mentions, err := svc.resolveContextEntities(ctx, judgment, 1, "p3-kb", []contextExtractedEntity{{Mention: "中文不同名称", Canonical: "中文不同名称", EntityType: "model", Quote: "中文不同名称"}})
	require.NoError(t, err)
	require.Equal(t, "vector-good", mentions["中文不同名称"].ID)
	require.Equal(t, 1, embedder.calls)
	for _, id := range []string{"vector-wrong-dimension", "vector-stale-model"} {
		var record types.KnowledgeEntity
		require.NoError(t, db.First(&record, "id=?", id).Error)
		require.False(t, record.EmbeddingNeedsRebuild, "mention resolution must not mark the registry")
	}
}

func TestP3EventRecallRequiresObjectsAndSevenDayWindow(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	timeAt := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	old := timeAt.Add(-8 * 24 * time.Hour)
	require.NoError(t, db.Create(&[]types.KnowledgeEntity{p3Entity("object", "Object", "company"), p3Entity("other", "Other", "company")}).Error)
	records := []types.KnowledgeEvent{{ID: "inside", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Inside", OccurredAt: &timeAt, EntityIDs: types.StringArray{"object"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, {ID: "outside", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Outside", OccurredAt: &old, EntityIDs: types.StringArray{"object"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, {ID: "other-object", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Other", OccurredAt: &timeAt, EntityIDs: types.StringArray{"other"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, {ID: "no-date", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Unknown", EntityIDs: types.StringArray{"object"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	require.NoError(t, db.Create(&records).Error)
	seedContextEventEntities(t, db, records...)
	input := contextExtractedEvent{Title: "Object release", OccurredAt: "2026-10-07", Quote: "Object release on 2026-10-07", EntityMentions: []string{"Object"}}
	candidates, err := svc.recallContextEvents(ctx, 1, input, types.StringArray{"object"})
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	for _, candidate := range candidates {
		require.Contains(t, []string{"inside", "no-date"}, candidate.ID)
		require.Equal(t, types.StringArray{"object"}, candidate.EntityIDs, "judgment payloads retain the recalled event's objects")
	}
	for _, source := range []struct{ id, published string }{{"published-inside", "2026-10-08"}, {"published-outside", "2026-09-29"}, {"published-invalid", "2026-02-31"}} {
		event := types.KnowledgeEvent{ID: source.id, TenantID: 1, KnowledgeBaseID: "p3-kb", Title: source.id, EntityIDs: types.StringArray{"object"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, db.Create(&event).Error)
		evidence := p3Evidence(t, db, source.id)
		require.NoError(t, db.Model(evidence).Update("published_at", source.published).Error)
		link := types.KnowledgeEventEvidence{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "p3-kb", EventID: event.ID, EvidenceID: evidence.ID, EntityIDs: types.StringArray{"object"}, CreatedAt: time.Now()}
		require.NoError(t, db.Create(&link).Error)
		seedContextEventEvidenceEntities(t, db, link)
	}
	candidates, err = svc.recallContextEvents(ctx, 1, input, types.StringArray{"object"})
	require.NoError(t, err)
	require.Len(t, candidates, 4)
	for _, candidate := range candidates {
		require.Contains(t, []string{"inside", "no-date", "published-inside", "published-invalid"}, candidate.ID)
	}
	published := p3Evidence(t, db, "The object changed.")
	published.PublishedAt = "2026-10-07"
	input.OccurredAt = ""
	candidates, err = svc.recallContextEvents(withContextResolutionEvidence(ctx, published), 1, input, types.StringArray{"object"})
	require.NoError(t, err)
	require.Len(t, candidates, 4)
	candidates, err = svc.recallContextEvents(ctx, 1, input, types.StringArray{"object"})
	require.NoError(t, err)
	require.Len(t, candidates, 6)
	model := &p3CountingChat{reply: func(context.Context, []chat.Message) string { return `{"relation":"UNRELATED","confidence":1}` }}
	_, err = svc.matchContextEvent(ctx, model, input, types.StringArray{"object"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, model.count("event"))
}

func TestP3ConcurrentMessagesCreateOneIdentityWithoutOrphans(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := "SimCo has 10 staff."
	messages := []*types.Knowledge{p3Message(t, db, "simultaneous-one", body), p3Message(t, db, "simultaneous-two", body)}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	model := &p3CountingChat{reply: func(callCtx context.Context, input []chat.Message) string {
		if strings.Contains(input[0].Content, "same real-world object") {
			require.Eventually(t, func() bool {
				var lockAvailable bool
				err := db.Transaction(func(tx *gorm.DB) error {
					return tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended(?,0))", "reliquary:knowledge-context:team:1").Scan(&lockAvailable).Error
				})
				return err == nil && lockAvailable
			}, 2*time.Second, 10*time.Millisecond, "model judgment must not hold the persistence barrier after the other message snapshots")
			entered <- struct{}{}
			select {
			case <-release:
			case <-callCtx.Done():
				return `{}`
			}
			return `{"decision":"new","confidence":1}`
		}
		return `{"entities":[{"mention":"SimCo","canonical_name":"SimCo","entity_type":"company","quote":"SimCo has 10 staff.","aliases":["SimCo"]}],"facts":[{"kind":"attribute","subject":"SimCo","predicate":"staff","value":10,"value_type":"number","quote":"SimCo has 10 staff."}],"events":[],"attention":{"reason":"Staff count."}}`
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	done := make(chan error, 2)
	for _, message := range messages {
		go func(message *types.Knowledge) { done <- svc.ProcessMessage(ctx, message, body) }(message)
	}
	for range messages {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("both messages must reach judgment before persistence")
		}
	}
	close(release)
	for range messages {
		require.NoError(t, <-done)
	}
	var entities, aliases, facts, orphans int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Count(&entities).Error)
	require.EqualValues(t, 1, entities)
	require.NoError(t, db.Model(&types.KnowledgeEntityAlias{}).Count(&aliases).Error)
	require.EqualValues(t, 1, aliases)
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Count(&facts).Error)
	require.EqualValues(t, 2, facts)
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM knowledge_context_facts f LEFT JOIN knowledge_context_entities e ON e.tenant_id=f.tenant_id AND e.id=f.subject_entity_id WHERE e.id IS NULL`).Scan(&orphans).Error)
	require.Zero(t, orphans)
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM knowledge_context_entity_aliases a LEFT JOIN knowledge_context_entities e ON e.tenant_id=a.tenant_id AND e.id=a.entity_id WHERE e.id IS NULL`).Scan(&orphans).Error)
	require.Zero(t, orphans)
}

func TestP3DeletingAliasSourceClearsEvidenceReference(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entity := p3Entity("retained-object", "Retained Object", "company")
	require.NoError(t, db.Create(&entity).Error)
	source := p3Evidence(t, db, "RetainedAlias has 10 staff.")
	surviving := p3Evidence(t, db, "Retained Object has 20 staff.")
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "source-alias", TenantID: 1, KnowledgeBaseID: "p3-kb", EntityType: "company", EntityID: entity.ID, Alias: "RetainedAlias", NormalizedAlias: "retainedalias", EvidenceID: source.ID, Quote: source.Body, ResolutionMethod: "model", CreatedAt: time.Now()}).Error)
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "retained-fact", TenantID: 1, KnowledgeBaseID: "p3-kb", SubjectEntityID: entity.ID, Kind: "attribute", Predicate: "staff", Value: types.JSON(`20`), ValueType: "number", EvidenceID: surviving.ID, Quote: surviving.Body, Status: "current", CreatedAt: time.Now()}).Error)
	require.NoError(t, svc.DeleteMessages(ctx, "p3-kb", []string{source.KnowledgeID}))
	var alias types.KnowledgeEntityAlias
	require.NoError(t, db.First(&alias, "id=?", "source-alias").Error)
	require.Empty(t, alias.EvidenceID)
	require.Empty(t, alias.Quote)
	require.Equal(t, "model", alias.ResolutionMethod)
	require.NoError(t, db.First(&entity, "id=?", entity.ID).Error)
}

func TestP3ConcurrentMessagesUpdateEventsInOppositeOrders(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := persistContextEntities(tx, []types.KnowledgeEntity{p3Entity("alpha", "Alpha", "company"), p3Entity("beta", "Beta", "company")})
		return err
	}))
	occurred := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	records := []types.KnowledgeEvent{{ID: "event-a", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Change one", OccurredAt: &occurred, EntityIDs: types.StringArray{"alpha", "beta"}, EvidenceIDs: types.StringArray{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, {ID: "event-b", TenantID: 1, KnowledgeBaseID: "p3-kb", Title: "Change two", OccurredAt: &occurred, EntityIDs: types.StringArray{"alpha", "beta"}, EvidenceIDs: types.StringArray{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	require.NoError(t, db.Create(&records).Error)
	seedContextEventEntities(t, db, records...)
	bodyAlpha := "Alpha changed one. Alpha changed two."
	bodyBeta := "Beta changed one. Beta changed two."
	sources := []*types.Knowledge{p3Message(t, db, "events-alpha", bodyAlpha), p3Message(t, db, "events-beta", bodyBeta)}
	planned := make(chan struct{}, 2)
	release := make(chan struct{})
	model := &p3CountingChat{reply: func(callCtx context.Context, messages []chat.Message) string {
		if strings.Contains(messages[0].Content, "same specific real-world change") {
			var input struct {
				NewEvent   contextExtractedEvent `json:"new_event"`
				Candidates []struct {
					ID string `json:"id"`
				} `json:"candidates"`
			}
			require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
			require.Len(t, input.Candidates, 2)
			id := "event-a"
			if strings.Contains(input.NewEvent.Title, "two") {
				id = "event-b"
			}
			if input.NewEvent.Title == "Alpha two" || input.NewEvent.Title == "Beta one" {
				planned <- struct{}{}
				select {
				case <-release:
				case <-callCtx.Done():
					return `{}`
				}
			}
			return fmt.Sprintf(`{"relation":"SAME_OCCURRENCE","event_id":%q,"confidence":0.99}`, id)
		}
		var metadata struct {
			Body string `json:"body"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &metadata))
		name := "Alpha"
		if strings.Contains(metadata.Body, "Beta") {
			name = "Beta"
		}
		extracted := contextExtraction{Entities: []contextExtractedEntity{{Mention: name, Canonical: name, EntityType: "company", Quote: name + " changed one."}}, Events: []contextExtractedEvent{{Title: name + " one", Quote: name + " changed one.", EntityMentions: []string{name}}, {Title: name + " two", Quote: name + " changed two.", EntityMentions: []string{name}}}, Attention: contextExtractedAttention{Reason: "Two source-supported changes."}}
		if name == "Beta" {
			extracted.Events[0], extracted.Events[1] = extracted.Events[1], extracted.Events[0]
		}
		encoded, err := json.Marshal(extracted)
		require.NoError(t, err)
		return string(encoded)
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	done := make(chan error, 2)
	go func() { done <- svc.ProcessMessage(ctx, sources[0], bodyAlpha) }()
	go func() { done <- svc.ProcessMessage(ctx, sources[1], bodyBeta) }()
	for range sources {
		select {
		case <-planned:
		case <-ctx.Done():
			t.Fatal("both messages must prepare reverse event orders")
		}
	}
	close(release)
	for range sources {
		require.NoError(t, <-done)
	}
	require.Equal(t, 4, model.count("event"))
	var events []types.KnowledgeEvent
	require.NoError(t, db.Find(&events).Error)
	require.NoError(t, loadContextEventEntityIDs(db, 1, events))
	require.Len(t, events, 2)
	for _, event := range events {
		require.Len(t, event.EvidenceIDs, 2)
		require.ElementsMatch(t, []string{"alpha", "beta"}, []string(event.EntityIDs))
	}
	var links int64
	require.NoError(t, db.Model(&types.KnowledgeEventEvidence{}).Count(&links).Error)
	require.EqualValues(t, 4, links)
}

func TestP3DifferentMentionsOfOneNewCanonicalObjectEachReceiveJudgment(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	model := &p3CountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	resolved, mentions, err := svc.resolveContextEntities(ctx, model, 1, "p3-kb", []contextExtractedEntity{{Mention: "Alpha Inc", Canonical: "Alpha", EntityType: "company", Quote: "Alpha Inc is A Corp."}, {Mention: "A Corp", Canonical: "Alpha", EntityType: "company", Quote: "Alpha Inc is A Corp."}})
	require.NoError(t, err)
	require.Equal(t, 2, model.count("entity"))
	require.Len(t, resolved, 1)
	require.Equal(t, mentions["alpha inc"].ID, mentions["a corp"].ID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, resolved); return err }))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&types.KnowledgeEntityAlias{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
}
