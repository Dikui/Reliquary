package service

import (
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"testing"
)

func TestIKB15ChatGPTLoggedInGenerationSmoke(t *testing.T) {
	h := newChatGPTGenerationHarness(t, func(chatGPTGenerationRequest) (string, string) { return "ikb15 signed-in generation is available", "" })
	model, err := h.models.GetChatModel(h.ctx, "chatgpt-generation")
	require.NoError(t, err)
	response, err := model.Chat(h.ctx, []chat.Message{{Role: "system", Content: "Return the verification phrase."}, {Role: "user", Content: "Verify signed-in generation once."}}, nil)
	require.NoError(t, err)
	require.Equal(t, "ikb15 signed-in generation is available", response.Content)
	require.Len(t, h.requests(), 1)
	policy, err := h.policy.Get(h.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, policy.DailyUsed)
	t.Log("one actual HTTP substitute call through the logged-in ChatGPT model service succeeded")
}
