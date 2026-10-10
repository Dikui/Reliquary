package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/agent/approval"
	"reliquary/internal/event"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestResolveToolApprovalRejectsNullWithoutResolvingPendingCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gate := approval.NewGate(nil, nil)
	bus := event.NewEventBus()
	pending := make(chan string, 1)
	bus.On(event.EventToolApprovalRequired, func(_ context.Context, evt event.Event) error {
		pending <- evt.Data.(event.ToolApprovalRequiredData).PendingID
		return nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	decision := make(chan approval.Decision, 1)
	go func() {
		d, _ := gate.RequireAndWait(ctx, approval.PendingRequest{
			TenantID: 1, UserID: "web_user:owner", EventBus: bus,
		})
		decision <- d
	}()
	var id string
	select {
	case id = <-pending:
	case <-ctx.Done():
		t.Fatal("approval request did not become pending")
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(types.WithPrincipal(c.Request.Context(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"}))
		c.Next()
	})
	r.POST("/approval/:pending_id", NewMCPServiceHandler(nil, nil, gate).ResolveToolApproval)
	invalid := httptest.NewRecorder()
	r.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"decision":"approve","modified_args":null}`)))
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
	require.Contains(t, invalid.Body.String(), "non-null JSON object")
	valid := httptest.NewRecorder()
	r.ServeHTTP(valid, httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"decision":"approve"}`)))
	require.Equal(t, http.StatusOK, valid.Code, valid.Body.String())
	select {
	case d := <-decision:
		require.True(t, d.Approved)
		require.Empty(t, d.ModifiedArgs)
	case <-ctx.Done():
		t.Fatal("valid approval did not resolve the pending call")
	}
}

func TestMCPServiceRejectsOAuthAuthenticationWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Next()
	})
	h := NewMCPServiceHandler(nil, nil, nil)
	r.POST("/mcp-services", h.CreateMCPService)
	r.PUT("/mcp-services/:id", h.UpdateMCPService)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/mcp-services"},
		{http.MethodPut, "/mcp-services/service-1"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"auth_config":{"auth_type":"oauth"}}`)))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var body struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, 1010, body.Error.Code)
			require.Contains(t, body.Error.Message, `unsupported MCP authentication type "oauth"`)
			require.Contains(t, body.Error.Message, "none, api_key or bearer")
		})
	}
}
