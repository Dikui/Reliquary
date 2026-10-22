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
	"reliquary/internal/types"
)

// The fixture chooses the library type first and obeys switches in the prompt.
type jointProcessingChat struct {
	retainedProcessingChat
	kind            string
	malicious       bool
	calls           int
	describeMissing bool
}

// These fixtures exercise the protocol and pipeline, not real-model accuracy.
func supportedGraphVerificationFixture(messages []chat.Message) (*types.ChatResponse, error) {
	var input graphVerificationInput
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	verdicts := []graphVerificationVerdict{}
	for _, candidate := range input.Candidates {
		verdict := graphVerificationVerdict{ID: candidate.ID, Status: "supported", Reason: "Fixture evidence"}
		if candidate.Entity != nil {
			verdict.DescriptionStatus = "supported"
		}
		verdicts = append(verdicts, verdict)
	}
	raw, err := json.Marshal(map[string]any{"verdicts": verdicts})
	return &types.ChatResponse{Content: string(raw)}, err
}

type graphVerificationChat struct {
	retainedProcessingChat
	respond func([]chat.Message) (*types.ChatResponse, error)
}

func (m graphVerificationChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	return m.respond(messages)
}

func TestGraphVerificationRetainsSupportedFactsAndClearsUnsupportedDescription(t *testing.T) {
	require.Equal(t, types.CallPurposeArticleProcessing, types.NormalizeModelCallPurpose("article_graph_verification", types.ModelTierGeneration))
	company, product := "company", "product"
	result := &articleProcessingResult{
		Entities: []articleProcessingEntity{
			{Name: "Playco", Type: &company, Description: "游戏公司客户", EvidenceIDs: []int{1}},
			{Name: "OpenAI", Type: &company, Description: "合作方", EvidenceIDs: []int{1}},
			{Name: "Model family", Type: &product, Description: "将型号提升为家族", EvidenceIDs: []int{1}},
			{Name: "Missing", Type: &company, EvidenceIDs: []int{1}},
		},
		Relations: []extractedRelation{
			{Subject: "Playco", Type: "合作", Object: "OpenAI", EvidenceIDs: []int{1}},
			{Subject: "OpenAI", Type: "发布", Object: "Model family", EvidenceIDs: []int{1}},
			{Subject: "Playco", Type: "合作", Object: "Missing", EvidenceIDs: []int{1}},
		},
	}
	source := articleProcessingInput{Body: "[P1]\nPlayco 与 OpenAI 合作开发游戏原型。", Author: "Reporter"}
	images := []string{"data:image/png;base64,fixture"}
	seen := []string{}
	model := &graphVerificationChat{respond: func(messages []chat.Message) (*types.ChatResponse, error) {
		var input graphVerificationInput
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		require.Equal(t, source.Body, input.Source.Body)
		require.Equal(t, source.Author, input.Source.Author)
		require.Equal(t, images, messages[1].Images)
		response, err := supportedGraphVerificationFixture(messages)
		require.NoError(t, err)
		var output struct {
			Verdicts []graphVerificationVerdict `json:"verdicts"`
		}
		require.NoError(t, json.Unmarshal([]byte(response.Content), &output))
		for i := range output.Verdicts {
			v := &output.Verdicts[i]
			seen = append(seen, v.ID)
			switch v.ID {
			case "e0":
				v.DescriptionStatus, v.Reason = "unsupported", "原文未说明行业身份"
			case "e2", "r1":
				v.Status, v.Reason = "unsupported", "具体型号不能提升为家族或发布关系"
			case "e3":
				v.Status, v.Reason = "insufficient_evidence", "缺少必要上下文"
			}
		}
		raw, err := json.Marshal(output)
		return &types.ChatResponse{Content: string(raw)}, err
	}}
	require.NoError(t, verifyArticleGraph(context.Background(), model, &types.Model{}, source, images, 416, result))
	require.Equal(t, []string{"e0", "e1", "e2", "e3", "r0", "r1", "r2"}, seen, "batching must cover every candidate")
	require.Len(t, result.Entities, 2)
	require.Empty(t, result.Entities[0].Description)
	require.Equal(t, "合作方", result.Entities[1].Description)
	require.Equal(t, []int{1}, result.Entities[0].EvidenceIDs)
	require.Len(t, result.Relations, 1, "a relation cannot retain a rejected endpoint")
	require.Equal(t, "合作", result.Relations[0].Type)
	audit := string(result.Extensions["graph_verification"][0].Value)
	require.Contains(t, audit, "游戏公司客户", "audit keeps the rejected original description")
	require.Contains(t, audit, "insufficient_evidence")
	require.Contains(t, audit, "原文未说明行业身份")
}

