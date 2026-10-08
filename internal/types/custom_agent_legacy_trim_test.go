package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomAgentConfigLoadsRemovedFieldsAsUnknown(t *testing.T) {
	for _, legacy := range []string{
		`{"memory_enabled":true,"skills_selection_mode":"all","selected_skills":["old"],"sandbox_config_id":"old"}`,
		`{"memory_enabled":{"old":true},"skills_selection_mode":false,"selected_skills":17,"sandbox_config_id":["old"]}`,
	} {
		var config CustomAgentConfig
		require.NoError(t, config.Scan([]byte(`{"agent_mode":"smart-reasoning","system_prompt":"retained",`+legacy[1:])))
		agent := &CustomAgent{Config: config}
		agent.EnsureDefaults()
		assert.Equal(t, "retained", agent.Config.SystemPrompt)
		assert.Equal(t, AgentModeSmartReasoning, agent.Config.AgentMode)
		stored, err := agent.Config.Value()
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(stored.([]byte), &fields))
		for _, key := range []string{"memory_enabled", "skills_selection_mode", "selected_skills", "sandbox_config_id"} {
			assert.NotContains(t, fields, key)
		}
	}
}
