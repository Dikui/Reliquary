package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// The fixture is deterministic across calls and produces different vectors
// for different title/summary inputs, unlike a constant embedding substitute.
type indexingRecordingEmbedder struct {
	chunkSearchEmbedder
	mu     sync.Mutex
	inputs []string
	err    error
}

func (e *indexingRecordingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.inputs = append(e.inputs, text)
	if e.err != nil {
		return nil, e.err
	}
	hash := sha256.Sum256([]byte(text))
	return []float32{(float32(hash[0]) + 1) / 256, (float32(hash[1]) + 1) / 256, (float32(hash[2]) + 1) / 256}, nil
}

func (e *indexingRecordingEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		value, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = value
	}
	return result, nil
}

func (e *indexingRecordingEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}

func (e *indexingRecordingEmbedder) recorded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.inputs...)
}

type indexingRecordingModels struct {
	articleProcessingModels
	embedder *indexingRecordingEmbedder
}

func (m indexingRecordingModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

func newArticleIndexingHarness(t *testing.T) (*articleProcessingHarness, *indexingRecordingEmbedder) {
	t.Helper()
	h := newArticleProcessingHarness(t, false)
	e := &indexingRecordingEmbedder{}
	h.service.modelService = indexingRecordingModels{h.service.modelService.(articleProcessingModels), e}
	h.service.chunkService.(*chunkService).modelService = h.service.modelService
	return h, e
}

func TestArticleChunkPrefixDoesNotEnterStoredBodyOrFullText(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	article := h.upload(t, "Vector context", "# Separate body\n\nchunkbodyfixture original observations.")
	var chunks []types.Chunk
	require.NoError(t, h.db.Where("knowledge_id=?", article.ID).Order("chunk_index").Find(&chunks).Error)
	require.NotEmpty(t, chunks)
	inputs := e.recorded()
	for _, chunk := range chunks {
		if chunk.ChunkType == types.ChunkTypeParentText {
			continue
		}
		prefix := article.Title + " · Acme · 2026-10-01\n"
		require.Contains(t, inputs, prefix+chunk.Content)
		require.NotContains(t, chunk.Content, prefix)
		require.NotContains(t, chunk.ContextHeader, " · ")
	}
	var searchText string
	require.NoError(t, h.db.Table("knowledges").Where("id=?", article.ID).Pluck("search_text", &searchText).Error)
	require.NotContains(t, searchText, " · ")
	require.NotContains(t, searchText, "chunkbodyfixture", "header index does not repeat the body")
	var hits int64
	require.NoError(t, h.db.Table("chunks").Where("knowledge_id=? AND seq_id @@@ paradedb.match_disjunction('content', ?)", article.ID, "chunkbodyfixture").Count(&hits).Error)
	require.Positive(t, hits)
	for _, date := range []string{"", "2026-10-01"} {
		copy := *article
		intake, err := copy.IntakeMetadata()
		require.NoError(t, err)
		intake.SourceInfo.PublishedAt = date
		require.NoError(t, copy.SetIntakeMetadata(intake))
		want := article.Title + " · Acme"
		if date != "" {
			want += " · " + date
		}
		require.Equal(t, want+"\nbody", buildKnowledgeIndexContent(&copy, "body"))
	}
}

func TestArticleChunkPrefixOmitsMissingPublicationDate(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	article, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID,
		newMultipartFileHeader(t, "Undated article.md", "# Independent observation\n\nundatedprefixfixture exact source observations."),
		map[string]string{"publisher": "Undated publisher", "original_url": "https://example.org/undated"},
		nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	// Indexing accepts approved source records with no known publication date.
	// Keep the preexisting intake review policy outside this index test.
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision = "approved"
	require.NoError(t, article.SetIntakeMetadata(intake))
	require.NoError(t, h.db.Save(article).Error)
	h.drain(t)
	article = h.article(t, article.ID)
	var chunks []types.Chunk
	require.NoError(t, h.db.Where("knowledge_id=?", article.ID).Find(&chunks).Error)
	require.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		if chunk.ChunkType != types.ChunkTypeParentText {
			require.Contains(t, e.recorded(), article.Title+" · Undated publisher\n"+chunk.Content)
		}
	}
	var date struct{ SearchPublishedAt *time.Time }
	require.NoError(t, h.db.Table("knowledges").Where("id=?", article.ID).First(&date).Error)
	require.Nil(t, date.SearchPublishedAt)
}