func TestGraphVerificationMissingVerdictFailsWithoutPublishingPartialResults(t *testing.T) {
	company := "company"
	result := &articleProcessingResult{Entities: []articleProcessingEntity{
		{Name: "Acme", Type: &company, Description: "Original description"},
		{Name: "Beta", Type: &company, Description: "Another description"},
	}}
	before, err := json.Marshal(result)
	require.NoError(t, err)
	calls := 0
	model := &graphVerificationChat{respond: func(messages []chat.Message) (*types.ChatResponse, error) {
		calls++
		if calls == 1 {
			return &types.ChatResponse{Content: `{"verdicts":[{"id":"e0","status":"supported","description_status":"unsupported","reason":"unsupported description"}]}`}, nil
		}
		if calls == 3 {
			require.Contains(t, messages[0].Content, "must return exactly 1 verdicts")
		}
		return &types.ChatResponse{Content: `{"verdicts":[]}`}, nil
	}}
	err = verifyArticleGraph(context.Background(), model, &types.Model{}, articleProcessingInput{}, nil, 224, result)
	require.ErrorContains(t, err, "graph verification call failed")
	require.Equal(t, 3, calls, "one accepted batch, then one correction for the missing verdict")
	after, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "failed verification must not apply even an earlier valid batch")
}

func TestArticleEvidenceRejectsUnqualifiedObjectsAndKeepsOriginalMarkdown(t *testing.T) {
	body := "# 公告\r\n\r\n[Acme](https://example.org/acme) 与 **Beta** 合作。\r\nAcme 的产品是 Suite。\r\n"
	numbered, blocks := articleEvidenceBody(body)
	require.Contains(t, numbered, "[P2]\n[Acme](https://example.org/acme)")
	company := "company"
	product := "product"
	person, project, other := "person", "project", "other"
	result, err := parseArticleProcessingResult(validProcessingFixture("公告"))
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
	result.Relations = []extractedRelation{
		{Subject: "Acme", Object: "Beta", Type: "合作", EvidenceIDs: []int{2, 2}},
		{Subject: "Acme", Object: "Beta", Type: "投资", EvidenceIDs: []int{999}},
		{Subject: "Acme", Object: "Footer", Type: "合作", EvidenceIDs: []int{2}},
		{Subject: "Suite", Object: "Acme", Type: "隶属", EvidenceIDs: []int{2}},
		{Subject: "Acme", Object: "Suite", Type: "隶属", EvidenceIDs: []int{2}},
	}
	library := &types.Library{ContentTypes: []types.LibraryContentType{{Name: "公告", Enabled: true, ExtractGraph: true}}}
	qualifySegmentOutput(library, &types.Entry{}, body, types.ProcessingSegment{End: len([]rune(body))}, result)
	require.Len(t, result.Entities, 3)
	require.Empty(t, result.Entities[2].Description, "a supported identity does not require a description")
	require.Len(t, result.Relations, 2)
	require.Equal(t, "Suite", result.Relations[1].Subject)
	require.Equal(t, "Acme", result.Relations[1].Object)
	require.Equal(t, blocks[1:2], result.Relations[0].Evidence[0].Locations)
	require.Equal(t, blocks[1:2], result.Entities[0].Evidence)
	location := result.Relations[0].Evidence[0].Locations[0]
	require.Equal(t, "[Acme](https://example.org/acme) 与 **Beta** 合作。\r\nAcme 的产品是 Suite。\r\n", string([]rune(body)[location.Start:location.End]))
	text := "Acme announced a product."
	require.Nil(t, processingRelationSpeaker("Reporter", "Ann", text, []types.SourceLocation{{End: len([]rune(text))}}), "a word substring is not a speaker name")
}

func (m *jointProcessingChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_graph_verification" {
		return supportedGraphVerificationFixture(messages)
	}
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
	output.Entities = []articleProcessingEntity{{Name: "Acme", Type: &company, Description: "A partner of Beta.", Relevance: "central", EvidenceIDs: []int{1}}, {Name: "Beta", Type: &company, Description: "A partner of Acme.", Relevance: "participant", EvidenceIDs: []int{1}}, {Name: "Alice", Type: &person, Description: "The speaker.", Relevance: "attribution", EvidenceIDs: []int{1}}}
	output.Relations = []extractedRelation{}
	if m.malicious || libraryGraphEnabled(l, libraryContentType(l, m.kind), override) {
		output.Relations = append(output.Relations, extractedRelation{Subject: "Acme", Object: "Beta", Type: "合作", EvidenceIDs: []int{1}, Speaker: "Alice"})
	}
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

