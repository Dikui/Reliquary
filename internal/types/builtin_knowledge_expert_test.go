package types

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinKnowledgePreservesSavedAgentIDAndLocalizes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "builtin_agents.yaml"))
	require.NoError(t, err)
	var file builtinAgentsFile
	require.NoError(t, yaml.Unmarshal(data, &file))
	entries := map[string]*BuiltinAgentEntry{}
	for i := range file.BuiltinAgents {
		entries[file.BuiltinAgents[i].ID] = &file.BuiltinAgents[i]
	}
	require.Equal(t, "builtin-knowledge-graph-expert", BuiltinKnowledgeExpertID)
	require.Contains(t, entries, BuiltinKnowledgeExpertID)
	restore := OverrideBuiltinAgentEntriesForTest(entries)
	defer restore()
	ctx := context.WithValue(context.Background(), LanguageContextKey, "zh-CN")
	agent := &CustomAgent{ID: "builtin-knowledge-graph-expert", TenantID: 1, Name: "旧名称"}
	ApplyBuiltinAgentLocalization(ctx, agent)
	require.Equal(t, "文档研究", agent.Name)
	require.Contains(t, agent.Description, "文档引用")
	builtin := GetBuiltinAgentWithContext(ctx, agent.ID, agent.TenantID)
	require.NotNil(t, builtin)
	require.Contains(t, builtin.Config.AllowedTools, "knowledge_search")
	smart := GetBuiltinAgentWithContext(ctx, BuiltinSmartReasoningID, agent.TenantID)
	require.Equal(t, smart.Config, builtin.Config)
}
