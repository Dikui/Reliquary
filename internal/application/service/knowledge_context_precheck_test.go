package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func TestFocusPrecheckNameMatching(t *testing.T) {
	for _, test := range []struct {
		name, body, object string
		want               bool
	}{
		{"chinese_substring", "今天阿里巴巴发布新模型", "阿里巴巴", true},
		{"japanese_substring", "新しいソニー製品", "ソニー", true},
		{"korean_substring", "새로운삼성제품", "삼성", true},
		{"english_word", "Meta released a model.", "Meta", true},
		{"english_word_boundary", "metadata changed.", "Meta", false},
		{"english_left_boundary", "superMeta released a model.", "Meta", false},
		{"english_case_fold", "META released a model.", "Meta", true},
		{"fullwidth_and_hyphen", "Ａｃｍｅ－Ｌａｂ announced it.", "Acme Lab", true},
		{"halfwidth_katakana", "最新ｿﾆｰ製品", "ソニー", true},
		{"whitespace", "Acme\t  Lab announced it.", "Acme-Lab", true},
		{"short_case_match", "IBM released it.", "IBM", true},
		{"short_case_mismatch", "ibm released it.", "IBM", false},
		{"short_width_match", "ＩＢＭ released it.", "IBM", true},
		{"short_word_boundary", "IBMs released it.", "IBM", false},
		{"digit_boundary", "1234 released it.", "123", false},
		{"empty_name", "Acme released it.", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, contextFocusNameMatches(test.body, test.object))
		})
	}
}

func TestFocusPrecheckUsesAliasesConcreteLabelsAndTracking(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	entity := p3Entity("focus-object", "Aurora", "model")
	require.NoError(t, db.Create(&entity).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "focus-alias", TenantID: 1, EntityID: entity.ID, EntityType: "model", Alias: "北极星", NormalizedAlias: "北极星"}).Error)
	k := p3Message(t, db, "focus-precheck", "北极星发布了新版本。")
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, true))
	matched, err := svc.PrechecksFocus(ctx, k, "北极星发布了新版本。")
	require.NoError(t, err)
	require.True(t, matched, "all stored aliases are eligible before model extraction")
	require.NoError(t, svc.SetEntityStar(ctx, entity.ID, false))
	focusWorkflow(t, db, "track-focus", true, map[string]any{"entity_id": entity.ID})
	matched, err = svc.PrechecksFocus(ctx, k, "北极星发布了新版本。")
	require.NoError(t, err)
	require.True(t, matched, "enabled tracking includes canonical names and aliases")
	matched, err = svc.PrechecksFocus(ctx, k, "An unnamed company released it.")
	require.NoError(t, err)
	require.False(t, matched)
	topic := focusTopic(t, db, "New releases", types.TagKindTopic)
	require.NoError(t, svc.SetTopicStar(ctx, topic.ID, true))
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	intake.Classification = []types.KnowledgeIntakeTag{{Kind: topic.Kind, Name: topic.Name}}
	require.NoError(t, k.SetIntakeMetadata(intake))
	matched, err = svc.PrechecksFocus(ctx, k, "An unnamed company released it.")
	require.NoError(t, err)
	require.True(t, matched, "accepted concrete labels match without an object name")
	intake.Classification[0].Name = "New release"
	require.NoError(t, k.SetIntakeMetadata(intake))
	matched, err = svc.PrechecksFocus(ctx, k, "An unnamed company released it.")
	require.NoError(t, err)
	require.False(t, matched, "labels require the same concrete topic identity")
}