func TestIKB11ProcessingPromptsAndSwitches(t *testing.T) {
	h := newArticleProcessingHarness(t, true)
	body := "Alice 说：Acme 与 Beta 合作。"
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
			require.Contains(t, prompt, "输入 title 为空时")
			require.Contains(t, prompt, "topics 只能从输入 existing_topics 中选择")
			require.Contains(t, prompt, "不输出逐图说明")
			require.NotContains(t, prompt, "image_descriptions")
			{
				require.Contains(t, prompt, "每个实体输出")
				require.Contains(t, prompt, "不使用固定名单")
				require.Contains(t, prompt, "不设数量上限")
				require.Contains(t, prompt, "先确定正文的主旨与重点事实")
				require.Contains(t, prompt, "产品同样按本文重点筛选")
				require.NotContains(t, prompt, "不按主次")
				require.NotContains(t, prompt, "产品不分主次")
				require.Contains(t, prompt, "人物暂不建档")
			}
			require.Equal(t, override == nil || *override, libraryGraphEnabled(defaults, "其他", override))
			for _, row := range defaults.ContentTypes {
				t.Run(name+"/"+row.Name, func(t *testing.T) {
					library := *defaults
					library.ModelTiers = h.library.ModelTiers
					fixture.kind = row.Name
					fixture.malicious = false
					result, err := h.service.callArticleProcessingPart(h.ctx, &library, k, body, nil, nil)
					require.NoError(t, err)
					wanted := true
					if override != nil {
						wanted = *override
					}
					require.Equal(t, wanted, len(result.Relations) > 0)
					qualifySegmentOutput(&library, k, body, types.ProcessingSegment{End: len([]rune(body))}, result)
					require.Equal(t, wanted, len(result.Relations) > 0)
					fixture.malicious = true
					result, err = h.service.callArticleProcessingPart(h.ctx, &library, k, body, nil, nil)
					require.NoError(t, err)
					qualifySegmentOutput(&library, k, body, types.ProcessingSegment{End: len([]rune(body))}, result)
					require.Equal(t, wanted, len(result.Relations) > 0, "server rejects relations despite a disobedient model")
					require.NotEmpty(t, result.Entities)
				})
			}
		}
	}
	require.Equal(t, "其他", libraryContentType(types.SystemLibraryDefaults()[0], "unknown"))
}

func TestIKB11SegmentRelationsResolveGlobalIDsAndKeepAllEvidence(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	kind := "organization"
	ct := "event"
	body := "# 新闻\n\nAcme 与 Beta 合作。\n\nAcme 与 Beta 再次合作。"
	parts := []*articleProcessingResult{}
	for _, p := range []types.ProcessingSegment{{Number: 1, Start: 0, End: 22}, {Number: 2, Start: 22, End: len([]rune(body))}} {
		part, _ := parseArticleProcessingResult(validProcessingFixture(ct))
		evidenceID := 1
		if p.Number == 1 {
			evidenceID = 2
		}
		part.Entities = []articleProcessingEntity{{Name: "Acme", Type: &kind, Description: "A partner of Beta.", Relevance: "central", EvidenceIDs: []int{evidenceID}}, {Name: "Beta", Type: &kind, Description: "A partner of Acme.", Relevance: "participant", EvidenceIDs: []int{evidenceID}}}
		part.Relations = []extractedRelation{{Subject: "Acme", Object: "Beta", Type: "合作", EvidenceIDs: []int{evidenceID}}}
		qualifySegmentOutput(h.library, k, body, p, part)
		parts = append(parts, part)
	}
	result := mergeArticleSegments(parts)
	require.Len(t, result.Relations, 1)
	require.Len(t, result.Relations[0].Evidence, 2)
	stored := entityRecord("acme-existing", "Acme", "company")
	require.NoError(t, h.db.Create(&stored).Error)
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = &entityCountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		var payload struct {
			Mention string `json:"mention"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		if payload.Mention == "Acme" {
			return `{"decision":"match","entity_id":"acme-existing","confidence":1}`
		}
		return `{"decision":"new","confidence":1}`
	}}
	candidates := []types.EntityCandidate{{Name: "Acme", EntityType: kind}, {Name: "Beta", EntityType: kind}}
	resolved, err := h.service.resolveProcessingEntities(h.ctx, k, candidates, result)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, result.Provenance.ResolvedEntities[stored.ID])
	k.ContentType = ct
	embeddingCalls := 0
	models.embeddingCalls = &embeddingCalls
	h.service.modelService = models
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, resolved))
	require.Zero(t, embeddingCalls, "relation types never use vector similarity")
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Len(t, out.Relations, 1)
	require.Len(t, out.Relations[0].Evidence, 2)
	require.NotEqual(t, "Acme", out.Relations[0].SubjectID)
	var subject types.Entity
	require.NoError(t, h.db.First(&subject, "id=?", out.Relations[0].SubjectID).Error)
	require.Equal(t, "Acme", subject.CanonicalName)
	require.Equal(t, "company", subject.EntityType)
	require.Equal(t, stored.ID, out.Relations[0].SubjectID)
	require.Equal(t, 22, out.Relations[0].Evidence[1].Locations[0].Start)
	// Final majority type and current graph override are validated again before saving.
	k.Graph = entrySearchBool(false)
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, resolved))
	out, err = ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Empty(t, out.Relations)
}

func TestIKB11RelationTypesKeepExactMeaningAdditionAndLog(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.modelService = nil
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: h.library.ID, Name: "合作", Position: 0, Enabled: true}).Error)
	mapping, err := h.service.canonicalRelationTypes(h.ctx, h.library, []string{"合作", "合作伙伴", " 联合研发 ", "联合研发项目", "被监管", "监管"})
	require.NoError(t, err)
	require.Equal(t, "合作", mapping["合作"])
	require.Equal(t, "合作伙伴", mapping["合作伙伴"])
	require.Equal(t, "联合研发", mapping[" 联合研发 "])
	require.Equal(t, "联合研发项目", mapping["联合研发项目"])
	require.Equal(t, "被监管", mapping["被监管"])
	require.Equal(t, "监管", mapping["监管"])
	var count int64
	require.NoError(t, h.db.Model(&types.LibraryRelationType{}).Where("library_id=?", h.library.ID).Count(&count).Error)
	require.EqualValues(t, 6, count)
	require.NoError(t, h.db.Model(&types.OperationLog{}).Where("operation_type='add_relation_type'").Count(&count).Error)
	require.EqualValues(t, 5, count)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_ikb11_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'log failure'; END $$; CREATE TRIGGER reject_ikb11_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_ikb11_log()`).Error)
	_, err = h.service.canonicalRelationTypes(h.ctx, h.library, []string{"投资"})
	require.ErrorContains(t, err, "log failure")
	require.NoError(t, h.db.Model(&types.LibraryRelationType{}).Where("library_id=? AND name='投资'", h.library.ID).Count(&count).Error)
	require.Zero(t, count, "addition and audit log roll back together")
}

