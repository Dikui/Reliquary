package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"sync"
	"testing"
	"time"
)

func entityFixture(t *testing.T) (*gorm.DB, context.Context, *EntityService) {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Entities"}).Error)
	kb := types.KnowledgeBase{ID: "entity-kb", SpaceID: 1, Name: "Entity articles", SummaryModelID: "entity-chat", ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50}}
	require.NoError(t, db.Create(&kb).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "entity-reviewer")
	return db, ctx, NewEntityService(db, nil)
}
func entityArticle(t *testing.T, db *gorm.DB, id string) *types.Entry {
	t.Helper()
	article := &types.Entry{ID: id, SpaceID: 1, KnowledgeBaseID: "entity-kb", Title: id, Type: types.EntryTypeManual, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, db.Create(article).Error)
	return article
}
func entityRecord(id, name, kind string) types.Entity {
	return types.Entity{ID: id, SpaceID: 1, CanonicalName: name, NormalizedName: normalizeEntityName(name), EntityType: types.NormalizeEntityType(kind), CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

type entityChatInterface interface{ chat.Chat }
type entityCountingChat struct {
	entityChatInterface
	mu    sync.Mutex
	calls int
	reply func(context.Context, []chat.Message) string
}

func (m *entityCountingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return &types.ChatResponse{Content: m.reply(ctx, messages)}, nil
}
func (m *entityCountingChat) GetModelID() string   { return "entity-chat" }
func (m *entityCountingChat) GetModelName() string { return "Entity test model" }
func (m *entityCountingChat) count() int           { m.mu.Lock(); defer m.mu.Unlock(); return m.calls }

type entityTestModels struct {
	interfaces.ModelService
	model chat.Chat
}

func (m entityTestModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.model, nil
}
func (m entityTestModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return nil, errors.New("embedding test model unconfigured")
}

type entityEmbeddingInterface interface{ embedding.Embedder }
type entityEmbedder struct {
	entityEmbeddingInterface
	dimension int
	calls     int
}

func (m *entityEmbedder) Embed(context.Context, string) ([]float32, error) {
	m.calls++
	v := make([]float32, m.dimension)
	v[0] = 1
	return v, nil
}
func (m *entityEmbedder) GetDimensions() int { return m.dimension }
func (m *entityEmbedder) GetModelID() string { return "entity-embedding" }

type entityEmbeddingModels struct {
	interfaces.ModelService
	model embedding.Embedder
}

func (m entityEmbeddingModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.model, nil
}
func TestEntityExactNamesRecallAliasesAndVersions(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	entities := []types.Entity{entityRecord("moonshot", "Moonshot AI", "company"), entityRecord("kimi-k2", "Kimi K2", "product"), entityRecord("different-type", "Same Name", "product")}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, entities); return err }))
	require.NoError(t, db.Create(&types.EntityAlias{ID: "moonshot-cn", SpaceID: 1, EntityType: "company", EntityID: "moonshot", Alias: "月之暗面", NormalizedAlias: "月之暗面", CreatedAt: time.Now()}).Error)
	model := &entityCountingChat{reply: func(_ context.Context, messages []chat.Message) string {
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
	input := []entityResolutionInput{{Mention: "MOONSHOT AI", Canonical: "Moonshot AI", EntityType: "company"}, {Mention: "月之暗面", Canonical: "Moonshot AI", EntityType: "company"}, {Mention: "MoonshotAI", Canonical: "MoonshotAI", EntityType: "company"}, {Mention: "Kimi K2.5", Canonical: "Kimi K2", EntityType: "product"}, {Mention: "Same Name", Canonical: "Same Name", EntityType: "company"}}
	resolved, mentions, err := svc.resolveEntities(ctx, model, 1, "entity-kb", input)
	require.NoError(t, err)
	require.Equal(t, 3, model.count())
	require.Equal(t, "moonshot", mentions[normalizeEntityName("MoonshotAI")].ID)
	require.NotEqual(t, "kimi-k2", mentions[normalizeEntityName("Kimi K2.5")].ID)
	require.Equal(t, "Kimi K2.5", mentions[normalizeEntityName("Kimi K2.5")].CanonicalName)
	require.NotEqual(t, "different-type", mentions[normalizeEntityName("Same Name")].ID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, resolved); return err }))
	var alias types.EntityAlias
	require.NoError(t, db.First(&alias, "normalized_alias=?", "moonshotai").Error)
	require.Equal(t, "model", alias.ResolutionMethod)
	before := model.count()
	_, _, err = svc.resolveEntities(ctx, model, 1, "entity-kb", []entityResolutionInput{input[2]})
	require.NoError(t, err)
	require.Equal(t, before, model.count())
}

