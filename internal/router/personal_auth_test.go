package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type personalAuthFixture struct {
	db     *gorm.DB
	router *gin.Engine
	token  string
	key    string
	userID string
}

func personalRequest(t *testing.T, r *gin.Engine, method, path, body, credential string, apiKey bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		if apiKey {
			req.Header.Set("X-API-Key", credential)
		} else {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func newPersonalAuthFixture(t *testing.T) *personalAuthFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := pgtest.Open(t)
	require.NoError(t, service.EnsureSingleTeamSpace(context.Background(), db))
	cfg := &config.Config{Conversation: &config.ConversationConfig{}, Tenant: &config.TenantConfig{}}
	tenantRepo := apprepo.NewTenantRepository(db)
	tenantSvc := service.NewTenantService(tenantRepo)
	users := service.NewUserService(cfg, apprepo.NewUserRepository(db), apprepo.NewAuthTokenRepository(db), tenantSvc)
	keys := service.NewTenantAPIKeyService(apprepo.NewTenantAPIKeyRepository(db))
	knowledgeRepo := apprepo.NewKnowledgeRepository(db)
	kbRepo := apprepo.NewKnowledgeBaseRepository(db)
	kb := service.NewKnowledgeBaseService(db, kbRepo, knowledgeRepo, nil, nil, nil, tenantRepo, nil, nil, nil, nil, nil, nil, nil)
	models := service.NewModelService(apprepo.NewModelRepository(db), kbRepo, nil, nil, nil, tenantSvc)
	knowledge, err := service.NewKnowledgeService(db, cfg, knowledgeRepo, kb, tenantRepo, tenantSvc,
		nil, nil, nil, nil, nil, nil, models, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	settings := service.NewSystemSettingService(apprepo.NewSystemSettingRepository(db), nil, cfg)
	system := handler.NewSystemHandler(cfg, tenantSvc, users, settings, keys, nil, nil)
	guards := newAccessGuards(kb, knowledge, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), middleware.Auth(tenantSvc, users, keys))
	v1 := r.Group("/api/v1")
	RegisterAuthRoutes(v1, handler.NewAuthHandler(users, tenantSvc), guards)
	RegisterTenantRoutes(v1, handler.NewTenantHandler(tenantSvc, keys, users, kb, cfg, settings), guards)
	RegisterSystemAdminRoutes(v1, system, guards)
	RegisterModelRoutes(v1, handler.NewModelHandler(models), nil, guards)
	RegisterKnowledgeDomainRoutes(v1, handler.NewKnowledgeDomainHandler(service.NewKnowledgeDomainService(db)), guards)
	RegisterKnowledgeContextRoutes(v1, handler.NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil), guards)
	v1.GET("/knowledge/search", handler.NewKnowledgeHandler(cfg, knowledge, kb, nil, nil).SearchKnowledge)
	f := &personalAuthFixture{db: db, router: r}
	register := personalRequest(t, r, http.MethodPost, "/api/v1/auth/register", `{"username":"personal","email":"personal@example.test","password":"initial-password"}`, "", false)
	require.Equal(t, http.StatusCreated, register.Code, register.Body.String())
	var registration types.RegisterResponse
	require.NoError(t, json.Unmarshal(register.Body.Bytes(), &registration))
	require.NotNil(t, registration.User)
	f.userID = registration.User.ID
	second := personalRequest(t, r, http.MethodPost, "/api/v1/auth/register", `{"username":"second","email":"second@example.test","password":"another-password"}`, "", false)
	require.Equal(t, http.StatusForbidden, second.Code, second.Body.String())
	var usersCount int64
	require.NoError(t, db.Model(&types.User{}).Count(&usersCount).Error)
	require.EqualValues(t, 1, usersCount)
	authConfig := personalRequest(t, r, http.MethodGet, "/api/v1/auth/config", "", "", false)
	require.Equal(t, http.StatusOK, authConfig.Code, authConfig.Body.String())
	require.Contains(t, authConfig.Body.String(), `"registration_enabled":false`)
	login := personalRequest(t, r, http.MethodPost, "/api/v1/auth/login", `{"email":"personal@example.test","password":"initial-password"}`, "", false)
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	var loggedIn types.LoginResponse
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &loggedIn))
	require.True(t, loggedIn.Success)
	f.token = loggedIn.Token
	created := personalRequest(t, r, http.MethodPost, "/api/v1/tenants/1/api-keys", `{"name":"personal integration"}`, f.token, false)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var createdKey struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdKey))
	f.key = createdKey.Data.Token
	require.NotEmpty(t, f.key)
	return f
}

