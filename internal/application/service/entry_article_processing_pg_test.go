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

	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

type articleProcessingChat struct {
	retainedProcessingChat
	output   string
	requests [][]chat.Message
	reject   bool
}

type correctedProcessingChat struct {
	retainedProcessingChat
	calls map[string]int
}

func (m *correctedProcessingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.calls[stage]++
	if stage == "intake_review" {
		return &types.ChatResponse{Content: "```json\n{\"approved\":true,\"reason\":\"Original measurements\"}\n```"}, nil
	}
	result, err := parseArticleProcessingResult(validProcessingFixture("event"))
	if err != nil {
		return nil, err
	}
	if m.calls[stage] == 1 {
		result.Summary.Points = result.Summary.Points[:1]
	} else if m.calls[stage] == 2 && !strings.Contains(messages[0].Content, "summary must contain 3 to 5 points") {
		return nil, fmt.Errorf("missing summary validation feedback")
	}
	raw, err := json.Marshal(result)
	return &types.ChatResponse{Content: "```json\n" + string(raw) + "\n```"}, err
}

func TestProcessingModelOutputAcceptsFencesAndCorrectsInvalidSummary(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.config.ArticleProcessing.MaxBodyChars = 140
	k := segmentedEntry(t, h)
	model := &correctedProcessingChat{calls: map[string]int{}}
	h.service.modelService.(articleProcessingModels).summaryStateModelService.model = model
	body := strings.Repeat("Original instrument measurements and observations. ", 12)
	approved, _, err := h.service.reviewSegments(h.ctx, h.library, k, body, types.SourceInfo{OriginalURL: "https://example.org/measurements"})
	require.NoError(t, err)
	require.True(t, approved)
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Len(t, result.Summary.Points, 3)
	require.Equal(t, 2, model.calls["article_summary_merge"])
	var run entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=? AND phase=?", k.ID, "processing").First(&run).Error)
	var parts []segmentCheckpoint
	require.NoError(t, json.Unmarshal(run.Plan, &parts))
	require.Equal(t, len(parts)+1, model.calls["article_processing"], "only the invalid response gets one corrective call")
}

func TestSummaryMergeReservesSpaceForOutputCorrection(t *testing.T) {
	h, model := segmentedHarness(t)
	k := segmentedEntry(t, h)
	parts := []*articleProcessingResult{{Summary: &types.EntrySummary{
		Overview: strings.Repeat("x", 400), Points: []string{"one", "two", "three"}, KeyTerms: []string{},
	}}}
	metadata := &types.Model{Parameters: types.ModelParameters{ContextWindow: 1024, MaxOutputTokens: 16}}
	_, err := h.service.mergeSegmentSummaries(h.ctx, h.library, k, parts, metadata)
	require.ErrorContains(t, err, "超过合并模型上下文")
	require.Zero(t, model.mergeCalls, "reject before sending a request with no room for correction")
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
	var input articleProcessingInput
	var output map[string]any
	if stage == "article_processing" && json.Unmarshal([]byte(messages[len(messages)-1].Content), &input) == nil && json.Unmarshal([]byte(m.output), &output) == nil {
		captions := []extractedImage{}
		for _, image := range input.Images {
			captions = append(captions, extractedImage{Index: image.Index, Description: "Fixture chart shows deployment counts."})
		}
		output["image_descriptions"] = captions
		data, _ := json.Marshal(output)
		return &types.ChatResponse{Content: string(data)}, nil
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
	for i, name := range []string{"event", "opinion", "research", "tutorial", "case", "reference", "other"} {
		row := types.LibraryContentType{LibraryID: h.library.ID, Name: name, Position: i, Enabled: true, ExtractGraph: name != "other"}
		require.NoError(t, h.db.Create(&row).Error)
		h.library.ContentTypes = append(h.library.ContentTypes, row)
	}
	model := &articleProcessingChat{output: validProcessingFixture("event")}
	metadata := &types.Model{ID: "local-fixture", SpaceID: h.library.SpaceID, Name: "Processing fixture", Type: types.ModelTypeGeneration, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{SupportsVision: vision, ContextWindow: 32768}}
	require.NoError(t, h.db.Create(metadata).Error)
	require.NoError(t, h.db.Create(&types.Model{ID: "embedding", SpaceID: h.library.SpaceID, Name: "Vector fixture", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	h.library.ModelTiers = types.ModelTierConfig{Light: "local-fixture", Generation: "local-fixture", EntityResolution: "local-fixture", Embedding: "embedding"}
	h.library.EmbeddingModelID = "embedding"
	h.library.IndexingStrategy = types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	require.NoError(t, h.db.Save(h.library).Error)
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=?", h.library.SpaceID).Update("embedding_model_id", h.library.EmbeddingModelID).Error)
	h.service.modelService = articleProcessingModels{summaryStateModelService: &summaryStateModelService{model: model, metadata: metadata}}
	h.service.retrieveEngine = retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(h.db))
	chunks := h.service.chunkService.(*chunkService)
	chunks.entryRepo = h.service.repo
	chunks.libraryRepository = repository.NewLibraryRepository(h.db)
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
	article, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, title+".md", body), map[string]string{"publisher": "Acme", "original_url": "https://example.org/" + title, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
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
		description := e.Description
		if description == "" {
			description = e.Name + " is a participant in this announcement."
		}
		values = append(values, map[string]interface{}{"name": e.Name, "type": e.EntityType, "aliases": e.Aliases, "description": description, "relevance": "central", "evidence_ids": []int{1}})
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
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.VectorRetrieverType, LibraryIDs: []string{h.library.ID}, Embedding: []float32{1, 0, 0}, TopK: 10, Threshold: .9}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotEmpty(t, hits[0].Results)
	var bodyChunk types.Chunk
	require.NoError(t, h.db.Where("entry_id=? AND chunk_type=?", article.ID, types.ChunkTypeText).First(&bodyChunk).Error)
	foundBody := false
	for _, hit := range hits[0].Results {
		if hit.ChunkID == bodyChunk.ID {
			foundBody = true
		}
	}
	require.True(t, foundBody, "body and description vectors may tie, but the body remains retrievable")
	retrieved := []*types.SearchResult{{ID: bodyChunk.ID}}
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
	require.NotEmpty(t, firstEntities[0].Description)
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

func TestRemovedProcessingTasksCannotBeQueued(t *testing.T) {
	for _, name := range []string{"summary:generation", "question:generation", "image:multimodal", "knowledge:post_process", "datatable:summary"} {
		_, exists := types.QueueForTaskType(name)
		require.False(t, exists, name)
	}
}

func TestArticleProcessingDefaultLimits(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	chars, images, completion := h.service.articleProcessingLimits(&types.Model{})
	require.Equal(t, 24000, chars)
	require.Equal(t, 8, images)
	require.Equal(t, 4096, completion)
	chars, _, completion = h.service.articleProcessingLimits(&types.Model{Parameters: types.ModelParameters{ContextWindow: 10000, MaxOutputTokens: 1000}})
	require.Equal(t, 6000, chars)
	require.Equal(t, 1000, completion)

}