func TestEntityVectorRecallSkipsStaleModelsAndMismatchedDimensions(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='entity-kb'").Update("model_tiers", types.ModelTierConfig{Embedding: "entity-embedding"}).Error)
	records := []types.Entity{entityRecord("vector-good", "Unique unrelated canonical", "product"), entityRecord("vector-wrong-dimension", "Foreign vector dimension", "product"), entityRecord("vector-stale-model", "Obsolete model vector", "product")}
	records[0].NameEmbedding = types.EntityNameEmbedding{1, 0}
	records[0].EmbeddingModelID = "entity-embedding"
	records[0].EmbeddingDimension = 2
	records[1].NameEmbedding = types.EntityNameEmbedding{1, 0, 0}
	records[1].EmbeddingModelID = "entity-embedding"
	records[1].EmbeddingDimension = 3
	records[2].NameEmbedding = types.EntityNameEmbedding{1, 0}
	records[2].EmbeddingModelID = "previous-embedding"
	records[2].EmbeddingDimension = 2
	require.NoError(t, db.Create(&records).Error)
	embedder := &entityEmbedder{dimension: 2}
	svc.models = entityEmbeddingModels{model: embedder}
	judgment := &entityCountingChat{reply: func(_ context.Context, messages []chat.Message) string {
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
	_, mentions, err := svc.resolveEntities(ctx, judgment, 1, "entity-kb", []entityResolutionInput{{Mention: "中文不同名称", Canonical: "中文不同名称", EntityType: "product"}})
	require.NoError(t, err)
	require.Equal(t, "vector-good", mentions["中文不同名称"].ID)
	require.Equal(t, 1, embedder.calls)
	for _, id := range []string{"vector-wrong-dimension", "vector-stale-model"} {
		var record types.Entity
		require.NoError(t, db.First(&record, "id=?", id).Error)
		require.False(t, record.EmbeddingNeedsRebuild, "mention resolution must not mark the registry")
	}
}

func TestEntityDifferentMentionsOfOneNewCanonicalEntityEachReceiveJudgment(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	resolved, mentions, err := svc.resolveEntities(ctx, model, 1, "entity-kb", []entityResolutionInput{{Mention: "Alpha Inc", Canonical: "Alpha", EntityType: "company"}, {Mention: "A Corp", Canonical: "Alpha", EntityType: "company"}})
	require.NoError(t, err)
	require.Equal(t, 2, model.count())
	require.Len(t, resolved, 1)
	require.Equal(t, mentions["alpha inc"].ID, mentions["a corp"].ID)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, resolved); return err }))
	var count int64
	require.NoError(t, db.Model(&types.Entity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&types.EntityAlias{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
}

func TestEntityResolutionCreatesAliasesAndIdempotentArticleMentions(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	article := entityArticle(t, db, "article-one")
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	svc.models = entityTestModels{model: model}
	first, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Acme", EntityType: " COMPANY ", Aliases: []string{"艾克米"}}})
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "company", first[0].EntityType)
	require.ElementsMatch(t, []string{"Acme", "艾克米"}, first[0].Aliases)
	second, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "ACME", EntityType: "company", Aliases: []string{"Acme Inc"}}, {Name: "艾克米", EntityType: "company", Aliases: []string{"Acme Corporation"}}})
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, first[0].ID, second[0].ID)
	require.ElementsMatch(t, []string{"Acme", "艾克米", "Acme Inc", "Acme Corporation"}, second[0].Aliases)
	var count int64
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entry_id=?", article.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Equal(t, 1, model.count(), "exact names and aliases reuse the identity without further judgment")
	detail, err := svc.EntityDetail(ctx, first[0].ID)
	require.NoError(t, err)
	require.Len(t, detail.Entries, 1)
	require.Equal(t, article.ID, detail.Entries[0].ID)
	entities, err := svc.ListEntities(ctx, types.EntityQuery{Entity: "艾克米", EntityType: "company"})
	require.NoError(t, err)
	require.Len(t, entities, 1)
}

