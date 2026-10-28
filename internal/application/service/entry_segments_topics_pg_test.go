package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type scopedSegmentChat struct {
	segmentedChat
	inputs      []articleProcessingInput
	topicIDs    []string
	beforeMerge func()
}

func (m *scopedSegmentChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_summary_merge" && m.beforeMerge != nil {
		m.beforeMerge()
		m.beforeMerge = nil
	}
	response, err := m.segmentedChat.Chat(ctx, messages, opts)
	if err != nil || stage != "article_processing" {
		return response, err
	}
	var input articleProcessingInput
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	m.inputs = append(m.inputs, input)
	result, err := parseArticleProcessingResult(response.Content)
	if err != nil {
		return nil, err
	}
	// Deliberately include an unavailable ID to exercise output filtering too.
	result.Topics = m.topicIDs
	raw, err := json.Marshal(result)
	response.Content = string(raw)
	return response, err
}

func TestSegmentTopicsRespectLibraryAtEveryCallAndPersistence(t *testing.T) {
	for _, scopeChanges := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable_scope", true: "topic_deleted_before_persistence"}[scopeChanges], func(t *testing.T) {
			h, _ := segmentedHarness(t)
			other := types.Library{ID: "segment-other-library", SpaceID: h.library.SpaceID, Name: "Other", Template: types.TemplateOwn}
			require.NoError(t, h.db.Create(&other).Error)
			topics := NewTopicService(h.db)
			allowed, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Allowed here", LibraryID: h.library.ID})
			require.NoError(t, err)
			shared, err := topics.CreateTopic(h.ctx, &types.Topic{LibraryID: h.library.ID, Name: "Second local topic"})
			require.NoError(t, err)
			excluded, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Other only", LibraryID: other.ID})
			require.NoError(t, err)
			m := &scopedSegmentChat{topicIDs: []string{allowed.ID, shared.ID, excluded.ID, allowed.ID}}
			if scopeChanges {
				m.beforeMerge = func() {
					require.NoError(t, topics.DeleteTopic(h.ctx, allowed.ID))
				}
			}
			models := h.service.modelService.(articleProcessingModels)
			models.summaryStateModelService.model = m
			entry := h.upload(t, "segmented-library-topics", "# Original\n\n"+strings.Repeat("Original product evidence with measurements. ", 18))
			require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus)
			require.Greater(t, len(m.inputs), 1)
			for _, input := range m.inputs {
				ids := []string{}
				for _, topic := range input.ExistingTopics {
					ids = append(ids, topic.ID)
				}
				require.ElementsMatch(t, []string{allowed.ID, shared.ID}, ids)
			}
			var ids []string
			require.NoError(t, h.db.Model(&types.EntryTopic{}).Where("entry_id=?", entry.ID).Pluck("topic_id", &ids).Error)
			wanted := []string{shared.ID}
			if !scopeChanges {
				wanted = append(wanted, allowed.ID)
			}
			require.ElementsMatch(t, wanted, ids)
			var run entrySegmentRun
			require.NoError(t, h.db.Where("entry_id=? AND phase='processing'", entry.ID).First(&run).Error)
			var result articleProcessingResult
			require.NoError(t, json.Unmarshal(run.Result, &result))
			require.NotContains(t, result.Provenance.Topics, excluded.ID)
			require.Len(t, result.Provenance.Topics[shared.ID], len(m.inputs))
		})
	}
}