func TestPersonalRegistrationAuthenticationAndManagement(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, path := range []string{"/api/v1/system/admin/settings", "/api/v1/system/admin/runtime/queues", "/api/v1/models", "/api/v1/knowledge-domains", "/api/v1/tenants/1/api-keys"} {
		for _, credential := range []string{"", "invalid-key"} {
			rec := personalRequest(t, f.router, http.MethodGet, path, "", credential, true)
			require.Equal(t, http.StatusUnauthorized, rec.Code, path+" "+rec.Body.String())
		}
		for _, apiKey := range []bool{false, true} {
			credential := f.token
			if apiKey {
				credential = f.key
			}
			rec := personalRequest(t, f.router, http.MethodGet, path, "", credential, apiKey)
			require.Equal(t, http.StatusOK, rec.Code, path+" "+rec.Body.String())
		}
	}
	for i, apiKey := range []bool{false, true} {
		credential := f.token
		if apiKey {
			credential = f.key
		}
		settings := personalRequest(t, f.router, http.MethodPut, "/api/v1/system/admin/settings/asynq.core_concurrency", `{"value":4}`, credential, apiKey)
		require.Equal(t, http.StatusOK, settings.Code, settings.Body.String())
		model := personalRequest(t, f.router, http.MethodPost, "/api/v1/models", fmt.Sprintf(`{"name":"test-model-%d","type":"KnowledgeQA","source":"remote","parameters":{}}`, i), credential, apiKey)
		require.Equal(t, http.StatusCreated, model.Code, model.Body.String())
		domain := personalRequest(t, f.router, http.MethodPost, "/api/v1/knowledge-domains", fmt.Sprintf(`{"name":"Personal domain %d","entity_types":["company"],"fields":[],"relations":[],"unit_families":[]}`, i), credential, apiKey)
		require.Equal(t, http.StatusCreated, domain.Code, domain.Body.String())
		var response struct {
			Data types.KnowledgeDomain `json:"data"`
		}
		require.NoError(t, json.Unmarshal(domain.Body.Bytes(), &response))
		edited := personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge-domains/"+response.Data.ID, fmt.Sprintf(`{"name":"Edited domain %d","entity_types":["company"],"fields":[{"key":"staff","label":"Staff","value_type":"number"}],"relations":[],"unit_families":[]}`, i), credential, apiKey)
		require.Equal(t, http.StatusOK, edited.Code, edited.Body.String())
	}
}

