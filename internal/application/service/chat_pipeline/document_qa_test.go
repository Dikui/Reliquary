package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type documentQAChatInterface interface{ chat.Chat }

type documentQAChat struct {
	documentQAChatInterface
	messages []chat.Message
}

func (m *documentQAChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.messages = messages
	return &types.ChatResponse{Content: `Acme ships widgets. <ref id="c1"/>`}, nil
}
func (*documentQAChat) GetModelID() string   { return "document-qa" }
func (*documentQAChat) GetModelName() string { return "document-qa" }

func TestKnowledgeQACompletesWithDocumentCitation(t *testing.T) {
	model := &documentQAChat{}
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "What does Acme ship?", SummaryConfig: types.SummaryConfig{Prompt: "Answer using source documents.", ContextTemplate: "{{query}}\n{{contexts}}"}},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{{ID: "article-chunk", KnowledgeID: "article", KnowledgeBaseID: "kb", KnowledgeTitle: "Acme", Content: "Acme ships widgets."}}},
	}
	ctx := context.Background()
	require.Nil(t, (&PluginIntoChatMessage{}).OnEvent(ctx, types.INTO_CHAT_MESSAGE, cm, func() *PluginError { return nil }))
	require.Nil(t, (&PluginChatCompletion{modelService: &stubModelService{model: model}}).OnEvent(ctx, types.CHAT_COMPLETION, cm, func() *PluginError { return nil }))
	require.Contains(t, cm.ChatResponse.Content, `<kb doc="Acme" chunk_id="article-chunk" kb_id="kb" />`)
	var supplied string
	for _, message := range model.messages {
		supplied += message.Content
	}
	require.True(t, strings.Contains(supplied, "Acme ships widgets."))
	require.NotContains(t, supplied, "structured_knowledge")
}