func TestEntityResolutionUnknownTypesBecomeOther(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	article := entityArticle(t, db, "article-unknown")
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}}
	entities, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Unknown thing", EntityType: "legacy-model-type"}})
	require.NoError(t, err)
	require.Len(t, entities, 1)
	require.Equal(t, "other", entities[0].EntityType)
	var link types.EntryEntity
	require.NoError(t, db.Where("entry_id=?", article.ID).First(&link).Error)
	require.Equal(t, entities[0].ID, link.EntityID)
}

func TestEntityResolutionUncertainIdentityCreatesDecidableSuggestions(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	article := entityArticle(t, db, "suggestion-article")
	entity := entityRecord("acme", "Acme Systems", "company")
	require.NoError(t, db.Create(&entity).Error)
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string {
		return `{"decision":"uncertain","candidate_ids":["acme"],"reason":"The legal suffix is unclear.","confidence":0.5}`
	}}}
	entities, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Acme Systems Ltd", EntityType: "company"}})
	require.NoError(t, err)
	require.Len(t, entities, 1)
	require.NotEqual(t, "acme", entities[0].ID)
	suggestions, err := svc.ListMergeSuggestions(ctx, types.EntityQuery{}, "pending")
	require.NoError(t, err)
	require.Len(t, suggestions, 1)
	require.Equal(t, article.ID, suggestions[0].EntryID)
	require.Equal(t, "Acme Systems Ltd", suggestions[0].NewEntityName)
	require.Equal(t, "Acme Systems", suggestions[0].CandidateEntityName)
	accepted, err := svc.DecideMergeSuggestion(ctx, suggestions[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "accepted", accepted.Status)
	require.Equal(t, "acme", readNameEntity(t, db, entities[0].ID).MergedIntoID)
	other := entityRecord("other-acme", "Acme Regional", "company")
	require.NoError(t, db.Create(&other).Error)
	rejected := types.EntityMergeSuggestion{ID: uuid.NewString(), SpaceID: 1, NewEntityID: other.ID, CandidateEntityID: "acme", EntryID: article.ID, Reason: "Unclear", Status: "pending", CreatedAt: time.Now()}
	require.NoError(t, db.Create(&rejected).Error)
	decided, err := svc.DecideMergeSuggestion(ctx, rejected.ID, false)
	require.NoError(t, err)
	require.Equal(t, "rejected", decided.Status)
	require.Empty(t, readNameEntity(t, db, other.ID).MergedIntoID)
	_, err = svc.DecideMergeSuggestion(ctx, rejected.ID, true)
	require.Error(t, err)
}

