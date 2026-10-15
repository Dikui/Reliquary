package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// The fixture chooses the library type first and obeys switches in the prompt.
type jointProcessingChat struct {
	retainedProcessingChat
	kind      string
	malicious bool
	calls     int
}

func (m *jointProcessingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "intake_review" {
		return &types.ChatResponse{Content: `{"approved":true,"reason":"fixture"}`}, nil
	}
	if stage == "article_summary_merge" {
		return &types.ChatResponse{Content: validProcessingFixture(m.kind)}, nil
	}
	if stage != "article_processing" {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":1}`}, nil
	}
	m.calls++
	var input articleProcessingInput
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	var rows []types.LibraryContentType
	var override *bool
	for _, line := range strings.Split(messages[0].Content, "\n") {
		if strings.HasPrefix(line, "content_types=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "content_types=")), &rows); err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(line, "graph_override=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "graph_override=")), &override); err != nil {
				return nil, err
			}
		}
	}
	l := &types.Library{ContentTypes: rows}
	output, _ := parseArticleProcessingResult(validProcessingFixture(m.kind))
	company := "company"
	person := "person"
	output.Entities = []articleProcessingEntity{{Name: "Acme", Type: &company}, {Name: "Beta", Type: &company}, {Name: "Alice", Type: &person}}
	output.Relations = []extractedRelation{}
	if m.malicious || libraryGraphEnabled(l, libraryContentType(l, m.kind), override) {
		output.Relations = append(output.Relations, extractedRelation{Subject: "Acme", Object: "Beta", Type: "合作", Start: 0, End: min(4, len([]rune(input.Body))), Speaker: "Alice"})
	}
	output.ImageDescriptions = []extractedImage{}
	for _, image := range input.Images {
		output.ImageDescriptions = append(output.ImageDescriptions, extractedImage{Index: image.Index, Description: "SpectralChart 图表显示 2026 年增长 42%，收入为 120 万。"})
	}
	data, err := json.Marshal(output)
	return &types.ChatResponse{Content: string(data)}, err
}

func TestIKB11PromptSnapshotsAndSwitchMatrix(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	fixture := &jointProcessingChat{}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	for _, defaults := range types.SystemLibraryDefaults() {
		for _, override := range []*bool{nil, entrySearchBool(true), entrySearchBool(false)} {
			mode := "default"
			if override != nil {
				mode = fmt.Sprint(*override)
			}
			k := &types.Entry{Graph: override}
			name := defaults.SystemRole + "-" + mode
			prompt := libraryProcessingPrompt(defaults, k)
			path := filepath.Join("testdata", "ikb11-prompts", name+".txt")
			if os.Getenv("UPDATE_IKB11_SNAPSHOTS") == "1" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(prompt+"\n"), 0644))
			}
			expected, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(expected), prompt+"\n")
			if defaults.Template == types.TemplateGenerated {
				require.False(t, libraryGraphEnabled(defaults, "其他", override))
				continue
			}
			for _, row := range defaults.ContentTypes {
				t.Run(name+"/"+row.Name, func(t *testing.T) {
					library := *defaults
					library.ModelTiers = h.library.ModelTiers
					fixture.kind = row.Name
					fixture.malicious = false
					result, err := h.service.callArticleProcessingPart(h.ctx, &library, k, "Acme 与 Beta 合作。", nil, nil)
					require.NoError(t, err)
					wanted := row.ExtractGraph && row.Name != "其他"
					if override != nil {
						wanted = *override
					}
					require.Equal(t, wanted, len(result.Relations) > 0)
					qualifySegmentOutput(&library, k, "Acme 与 Beta 合作。", types.ProcessingSegment{End: 15}, result)
					require.Equal(t, wanted, len(result.Relations) > 0)
					fixture.malicious = true
					result, err = h.service.callArticleProcessingPart(h.ctx, &library, k, "Acme 与 Beta 合作。", nil, nil)
					require.NoError(t, err)
					qualifySegmentOutput(&library, k, "Acme 与 Beta 合作。", types.ProcessingSegment{End: 15}, result)
					require.Equal(t, wanted, len(result.Relations) > 0, "server rejects relations despite a disobedient model")
				})
			}
		}
	}
	require.Equal(t, "其他", libraryContentType(types.SystemLibraryDefaults()[0], "unknown"))
}

