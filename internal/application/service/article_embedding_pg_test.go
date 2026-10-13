package service

import (
	"context"
	"crypto/sha256"
	"errors"

	"strings"
	"sync"
	"testing"
	"time"

	"reliquary/internal/models/embedding"
	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, h.db.Where("entry_id=?", article.ID).Order("chunk_index").Find(&chunks).Error)
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
	require.NoError(t, h.db.Table("entries").Where("id=?", article.ID).Pluck("search_text", &searchText).Error)
	require.NotContains(t, searchText, " · ")
	require.NotContains(t, searchText, "chunkbodyfixture", "header index does not repeat the body")
	var hits int64
	require.NoError(t, h.db.Table("chunks").Where("entry_id=? AND seq_id @@@ paradedb.match_disjunction('content', ?)", article.ID, "chunkbodyfixture").Count(&hits).Error)
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
		require.Equal(t, want+"\nbody", buildEntryIndexContent(&copy, "body"))
	}
}

func TestArticleChunkPrefixOmitsMissingPublicationDate(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	article, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
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
	require.NoError(t, h.db.Where("entry_id=?", article.ID).Find(&chunks).Error)
	require.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		if chunk.ChunkType != types.ChunkTypeParentText {
			require.Contains(t, e.recorded(), article.Title+" · Undated publisher\n"+chunk.Content)
		}
	}
	var date struct{ SearchPublishedAt *time.Time }
	require.NoError(t, h.db.Table("entries").Where("id=?", article.ID).First(&date).Error)
	require.Nil(t, date.SearchPublishedAt)
}

func TestArticleTitleVectorChangeRollsBackWhenEmbeddingFails(t *testing.T) {
	h, e := newArticleIndexingHarness(t)
	article := h.upload(t, "Original title", "Measured deployment details and original observations.")
	var originalHash string
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", article.ID).Pluck("content_hash", &originalHash).Error)
	e.err = errors.New("fixture embedding unavailable")
	err := h.service.UpdateEntry(h.ctx, &types.Entry{ID: article.ID, Title: "Changed title"})
	require.ErrorContains(t, err, "fixture embedding unavailable")
	reloaded := h.article(t, article.ID)
	require.Equal(t, article.Title, reloaded.Title)
	var currentHash string
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", article.ID).Pluck("content_hash", &currentHash).Error)
	require.Equal(t, originalHash, currentHash)
	e.err = nil
	require.NoError(t, h.service.UpdateEntry(h.ctx, &types.Entry{ID: article.ID, Title: "Changed title"}))
	reloaded = h.article(t, article.ID)
	require.Equal(t, "Changed title", reloaded.Title)
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", article.ID).Pluck("content_hash", &currentHash).Error)
	require.NotEqual(t, originalHash, currentHash)
}

