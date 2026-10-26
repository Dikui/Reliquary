package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fixture chooses the library type first and obeys switches in the prompt.
type jointProcessingChat struct {
	retainedProcessingChat
	kind            string
	calls           int
	describeMissing bool
}

// These fixtures exercise the protocol and pipeline, not real-model accuracy.

func TestArticleEvidenceRejectsUnqualifiedObjectsAndKeepsOriginalMarkdown(t *testing.T) {
	body := "# 公告\r\n\r\n[Acme](https://example.org/acme) 与 **Beta** 合作。\r\nAcme 的产品是 Suite。\r\n"
	numbered, blocks := articleEvidenceBody(body)
	require.Contains(t, numbered, "[P2]\n[Acme](https://example.org/acme)")
	company := "company"
	product := "product"
	person, project, other := "person", "project", "other"
	result, err := parseArticleProcessingResult(validProcessingFixture())
	require.NoError(t, err)
	result.Entities = []articleProcessingEntity{
		{Name: "Acme", Type: &company, Description: "Beta 的合作方。", Relevance: "central", EvidenceIDs: []int{2}},
		{Name: "Beta", Type: &company, Description: "Acme 的合作方。", Relevance: "participant", EvidenceIDs: []int{2}},
		{Name: "Suite", Type: &product, Relevance: "participant", EvidenceIDs: []int{2}},
		{Name: "Footer", Type: &company, Description: "导航项。", Relevance: "incidental", EvidenceIDs: []int{1}},
		{Name: "Uncited", Type: &company, Description: "没有原文支持。", Relevance: "central", EvidenceIDs: []int{999}},
		{Name: "Alice", Type: &person, Description: "发言人。", Relevance: "participant", EvidenceIDs: []int{2}},
		{Name: "Protocol", Type: &project, Description: "协议。", Relevance: "central", EvidenceIDs: []int{2}},
		{Name: "Concept", Type: &other, Description: "概念。", Relevance: "central", EvidenceIDs: []int{2}},
		{Name: "Byline", Type: &company, Description: "署名。", Relevance: "attribution", EvidenceIDs: []int{2}},
	}
	library := &types.Library{}
	qualifySegmentOutput(library, &types.Entry{}, body, types.ProcessingSegment{End: len([]rune(body))}, result)
	require.Empty(t, result.Entities)
	require.NotEmpty(t, blocks)
}

func TestEntityMentionsRejectUnsupportedBindingsAndKeepNewNames(t *testing.T) {
	result, err := parseArticleProcessingResult(`{"title":"DeepSeek 文档","summary":{"overview":"介绍文档解析。"},"entity_ids":["anthropic"],"entity_mentions":[
{"name":"DeepSeek","type":"organization","entity_id":"anthropic","quote":"DeepSeek 发布文档解析模型。"},
{"name":"Anthropic","type":"organization","entity_id":"anthropic","quote":"DeepSeek 发布文档解析模型。"},
{"name":"虚构机构","quote":"虚构机构发布模型。"},
{"name":"Claude Code","quote":"Claude Code handles local work."}]}`)
	require.NoError(t, err)
	qualifyUnderstandingEntityMentions(result, "DeepSeek 发布文档解析模型。Claude Code handles local work.")
	options := articleProcessingInput{EntityOptions: []understandingEntityOption{
		{ID: "anthropic", CanonicalName: "Anthropic"},
		{ID: "first-name", CanonicalName: "DeepSeek"},
		{ID: "second-name", CanonicalName: "DeepSeek"},
	}}
	qualifyUnderstandingChoices(result, options)
	require.Empty(t, result.EntityIDs, "an unrelated but valid ID cannot force a binding")
	require.Len(t, result.EntityMentions, 2)
	require.Equal(t, "DeepSeek", result.EntityMentions[0].Name)
	require.Empty(t, result.EntityMentions[0].EntityID)

	merged := &articleProcessingResult{EntityMentions: append(append([]understandingEntityMention{}, result.EntityMentions...), understandingEntityMention{Name: "Anthropic", Quote: "invented"})}
	qualifyMergedEntityMentions(merged, []*articleProcessingResult{result})
	require.Equal(t, result.EntityMentions, merged.EntityMentions, "long-document merge keeps only verified segment evidence")
}

func TestEntityReferencesPreserveSourceAndNormalizeModelSeries(t *testing.T) {
	body := "# Models\n\nGPT‑5.6 Sol and GPT-6 are available.\n\nClaude Code runs tools.\n\nOpus 5.5 is the model used in this example.\n\n[Model Context Protocol (MCP)](https://example.org/mcp) connects agents.\n\nEngram uses conditional\n    memory and lookup.\n"
	_, blocks := articleEvidenceBody(body)
	paragraph := func(text string) string {
		for i, block := range blocks {
			if strings.Contains(string([]rune(body)[block.Start:block.End]), text) {
				return fmt.Sprintf("P%d", i+1)
			}
		}
		t.Fatalf("missing evidence paragraph: %s", text)
		return ""
	}
	first := &articleProcessingResult{EntityMentions: []understandingEntityMention{
		{Name: "GPT‑5.6 Sol", SourceID: paragraph("GPT‑5.6")},
		{Name: "GPT-6", SourceID: paragraph("GPT‑5.6")},
		{Name: "Claude", SurfaceName: "Opus 5.5", SourceID: paragraph("Opus 5.5")},
		{Name: "Claude", SurfaceName: "Claude Code", SourceID: paragraph("runs tools")},
	}}
	require.NoError(t, qualifyUnderstandingEntityMentions(first, body))
	require.Equal(t, []string{"GPT", "Claude", "Claude Code"}, []string{first.EntityMentions[0].Name, first.EntityMentions[1].Name, first.EntityMentions[2].Name})
	require.Equal(t, "GPT‑5.6 Sol", first.EntityMentions[0].SurfaceName)
	second := &articleProcessingResult{EntityMentions: []understandingEntityMention{
		{Name: "MCP", SourceID: paragraph("Model Context Protocol")},
		{Name: "Engram", SourceID: paragraph("Engram"), Quote: "Engram uses conditional memory and lookup."},
	}}
	require.NoError(t, qualifyUnderstandingEntityMentions(second, body))
	require.Equal(t, "[Model Context Protocol (MCP)](https://example.org/mcp) connects agents.", second.EntityMentions[0].Quote)
	require.Equal(t, "Engram uses conditional\n    memory and lookup.", second.EntityMentions[1].Quote)
	merged := &articleProcessingResult{EntityMentions: []understandingEntityMention{
		{SourceID: "S2E1"}, {SourceID: "S1E1"}, {SourceID: "S1E1"}, {SourceID: "S1E3"},
	}}
	require.NoError(t, qualifyMergedEntityMentions(merged, []*articleProcessingResult{first, second}))
	require.Equal(t, []understandingEntityMention{second.EntityMentions[0], first.EntityMentions[0], first.EntityMentions[2]}, merged.EntityMentions)
}