func TestEntityMergeTransfersAliasesArticlesAndKeepsAudit(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	one := entityArticle(t, db, "shared-article")
	two := entityArticle(t, db, "merged-article")
	keep := entityRecord("keep", "Acme", "company")
	merged := entityRecord("merge", "Acme Inc", "company")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, []types.Entity{keep, merged}); return err }))
	require.NoError(t, saveEntityAlias(db, keep, "Shared name", "exact"))
	// Distinct old types allow an alias collision that the merge has to deduplicate.
	require.NoError(t, db.Model(&merged).Update("entity_type", "organization").Error)
	merged.EntityType = "organization"
	require.NoError(t, db.Model(&types.EntityAlias{}).Where("entity_id=?", merged.ID).Update("entity_type", "organization").Error)
	require.NoError(t, saveEntityAlias(db, merged, "Shared name", "exact"))
	require.NoError(t, saveEntityAlias(db, merged, "艾克米", "exact"))
	links := []types.EntryEntity{{SpaceID: 1, EntryID: one.ID, EntityID: keep.ID, CreatedAt: time.Now()}, {SpaceID: 1, EntryID: one.ID, EntityID: merged.ID, CreatedAt: time.Now()}, {SpaceID: 1, EntryID: two.ID, EntityID: merged.ID, CreatedAt: time.Now()}}
	require.NoError(t, db.Create(&links).Error)
	require.NoError(t, svc.MergeEntities(ctx, keep.ID, merged.ID))
	detail, err := svc.EntityDetail(ctx, keep.ID)
	require.NoError(t, err)
	require.Len(t, detail.Entries, 2)
	require.ElementsMatch(t, []string{"Acme", "Acme Inc", "Shared name", "艾克米"}, detail.Entity.Aliases)
	var count int64
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entity_id=?", merged.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entity_id=?", keep.ID).Count(&count).Error)
	require.EqualValues(t, 2, count)
	records, err := svc.ListMergeRecords(ctx, keep.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, keep.ID, records[0].KeepEntityID)
	require.Equal(t, merged.ID, records[0].MergeEntityID)
	require.NotEmpty(t, records[0].Actor)
	require.NoError(t, svc.MergeEntities(ctx, keep.ID, merged.ID))
	records, err = svc.ListMergeRecords(ctx, merged.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestEntityConcurrentPersistenceRechecksNamesAndMergedIdentity(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	a, _, err := svc.resolveEntities(ctx, model, 1, "", []entityResolutionInput{{Mention: "Concurrent", Canonical: "Concurrent", EntityType: "company"}})
	require.NoError(t, err)
	b, _, err := svc.resolveEntities(ctx, model, 1, "", []entityResolutionInput{{Mention: "Concurrent", Canonical: "Concurrent", EntityType: "company"}})
	require.NoError(t, err)
	done := make(chan error, 2)
	for _, plan := range [][]types.Entity{a, b} {
		go func(plan []types.Entity) {
			done <- db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, plan); return err })
		}(plan)
	}
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	var count int64
	require.NoError(t, db.Model(&types.Entity{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&types.EntityAlias{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	keep := entityRecord("final-keep", "Final", "company")
	require.NoError(t, db.Create(&keep).Error)
	var existing types.Entity
	require.NoError(t, db.Where("normalized_name=?", "concurrent").First(&existing).Error)
	require.NoError(t, svc.MergeEntities(ctx, keep.ID, existing.ID))
	var ids map[string]string
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { var err error; ids, err = persistEntities(tx, a); return err }))
	require.Equal(t, keep.ID, ids[a[0].ID])
}

func TestEntityJudgmentBudgetDoesNotGrowWithRegistry(t *testing.T) {
	counts := map[int]int{}
	for _, size := range []int{50, 5000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db, ctx, svc := entityFixture(t)
			seeded := make([]types.Entity, size)
			for i := range seeded {
				seeded[i] = entityRecord(fmt.Sprintf("registered-%d", i), fmt.Sprintf("Registered Entity %04d", i), "product")
			}
			require.NoError(t, db.CreateInBatches(&seeded, 500).Error)
			require.NoError(t, saveEntityAlias(db, seeded[2], "已有别名", "exact"))
			names := []string{"Registered Entity 0000", "REGISTERED ENTITY 0001", "已有别名", "New Mention Alpha", "New Mention Beta", "New Mention Gamma", "New Mention Delta", "New Mention Epsilon", "New Mention Zeta", "New Mention Eta"}
			candidates := []types.EntityCandidate{}
			for _, name := range names {
				candidates = append(candidates, types.EntityCandidate{Name: name, EntityType: "product"})
			}
			model := &entityCountingChat{reply: func(_ context.Context, messages []chat.Message) string {
				var payload struct {
					Candidates []types.Entity `json:"candidates"`
				}
				require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
				require.LessOrEqual(t, len(payload.Candidates), 20)
				return `{"decision":"new","confidence":1}`
			}}
			svc.models = entityTestModels{model: model}
			article := entityArticle(t, db, "scale-article")
			_, err := svc.ResolveEntities(ctx, article.ID, candidates)
			require.NoError(t, err)
			require.Equal(t, 7, model.count())
			counts[size] = model.count()
		})
	}
	require.Equal(t, counts[50], counts[5000])
}