func TestUnprocessedArticleTitleUpdateDoesNotRequireEmbedding(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	article := &types.Entry{ID: "pending-article", SpaceID: h.library.SpaceID, LibraryID: h.library.ID,
		Type: "url", SourceKind: "article", Title: "Pending title", Source: "https://example.org/pending",
		ParseStatus: types.ParseStatusPending, EnableStatus: "enabled"}
	require.NoError(t, h.db.Create(article).Error)
	h.service.modelService = nil
	require.NoError(t, h.service.UpdateEntry(h.ctx, &types.Entry{ID: article.ID, Title: "Edited pending title"}))
	reloaded := h.article(t, article.ID)
	require.Equal(t, "Edited pending title", reloaded.Title)
	var count int64
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestImportedDocumentHasSummaryVectorsAndReportsKeepChunkPrefixes(t *testing.T) {
	h, vault := newIngestHarness(t)
	e := &indexingRecordingEmbedder{}
	models := indexingRecordingModels{articleProcessingModels: h.service.modelService.(articleProcessingModels), embedder: e}
	h.service.modelService = models
	h.service.chunkService.(*chunkService).modelService = models
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "mine", Acquisition: "verbatim", Title: "Personal notebook", Content: "# Personal notebook\n\npersonalprefixfixture preserved observations with complete supporting evidence."})
	require.NoError(t, err)
	h.drain(t)
	mine := h.article(t, result.EntryID)
	documents, err := NewVaultDocumentService(h.db, h.service.modelService, h.service.retrieveEngine, vault.root)
	require.NoError(t, err)
	defer documents.Close()
	report, err := documents.CreateReport(h.ctx, "Synthesis", "# Synthesis\n\nreportprefixfixture preserved analysis", nil)
	require.NoError(t, err)
	for _, pair := range []struct {
		entry          *types.Entry
		label          string
		summaryVectors int64
	}{{mine, "本人", 1}, {report.Entry, "整理结果", 0}} {
		var chunks []types.Chunk
		require.NoError(t, h.db.Where("entry_id=?", pair.entry.ID).Find(&chunks).Error)
		require.NotEmpty(t, chunks)
		for _, chunk := range chunks {
			require.Contains(t, e.recorded(), pair.entry.Title+" · "+pair.label+"\n"+chunk.Content)
			require.NotContains(t, chunk.Content, pair.label)
		}
		var count int64
		require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", pair.entry.ID).Count(&count).Error)
		require.EqualValues(t, pair.summaryVectors, count)
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
	article.StructuredSummary = &types.EntrySummary{Overview: "Overviewmarker", Points: []string{"Pointmarker"}, KeyTerms: []string{"Termmarker"}}
	require.NoError(t, h.db.Save(article).Error)
	var vectors int64
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", article.ID).Count(&vectors).Error)
	require.Zero(t, vectors, "summary updates invalidate the old article vector until it is rebuilt")
	assertHit := func(term string, want int64) {
		t.Helper()
		var count int64
		require.NoError(t, h.db.Table("entries").Where("id=? AND search_seq_id @@@ paradedb.match_disjunction('search_text', ?)", article.ID, term).Count(&count).Error)
		require.Equal(t, want, count, term)
	}
	for _, term := range []string{"Headermarker", "Publishermarker", "Authormarker", "Overviewmarker", "Pointmarker", "Termmarker", "Entitymarker", "Aliasmarker", "Topicmarker"} {
		assertHit(term, 1)
	}
	assertHit("discardedfixture", 0)
	var entityID string
	require.NoError(t, h.db.Table("entry_entities").Where("entry_id=?", article.ID).Pluck("entity_id", &entityID).Error)
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
	require.NoError(t, h.db.Where("entry_id=?", article.ID).Delete(&types.EntryTopic{}).Error)
	assertHit("Updatedtopicmarker", 0)
	require.NoError(t, h.db.Where("entry_id=?", article.ID).Delete(&types.EntryEntity{}).Error)
	assertHit("Renamedentitymarker", 0)
	var text string
	require.NoError(t, h.db.Table("entries").Where("id=?", article.ID).Pluck("search_text", &text).Error)
	require.False(t, strings.Contains(text, " · "))
}

func TestEntrySearchPublishedAtUsesExplicitValidSourceDate(t *testing.T) {
	h, _ := newArticleIndexingHarness(t)
	article := h.upload(t, "Known date", "Date independent observations for a published article.")
	for _, date := range []string{"2026-10-01", "2026-10-01T08:00:00+08:00", "", "tomorrow", "2026-02-30"} {
		intake, err := article.IntakeMetadata()
		require.NoError(t, err)
		intake.SourceInfo.PublishedAt = date
		require.NoError(t, article.SetIntakeMetadata(intake))
		require.NoError(t, h.db.Save(article).Error)
		var row struct{ SearchPublishedAt *time.Time }
		require.NoError(t, h.db.Table("entries").Where("id=?", article.ID).First(&row).Error)
		if date == "2026-10-01" || date == "2026-10-01T08:00:00+08:00" {
			require.NotNil(t, row.SearchPublishedAt)
			require.Equal(t, "2026-10-01T00:00:00Z", row.SearchPublishedAt.UTC().Format(time.RFC3339))
		} else {
			require.Nil(t, row.SearchPublishedAt, "a missing or invalid date never uses collected_at")
		}
	}
}
