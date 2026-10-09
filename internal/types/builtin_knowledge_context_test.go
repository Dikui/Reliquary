package types

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinKnowledgeContextPreservesSavedAgentIDAndLocalizes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "builtin_agents.yaml"))
	require.NoError(t, err)
	var file builtinAgentsFile
	require.NoError(t, yaml.Unmarshal(data, &file))
	entries := map[string]*BuiltinAgentEntry{}
	for i := range file.BuiltinAgents {
		entries[file.BuiltinAgents[i].ID] = &file.BuiltinAgents[i]
	}
	require.Equal(t, "builtin-knowledge-graph-expert", BuiltinKnowledgeContextExpertID)
	require.Contains(t, entries, BuiltinKnowledgeContextExpertID)
	restore := OverrideBuiltinAgentEntriesForTest(entries)
	defer restore()
	ctx := context.WithValue(context.Background(), LanguageContextKey, "zh-CN")
	agent := &CustomAgent{ID: "builtin-knowledge-graph-expert", TenantID: 1, Name: "旧名称"}
	ApplyBuiltinAgentLocalization(ctx, agent)
	require.Equal(t, "知识上下文", agent.Name)
	require.Contains(t, agent.Description, "来源证据")
	builtin := GetBuiltinAgentWithContext(ctx, agent.ID, agent.TenantID)
	require.NotNil(t, builtin)
	require.Contains(t, builtin.Config.AllowedTools, "query_knowledge_context")
	require.NotContains(t, builtin.Config.AllowedTools, "query_knowledge_graph")
	smart := GetBuiltinAgentWithContext(ctx, BuiltinSmartReasoningID, agent.TenantID)
	require.Equal(t, smart.Config, builtin.Config)
}
