package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
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
	params RouterParams
	token  string
	key    string
	userID string
}

func personalRequest(t *testing.T, r *gin.Engine, method, path, body, credential string, apiKey bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// Each test gets its own client IP so auth budgets do not leak across fixtures.
	client := crc32.ChecksumIEEE([]byte(t.Name()))
	req.RemoteAddr = fmt.Sprintf("192.%d.%d.%d:12345", byte(client>>16), byte(client>>8), byte(client))
	req.Host = "localhost"
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
	require.NoError(t, service.EnsurePersonalSpace(context.Background(), db))
	_, err := apprepo.EnsureSystemLibraries(context.Background(), db, types.SingleSpaceID, "")
	require.NoError(t, err)
	cfg := &config.Config{}
	spaceRepo := apprepo.NewSpaceRepository(db)
	spaceSvc := service.NewSpaceService(spaceRepo)
	users := service.NewUserService(cfg, apprepo.NewUserRepository(db), spaceSvc)
	keys := service.NewAPIKeyService(apprepo.NewAPIKeyRepository(db))
	entryRepo := apprepo.NewEntryRepository(db)
	libraryRepo := apprepo.NewLibraryRepository(db)
	library := service.NewLibraryService(db, libraryRepo, entryRepo, nil, nil, nil, nil)
	models := service.NewModelService(apprepo.NewModelRepository(db), libraryRepo, nil, nil, spaceSvc)
	entry, err := service.NewEntryService(db, cfg, entryRepo, library, spaceRepo, spaceSvc,
		nil, nil, nil, nil, models, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	settings := service.NewSystemSettingService(apprepo.NewSystemSettingRepository(db), cfg)
	system := handler.NewSystemHandler(cfg, spaceSvc, users, settings, keys, nil, nil)
	params := RouterParams{
		Config: cfg, UserService: users, SpaceService: spaceSvc,
		APIKeyService: keys, LibraryService: library, EntryService: entry,
		LibraryHandler: handler.NewLibraryHandler(library),
		EntryHandler:   handler.NewEntryHandler(cfg, entry, library, nil, nil),
		SearchHandler:  &handler.SearchHandler{},
		ModelHandler:   handler.NewModelHandler(models),
		EntityHandler:  handler.NewEntityHandler(service.NewEntityService(db, nil)),
		SpaceHandler:   handler.NewSpaceHandler(spaceSvc, keys, users, library, cfg, settings),
		AuthHandler:    handler.NewAuthHandler(users, spaceSvc), SystemHandler: system,
	}
	guards := newAccessGuards(library, entry, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), middleware.Auth(spaceSvc, users, keys))
	v1 := r.Group("/api/v1")
	RegisterAuthRoutes(v1, params.AuthHandler, guards)
	RegisterSpaceRoutes(v1, params.SpaceHandler, guards)
	RegisterSystemRoutes(v1, system, guards)
	RegisterModelRoutes(v1, params.ModelHandler, nil, guards)
	RegisterEntityRoutes(v1, params.EntityHandler)
	RegisterEntryRoutes(v1, params.EntryHandler, guards)
	RegisterLibraryRoutes(v1, params.LibraryHandler, guards)
	f := &personalAuthFixture{db: db, router: r, params: params}
	sole, err := users.EnsureSoleUser(context.Background())
	require.NoError(t, err)
	f.userID = sole.ID
	created := personalRequest(t, r, http.MethodPost, "/api/v1/space/api-keys", `{"name":"personal integration"}`, "", false)
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

func TestPersonalLocalAccessAndAPIKeys(t *testing.T) {
	f := newPersonalAuthFixture(t)
	var usersCount int64
	require.NoError(t, f.db.Model(&types.User{}).Count(&usersCount).Error)
	require.EqualValues(t, 1, usersCount)
	for _, path := range []string{"/api/v1/system/settings", "/api/v1/system/runtime/queues", "/api/v1/models", "/api/v1/space/api-keys", "/api/v1/auth/me"} {
		rec := personalRequest(t, f.router, http.MethodGet, path, "", "invalid-key", true)
		require.Equal(t, http.StatusUnauthorized, rec.Code, path+" "+rec.Body.String())
		for _, credential := range []string{"", f.key} {
			rec := personalRequest(t, f.router, http.MethodGet, path, "", credential, credential != "")
			require.Equal(t, http.StatusOK, rec.Code, path+" "+rec.Body.String())
		}
	}
	for i, credential := range []string{"", f.key} {
		settings := personalRequest(t, f.router, http.MethodPut, "/api/v1/system/settings/queue.core_concurrency", `{"value":4}`, credential, credential != "")
		require.Equal(t, http.StatusOK, settings.Code, settings.Body.String())
		model := personalRequest(t, f.router, http.MethodPost, "/api/v1/models", fmt.Sprintf(`{"name":"test-model-%d","type":"KnowledgeQA","source":"remote","parameters":{}}`, i), credential, credential != "")
		require.Equal(t, http.StatusCreated, model.Code, model.Body.String())
	}
}

func TestCredentialLessAccessRejectsCrossSiteAndRemoteHosts(t *testing.T) {
	f := newPersonalAuthFixture(t)
	for name, mutate := range map[string]func(*http.Request){
		"cross-site page": func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"rebinding host":  func(r *http.Request) { r.Host = "attacker.example:8080" },
		"lan address":     func(r *http.Request) { r.Host = "192.168.1.20" },
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Host = "localhost"
		mutate(req)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, name)
	}
	for _, host := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Host = host
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, host)
	}
}