func TestArticleEmbeddingGeneratesForSummaryOnlyAndRecomputesAfterReprocessing(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	article := h.upload(t, "Summary vector", "bodyonlyfixture this tutorial body is discarded.")
	var chunkCount, bodyVectorCount int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("knowledge_id=?", article.ID).Count(&chunkCount).Error)
	require.Zero(t, chunkCount)
	require.NoError(t, h.db.Table("embeddings").Where("knowledge_id=?", article.ID).Count(&bodyVectorCount).Error)
	require.Zero(t, bodyVectorCount)
	var before struct {
		ContentHash string
		Embedding   pgvector.HalfVector
	}
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).First(&before).Error)
	wantInput := article.Title + "\n" + article.StructuredSummary.RenderText()
	require.Contains(t, e.recorded(), wantInput)
	require.Equal(t, vaultContentHash(wantInput), before.ContentHash)
	count := len(e.recorded())
	require.NoError(t, indexArticleEmbedding(h.ctx, h.db, h.service.modelService, h.kb, article))
	require.Len(t, e.recorded(), count, "unchanged input retains the existing vector")
	var output map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(h.processor.output), &output))
	output["summary"].(map[string]interface{})["overview"] = "reprocessedfixture updated tutorial observations."
	raw, err := json.Marshal(output)
	require.NoError(t, err)
	h.processor.output = string(raw)
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>Summary vector</title></head><body><article><p>bodyonlyfixture this tutorial body is discarded again.</p></article></body></html>"))
	}))
	defer server.Close()
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.SourceInfo.OriginalURL = server.URL + "/tutorial"
	require.NoError(t, article.SetIntakeMetadata(intake))
	article.Source = intake.SourceInfo.OriginalURL
	require.NoError(t, h.db.Save(article).Error)
	_, err = h.service.ReparseKnowledge(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	article = h.article(t, article.ID)
	var after struct {
		ContentHash string
		Embedding   pgvector.HalfVector
	}
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).First(&after).Error)
	require.NotEqual(t, before.ContentHash, after.ContentHash)
	require.NotEqual(t, before.Embedding.Slice(), after.Embedding.Slice())
	require.Contains(t, e.recorded(), article.Title+"\n"+article.StructuredSummary.RenderText())
	var hits int64
	require.NoError(t, h.db.Table("knowledges").Where("id=? AND search_seq_id @@@ paradedb.match_disjunction('search_text', ?)", article.ID, "bodyonlyfixture").Count(&hits).Error)
	require.Zero(t, hits, "summary-only body is not full-text indexed")
	require.NoError(t, h.db.Table("knowledges").Where("id=? AND search_seq_id @@@ paradedb.match_disjunction('search_text', ?)", article.ID, "reprocessedfixture").Count(&hits).Error)
	require.EqualValues(t, 1, hits)
}

func TestArticleTitleVectorChangeRollsBackWhenEmbeddingFails(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	article := h.upload(t, "Original title", "Measured deployment details and original observations.")
	var originalHash string
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).Pluck("content_hash", &originalHash).Error)
	e.err = errors.New("fixture embedding unavailable")
	err := h.service.UpdateKnowledge(h.ctx, &types.Knowledge{ID: article.ID, Title: "Changed title"})
	require.ErrorContains(t, err, "fixture embedding unavailable")
	reloaded := h.article(t, article.ID)
	require.Equal(t, article.Title, reloaded.Title)
	var currentHash string
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).Pluck("content_hash", &currentHash).Error)
	require.Equal(t, originalHash, currentHash)
	e.err = nil
	require.NoError(t, h.service.UpdateKnowledge(h.ctx, &types.Knowledge{ID: article.ID, Title: "Changed title"}))
	reloaded = h.article(t, article.ID)
	require.Equal(t, "Changed title", reloaded.Title)
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).Pluck("content_hash", &currentHash).Error)
	require.NotEqual(t, originalHash, currentHash)
}

