package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/agent/tools"
	"reliquary/internal/event"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type fakeAgentKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *fakeAgentKnowledgeBaseService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	if s.kb == nil {
		return nil, errors.New("knowledge base not found")
	}
	return s.kb, nil
}

type fakeAgentKnowledgeService struct {
	interfaces.KnowledgeService
	knowledges []*types.Knowledge
	lastFilter types.KnowledgeListFilter
	lastTenant uint64
}

type fakeAgentChatModel struct {
	lastToolNames []string
}

func (*fakeAgentChatModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{}, nil
}

func (m *fakeAgentChatModel) ChatStream(
	_ context.Context, _ []chat.Message, opts *chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	m.lastToolNames = nil
	if opts != nil {
		for _, tool := range opts.Tools {
			m.lastToolNames = append(m.lastToolNames, tool.Function.Name)
		}
	}

	ch := make(chan types.StreamResponse, 1)
	ch <- types.StreamResponse{
		ResponseType: types.ResponseTypeAnswer,
		Content:      "ok",
		Done:         true,
		FinishReason: "stop",
	}
	close(ch)
	return ch, nil
}

func (*fakeAgentChatModel) GetModelName() string { return "fake-chat" }
func (*fakeAgentChatModel) GetModelID() string   { return "fake-chat-id" }

func (s *fakeAgentKnowledgeService) ListPagedKnowledgeByKnowledgeBaseID(
	ctx context.Context,
	_ string,
	page *types.Pagination,
	filter types.KnowledgeListFilter,
) (*types.PageResult, error) {
	s.lastFilter = filter
	s.lastTenant, _ = types.TenantIDFromContext(ctx)

	filtered := make([]*types.Knowledge, 0, len(s.knowledges))
	for _, knowledge := range s.knowledges {
		if filter.ParseStatus != "" && knowledge.ParseStatus != filter.ParseStatus {
			continue
		}
		filtered = append(filtered, knowledge)
	}
	return types.NewPageResult(int64(len(filtered)), page, filtered), nil
}

func toolRegistered(registry *tools.ToolRegistry, name string) bool {
	_, err := registry.GetTool(name)
	return err == nil
}

func toolOffered(names []string, name string) bool {
	for _, got := range names {
		if got == name {
			return true
		}
	}
	return false
}

func TestGetKnowledgeBaseInfos_ExcludesUnprocessedDocuments(t *testing.T) {
	now := time.Now()
	knowledgeService := &fakeAgentKnowledgeService{
		knowledges: []*types.Knowledge{
			{
				ID:              "doc-processing",
				KnowledgeBaseID: "kb-1",
				Title:           "still parsing",
				FileName:        "processing.pdf",
				FileType:        "pdf",
				ParseStatus:     types.ParseStatusProcessing,
				CreatedAt:       now,
			},
			{
				ID:              "doc-completed",
				KnowledgeBaseID: "kb-1",
				Title:           "ready document",
				FileName:        "ready.pdf",
				FileType:        "pdf",
				ParseStatus:     types.ParseStatusCompleted,
				CreatedAt:       now.Add(-time.Minute),
			},
		},
	}
	service := &agentService{
		knowledgeBaseService: &fakeAgentKnowledgeBaseService{
			kb: &types.KnowledgeBase{
				ID:       "kb-1",
				Name:     "KB",
				Type:     types.KnowledgeBaseTypeDocument,
				TenantID: 1,
			},
		},
		knowledgeService: knowledgeService,
	}

	infos, err := service.getKnowledgeBaseInfos(context.Background(), []string{"kb-1"})

	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, types.ParseStatusCompleted, knowledgeService.lastFilter.ParseStatus)
	assert.Equal(t, 1, infos[0].DocCount)
	require.Len(t, infos[0].RecentDocs, 1)
	assert.Equal(t, "doc-completed", infos[0].RecentDocs[0].KnowledgeID)
}

func TestValidateConfigMaxIterationsUnlimited(t *testing.T) {
	s := &agentService{}

	unlimited := &types.AgentConfig{MaxIterations: -1}
	require.NoError(t, s.ValidateConfig(unlimited))
	assert.Equal(t, types.UnlimitedMaxIterations, unlimited.MaxIterations)

	normalized := &types.AgentConfig{MaxIterations: -9}
	require.NoError(t, s.ValidateConfig(normalized))
	assert.Equal(t, types.UnlimitedMaxIterations, normalized.MaxIterations)

	unset := &types.AgentConfig{}
	require.NoError(t, s.ValidateConfig(unset))
	assert.Equal(t, 5, unset.MaxIterations)

	tooHigh := &types.AgentConfig{MaxIterations: MAX_ITERATIONS + 1}
	require.Error(t, s.ValidateConfig(tooHigh))
}

func TestAgentRegistersURLFetchOnlyWhenAllowed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		want    bool
	}{
		{name: "specified URL fetch", allowed: []string{tools.ToolWebFetch}, want: true},
		{name: "explicit tool selection", allowed: []string{tools.ToolThinking}, want: false},
		{name: "default tools", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := tools.NewToolRegistry()
			err := (&agentService{}).registerTools(context.Background(), registry,
				&types.AgentConfig{AllowedTools: tc.allowed}, nil, &fakeAgentChatModel{}, "session")
			require.NoError(t, err)
			_, err = registry.GetTool(tools.ToolWebFetch)
			if tc.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestAgentAnswerWithoutRemovedCapabilities(t *testing.T) {
	removed := []string{
		"wiki_search", "wiki_read_page", "wiki_read_source_doc", "wiki_flag_issue",
		"wiki_write_page", "wiki_replace_text", "wiki_rename_page", "wiki_delete_page",
		"wiki_read_issue", "wiki_update_issue", "search_memory", "search_conversations",
		"shell_exec", "read_skill", "execute_skill_script", "list_sandbox_files",
		"read_sandbox_file", "write_sandbox_file", "edit_sandbox_file",
		"write_skill_file", "edit_skill_file",
	}
	legacy := map[string]any{
		"allowed_tools":  append(append([]string(nil), removed...), tools.ToolWebFetch),
		"memory_enabled": map[string]any{"legacy": true},
		"skills_enabled": "legacy", "skill_dirs": false, "allowed_skills": 17,
		"sandbox_config_id": map[string]any{"legacy": "config"},
	}
	payload, err := json.Marshal(legacy)
	require.NoError(t, err)
	var cfg types.AgentConfig
	require.NoError(t, json.Unmarshal(payload, &cfg))
	model := &fakeAgentChatModel{}
	bus := event.NewEventBus()
	engine, err := (&agentService{}).CreateAgentEngine(context.Background(), &cfg, model, nil, bus, "session", "answer")
	require.NoError(t, err)
	result, err := engine.Execute(context.Background(), "session", "answer", "hello", nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.FinalAnswer)
	assert.ElementsMatch(t, []string{tools.ToolWebFetch}, model.lastToolNames)
	for _, name := range removed {
		assert.NotContains(t, model.lastToolNames, name)
		assert.NotContains(t, tools.DefaultAllowedTools(), name)
		for _, definition := range tools.AvailableToolDefinitions() {
			assert.NotEqual(t, name, definition.Name)
		}
	}
}