func TestEntityAliasesAndCanonicalNamesAreUniqueBySpaceAndType(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	for _, kind := range []string{"company", "company", "product"} {
		entities, _, err := svc.resolveEntities(ctx, model, 1, "", []entityResolutionInput{{Mention: "Acme", Canonical: "Acme", EntityType: kind}})
		require.NoError(t, err)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, entities); return err }))
	}
	var entities []types.Entity
	var aliases []types.EntityAlias
	require.NoError(t, db.Find(&entities).Error)
	require.Len(t, entities, 2)
	require.NoError(t, db.Find(&aliases).Error)
	require.Len(t, aliases, 2)
	duplicate := entityRecord("duplicate", "Acme", "company")
	require.Error(t, db.Create(&duplicate).Error)
	duplicateAlias := types.EntityAlias{ID: "duplicate-alias", SpaceID: 1, EntityType: "company", EntityID: entities[0].ID, Alias: "Acme", NormalizedAlias: "acme", CreatedAt: time.Now()}
	require.Error(t, db.Create(&duplicateAlias).Error)
}

func TestEntityMergeAliasCollisionRollsBackGlobalChanges(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	entities := []types.Entity{entityRecord("keep", "Keep", "company"), entityRecord("merge", "Merged", "product"), entityRecord("third", "Third", "company")}
	require.NoError(t, db.Create(&entities).Error)
	aliases := []types.EntityAlias{{ID: "source-alias", SpaceID: 1, EntityType: "product", EntityID: "merge", Alias: "Shared", NormalizedAlias: "shared", CreatedAt: time.Now()}, {ID: "third-alias", SpaceID: 1, EntityType: "company", EntityID: "third", Alias: "Shared", NormalizedAlias: "shared", CreatedAt: time.Now()}}
	require.NoError(t, db.Create(&aliases).Error)
	err := svc.MergeEntities(ctx, "keep", "merge")
	require.ErrorContains(t, err, "conflicts")
	require.Empty(t, readNameEntity(t, db, "merge").MergedIntoID)
	var alias types.EntityAlias
	require.NoError(t, db.First(&alias, "id=?", "source-alias").Error)
	require.Equal(t, "merge", alias.EntityID)
	require.Equal(t, "product", alias.EntityType)
	records, err := svc.ListMergeRecords(ctx, "keep")
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestEntityResolutionCannotRecreateMentionsAfterConcurrentArticleDeletion(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	article := entityArticle(t, db, "deleted-during-judgment")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string {
		once.Do(func() { close(entered); <-release })
		return `{"decision":"new","confidence":1}`
	}}}
	result := make(chan error, 1)
	go func() {
		_, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Late entity", EntityType: "company"}})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("entity judgment did not start")
	}
	repo := repository.NewEntryRepository(db)
	require.NoError(t, repo.DeleteEntry(ctx, 1, article.ID))
	close(release)
	require.Error(t, <-result)
	var count int64
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entry_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.Entity{}).Where("normalized_name=?", "late entity").Count(&count).Error)
	require.Zero(t, count, "a removed article cannot commit a new identity")
}

func TestEntityEntryDeletionWaitsForResolutionAndRemovesItsCommittedMentions(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	article := entityArticle(t, db, "deleted-during-persistence")
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("block_entity_mention", func(tx *gorm.DB) {
		if tx.Statement.Table == "entry_entities" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	defer db.Callback().Create().Remove("block_entity_mention")
	resolved := make(chan error, 1)
	go func() {
		_, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Persisted identity", EntityType: "company"}})
		resolved <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("entity mention persistence did not start")
	}
	deleted := make(chan error, 1)
	go func() { deleted <- repository.NewEntryRepository(db).DeleteEntry(ctx, 1, article.ID) }()
	select {
	case err := <-deleted:
		t.Fatalf("deletion must wait for active resolution: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-resolved)
	require.NoError(t, <-deleted)
	var count int64
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entry_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&types.Entity{}).Where("normalized_name=?", "persisted identity").Count(&count).Error)
	require.EqualValues(t, 1, count, "deleting the article preserves the committed entity")
}