func TestFocusPrecheckExtractionPathsAndPromptRequests(t *testing.T) {
	const indirectBody = "This company has license MIT and 10 staff. This company released a product on 2026-10-07."
	const indirectOutput = `{"entities":[{"mention":"This company","canonical_name":"Acme","entity_type":"company","quote":"This company has license MIT and 10 staff."}],"facts":[{"kind":"attribute","subject":"This company","predicate":"license","value":"MIT","value_type":"text","quote":"This company has license MIT and 10 staff."},{"kind":"attribute","subject":"This company","predicate":"staff","value":10,"value_type":"number","quote":"This company has license MIT and 10 staff."}],"events":[{"title":"Product release","event_type":"release","description":"This company released a product","occurred_at":"2026-10-07","entity_mentions":["This company"],"quote":"This company released a product on 2026-10-07."}]}`
	for _, test := range []struct {
		name, body, path, mode string
		star, falsePositive    bool
		prompts                [][]string
		facts, events          int64
	}{
		{"hit", onDemandBody, "precheck_hit", types.KnowledgeExtractionFull, true, false, [][]string{{"entities", "events", "facts"}}, 3, 1},
		{"hit_resolves_outside", onDemandBody, "precheck_hit", types.KnowledgeExtractionTemplateOnly, true, true, [][]string{{"entities", "events", "facts"}}, 2, 0},
		{"miss_resolves_inside", indirectBody, "precheck_miss_supplemented", types.KnowledgeExtractionFull, true, false, [][]string{{"entities", "facts"}, {"events", "facts"}}, 2, 1},
		{"miss_resolves_outside", onDemandBody, "precheck_miss", types.KnowledgeExtractionTemplateOnly, false, false, [][]string{{"entities", "facts"}}, 2, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, ctx, svc, _ := onDemandFixture(t)
			if test.star {
				require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
			}
			var reply contextExtraction
			output := onDemandOutput
			if test.body == indirectBody {
				output = indirectOutput
			}
			require.NoError(t, json.Unmarshal([]byte(output), &reply))
			if test.falsePositive {
				reply.Entities[0].Canonical, reply.Entities[0].EntityType = "Acme Product", "model"
			}
			var prompts [][]string
			model := &p3SegmentChat{reply: func(callCtx context.Context, messages []chat.Message, _ *chat.ChatOptions) (string, error) {
				stage, _ := types.LLMCallMetadataFromContext(callCtx)
				if stage == "context_entity_judgment" {
					if test.falsePositive {
						return `{"decision":"new","confidence":1}`, nil
					}
					return `{"decision":"match","entity_id":"acme","confidence":1}`, nil
				}
				if stage == "context_event_judgment" {
					return `{"relation":"unrelated","confidence":1}`, nil
				}
				require.Contains(t, []string{"context_extraction", "context_full_extraction"}, stage)
				schema := onDemandPromptSchema(t, messages[0].Content)
				keys := []string{}
				for _, key := range []string{"entities", "events", "facts"} {
					if _, exists := schema[key]; exists {
						keys = append(keys, key)
					}
				}
				prompts = append(prompts, keys)
				if stage == "context_full_extraction" {
					require.Contains(t, messages[1].Content, `"verified_entities"`)
					require.Contains(t, messages[1].Content, `"canonical_name":"Acme"`)
				}
				encoded, err := json.Marshal(reply)
				return string(encoded), err
			}}
			svc.models = knowledgeContextTestModelService{model: model}
			k := p3Message(t, db, "precheck-source", test.body)
			require.NoError(t, svc.ProcessMessage(ctx, k, test.body))
			require.Equal(t, test.prompts, prompts, "each request must use the required prompt shape")
			evidence := onDemandEvidence(t, db, k.ID)
			require.Equal(t, test.mode, evidence.ExtractionMode)
			require.Equal(t, test.path, evidence.ExtractionPath)
			var facts, events int64
			require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("evidence_id = ?", evidence.ID).Count(&facts).Error)
			require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
			require.Equal(t, test.facts, facts)
			require.Equal(t, test.events, events)
			if test.name == "miss_resolves_inside" {
				prompts = nil
				next := p3Message(t, db, "saved-alias-source", test.body)
				require.NoError(t, svc.ProcessMessage(ctx, next, test.body))
				require.Equal(t, [][]string{{"entities", "events", "facts"}}, prompts, "the saved indirect spelling becomes a precheck alias on the next message")
				require.Equal(t, "precheck_hit", onDemandEvidence(t, db, next.ID).ExtractionPath)
			}
		})
	}
}

func TestSupplementAcceptsStoredCanonicalNamesAndAliases(t *testing.T) {
	quote := "北极星 released a model on 2026-10-07."
	verified := []contextExtractedEntity{{Mention: "Aurora", Canonical: "Aurora", Aliases: []string{"北极星", "Northern Star"}}}
	model := &p3SegmentChat{reply: func(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (string, error) {
		require.Contains(t, messages[1].Content, `"canonical_name":"Aurora"`)
		require.Contains(t, messages[1].Content, "北极星")
		require.Contains(t, messages[1].Content, "Northern Star")
		require.Contains(t, messages[1].Content, `"allowed_mentions":["Aurora","北极星","Northern Star"]`)
		return `{"facts":[],"events":[{"title":"Model release","event_type":"release","entity_mentions":["北极星"],"occurred_at":"2026-10-07","quote":"北极星 released a model on 2026-10-07."}]}`, nil
	}}
	result, _, err := extractContextSupplementSegmentsWithPlan(context.Background(), model, &types.KnowledgeEvidence{Body: quote}, BuiltinAIKnowledgeDomain(),
		contextSegmentPlan{Segments: []string{quote}, Total: 1, OutputLimit: 512, Coverage: "complete"}, verified)
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, []string{"北极星"}, result.Events[0].EntityMentions)
}

func TestSupplementIntakeReferencesStayOnVerifiedMentions(t *testing.T) {
	entity := contextExtractedEntity{Mention: "Acme Inc", Canonical: "Acme", Aliases: []string{"A Company"}, Quote: "Acme Inc is Acme and A Company."}
	require.Equal(t, []string{"Acme Inc"}, contextSupplementMentions(entity))
	extracted := contextExtraction{Facts: []contextExtractedFact{{Kind: "attribute", Subject: "Acme", Predicate: "staff", Value: json.RawMessage("10"), ValueType: "number", Quote: "Acme has 10 staff."}}, Attention: contextExtractedAttention{Reason: "Already assessed."}}
	require.ErrorContains(t, validateContextExtraction(extracted, "Acme has 10 staff.", []contextExtractedEntity{entity}), "quoted subject")
}
