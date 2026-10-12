package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"

	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
)

type articleProcessingChat struct {
	retainedProcessingChat
	output   string
	requests [][]chat.Message
	reject   bool
}

func (m *articleProcessingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.stages = append(m.stages, stage)
	if stage == "intake_review" {
		return &types.ChatResponse{Content: fmt.Sprintf(`{"approved":%t,"reason":"Fixture sourced article"}`, !m.reject)}, nil
	}
	if len(messages) > 0 && strings.Contains(messages[0].Content, "Decide whether the source mention") {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":1}`}, nil
	}
	m.requests = append(m.requests, messages)
	if stage == "table_metadata" {
		return &types.ChatResponse{Content: "The table records product counts with product and count columns."}, nil
	}
	return &types.ChatResponse{Content: m.output}, nil
}

type articleProcessingModels struct {
	*summaryStateModelService
	embeddingCalls *int
	embeddingErr   error
}

func (s articleProcessingModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	if s.embeddingCalls != nil {
		*s.embeddingCalls++
	}
	return chunkSearchEmbedder{}, s.embeddingErr
}

type articleProcessingFiles struct{ retainedIntakeFiles }

func (s *articleProcessingFiles) SaveBytes(_ context.Context, body []byte, _ uint64, name string, _ bool) (string, error) {
	path := "local://1/exports/" + name
	if s.saved == nil {
		s.saved = map[string][]byte{}
	}
	if s.written == nil {
		s.written = map[string][]byte{}
	}
	s.saved[path] = bytes.Clone(body)
	s.written[path] = bytes.Clone(body)
	return path, nil
}
func (s *articleProcessingFiles) SaveFile(ctx context.Context, h *multipart.FileHeader, space uint64, id string) (string, error) {
	f, err := h.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, space, id, false)
}

type articleProcessingHarness struct {
	*retainedIntakeHarness
	processor *articleProcessingChat
}

func newArticleProcessingHarness(t *testing.T, vision bool) *articleProcessingHarness {
	t.Helper()
	h := newRetainedIntakeHarness(t)
	model := &articleProcessingChat{output: validProcessingFixture("event")}
	metadata := &types.Model{ID: "local-fixture", SpaceID: h.kb.SpaceID, Name: "Processing fixture", Type: types.ModelTypeGeneration, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{SupportsVision: vision, ContextWindow: 32768}}
	require.NoError(t, h.db.Create(metadata).Error)
	require.NoError(t, h.db.Create(&types.Model{ID: "embedding", SpaceID: h.kb.SpaceID, Name: "Vector fixture", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	h.kb.ModelTiers = types.ModelTierConfig{Light: "local-fixture", Generation: "local-fixture", EntityResolution: "local-fixture", Embedding: "embedding"}
	h.kb.EmbeddingModelID = "embedding"
	h.kb.IndexingStrategy = types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	require.NoError(t, h.db.Save(h.kb).Error)
	h.service.modelService = articleProcessingModels{summaryStateModelService: &summaryStateModelService{model: model, metadata: metadata}}
	h.service.retrieveEngine = retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(h.db))
	chunks := h.service.chunkService.(*chunkService)
	chunks.entryRepo = h.service.repo
	chunks.kbRepository = repository.NewKnowledgeBaseRepository(h.db)
	chunks.modelService = h.service.modelService
	chunks.retrieveEngine = h.service.retrieveEngine
	h.service.imageResolver = docparser.NewImageResolver()
	h.service.fileSvc = &articleProcessingFiles{}
	h.service.config = &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{MaxImages: 8}}
	return &articleProcessingHarness{retainedIntakeHarness: h, processor: model}
}
func articleImageDataURI(t *testing.T, n uint8) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{n, uint8(x), uint8(y), 255})
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}
func (h *articleProcessingHarness) upload(t *testing.T, title, body string) *types.Entry {
	t.Helper()
	article, err := h.service.createLegacyFileFixture(h.ctx, h.kb.ID, newMultipartFileHeader(t, title+".md", body), map[string]string{"publisher": "Acme", "original_url": "https://example.org/" + title, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	return h.article(t, article.ID)
}
func articleProcessingOutput(t *testing.T, contentType string, entities []types.EntityCandidate, topics []string) string {
	t.Helper()
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(validProcessingFixture(contentType)), &result))
	values := make([]map[string]interface{}, 0, len(entities))
	for _, e := range entities {
		values = append(values, map[string]interface{}{"name": e.Name, "type": e.EntityType, "aliases": e.Aliases})
	}
	result["entities"] = values
	result["topics"] = topics
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	return string(raw)
}