func TestEntityReferencesRejectMissingSourceAndInventedNames(t *testing.T) {
	body := "# Source\n\nClaude Code runs tools.\n"
	result := &articleProcessingResult{EntityMentions: []understandingEntityMention{
		{Name: "Claude Code", SourceID: "P99"},
		{Name: "Invented product", SourceID: "P2"},
		{Name: "Claude Code", Quote: "Claude Code has invented capabilities."},
	}}
	require.Error(t, qualifyUnderstandingEntityMentions(result, body))
	require.Empty(t, result.EntityMentions)
	merged := &articleProcessingResult{EntityMentions: []understandingEntityMention{{SourceID: "S9E9"}}}
	require.Error(t, qualifyMergedEntityMentions(merged, []*articleProcessingResult{result}))
	require.Empty(t, merged.EntityMentions)
}

func (m *jointProcessingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "intake_review" {
		return &types.ChatResponse{Content: `{"approved":true,"reason":"fixture"}`}, nil
	}
	if stage == "article_summary_merge" {
		return &types.ChatResponse{Content: validProcessingFixture()}, nil
	}
	if stage != "entry_understanding" {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":1}`}, nil
	}
	m.calls++
	var input articleProcessingInput
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	output, _ := parseArticleProcessingResult(validProcessingFixture())
	company := "company"
	person := "person"
	output.Entities = []articleProcessingEntity{{Name: "Acme", Type: &company, Description: "A partner of Beta.", Relevance: "central", EvidenceIDs: []int{1}}, {Name: "Beta", Type: &company, Description: "A partner of Acme.", Relevance: "participant", EvidenceIDs: []int{1}}, {Name: "Alice", Type: &person, Description: "The speaker.", Relevance: "attribution", EvidenceIDs: []int{1}}}
	data, err := json.Marshal(output)
	if err != nil || !m.describeMissing {
		return &types.ChatResponse{Content: string(data)}, err
	}
	var unsolicited map[string]any
	if err := json.Unmarshal(data, &unsolicited); err != nil {
		return nil, err
	}
	unsolicited["image_descriptions"] = "unrequested output is ignored without caption validation"
	data, err = json.Marshal(unsolicited)
	return &types.ChatResponse{Content: string(data)}, err
}

func TestIKB11ImagesRetainReferencesWithoutCaptionsOrDescriptionChunks(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	k := segmentedEntry(t, h)
	fixture := &jointProcessingChat{kind: "event", describeMissing: true}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	body := "# 图片新闻\n\nAcme 与 Beta 合作。\n\n![缺图](https://example.org/missing.png)\n\n![统计](" + articleImageDataURI(t, 30) + ")\n\n![损坏图](data:image/png;base64,bm90LWltYWdl)\n"
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, 1, fixture.calls)
	require.Len(t, result.Images, 3)
	require.Contains(t, result.Images[2].SkipReason, "不可读")
	require.Empty(t, result.Images[1].Description)
	require.Contains(t, result.Images[0].SkipReason, "未提供图片")
	require.Empty(t, result.Images[0].Description, "a missing image cannot have model-generated evidence")
	require.NoError(t, h.service.persistArticleProcessingResult(h.ctx, h.library, k, body, nil, types.EffectiveProcessConfig{ChunkingConfig: h.library.ChunkingConfig}, result, false))
	require.Empty(t, k.VaultBody, "processing does not write descriptions into the body")
	var chunks []types.Chunk
	require.NoError(t, h.db.Where("entry_id=? AND chunk_type=?", k.ID, types.ChunkTypeImageDescription).Find(&chunks).Error)
	require.Empty(t, chunks, "unsolicited image descriptions never create retrieval chunks")
	// Image reference localization preserves the original image locations.
	next := strings.Replace(body, articleImageDataURI(t, 30), "../attachments/chart.png", 1)
	require.NoError(t, recalculateBodyLocations(h.ctx, h.db, k, body, next, nil))
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Equal(t, "../attachments/chart.png", out.Images[1].URL)
	require.Empty(t, out.Images[1].Description)
	require.NotEmpty(t, out.Images[1].Locations)
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

func TestIKB11ImageCrossingTextSegmentBoundaryStillAttachedOnce(t *testing.T) {
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
	require.Empty(t, result.Images[0].Description)
	require.Equal(t, 1, result.Images[0].Ordinal)
	require.NotEmpty(t, result.Images[0].Locations)
	require.Equal(t, "image_description", result.Images[0].Locations[0].Mapping)
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