func seedPersonalContext(t *testing.T, f *personalAuthFixture) {
	t.Helper()
	now := time.Now().UTC()
	domain := service.BuiltinAIKnowledgeDomain()
	require.NoError(t, f.db.Create(domain).Error)
	for i := 1; i <= 2; i++ {
		kbID := fmt.Sprintf("personal-kb-%d", i)
		knowledgeID := fmt.Sprintf("personal-message-%d", i)
		evidenceID := fmt.Sprintf("personal-evidence-%d", i)
		entityID := fmt.Sprintf("personal-entity-%d", i)
		eventID := fmt.Sprintf("personal-event-%d", i)
		body := fmt.Sprintf("Acme %d released a product with %d staff.", i, i*10)
		kb := &types.KnowledgeBase{ID: kbID, TenantID: 1, Name: kbID, DomainID: domain.ID, CreatorID: f.userID, Type: types.KnowledgeBaseTypeDocument}
		kb.EnsureDefaults()
		require.NoError(t, f.db.Create(kb).Error)
		require.NoError(t, f.db.Create(&types.Knowledge{ID: knowledgeID, TenantID: 1, KnowledgeBaseID: kbID, Type: types.KnowledgeTypeManual, Title: "Acme source", FileName: knowledgeID, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: now, UpdatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEntity{ID: entityID, TenantID: 1, CanonicalName: fmt.Sprintf("Acme %d", i), NormalizedName: fmt.Sprintf("acme %d", i), EntityType: "company", CreatedAt: now, UpdatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEvidence{ID: evidenceID, TenantID: 1, KnowledgeBaseID: kbID, KnowledgeID: knowledgeID, Version: 1, BodyHash: strings.Repeat(fmt.Sprint(i), 64), Title: "Acme source", Body: body, Publisher: "News", OriginalURL: fmt.Sprintf("https://example.com/%d", i), PublishedAt: "2026-10-01T00:00:00Z", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: "completed", CreatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: evidenceID, EntityID: entityID}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeFact{ID: fmt.Sprintf("personal-fact-%d", i), TenantID: 1, KnowledgeBaseID: kbID, SubjectEntityID: entityID, Kind: types.KnowledgeFactAttribute, Predicate: "staff", OriginalPredicate: "staff", Value: types.JSON(fmt.Sprint(i * 10)), ValueType: "number", EvidenceID: evidenceID, Quote: body, Status: types.KnowledgeFactCurrent, CreatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEvent{ID: eventID, TenantID: 1, KnowledgeBaseID: kbID, Title: fmt.Sprintf("Acme %d product release", i), EventType: "release", Description: body, EvidenceIDs: types.StringArray{evidenceID}, SourceCount: 1, CreatedAt: now, UpdatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEventEvidence{ID: fmt.Sprintf("personal-link-%d", i), TenantID: 1, KnowledgeBaseID: kbID, EventID: eventID, EvidenceID: evidenceID, SourceTitle: "Acme source", SourceEventType: "release", SourceDescription: body, SourceKey: fmt.Sprint(i), CreatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEventEntity{TenantID: 1, EventID: eventID, EntityID: entityID}).Error)
		require.NoError(t, f.db.Create(&types.KnowledgeEventEvidenceEntity{TenantID: 1, EventID: eventID, EvidenceID: evidenceID, EntityID: entityID}).Error)
	}
}

func TestPersonalAPIKeyAndLoginReturnAllKnowledgeContext(t *testing.T) {
	f := newPersonalAuthFixture(t)
	seedPersonalContext(t, f)
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/knowledge/search?keyword=Acme", ""},
		{http.MethodGet, "/api/v1/knowledge-context/entities?scope=team", ""},
		{http.MethodGet, "/api/v1/knowledge-context/entities/personal-entity-2", ""},
		{http.MethodGet, "/api/v1/knowledge-context/events?scope=team", ""},
		{http.MethodPost, "/api/v1/knowledge-context/query", `{"scope":"team","include_history":true,"limit":50}`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			missing := personalRequest(t, f.router, tc.method, tc.path, tc.body, "", false)
			require.Equal(t, http.StatusUnauthorized, missing.Code)
			invalid := personalRequest(t, f.router, tc.method, tc.path, tc.body, "invalid-key", true)
			require.Equal(t, http.StatusUnauthorized, invalid.Code)
			login := personalRequest(t, f.router, tc.method, tc.path, tc.body, f.token, false)
			key := personalRequest(t, f.router, tc.method, tc.path, tc.body, f.key, true)
			require.Equal(t, http.StatusOK, login.Code, login.Body.String())
			require.Equal(t, http.StatusOK, key.Code, key.Body.String())
			require.JSONEq(t, login.Body.String(), key.Body.String())
			require.True(t, bytes.Contains(key.Body.Bytes(), []byte("personal-entity-2")) || bytes.Contains(key.Body.Bytes(), []byte("personal-message-2")))
			if tc.method == http.MethodPost {
				require.Contains(t, key.Body.String(), "personal-fact-2")
				require.Contains(t, key.Body.String(), "personal-event-2")
				require.Contains(t, key.Body.String(), "personal-evidence-2")
			}
		})
	}
}

func TestPersonalAPIKeyWithoutExpirationIsUsable(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for _, body := range []string{`{"name":"No expiration"}`, `{"name":"Zero expiration","expires_at_unix":0}`} {
		created := personalRequest(t, f.router, http.MethodPost, "/api/v1/tenants/1/api-keys", body, f.token, false)
		require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
		var response struct {
			Data struct {
				ID        uint64     `json:"id"`
				Token     string     `json:"token"`
				ExpiresAt *time.Time `json:"expires_at"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(created.Body.Bytes(), &response))
		require.Nil(t, response.Data.ExpiresAt)
		var stored types.TenantAPIKey
		require.NoError(t, f.db.First(&stored, response.Data.ID).Error)
		require.Nil(t, stored.ExpiresAt)
		queried := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-context/entities?scope=team", "", response.Data.Token, true)
		require.Equal(t, http.StatusOK, queried.Code, queried.Body.String())
	}
}