func TestIKB11SegmentRelationsResolveGlobalIDsAndKeepAllEvidence(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	kind := "company"
	ct := "event"
	body := "# 新闻\n\nAcme 与 Beta 合作。\n\nAcme 与 Beta 再次合作。"
	parts := []*articleProcessingResult{}
	for _, p := range []types.ProcessingSegment{{Number: 1, Start: 0, End: 22}, {Number: 2, Start: 22, End: len([]rune(body))}} {
		part, _ := parseArticleProcessingResult(validProcessingFixture(ct))
		part.Entities = []articleProcessingEntity{{Name: "Acme", Type: &kind}, {Name: "Beta", Type: &kind}}
		part.Relations = []extractedRelation{{Subject: "Acme", Object: "Beta", Type: "合作", Start: 0, End: 4}}
		qualifySegmentOutput(h.library, k, body, p, part)
		parts = append(parts, part)
	}
	result := mergeArticleSegments(parts)
	require.Len(t, result.Relations, 1)
	require.Len(t, result.Relations[0].Evidence, 2)
	candidates := []types.EntityCandidate{{Name: "Acme", EntityType: kind}, {Name: "Beta", EntityType: kind}}
	resolved, err := h.service.resolveProcessingEntities(h.ctx, k, candidates, result)
	require.NoError(t, err)
	k.ContentType = ct
	relationCalls := 0
	fixture := &relationVectorFixture{vectors: map[string][]float32{"合作": {1, 0, 0}}, onCall: func(ctx context.Context) {
		relationCalls++
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		require.Equal(t, "relation_type_embedding", stage)
		require.Equal(t, types.CallPurposeOther, types.NormalizeModelCallPurpose(stage, types.ModelTierFromContext(ctx)))
		require.Equal(t, k.ID, *types.ModelCallEntryFromContext(ctx))
	}}
	h.service.modelService = relationVectorModels{articleProcessingModels: h.service.modelService.(articleProcessingModels), fixture: fixture}
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, resolved))
	require.Equal(t, 1, relationCalls)
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Len(t, out.Relations, 1)
	require.Len(t, out.Relations[0].Evidence, 2)
	require.NotEqual(t, "Acme", out.Relations[0].SubjectID)
	var subject types.Entity
	require.NoError(t, h.db.First(&subject, "id=?", out.Relations[0].SubjectID).Error)
	require.Equal(t, "Acme", subject.CanonicalName)
	require.Equal(t, 22, out.Relations[0].Evidence[1].Locations[0].Start)
	// Final majority type and current graph override are validated again before saving.
	k.Graph = entrySearchBool(false)
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, resolved))
	out, err = ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Empty(t, out.Relations)
}

