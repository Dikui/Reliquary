package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type ownerKnowledgePermissionsService struct {
	interfaces.KnowledgeService
	actions int
}

func (s *ownerKnowledgePermissionsService) CreateKnowledgeFromManual(context.Context, string, *types.ManualKnowledgePayload, string) (*types.Knowledge, error) {
	s.actions++
	return &types.Knowledge{ID: "created-manual", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerKnowledgePermissionsService) CreateKnowledgeFromURL(context.Context, string, string, string, string, *bool, string, []string, string, *types.KnowledgeProcessOverrides, ...types.SourceInfo) (*types.Knowledge, error) {
	s.actions++
	return &types.Knowledge{ID: "created-url", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerKnowledgePermissionsService) CreateKnowledgeFromFile(context.Context, string, *multipart.FileHeader, map[string]string, *bool, string, []string, string, *types.KnowledgeProcessOverrides) (*types.Knowledge, error) {
	s.actions++
	return &types.Knowledge{ID: "created-file", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerKnowledgePermissionsService) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	items := make([]*types.Knowledge, 0, len(ids))
	for _, id := range ids {
		items = append(items, &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "kb-1"})
	}
	return items, nil
}

func (s *ownerKnowledgePermissionsService) MoveKnowledgeToFolder(context.Context, string, []string, string) (int64, error) {
	s.actions++
	return 1, nil
}

func (s *ownerKnowledgePermissionsService) RenameKnowledgeFolder(context.Context, string, string, string) (int64, error) {
	s.actions++
	return 1, nil
}

func (s *ownerKnowledgePermissionsService) UpdateKnowledgeTagBatch(context.Context, string, map[string][]string, ...string) error {
	s.actions++
	return nil
}

func (s *ownerKnowledgePermissionsService) ListKnowledgeByKnowledgeBaseID(context.Context, string) ([]*types.Knowledge, error) {
	s.actions++
	return []*types.Knowledge{}, nil
}

type ownerKnowledgePermissionsQueue struct{ actions int }

func (s *ownerKnowledgePermissionsQueue) Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	s.actions++
	return &asynq.TaskInfo{ID: "owner-permissions-task"}, nil
}

type ownerKnowledgePermissionsCase struct {
	name        string
	method      string
	pattern     string
	path        string
	body        string
	minimumRole types.TenantRole
	status      int
	handler     func(*KnowledgeHandler) gin.HandlerFunc
}

