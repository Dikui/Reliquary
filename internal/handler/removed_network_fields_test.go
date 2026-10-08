package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type agentCreateForRemovedFields struct {
	interfaces.CustomAgentService
	created *types.CustomAgent
}

func (s *agentCreateForRemovedFields) CreateAgent(_ context.Context, agent *types.CustomAgent) (*types.CustomAgent, error) {
	s.created = agent
	agent.ID = "test-agent"
	return agent, nil
}

func TestAgentConfigRemovedNetworkFieldsFollowUnknownFieldRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, extra := range []string{
		`"unknown_option": {"ignored": true}`,
		`"web_search_enabled": true, "web_search_max_results": 5, "web_search_provider_id":"old"`,
		`"web_search_enabled": {"ignored": true}, "web_search_max_results":"ignored", "system_prompt_web_enabled":42`,
	} {
		t.Run(extra, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"test","config":{"agent_mode":"quick-answer","system_prompt":"retained",`+extra+`}}`))
			c.Request.Header.Set("Content-Type", "application/json")
			s := &agentCreateForRemovedFields{}
			h := &CustomAgentHandler{service: s}
			h.CreateAgent(c)
			require.Empty(t, c.Errors)
			require.Equal(t, http.StatusCreated, rec.Code)
			require.NotNil(t, s.created)
			require.Equal(t, "retained", s.created.Config.SystemPrompt)
			encoded, err := json.Marshal(s.created.Config)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "web_search")
			require.NotContains(t, string(encoded), "system_prompt_web_enabled")
		})
	}
}
