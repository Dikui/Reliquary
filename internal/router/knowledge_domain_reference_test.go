package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestKnowledgeDomainDeleteReturnsStructuredReferenceConflict(t *testing.T) {
	db := fusionFixtureDB(t)
	domains := service.NewKnowledgeDomainService(db)
	ctx := fusionContext(types.SingleTenantID, "administrator")
	domain, err := domains.Create(ctx, types.KnowledgeDomain{Name: "Used domain"})
	require.NoError(t, err)
	otherDomain, err := domains.Create(ctx, types.KnowledgeDomain{Name: "Other domain"})
	require.NoError(t, err)
	for _, item := range []struct {
		name     string
		domainID string
		tenantID uint64
		deleted  bool
	}{
		{"First reference", domain.ID, types.SingleTenantID, false},
		{"Second reference", domain.ID, types.SingleTenantID, false},
		{"Deleted reference", domain.ID, types.SingleTenantID, true},
		{"Other domain", otherDomain.ID, types.SingleTenantID, false},
		{"Historical foreign team", domain.ID, 42, false},
	} {
		kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: item.tenantID, DomainID: item.domainID, Name: item.name}
		kb.EnsureDefaults()
		require.NoError(t, db.Create(kb).Error)
		if item.deleted {
			require.NoError(t, db.Delete(kb).Error)
		}
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	guards := newAccessGuards(nil, nil, nil)
	RegisterKnowledgeDomainRoutes(engine.Group("/api/v1"), handler.NewKnowledgeDomainHandler(domains), guards)

	response := fusionRequest(t, engine, http.MethodDelete, "/api/v1/knowledge-domains/"+domain.ID, "",
		types.SingleTenantID, "administrator")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code    apperrors.ErrorCode                   `json:"code"`
			Message string                                `json:"message"`
			Details types.KnowledgeDomainReferenceDetails `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.False(t, body.Success)
	require.Equal(t, apperrors.ErrKnowledgeDomainReferenced, body.Error.Code)
	require.Equal(t, int64(2), body.Error.Details.KnowledgeBaseCount)
	require.Contains(t, body.Error.Message, "2 knowledge bases")

	var preserved types.KnowledgeDomain
	require.NoError(t, db.First(&preserved, "id = ?", domain.ID).Error)
	require.False(t, preserved.DeletedAt.Valid)
	require.Equal(t, domain.Name, preserved.Name)
}