func ownerKnowledgePermissionsCases() []ownerKnowledgePermissionsCase {
	return []ownerKnowledgePermissionsCase{
		{name: "manual", method: http.MethodPost, pattern: "/knowledge-bases/:id/knowledge/manual", path: "/knowledge-bases/kb-1/knowledge/manual", body: `{"title":"Manual","content":"Complete sourced article","status":"publish"}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.CreateManualKnowledge }},
		{name: "url", method: http.MethodPost, pattern: "/knowledge-bases/:id/knowledge/url", path: "/knowledge-bases/kb-1/knowledge/url", body: `{"url":"https://owner-permissions.invalid/article"}`, minimumRole: types.TenantRoleContributor, status: http.StatusCreated, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.CreateKnowledgeFromURL }},
		{name: "file", method: http.MethodPost, pattern: "/knowledge-bases/:id/knowledge/file", path: "/knowledge-bases/kb-1/knowledge/file", minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.CreateKnowledgeFromFile }},
		{name: "rename_folder", method: http.MethodPut, pattern: "/knowledge-bases/:id/knowledge/folders", path: "/knowledge-bases/kb-1/knowledge/folders", body: `{"from":"before","to":"after"}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.RenameKnowledgeFolder }},
		{name: "move_folder", method: http.MethodPost, pattern: "/knowledge/folder", path: "/knowledge/folder", body: `{"kb_id":"kb-1","knowledge_ids":["message"],"folder_path":"after"}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.MoveKnowledgeToFolder }},
		{name: "batch_delete", method: http.MethodPost, pattern: "/knowledge/batch-delete", path: "/knowledge/batch-delete", body: `{"kb_id":"kb-1","ids":["message"]}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.BatchDeleteKnowledge }},
		{name: "batch_reparse", method: http.MethodPost, pattern: "/knowledge/batch-reparse", path: "/knowledge/batch-reparse", body: `{"kb_id":"kb-1","ids":["message"]}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.BatchReparseKnowledge }},
		{name: "batch_tags", method: http.MethodPut, pattern: "/knowledge/tags", path: "/knowledge/tags", body: `{"kb_id":"kb-1","updates":{"message":["tag"]}}`, minimumRole: types.TenantRoleContributor, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.UpdateKnowledgeTagBatch }},
		{name: "clear", method: http.MethodDelete, pattern: "/knowledge-bases/:id/knowledge", path: "/knowledge-bases/kb-1/knowledge", minimumRole: types.TenantRoleAdmin, status: http.StatusOK, handler: func(h *KnowledgeHandler) gin.HandlerFunc { return h.ClearKnowledgeBaseContents }},
	}
}

func ownerKnowledgePermissionsRequest(t *testing.T, item ownerKnowledgePermissionsCase) *http.Request {
	t.Helper()
	if item.name == "file" {
		var content bytes.Buffer
		form := multipart.NewWriter(&content)
		file, err := form.CreateFormFile("file", "article.md")
		require.NoError(t, err)
		_, err = file.Write([]byte("Complete original article"))
		require.NoError(t, err)
		require.NoError(t, form.Close())
		request := httptest.NewRequest(item.method, item.path, &content)
		request.Header.Set("Content-Type", form.FormDataContentType())
		return request
	}
	request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func ownerKnowledgePermissionsRouter(item ownerKnowledgePermissionsCase, role types.TenantRole, key *types.TenantAPIKeyScope) (*gin.Engine, *ownerKnowledgePermissionsService, *ownerKnowledgePermissionsQueue) {
	gin.SetMode(gin.TestMode)
	service := &ownerKnowledgePermissionsService{}
	queue := &ownerKnowledgePermissionsQueue{}
	h := &KnowledgeHandler{kbService: &stubFolderKBService{}, kgService: service, asynqClient: queue}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "owner-matrix-user")
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "owner-matrix-user")
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		if key != nil {
			ctx = types.WithTenantAPIKeyScope(ctx, *key)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Handle(item.method, item.pattern, item.handler(h))
	return router, service, queue
}

func TestKnowledgeHandlersHonorTeamRoleHierarchyIncludingOwner(t *testing.T) {
	// The handler still runs URL validation; this local test host never needs
	// DNS or a remote fetch because the configured whitelist and service stub
	// keep the permission matrix fully offline.
	t.Setenv("SSRF_WHITELIST", "owner-permissions.invalid")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, item := range ownerKnowledgePermissionsCases() {
		for _, role := range []types.TenantRole{types.TenantRoleOwner, types.TenantRoleAdmin, types.TenantRoleContributor, types.TenantRoleViewer} {
			t.Run(item.name+"/"+string(role), func(t *testing.T) {
				router, service, queue := ownerKnowledgePermissionsRouter(item, role, nil)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, ownerKnowledgePermissionsRequest(t, item))
				allowed := role == types.TenantRoleOwner || role == types.TenantRoleAdmin || (item.minimumRole == types.TenantRoleContributor && role == types.TenantRoleContributor)
				if allowed {
					require.Equal(t, item.status, recorder.Code, recorder.Body.String())
					require.Equal(t, 1, service.actions+queue.actions, "authorized requests reach the actual handler action")
				} else {
					require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
					require.Zero(t, service.actions+queue.actions, "denied requests never call the write service or queue")
				}
			})
		}
	}
}

func TestKnowledgeOwnerHierarchyKeepsAPIKeyKnowledgeBaseScope(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "owner-permissions.invalid")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, item := range ownerKnowledgePermissionsCases() {
		for _, allowed := range []bool{true, false} {
			name, kbID := "allowed", "kb-1"
			if !allowed {
				name, kbID = "out_of_scope", "different-kb"
			}
			t.Run(item.name+"/"+name, func(t *testing.T) {
				key := types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{kbID}}
				router, service, queue := ownerKnowledgePermissionsRouter(item, types.TenantRoleOwner, &key)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, ownerKnowledgePermissionsRequest(t, item))
				if allowed {
					require.Equal(t, item.status, recorder.Code, recorder.Body.String())
					require.Equal(t, 1, service.actions+queue.actions)
				} else {
					require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
					require.Zero(t, service.actions+queue.actions)
				}
			})
		}
	}
}
