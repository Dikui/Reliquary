package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestTopicsCatalogSelectionAndArticleDisplay(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	svc := NewTopicService(db)
	first, err := svc.CreateTopic(ctx, &types.Topic{Name: "Artificial intelligence", Description: "Products and research"})
	require.NoError(t, err)
	second, err := svc.CreateTopic(ctx, &types.Topic{Name: "Engineering", Description: "Implementation guides"})
	require.NoError(t, err)
	_, err = svc.CreateTopic(ctx, &types.Topic{Name: first.Name})
	require.Error(t, err, "topic names are unique inside a space")
	otherCtx := context.WithValue(ctx, types.SpaceIDContextKey, uint64(2))
	other, err := svc.CreateTopic(otherCtx, &types.Topic{Name: first.Name})
	require.NoError(t, err)
	catalog, err := svc.ListTopics(ctx)
	require.NoError(t, err)
	require.Len(t, catalog, 2)

	article := &types.Knowledge{ID: uuid.NewString(), SpaceID: 1, KnowledgeBaseID: uuid.NewString(), Type: "file", Title: "Event", Source: "manual", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, db.Create(article).Error)
	require.NoError(t, svc.ReplaceKnowledgeTopics(ctx, article.ID, []string{first.ID, first.ID, "missing", other.ID}))
	var links []types.KnowledgeTopic
	require.NoError(t, db.Where("knowledge_id=?", article.ID).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, first.ID, links[0].TopicID)
	require.Error(t, svc.ReplaceKnowledgeTopics(otherCtx, article.ID, []string{other.ID}))

	entity := &types.Entity{ID: uuid.NewString(), SpaceID: 1, CanonicalName: "Reliquary", NormalizedName: "reliquary", EntityType: "project"}
	require.NoError(t, db.Create(entity).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntity{SpaceID: 1, KnowledgeID: article.ID, EntityID: entity.ID}).Error)
	kg := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db)}
	detail, err := kg.GetKnowledgeByID(ctx, article.ID)
	require.NoError(t, err)
	require.Len(t, detail.Entities, 1)
	require.Equal(t, entity.ID, detail.Entities[0].ID)
	require.Len(t, detail.Topics, 1)
	require.Equal(t, first.ID, detail.Topics[0].ID)
	page, err := kg.ListPagedKnowledgeByKnowledgeBaseID(ctx, article.KnowledgeBaseID, &types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{})
	require.NoError(t, err)
	listed := page.Data.([]*types.Knowledge)
	require.Len(t, listed[0].Entities, 1)
	require.Len(t, listed[0].Topics, 1)

	require.NoError(t, svc.ReplaceKnowledgeTopics(ctx, article.ID, []string{second.ID}))
	require.NoError(t, db.Where("knowledge_id=?", article.ID).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, second.ID, links[0].TopicID)
	require.NoError(t, svc.ReplaceKnowledgeTopics(ctx, article.ID, nil))
	require.NoError(t, db.Where("knowledge_id=?", article.ID).Find(&links).Error)
	require.Empty(t, links)
	require.NoError(t, svc.ReplaceKnowledgeTopics(ctx, article.ID, []string{first.ID}))
	require.NoError(t, kg.repo.DeleteKnowledge(ctx, 1, article.ID))
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeTopic{}).Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count, "soft deletion must clear topic associations")
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count, "soft deletion must clear entity associations")
}

func TestArticleSummaryKeywordSearchWithoutChunksOrVectors(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	kb := &types.KnowledgeBase{ID: uuid.NewString(), SpaceID: 1, Name: "Knowledge", IndexingStrategy: types.DefaultIndexingStrategy()}
	require.NoError(t, db.Create(kb).Error)
	summary := &types.KnowledgeSummary{Overview: "Build a nebulaengine application.", Points: []string{"Install the tool", "Configure a project", "Run the project"}, KeyTerms: []string{"nebulaengine", "Reliquary"}}
	article := &types.Knowledge{ID: uuid.NewString(), SpaceID: 1, KnowledgeBaseID: kb.ID, Type: "url", Title: "A tutorial", Source: "https://example.com/tutorial", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", ContentType: types.ContentTypeTutorial, ProcessingMode: types.ProcessingModeSummaryOnly, StructuredSummary: summary, Description: summary.RenderText()}
	require.NoError(t, db.Create(article).Error)
	var loaded types.Knowledge
	require.NoError(t, db.First(&loaded, "id=?", article.ID).Error)
	require.Equal(t, summary, loaded.StructuredSummary)
	var count int64
	require.NoError(t, db.Table("chunks").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Table("embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	engine := pgretriever.NewPostgresRetrieveEngineRepository(db)
	hits, err := engine.Retrieve(ctx, types.RetrieveParams{Query: "nebulaengine", KnowledgeBaseIDs: []string{kb.ID}, TopK: 10, RetrieverType: types.KeywordsRetrieverType})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Len(t, hits[0].Results, 1)
	require.Equal(t, article.ID, hits[0].Results[0].KnowledgeID)
	require.Equal(t, types.KnowledgeSummarySearchID(article.ID), hits[0].Results[0].ChunkID)
	search := &knowledgeBaseService{kgRepo: repository.NewKnowledgeRepository(db), chunkRepo: repository.NewChunkRepository(db)}
	results, err := search.processSearchResults(ctx, hits[0].Results, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, article.Description, results[0].Content)
	require.Equal(t, article.Source, results[0].KnowledgeSource)

	// Overwriting processing results updates the keyword index automatically.
	require.NoError(t, db.Model(article).Update("description", "replacementquasar").Error)
	hits, err = engine.Retrieve(ctx, types.RetrieveParams{Query: "replacementquasar", KnowledgeBaseIDs: []string{kb.ID}, TopK: 10, RetrieverType: types.KeywordsRetrieverType})
	require.NoError(t, err)
	require.Len(t, hits[0].Results, 1)
	require.NoError(t, db.Model(article).Update("parse_status", types.ParseStatusFailed).Error)
	hits, err = engine.Retrieve(ctx, types.RetrieveParams{Query: "replacementquasar", KnowledgeBaseIDs: []string{kb.ID}, TopK: 10, RetrieverType: types.KeywordsRetrieverType})
	require.NoError(t, err)
	require.Empty(t, hits[0].Results)
}
