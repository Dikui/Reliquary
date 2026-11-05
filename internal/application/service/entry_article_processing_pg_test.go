package service

import (
	"bytes"
	"context"
	"crypto/sha256"
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

	"reliquary/internal/application/graph"
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
	"gorm.io/gorm"
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
		return &types.ChatResponse{Content: "```json\n" + reviewFixtureContent(`{"approved":true,"reason":"Original measurements"}`, messages) + "\n```"}, nil
	}
	result, err := parseArticleProcessingResult(validProcessingFixture("event"))
	if err != nil {
		return nil, err
	}
	if m.calls[stage] == 1 {
		result.Summary.Points = result.Summary.Points[:1]
	}
	raw, err := json.Marshal(result)
	return &types.ChatResponse{Content: "```json\n" + string(raw) + "\n```"}, err
}

func TestProcessingModelOutputAcceptsFencesAndSinglePointSummary(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.config.ArticleProcessing.MaxBodyChars = 140
	k := segmentedEntry(t, h)
	k.Title = ""
	model := &correctedProcessingChat{calls: map[string]int{}}
	h.service.modelService.(articleProcessingModels).summaryStateModelService.model = model
	body := strings.Repeat("Original instrument measurements and observations. ", 12)
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Len(t, result.Summary.Points, 1)
	require.Equal(t, "Acme announces a widget", result.Title, "the merge returns a title for the whole document")
	require.Equal(t, 1, model.calls["article_summary_merge"])
	var run entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=? AND phase=?", k.ID, "processing").First(&run).Error)
	var parts []segmentCheckpoint
	require.NoError(t, json.Unmarshal(run.Plan, &parts))
	require.Equal(t, len(parts), model.calls["article_processing"], "a single supported point is valid")
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
	if stage == "article_graph_verification" {
		return supportedGraphVerificationFixture(messages)
	}
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

func TestArticleProcessingGeneratesMissingTitleWithSummary(t *testing.T) {
	h, vault := newIngestHarness(t)
	request := &types.IngestRequest{Library: "official", URL: "https://example.org/widget-release", Acquisition: "verbatim", Publisher: "Acme",
		Content: "Acme released a widget. The announcement describes its deployment capabilities and reports original measurements."}
	result, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, request.URL, h.article(t, result.EntryID).Title)
	h.drain(t)
	article := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
	require.Equal(t, "Acme announces a widget", article.Title)
	require.NotNil(t, article.StructuredSummary)
	require.Len(t, h.processor.requests, 1, "title generation shares the summary call")
	var input articleProcessingInput
	require.NoError(t, json.Unmarshal([]byte(h.processor.requests[0][1].Content), &input))
	require.Empty(t, input.Title, "the URL placeholder is not treated as a real title")
	data, _, body := readVaultArticle(t, vault, article)
	header, _, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Equal(t, article.Title, header.Title)
	require.Equal(t, request.Content, strings.TrimSpace(string(body)))
}

func TestArticleProcessingRejectsMultilineGeneratedTitle(t *testing.T) {
	raw := strings.Replace(validProcessingFixture("event"), "Acme announces a widget", `Acme\nannounces a widget`, 1)
	_, err := parseArticleProcessingResult(raw)
	require.ErrorContains(t, err, "title must be a single line")
}

func TestAllTemplatesProcessEntitiesAndGraph(t *testing.T) {
	for _, library := range []string{"personal", "references", "ai-output"} {
		t.Run(library, func(t *testing.T) {
			h, vault := newIngestHarness(t)
			var topicLibrary types.Library
			require.NoError(t, h.db.Where("system_role=?", library).First(&topicLibrary).Error)
			topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{LibraryID: topicLibrary.ID, Name: "Deployment notes", Description: "Acme and Beta deployment measurements"})
			require.NoError(t, err)
			kind := "方案"
			if library == "references" {
				kind = "论文"
			} else if library == "ai-output" {
				kind = "other"
			}
			output, err := parseArticleProcessingResult(articleProcessingOutput(t, kind, []types.EntityCandidate{{Name: "Acme", EntityType: "company", Description: "A partner of Beta."}, {Name: "Beta", EntityType: "company", Description: "A partner of Acme."}}, []string{topic.ID}))
			require.NoError(t, err)
			output.Relations = []extractedRelation{{Subject: "Acme", Object: "Beta", Type: "合作", EvidenceIDs: []int{1}}}
			raw, err := json.Marshal(output)
			require.NoError(t, err)
			h.processor.output = string(raw)
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: library, URL: "https://example.org/measurements", Acquisition: "verbatim", Graph: entrySearchBool(true),
				Content: "Acme and Beta jointly measured deployment performance. The original report describes the method and complete measurements."})
			require.NoError(t, err)
			h.drain(t)
			article := h.article(t, result.EntryID)
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus, article.ErrorMessage)
			require.Equal(t, "Acme announces a widget", article.Title)
			require.NotNil(t, article.StructuredSummary)
			require.Empty(t, article.Entities)
			var topicCount int64
			require.NoError(t, h.db.Model(&types.EntryTopic{}).Where("entry_id=? AND topic_id=?", article.ID, topic.ID).Count(&topicCount).Error)
			require.EqualValues(t, 1, topicCount)
			for _, table := range []string{"entities", "entry_entities", "relations"} {
				var count int64
				require.NoError(t, h.db.Table(table).Count(&count).Error)
				require.Positive(t, count, table)
			}
			for _, table := range []string{"chunks", "article_embeddings"} {
				if library == "ai-output" && table == "article_embeddings" {
					continue // Reports retain body vectors rather than a separate summary vector.
				}
				var count int64
				require.NoError(t, h.db.Table(table).Where("entry_id=?", article.ID).Count(&count).Error)
				require.Positive(t, count, table)
			}
			enabled, err := graph.Enabled(h.db, article)
			require.NoError(t, err)
			require.True(t, enabled)
			stored, err := ReadEntryProcessingOutput(h.ctx, h.db, article.SpaceID, article.ID)
			require.NoError(t, err)
			require.NotEmpty(t, stored.Relations)
			_, header, _ := readVaultArticle(t, vault, article)
			require.Contains(t, string(header), "[[Acme]]")
			require.Len(t, h.processor.requests, 1, "no entity judgement calls")
			require.Contains(t, h.processor.requests[0][0].Content, "每个实体输出")
			require.Contains(t, h.processor.requests[0][0].Content, "relation_types=")
			require.NotContains(t, h.processor.requests[0][0].Content, "本库不抽取实体")
		})
	}
}

