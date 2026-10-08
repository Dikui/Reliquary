package handler

import (
	"context"
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