func seedPersonalEntities(t *testing.T, f *personalAuthFixture) {
	t.Helper()
	now := time.Now().UTC()
	library, err := f.params.LibraryService.GetSystemLibrary(context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID))
	require.NoError(t, err)
	for i := 1; i <= 2; i++ {
		libraryID := library.ID
		entryID := fmt.Sprintf("personal-message-%d", i)
		entityID := fmt.Sprintf("personal-entity-%d", i)
		require.NoError(t, f.db.Create(&types.Entry{ID: entryID, SpaceID: 1, LibraryID: libraryID, Type: types.EntryTypeManual, Title: "Acme source", FileName: entryID, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: now, UpdatedAt: now}).Error)
		require.NoError(t, f.db.Create(&types.Entity{ID: entityID, SpaceID: 1, LibraryID: libraryID, CanonicalName: fmt.Sprintf("Acme %d", i), NormalizedName: fmt.Sprintf("acme %d", i), EntityType: "company", CreatedAt: now, UpdatedAt: now}).Error)

		require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entryID, EntityID: entityID, CreatedAt: now}).Error)
	}
}

func TestPersonalAPIKeyAndLocalUserReturnAllEntities(t *testing.T) {
	f := newPersonalAuthFixture(t)
	seedPersonalEntities(t, f)
	library, err := f.params.LibraryService.GetSystemLibrary(context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID))
	require.NoError(t, err)
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/entries/search?keyword=Acme", ""},
		{http.MethodGet, "/api/v1/entities?library_id=" + library.ID, ""},
		{http.MethodGet, "/api/v1/entities/personal-entity-2", ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			invalid := personalRequest(t, f.router, tc.method, tc.path, tc.body, "invalid-key", true)
			require.Equal(t, http.StatusUnauthorized, invalid.Code)
			login := personalRequest(t, f.router, tc.method, tc.path, tc.body, f.token, false)
			key := personalRequest(t, f.router, tc.method, tc.path, tc.body, f.key, true)
			require.Equal(t, http.StatusOK, login.Code, login.Body.String())
			require.Equal(t, http.StatusOK, key.Code, key.Body.String())
			require.JSONEq(t, login.Body.String(), key.Body.String())
			require.True(t, bytes.Contains(key.Body.Bytes(), []byte("personal-entity-2")) || bytes.Contains(key.Body.Bytes(), []byte("personal-message-2")))
		})
	}
}

func TestEntityRoutesIgnoreRemovedScopeParameter(t *testing.T) {
	f := newPersonalAuthFixture(t)
	seedPersonalEntities(t, f)
	library, err := f.params.LibraryService.GetSystemLibrary(context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID))
	require.NoError(t, err)
	expected := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities?library_id="+library.ID, "", f.token, false)
	require.Equal(t, http.StatusOK, expected.Code, expected.Body.String())
	for _, query := range []string{"scope=team", "scope=library", "scope=all"} {
		got := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities?library_id="+library.ID+"&"+query, "", f.key, true)
		require.Equal(t, http.StatusOK, got.Code, got.Body.String())
		require.JSONEq(t, expected.Body.String(), got.Body.String())
	}
}

func TestPersonalAPIKeyWithoutExpirationIsUsable(t *testing.T) {
	f := newPersonalAuthFixture(t)
	library, err := f.params.LibraryService.GetSystemLibrary(context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID))
	require.NoError(t, err)
	for _, body := range []string{`{"name":"No expiration"}`, `{"name":"Zero expiration","expires_at_unix":0}`} {
		created := personalRequest(t, f.router, http.MethodPost, "/api/v1/space/api-keys", body, f.token, false)
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
		var stored types.APIKey
		require.NoError(t, f.db.First(&stored, response.Data.ID).Error)
		require.Nil(t, stored.ExpiresAt)
		queried := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities?library_id="+library.ID, "", response.Data.Token, true)
		require.Equal(t, http.StatusOK, queried.Code, queried.Body.String())
	}
}