func TestUnprocessedArticleTitleUpdateDoesNotRequireEmbedding(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	article := &types.Knowledge{ID: "pending-article", SpaceID: h.kb.SpaceID, KnowledgeBaseID: h.kb.ID,
		Type: "url", SourceKind: "article", Title: "Pending title", Source: "https://example.org/pending",
		ParseStatus: types.ParseStatusPending, EnableStatus: "enabled", ProcessingMode: types.ProcessingModeFull}
	require.NoError(t, h.db.Create(article).Error)
	h.service.modelService = nil
	require.NoError(t, h.service.UpdateKnowledge(h.ctx, &types.Knowledge{ID: article.ID, Title: "Edited pending title"}))
	reloaded := h.article(t, article.ID)
	require.Equal(t, "Edited pending title", reloaded.Title)
	var count int64
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestVaultDocumentChunkPrefixesHaveNoArticleVectors(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Millisecond)
	e := &indexingRecordingEmbedder{}
	h.documents.models = indexingRecordingModels{embedder: e}
	h.write(t, "mine/prefix.md", "# Personal notebook\n\npersonalprefixfixture preserved body")
	require.NoError(t, h.documents.Scan(h.ctx, true))
	mine := h.byPath(t, "mine/prefix.md")
	report, err := h.documents.CreateReport(h.ctx, "Synthesis", "# Synthesis\n\nreportprefixfixture preserved analysis", nil)
	require.NoError(t, err)
	for _, pair := range []struct {
		knowledge *types.Knowledge
		label     string
	}{{mine, "本人"}, {report, "整理结果"}} {
		var chunks []types.Chunk
		require.NoError(t, h.db.Where("knowledge_id=?", pair.knowledge.ID).Find(&chunks).Error)
		require.NotEmpty(t, chunks)
		for _, chunk := range chunks {
			require.Contains(t, e.recorded(), pair.knowledge.Title+" · "+pair.label+"\n"+chunk.Content)
			require.NotContains(t, chunk.Content, pair.label)
		}
		var count int64
		require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", pair.knowledge.ID).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestArticleHeaderFullTextIncludesEntitiesAliasesTopicsAndAllSummaryFields(t *testing.T) {
	h, _ := newArticleIndexingHarness(t)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Topicmarker"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "reference", []types.EntityCandidate{{Name: "Entitymarker", EntityType: "company", Aliases: []string{"Aliasmarker"}}}, []string{topic.ID})
	article := h.upload(t, "Headermarker", "discardedfixture reference source body")
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.SourceInfo.Publisher, intake.SourceInfo.Author = "Publishermarker", "Authormarker"
	require.NoError(t, article.SetIntakeMetadata(intake))
	article.StructuredSummary = &types.KnowledgeSummary{Overview: "Overviewmarker", Points: []string{"Pointmarker"}, KeyTerms: []string{"Termmarker"}}
	require.NoError(t, h.db.Save(article).Error)
	var vectors int64
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", article.ID).Count(&vectors).Error)
	require.Zero(t, vectors, "summary updates invalidate the old article vector until it is rebuilt")
	assertHit := func(term string, want int64) {
		t.Helper()
		var count int64
		require.NoError(t, h.db.Table("knowledges").Where("id=? AND search_seq_id @@@ paradedb.match_disjunction('search_text', ?)", article.ID, term).Count(&count).Error)
		require.Equal(t, want, count, term)
	}
	for _, term := range []string{"Headermarker", "Publishermarker", "Authormarker", "Overviewmarker", "Pointmarker", "Termmarker", "Entitymarker", "Aliasmarker", "Topicmarker"} {
		assertHit(term, 1)
	}
	assertHit("discardedfixture", 0)
	var entityID string
	require.NoError(t, h.db.Table("knowledge_entities").Where("knowledge_id=?", article.ID).Pluck("entity_id", &entityID).Error)
	require.NoError(t, h.db.Table("entities").Where("id=?", entityID).Update("canonical_name", "Renamedentitymarker").Error)
	assertHit("Renamedentitymarker", 1)
	assertHit("Entitymarker", 1) // The original mention remains an alias after a canonical rename.
	require.NoError(t, h.db.Table("entity_aliases").Where("entity_id=?", entityID).Update("alias", "Updatedaliasmarker").Error)
	assertHit("Updatedaliasmarker", 1)
	assertHit("Aliasmarker", 0)
	require.NoError(t, h.db.Table("entity_aliases").Where("entity_id=?", entityID).Delete(nil).Error)
	assertHit("Updatedaliasmarker", 0)
	assertHit("Entitymarker", 0)
	require.NoError(t, h.db.Table("topics").Where("id=?", topic.ID).Update("name", "Updatedtopicmarker").Error)
	assertHit("Updatedtopicmarker", 1)
	assertHit("Topicmarker", 0)
	require.NoError(t, h.db.Where("knowledge_id=?", article.ID).Delete(&types.KnowledgeTopic{}).Error)
	assertHit("Updatedtopicmarker", 0)
	require.NoError(t, h.db.Where("knowledge_id=?", article.ID).Delete(&types.KnowledgeEntity{}).Error)
	assertHit("Renamedentitymarker", 0)
	var text string
	require.NoError(t, h.db.Table("knowledges").Where("id=?", article.ID).Pluck("search_text", &text).Error)
	require.False(t, strings.Contains(text, " · "))
}

func TestArticleSearchPublishedAtUsesExplicitValidSourceDate(t *testing.T) {
	h, _ := newArticleIndexingHarness(t)
	article := h.upload(t, "Known date", "Date independent observations for a published article.")
	for _, date := range []string{"2026-10-01", "2026-10-01T08:00:00+08:00", "", "tomorrow", "2026-02-30"} {
		intake, err := article.IntakeMetadata()
		require.NoError(t, err)
		intake.SourceInfo.PublishedAt = date
		require.NoError(t, article.SetIntakeMetadata(intake))
		require.NoError(t, h.db.Save(article).Error)
		var row struct{ SearchPublishedAt *time.Time }
		require.NoError(t, h.db.Table("knowledges").Where("id=?", article.ID).First(&row).Error)
		if date == "2026-10-01" || date == "2026-10-01T08:00:00+08:00" {
			require.NotNil(t, row.SearchPublishedAt)
			require.Equal(t, "2026-10-01T00:00:00Z", row.SearchPublishedAt.UTC().Format(time.RFC3339))
		} else {
			require.Nil(t, row.SearchPublishedAt, "a missing or invalid date never uses collected_at")
		}
	}
}