func TestIKB11RelationTypesCatalogSynonymsAdditionAndLog(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	// Disable vector lookup in this fixture to isolate exact and containment rules.
	h.service.modelService = nil
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: h.library.ID, Name: "合作", Position: 0, Enabled: true}).Error)
	mapping, err := h.service.canonicalRelationTypes(h.ctx, h.library, []string{"合作", "合作伙伴", " 联 合 研 发 ", "联合研发项目"})
	require.NoError(t, err)
	require.Equal(t, "合作", mapping["合作"])
	require.Equal(t, "合作", mapping["合作伙伴"])
	require.Equal(t, mapping[" 联 合 研 发 "], mapping["联合研发项目"])
	var count int64
	require.NoError(t, h.db.Model(&types.LibraryRelationType{}).Where("library_id=?", h.library.ID).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, h.db.Model(&types.OperationLog{}).Where("operation_type='add_relation_type'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_ikb11_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'log failure'; END $$; CREATE TRIGGER reject_ikb11_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_ikb11_log()`).Error)
	_, err = h.service.canonicalRelationTypes(h.ctx, h.library, []string{"投资"})
	require.ErrorContains(t, err, "log failure")
	require.NoError(t, h.db.Model(&types.LibraryRelationType{}).Where("library_id=? AND name='投资'", h.library.ID).Count(&count).Error)
	require.Zero(t, count, "addition and audit log roll back together")
	require.Greater(t, relationCosine([]float32{1, 0}, []float32{.99, .01}), .92)
	require.Less(t, relationCosine([]float32{1, 0}, []float32{0, 1}), .92)
}

func TestIKB11OpinionsSpeakerUsesExtractedPersonOrAuthor(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	h.library.SystemRole = "opinions"
	intake, _ := k.IntakeMetadata()
	intake.SourceInfo.Author = "Author fallback"
	require.NoError(t, k.SetIntakeMetadata(intake))
	fixture := &jointProcessingChat{kind: "event"}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, "Acme 与 Beta 合作。", nil)
	require.NoError(t, err)
	candidates := []types.EntityCandidate{{Name: "Acme", EntityType: "company"}, {Name: "Beta", EntityType: "company"}, {Name: "Alice", EntityType: "person"}}
	resolved, err := h.service.resolveProcessingEntities(h.ctx, k, candidates, result)
	require.NoError(t, err)
	k.ContentType = "event"
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, "Acme 与 Beta 合作。", result, resolved))
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	speaker := out.Relations[0].Evidence[0].Speaker
	require.Equal(t, "extracted_person", speaker.Selection)
	require.NotEmpty(t, speaker.EntityID)
	result.Relations[0].Speaker = "Unknown"
	result.Relations[0].Evidence[0].Speaker = nil
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, "Acme 与 Beta 合作。", result, resolved))
	out, err = ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Equal(t, "Author fallback", out.Relations[0].Evidence[0].Speaker.Name)
	require.Equal(t, "author", out.Relations[0].Evidence[0].Speaker.Selection)
}

func TestIKB11ImagesIndexedSearchMarkedBodyUnchangedAndUnreadableSkipped(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	k := segmentedEntry(t, h)
	fixture := &jointProcessingChat{kind: "event"}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	body := "# 图片新闻\n\nAcme 与 Beta 合作。\n\n![统计](" + articleImageDataURI(t, 30) + ")\n\n![缺图](https://example.org/missing.png)\n\n![损坏图](data:image/png;base64,bm90LWltYWdl)\n"
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, 1, fixture.calls)
	require.Len(t, result.Images, 3)
	require.Contains(t, result.Images[2].SkipReason, "不可读")
	require.NotEmpty(t, result.Images[0].Description)
	require.Contains(t, result.Images[1].SkipReason, "未提供图片")
	require.NoError(t, h.service.persistArticleProcessingResult(h.ctx, h.library, k, body, nil, types.EffectiveProcessConfig{ChunkingConfig: h.library.ChunkingConfig}, result, false))
	require.Empty(t, k.VaultBody, "processing does not write descriptions into the body")
	var chunks []types.Chunk
	require.NoError(t, h.db.Where("entry_id=? AND chunk_type=?", k.ID, types.ChunkTypeImageDescription).Find(&chunks).Error)
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Content, "42%")
	require.NotEmpty(t, chunks[0].SourceLocations)
	require.Equal(t, "image_description", chunks[0].SourceLocations[0].Mapping)
	var count int64
	require.NoError(t, h.db.Table("embeddings").Where("source_id=?", chunks[0].ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Model(h.library).Update("indexing_strategy", types.IndexingStrategy{KeywordEnabled: true}).Error)
	search := &searchService{db: h.db}
	response, err := search.Search(h.ctx, types.EntrySearchRequest{Libraries: []string{h.library.ID}, Query: "SpectralChart"})
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
	require.Equal(t, "image_description", response.Results[0].Snippets[0].ChunkType)
	require.NotEmpty(t, response.Results[0].Snippets[0].Locations)
	// Image reference localization moves evidence and caption pointers, never caption text.
	next := strings.Replace(body, articleImageDataURI(t, 30), "../attachments/chart.png", 1)
	require.NoError(t, recalculateBodyLocations(h.ctx, h.db, k, body, next, nil))
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Equal(t, "../attachments/chart.png", out.Images[0].URL)
	require.NoError(t, h.db.First(&chunks[0], "id=?", chunks[0].ID).Error)
	require.Contains(t, chunks[0].Content, "42%")
	require.Contains(t, chunks[0].ImageInfo, "../attachments/chart.png")
	require.NotContains(t, next, "SpectralChart")
	require.NotEmpty(t, out.Images[0].Locations)
	require.Equal(t, out.Images[0].Locations, chunks[0].SourceLocations)
}