func TestIKB11OpinionsKeepSpeakerAndRejectStaleExtractionScope(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	k := segmentedEntry(t, h)
	h.library.SystemRole = "media"
	intake, _ := k.IntakeMetadata()
	intake.SourceInfo.Author = "Author fallback"
	require.NoError(t, k.SetIntakeMetadata(intake))
	fixture := &jointProcessingChat{kind: "event"}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = fixture
	body := "Alice 说：Acme 与 Beta 合作。"
	result, err := h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	// Simulate a cached result produced before the extraction policy changed.
	person := "person"
	result.Entities = append(result.Entities, articleProcessingEntity{Name: "Alice", Type: &person, Description: "The speaker.", Relevance: "attribution"})
	result.Entities[0].Aliases = []string{"Unverified alias"}
	require.NoError(t, h.service.persistArticleProcessingResult(h.ctx, h.library, k, body, nil, types.EffectiveProcessConfig{ChunkingConfig: h.library.ChunkingConfig}, result, false))
	var count int64
	require.NoError(t, h.db.Model(&types.Entity{}).Where("entity_type='person'").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Model(&types.EntityAlias{}).Where("alias='Unverified alias'").Count(&count).Error)
	require.Zero(t, count)
	out, err := ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	speaker := out.Relations[0].Evidence[0].Speaker
	require.Equal(t, "extracted_person", speaker.Selection)
	require.Equal(t, "Alice", speaker.Name)
	require.Empty(t, speaker.EntityID)
	result.Relations[0].Speaker = ""
	result.Relations[0].Evidence[0].Speaker = nil
	require.NoError(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, nil))
	out, err = ReadEntryProcessingOutput(h.ctx, h.db, k.SpaceID, k.ID)
	require.NoError(t, err)
	require.Equal(t, "Author fallback", out.Relations[0].Evidence[0].Speaker.Name)
	require.Equal(t, "author", out.Relations[0].Evidence[0].Speaker.Selection)
	result.Relations[0].Speaker = "Unknown"
	result.Relations[0].Evidence[0].Speaker = nil
	require.ErrorContains(t, h.service.saveProcessingOutput(h.ctx, h.library, k, body, result, nil), "no identifiable speaker")
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

func TestIKB11RelationNamesRequireExactIdentity(t *testing.T) {
	require.Equal(t, "合作", matchingRelationType(" 合作 ", []string{"任职", "合作"}))
	require.Empty(t, matchingRelationType("协作", []string{"任职", "合作"}))
	require.Empty(t, matchingRelationType("合作伙伴", []string{"任职", "合作"}))
	require.Empty(t, matchingRelationType("被监管", []string{"监管"}))
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

func TestIKB11GraphDefaultsOnAndHonorsExplicitOverride(t *testing.T) {
	l := types.SystemLibraryDefaults()[0]
	require.True(t, libraryGraphEnabled(l, "其他", nil))
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
