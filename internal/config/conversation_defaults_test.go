package config

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"testing"
)

func TestDefaultQuestionGenerationPromptIsResolved(t *testing.T) {
	data, err := os.ReadFile("../../config/config.yaml")
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	cfg.PromptTemplates, err = loadPromptTemplates("../../config")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Conversation.GenerateQuestionsPromptID)
	template := FindTemplateByID(cfg.PromptTemplates, cfg.Conversation.GenerateQuestionsPromptID)
	require.NotNil(t, template)
	backfillConversationDefaults(&cfg)
	require.NotEmpty(t, cfg.Conversation.GenerateQuestionsPrompt)
	require.Equal(t, template.Content, cfg.Conversation.GenerateQuestionsPrompt)
}