func TestIKB11RelationVectorThresholdAndLexicalPriority(t *testing.T) {
	vectors := map[string][]float32{"合作": {1, 0}, "任职": {0, 1}, "协作": {.99, .05}, "合作伙伴": {0, 1}, "不相关": {.7, .7}}
	require.Equal(t, "合作", matchingRelationType("协作", []string{"任职", "合作"}, vectors, .92))
	require.Empty(t, matchingRelationType("协作", []string{"任职", "合作"}, vectors, .9999))
	require.Empty(t, matchingRelationType("不相关", []string{"任职", "合作"}, vectors, .92))
	require.Equal(t, "合作", matchingRelationType("合作伙伴", []string{"任职", "合作"}, vectors, .92))
}

func TestIKB11ImageBoundedSegmentsCoverEveryReference(t *testing.T) {
	body := strings.Repeat("![chart](local://fixture.png)\n\n", 19)
	plan, err := types.PlanProcessingSegments(body, 24000, 128, 128)
	require.NoError(t, err)
	bounded, err := imageBoundedSegments(body, plan, 8, 128)
	require.NoError(t, err)
	require.Len(t, bounded, 3)
	end := 0
	total := 0
	for _, p := range bounded {
		require.Equal(t, end, p.Start)
		end = p.End
		refs := bodyImageReferences(string([]rune(body)[p.Start:p.End]))
		require.LessOrEqual(t, len(refs), 8)
		total += len(refs)
	}
	require.Equal(t, len([]rune(body)), end)
	require.Equal(t, 19, total)
}

func TestIKB11CustomContentTypeAndCorrectionBelongToLibrary(t *testing.T) {
	h, _ := newIngestHarness(t)
	row := types.LibraryContentType{LibraryID: h.library.ID, Name: "实验记录", Position: 100, Enabled: true, ExtractGraph: true}
	require.NoError(t, h.db.Create(&row).Error)
	h.processor.output = validProcessingFixture("实验记录")
	article := h.upload(t, "custom-content-type", "# 记录\n\n完整的实验步骤和观测数据。")
	require.Equal(t, "实验记录", article.ContentType)
	_, err := h.service.CorrectContentType(h.ctx, article.ID, "别的库类型")
	require.ErrorContains(t, err, "belong to this library")
	row.Enabled = false
	require.NoError(t, h.db.Save(&row).Error)
	_, err = h.service.CorrectContentType(h.ctx, article.ID, "实验记录")
	require.ErrorContains(t, err, "belong to this library")
	corrected, err := h.service.CorrectContentType(h.ctx, article.ID, "event")
	require.NoError(t, err)
	require.Equal(t, "event", corrected.ContentType)
}

type relationVectorFixture struct {
	chunkSearchEmbedder
	calls   int
	vectors map[string][]float32
	onCall  func(context.Context)
}

func (m *relationVectorFixture) BatchEmbed(ctx context.Context, names []string) ([][]float32, error) {
	m.calls++
	if m.onCall != nil {
		m.onCall(ctx)
	}
	out := [][]float32{}
	for _, name := range names {
		value, ok := m.vectors[name]
		if !ok {
			return nil, fmt.Errorf("missing fixture vector %s", name)
		}
		out = append(out, value)
	}
	return out, nil
}

type relationVectorModels struct {
	articleProcessingModels
	fixture *relationVectorFixture
}

func (m relationVectorModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.fixture, nil
}

