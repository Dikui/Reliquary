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

	apperrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type domainUpdateErrorService struct {
	interfaces.KnowledgeBaseService
	domainIDs []string
	updateErr error
	updated   bool
	deleted   bool
}

func (s *domainUpdateErrorService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", TenantID: types.SingleTenantID, DomainID: "current-domain"}, nil
}

func (s *domainUpdateErrorService) UpdateKnowledgeBase(_ context.Context, id string, name string, description string, _ *types.KnowledgeBaseConfig, domainIDs ...string) (*types.KnowledgeBase, error) {
	s.domainIDs = domainIDs
	s.updated = true
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	kb := &types.KnowledgeBase{ID: id, Name: name, Description: description, TenantID: types.SingleTenantID}
	if len(domainIDs) > 0 {
		kb.DomainID = domainIDs[0]
	}
	return kb, nil
}

func (s *domainUpdateErrorService) DeleteKnowledgeBase(context.Context, string) error {
	s.deleted = true
	return nil
}

func newDomainKnowledgeBaseTestRouter(svc interfaces.KnowledgeBaseService, role types.TenantRole) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), types.SingleTenantID)
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.SingleTenantID)
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, role)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc}
	r.PUT("/api/v1/knowledge-bases/:id", h.UpdateKnowledgeBase)
	r.DELETE("/api/v1/knowledge-bases/:id", h.DeleteKnowledgeBase)
	return r
}

func TestUpdateKnowledgeBaseInvalidDomainReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, domainID := range []string{"", "missing-domain", "deleted-domain"} {
		t.Run(domainID, func(t *testing.T) {
			svc := &domainUpdateErrorService{updateErr: apperrors.NewBadRequestError("domain_id must reference an existing team domain")}
			r := newDomainKnowledgeBaseTestRouter(svc, types.TenantRoleContributor)
			body, err := json.Marshal(map[string]string{"name": "News", "domain_id": domainID})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, []string{domainID}, svc.domainIDs)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"code":1000`)
		})
	}
}

func TestKnowledgeBaseDomainUpdateAndDeletionRolePermissions(t *testing.T) {
	for _, role := range []types.TenantRole{types.TenantRoleOwner, types.TenantRoleAdmin, types.TenantRoleContributor, types.TenantRoleViewer} {
		t.Run(string(role)+"/update_domain", func(t *testing.T) {
			svc := &domainUpdateErrorService{}
			r := newDomainKnowledgeBaseTestRouter(svc, role)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/knowledge-bases/kb", strings.NewReader(`{"name":"News","domain_id":"new-domain"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			if role.HasPermission(types.TenantRoleContributor) {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.True(t, svc.updated)
				require.Equal(t, []string{"new-domain"}, svc.domainIDs)
				require.Contains(t, response.Body.String(), `"domain_id":"new-domain"`)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				require.False(t, svc.updated)
			}
		})
		t.Run(string(role)+"/delete", func(t *testing.T) {
			svc := &domainUpdateErrorService{}
			r := newDomainKnowledgeBaseTestRouter(svc, role)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/v1/knowledge-bases/kb", nil))
			if role.HasPermission(types.TenantRoleAdmin) {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.True(t, svc.deleted)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				require.False(t, svc.deleted)
			}
		})
	}
}