func TestArticleProcessingFullMarkdownPersistsSummaryEntitiesTopicsAndVectors(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{LibraryID: h.library.ID, Name: "Product news", Description: "Original product announcements"})
	require.NoError(t, err)
	secondTopic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{LibraryID: h.library.ID, Name: "Research", Description: "Experimental results"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Acme", EntityType: "company", Aliases: []string{"Acme Inc"}}}, []string{topic.ID, "missing-topic"})
	body := "# First announcement\n\nAcme released a widget with original measurements.\n\n![chart](" + articleImageDataURI(t, 10) + ")"
	article := h.upload(t, "first-event", body)
	require.Equal(t, "first-event.md", article.Title, "processing preserves the supplied title")
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
	firstEntities, err := NewEntityService(h.db, h.service.modelService).ListEntities(h.ctx, types.EntityQuery{LibraryID: article.LibraryID})
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

// Managed libraries associate recognized products without extending the catalog
// or allowing document processing to replace manually maintained relationships.
func TestManagedArticleProcessingOnlyAssociatesExistingEntities(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.library.EntityMode = types.EntityModeManaged
	require.NoError(t, h.db.Save(h.library).Error)
	widget := types.Entity{ID: "managed-widget", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, CanonicalName: "Widget", NormalizedName: "widget", EntityType: "product", Description: "Maintained description"}
	parent := types.Entity{ID: "managed-parent", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, CanonicalName: "Family", NormalizedName: "family", EntityType: "product"}
	require.NoError(t, h.db.Create(&[]types.Entity{widget, parent}).Error)
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: h.library.ID, Name: "belongs_to", Enabled: true}).Error)
	relation := types.Relation{ID: "managed-relation", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, SubjectID: widget.ID, ObjectID: parent.ID, RelationType: "belongs_to", Version: 1}
	require.NoError(t, h.db.Create(&relation).Error)
	var output map[string]any
	require.NoError(t, json.Unmarshal([]byte(articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Widget", EntityType: "product", Description: "Overwrite attempt"}, {Name: "Unknown", EntityType: "product"}}, []string{})), &output))
	output["relations"] = []extractedRelation{{Subject: "Widget", Object: "Unknown", Type: "invented", EvidenceIDs: []int{1}}}
	raw, err := json.Marshal(output)
	require.NoError(t, err)
	h.processor.output = string(raw)
	entry := h.upload(t, "managed-products", "Widget and Unknown have new versions. Widget is still part of Family.")
	require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus, entry.ErrorMessage)
	var links []types.EntryEntity
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, widget.ID, links[0].EntityID)
	require.NotEmpty(t, links[0].Evidence)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(entry.VaultBody))), links[0].BodyHash)
	var saved types.Entity
	require.NoError(t, h.db.First(&saved, "id=?", widget.ID).Error)
	require.Equal(t, widget.Description, saved.Description)
	var count int64
	require.NoError(t, h.db.Model(&types.Entity{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, h.db.Model(&types.Relation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	result, err := NewGraphSearchService(h.db, nil).Search(h.ctx, types.GraphSearchRequest{Libraries: []string{h.library.ID}, Mode: "all"})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1, "source-free managed relationships remain visible")
	require.NoError(t, h.service.cleanupLibraryRebuild(h.ctx, entry))
	require.NoError(t, h.db.Model(&types.Relation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, h.library.SpaceID); err != nil {
			return err
		}
		_, err := graph.DeleteRelationTx(tx, h.library.SpaceID, relation.ID, "test")
		return err
	}))
	require.NoError(t, h.db.Model(&types.GraphPendingSync{}).Where("relation_id=? AND deleted=true", relation.ID).Count(&count).Error)
	require.EqualValues(t, 2, count, "explicit deletion notifies both endpoints")
}
