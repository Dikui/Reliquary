package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type conflictWorkflowKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s conflictWorkflowKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type conflictWorkflowQueue struct{ calls int }

func (q *conflictWorkflowQueue) Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	return &asynq.TaskInfo{}, nil
}

func TestDisabledWorkflowRunRetryResumeConflict(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "conflict"}).Error)
	require.NoError(t, db.Create(&types.User{ID: "conflict-owner", TenantID: 1, Username: "conflict-owner", Email: "conflict@example.com", IsActive: true}).Error)
	domain := service.BuiltinAIKnowledgeDomain()
	domain.ID, domain.TenantID = "conflict-domain", 1
	require.NoError(t, db.Create(domain).Error)
	kb := &types.KnowledgeBase{DomainID: domain.ID, ID: "conflict-kb", TenantID: 1, Name: "conflict"}
	require.NoError(t, db.Create(kb).Error)
	wf := &types.KnowledgeWorkflow{ID: "conflict-workflow", TenantID: 1, KnowledgeBaseID: kb.ID, OwnerID: "conflict-owner", Name: "conflict", Steps: []types.KnowledgeWorkflowStep{{ID: "search", Type: types.WorkflowStepSearch, Config: map[string]any{"query": "test"}}}}
	require.NoError(t, db.Create(wf).Error)
	for _, status := range []string{types.WorkflowStatusFailed, types.WorkflowStatusPaused} {
		require.NoError(t, db.Create(&types.KnowledgeWorkflowRun{ID: "conflict-" + status, TenantID: 1, KnowledgeBaseID: kb.ID, WorkflowID: wf.ID, OwnerID: wf.OwnerID, Status: status, Trigger: "manual", IdempotencyKey: status, WorkflowSnapshot: wf.Steps}).Error)
	}
	queue := &conflictWorkflowQueue{}
	handler := NewKnowledgeWorkflowHandler(service.NewKnowledgeWorkflowService(db, conflictWorkflowKB{kb: kb}, nil, nil, nil, queue))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/knowledge-bases/:id/workflows/:workflow_id/run", handler.Run)
	router.POST("/knowledge-bases/:id/workflows/runs/:run_id/retry", handler.Retry)
	router.POST("/knowledge-bases/:id/workflows/runs/:run_id/resume", handler.Resume)
	paths := []string{"/knowledge-bases/conflict-kb/workflows/conflict-workflow/run", "/knowledge-bases/conflict-kb/workflows/runs/conflict-failed/retry", "/knowledge-bases/conflict-kb/workflows/runs/conflict-paused/resume"}
	request := func(path, locale string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		ctx := context.WithValue(req.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, wf.OwnerID)
		ctx = context.WithValue(ctx, types.LanguageContextKey, locale)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req.WithContext(ctx))
		return recorder
	}
	for _, locale := range []string{"zh-CN", "en-US"} {
		for _, path := range paths {
			t.Run(locale+path, func(t *testing.T) {
				response := request(path, locale)
				require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
				var body struct {
					Success bool `json:"success"`
					Error   struct {
						Code    werrors.ErrorCode `json:"code"`
						Message string            `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				require.False(t, body.Success)
				require.Equal(t, werrors.ErrConflict, body.Error.Code)
				if strings.HasPrefix(locale, "zh") {
					require.Equal(t, "工作流已停用，请先启用后再运行、重试或恢复。", body.Error.Message)
				} else {
					require.Equal(t, "This workflow is disabled. Enable it before running, retrying, or resuming.", body.Error.Message)
				}
			})
		}
	}
	require.Zero(t, queue.calls)
	var runs []types.KnowledgeWorkflowRun
	require.NoError(t, db.Where("workflow_id = ?", wf.ID).Order("status").Find(&runs).Error)
	require.Len(t, runs, 2)
	require.Equal(t, types.WorkflowStatusFailed, runs[0].Status)
	require.Equal(t, types.WorkflowStatusPaused, runs[1].Status)
	require.NoError(t, db.Model(wf).Update("enabled", true).Error)
	for _, path := range paths {
		response := request(path, "en-US")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"status":"queued"`)
	}
	require.Equal(t, 3, queue.calls)
}