func TestArticleProcessingFullMarkdownPersistsSummaryEntitiesTopicsAndVectors(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Product news", Description: "Original product announcements"})
	require.NoError(t, err)
	secondTopic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Research", Description: "Experimental results"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Acme", EntityType: "company", Aliases: []string{"Acme Inc"}}}, []string{topic.ID, "missing-topic"})
	body := "# First announcement\n\nAcme released a widget with original measurements.\n\n![chart](" + articleImageDataURI(t, 10) + ")"
	article := h.upload(t, "first-event", body)
	require.Equal(t, "event", article.ContentType)
	require.Equal(t, "full", article.ProcessingMode)
	require.NotNil(t, article.StructuredSummary)
	require.Len(t, article.StructuredSummary.Points, 3)
	require.NotEmpty(t, article.StructuredSummary.KeyTerms)
	require.Contains(t, article.Description, article.StructuredSummary.Overview)
	var chunks, links, topicLinks int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", article.ID).Count(&chunks).Error)
	require.Positive(t, chunks)
	require.NoError(t, h.db.Model(&types.EntryEntity{}).Where("entry_id=?", article.ID).Count(&links).Error)
	require.EqualValues(t, 1, links)
	require.NoError(t, h.db.Model(&types.EntryTopic{}).Where("entry_id=?", article.ID).Count(&topicLinks).Error)
	require.EqualValues(t, 1, topicLinks)
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.VectorRetrieverType, KnowledgeBaseIDs: []string{h.kb.ID}, Embedding: []float32{1, 0, 0}, TopK: 10, Threshold: .9}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotEmpty(t, hits[0].Results)
	retrieved := []*types.SearchResult{{ID: hits[0].Results[0].ChunkID}}
	searchutil.EnrichSearchResultsImageInfo(h.ctx, h.service.chunkService.GetRepository(), article.SpaceID, retrieved)
	var imageInfos []types.ImageInfo
	require.NoError(t, json.Unmarshal([]byte(retrieved[0].ImageInfo), &imageInfos))
	require.Len(t, imageInfos, 1, "retrieved body chunk keeps its image reference")
	require.True(t, strings.HasPrefix(imageInfos[0].URL, "local://"))
	require.Empty(t, imageInfos[0].Caption)
	require.Empty(t, imageInfos[0].OCRText)
	read, _, err := h.service.GetEntryBody(h.ctx, article.ID)
	require.NoError(t, err)
	read.Close()
	require.NotEmpty(t, article.FilePath)
	firstEntities, err := NewEntityService(h.db, h.service.modelService).ListEntities(h.ctx, types.EntityQuery{})
	require.NoError(t, err)
	require.Len(t, firstEntities, 1)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Acme", EntityType: "company"}}, []string{})
	second := h.upload(t, "second-event", "# Second announcement\n\nAcme opened a new office on a different continent. This is an original report of the expansion and its date.")
	var entityLinks []types.EntryEntity
	require.NoError(t, h.db.Where("entry_id IN ?", []string{article.ID, second.ID}).Find(&entityLinks).Error)
	require.Len(t, entityLinks, 2)
	require.Equal(t, entityLinks[0].EntityID, entityLinks[1].EntityID)
	request := h.processor.requests[0][1]
	require.Len(t, request.Images, 1)
	require.Contains(t, request.Content, topic.ID)
	require.Contains(t, request.Content, topic.Description)
	require.Contains(t, request.Content, secondTopic.ID)
	require.Len(t, h.processor.requests, 2, "one processing call per approved article")
}

func TestArticleProcessingSummaryOnlyDeletesBodiesImagesOriginalAndSearchesSummary(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	h.processor.output = validProcessingFixture("tutorial")

}

func TestRemovedProcessingTasksCannotBeQueued(t *testing.T) {
	for _, name := range []string{"summary:generation", "question:generation", "image:multimodal", "knowledge:post_process", "datatable:summary"} {
		_, exists := types.QueueForTaskType(name)
		require.False(t, exists, name)
	}
}

func TestArticleProcessingDefaultLimitsAndTableInput(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	chars, images, completion := h.service.articleProcessingLimits(&types.Model{})
	require.Equal(t, 24000, chars)
	require.Equal(t, 8, images)
	require.Equal(t, 4096, completion)
	chars, _, completion = h.service.articleProcessingLimits(&types.Model{Parameters: types.ModelParameters{ContextWindow: 10000, MaxOutputTokens: 1000}})
	require.Equal(t, 6000, chars)
	require.Equal(t, 1000, completion)
	h.processor.output = validProcessingFixture("event")
	article, err := h.service.createLegacyFileFixture(h.ctx, h.kb.ID, newMultipartFileHeader(t, "measurements.csv", "product,count\nwidget,5\ngadget,8\n"), map[string]string{"publisher": "Acme", "original_url": "https://example.org/table", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, article.ID).ParseStatus)
	require.Len(t, h.processor.requests, 3, "two retained table metadata calls and one article processing call")
	var input articleProcessingInput
	require.NoError(t, json.Unmarshal([]byte(h.processor.requests[2][1].Content), &input))
	require.Contains(t, input.Body, "# Table Summary")
	require.Equal(t, 1, strings.Count(strings.Join(h.processor.stages, ","), "article_processing"))
	require.Len(t, h.queue.tasks, 1, "table preparation uses the document worker without a separate task")
}

func TestSummaryOnlyProcessingEmbedsArticleAndCleansPreviousFull(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	full := h.upload(t, "change-mode", "An original announcement with a supporting image.\n\n![image]("+articleImageDataURI(t, 70)+")")
	h.processor.output = validProcessingFixture("tutorial")
	_, err := h.service.ReparseEntry(h.ctx, full.ID, nil)
	require.NoError(t, err)
	calls := 0
	current := h.service.modelService.(articleProcessingModels)
	current.embeddingCalls = &calls
	h.service.modelService = current
	h.drain(t)
	article := h.article(t, full.ID)
	require.Equal(t, "summary_only", article.ProcessingMode)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Positive(t, calls, "summary-only processing generates an article vector")
	require.Empty(t, h.service.fileSvc.(*articleProcessingFiles).saved)
	var count int64
	require.NoError(t, h.db.Table("embeddings").Where("entry_id=?", full.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", full.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", full.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