func TestIKB11RelationVectorCanonicalizationUsesConfiguredThreshold(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: h.library.ID, Name: "合作", Enabled: true}).Error)
	fixture := &relationVectorFixture{vectors: map[string][]float32{"合作": {1, 0, 0}, "协作": {.99, .05, 0}, "投资": {0, 1, 0}, "资金投入": {0, .99, .05}}}
	h.service.modelService = relationVectorModels{articleProcessingModels: h.service.modelService.(articleProcessingModels), fixture: fixture}
	mapping, err := h.service.canonicalRelationTypes(h.ctx, h.library, []string{"合作"})
	require.NoError(t, err)
	require.Equal(t, "合作", mapping["合作"])
	require.Zero(t, fixture.calls)
	mapping, err = h.service.canonicalRelationTypes(h.ctx, h.library, []string{"协作", "投资", "资金投入"})
	require.NoError(t, err)
	require.Equal(t, "合作", mapping["协作"])
	require.Equal(t, "投资", mapping["投资"])
	require.Equal(t, "投资", mapping["资金投入"])
	require.Equal(t, 1, fixture.calls)
	h.service.config.ArticleProcessing.RelationTypeSimilarityThreshold = .9999
	mapping, err = h.service.canonicalRelationTypes(h.ctx, h.library, []string{"协作"})
	require.NoError(t, err)
	require.Equal(t, "协作", mapping["协作"])
	var count int64
	require.NoError(t, h.db.Model(&types.OperationLog{}).Where("operation_type='add_relation_type'").Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestIKB11ImageCrossingTextSegmentBoundaryStillDescribedOnce(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	h.service.config.ArticleProcessing.MaxBodyChars = 50
	k := segmentedEntry(t, h)
	fixture := &jointProcessingChat{kind: "event"}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	body := strings.Repeat("原始论据", 13) + "![chart](" + articleImageDataURI(t, 40) + ")" + strings.Repeat("实验结果", 30)
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Greater(t, fixture.calls, 1)
	require.Len(t, result.Images, 1)
	require.NotEmpty(t, result.Images[0].Description)
	require.Equal(t, 1, result.Images[0].Ordinal)
	require.NotEmpty(t, result.Images[0].Locations)
	require.Equal(t, "image_description", result.Images[0].Locations[0].Mapping)
}

func TestIKB11OtherDefaultsOffAndHonorsExplicitLibraryConfiguration(t *testing.T) {
	l := types.SystemLibraryDefaults()[0]
	require.False(t, libraryGraphEnabled(l, "其他", nil))
	for i := range l.ContentTypes {
		if l.ContentTypes[i].Name == "其他" {
			l.ContentTypes[i].ExtractGraph = true
		}
	}
	require.True(t, libraryGraphEnabled(l, "其他", nil))
	require.False(t, libraryGraphEnabled(l, "其他", entrySearchBool(false)))
}

func TestIKB11UnreadableStoredAttachmentSkipsCaptionAndCompletes(t *testing.T) {
	h, vault := newIngestHarness(t)
	k := h.upload(t, "unreadable-stored-image", "# 新闻\n\n完整的原始文字。\n\n![chart]("+articleImageDataURI(t, 50)+")")
	body := k.VaultBody
	refs := bodyImageReferences(body)
	require.Len(t, refs, 1)
	require.NoError(t, os.Remove(filepath.Join(vault.root, filepath.Dir(k.VaultPath), refs[0].URL)))
	require.NoError(t, h.service.processArticleBody(h.ctx, h.library, k, body, nil, types.EffectiveProcessConfig{ChunkingConfig: h.library.ChunkingConfig}))
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Len(t, out.Images, 1)
	require.NotEmpty(t, out.Images[0].SkipReason)
	require.Empty(t, out.Images[0].Description)
	saved := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.Equal(t, body, saved.VaultBody)
}

func TestIKB11VaultRebuildDoesNotReuseCaptionAsIdenticalBodyText(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	text := "文字与图片说明相同。"
	old := &types.Chunk{ID: "old-caption", SpaceID: k.SpaceID, EntryID: k.ID, LibraryID: k.LibraryID, Content: text, ChunkType: types.ChunkTypeImageDescription, IsEnabled: true, ContentHash: vaultContentHash(buildEntryIndexContent(k, text))}
	require.NoError(t, h.db.Create(old).Error)
	library := *h.library
	library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	documents := &VaultDocumentService{db: h.db, models: h.service.modelService, retrieval: h.service.retrieveEngine}
	require.NoError(t, documents.indexVaultChunks(h.ctx, &library, k, text))
	var chunks []types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", k.ID).Find(&chunks).Error)
	require.Len(t, chunks, 1)
	require.Equal(t, types.ChunkTypeText, chunks[0].ChunkType)
	require.NotEqual(t, old.ID, chunks[0].ID)
}
