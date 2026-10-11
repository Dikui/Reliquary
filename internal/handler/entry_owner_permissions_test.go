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
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type ownerEntryPermissionsService struct {
	interfaces.EntryService
	actions int
}

func (s *ownerEntryPermissionsService) CreateEntryFromManual(context.Context, string, *types.ManualEntryPayload, string) (*types.Entry, error) {
	s.actions++
	return &types.Entry{ID: "created-manual", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerEntryPermissionsService) CreateEntryFromURL(context.Context, string, string, string, string, *bool, string, string, *types.EntryProcessOverrides, ...types.SourceInfo) (*types.Entry, error) {
	s.actions++
	return &types.Entry{ID: "created-url", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerEntryPermissionsService) CreateEntryFromFile(context.Context, string, *multipart.FileHeader, map[string]string, *bool, string, string, *types.EntryProcessOverrides) (*types.Entry, error) {
	s.actions++
	return &types.Entry{ID: "created-file", KnowledgeBaseID: "kb-1"}, nil
}

func (s *ownerEntryPermissionsService) GetEntryBatch(_ context.Context, _ uint64, ids []string) ([]*types.Entry, error) {
	items := make([]*types.Entry, 0, len(ids))
	for _, id := range ids {
		items = append(items, &types.Entry{ID: id, SpaceID: 1, KnowledgeBaseID: "kb-1"})
	}
	return items, nil
}

func (s *ownerEntryPermissionsService) ListEntryByKnowledgeBaseID(context.Context, string) ([]*types.Entry, error) {
	s.actions++
	return []*types.Entry{}, nil
}

type ownerEntryPermissionsQueue struct{ actions int }

func (s *ownerEntryPermissionsQueue) Enqueue(*taskqueue.Task, ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	s.actions++
	return &taskqueue.TaskInfo{ID: "owner-permissions-task"}, nil
}

type ownerEntryPermissionsCase struct {
	name    string
	method  string
	pattern string
	path    string
	body    string
	status  int
	handler func(*EntryHandler) gin.HandlerFunc
}

func ownerEntryPermissionsCases() []ownerEntryPermissionsCase {
	return []ownerEntryPermissionsCase{
		{name: "manual", method: http.MethodPost, pattern: "/knowledge-bases/:id/entries/manual", path: "/knowledge-bases/kb-1/entries/manual", body: `{"title":"Manual","content":"Complete sourced article","status":"publish"}`, status: http.StatusOK, handler: func(h *EntryHandler) gin.HandlerFunc { return h.CreateManualEntry }},
		{name: "url", method: http.MethodPost, pattern: "/knowledge-bases/:id/entries/url", path: "/knowledge-bases/kb-1/entries/url", body: `{"url":"https://owner-permissions.invalid/article"}`, status: http.StatusCreated, handler: func(h *EntryHandler) gin.HandlerFunc { return h.CreateEntryFromURL }},
		{name: "file", method: http.MethodPost, pattern: "/knowledge-bases/:id/entries/file", path: "/knowledge-bases/kb-1/entries/file", status: http.StatusOK, handler: func(h *EntryHandler) gin.HandlerFunc { return h.CreateEntryFromFile }},
		{name: "batch_delete", method: http.MethodPost, pattern: "/entries/batch-delete", path: "/entries/batch-delete", body: `{"ids":["message"]}`, status: http.StatusOK, handler: func(h *EntryHandler) gin.HandlerFunc { return h.BatchDeleteEntry }},
		{name: "batch_reparse", method: http.MethodPost, pattern: "/entries/batch-reparse", path: "/entries/batch-reparse", body: `{"ids":["message"]}`, status: http.StatusOK, handler: func(h *EntryHandler) gin.HandlerFunc { return h.BatchReparseEntry }},
		{name: "clear", method: http.MethodDelete, pattern: "/knowledge-bases/:id/entries", path: "/knowledge-bases/kb-1/entries", status: http.StatusOK, handler: func(h *EntryHandler) gin.HandlerFunc { return h.ClearKnowledgeBaseContents }},
	}
}

func ownerEntryPermissionsRequest(t *testing.T, item ownerEntryPermissionsCase) *http.Request {
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

func ownerEntryPermissionsRouter(item ownerEntryPermissionsCase) (*gin.Engine, *ownerEntryPermissionsService, *ownerEntryPermissionsQueue) {
	gin.SetMode(gin.TestMode)
	service := &ownerEntryPermissionsService{}
	queue := &ownerEntryPermissionsQueue{}
	h := &EntryHandler{kbService: &stubArticleKBService{}, kgService: service, taskClient: queue}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "owner-matrix-user")
		ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.UserIDContextKey, "owner-matrix-user")

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Handle(item.method, item.pattern, item.handler(h))
	return router, service, queue
}

func TestEntryHandlersAllowPersonalUser(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "owner-permissions.invalid")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, item := range ownerEntryPermissionsCases() {
		t.Run(item.name, func(t *testing.T) {
			router, service, queue := ownerEntryPermissionsRouter(item)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, ownerEntryPermissionsRequest(t, item))
			require.Equal(t, item.status, recorder.Code, recorder.Body.String())
			require.Equal(t, 1, service.actions+queue.actions)
		})
	}
}
