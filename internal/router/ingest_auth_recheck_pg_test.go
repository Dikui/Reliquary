package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const ingestAuthCheckBody = `{"urls":["https://example.org/auth-recheck"]}`
const ingestAuthBatchBody = `{"items":[{"url":"https://example.org/auth-recheck","content":"Complete original measurements.","acquisition":"extracted","publisher":"Fixture Journal"}]}`
const ingestAuthMCPBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check_urls","arguments":{"urls":["https://example.org/auth-recheck"]}}}`

func ingestAuthMCPRequest(router *gin.Engine, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", "2025-11-25")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func mutateIngestAuthKey(db *gorm.DB, state string) error {
	switch state {
	case "revoked":
		return db.Model(&types.APIKey{}).Where("name=?", "personal integration").Update("revoked_at", time.Now()).Error
	case "expired":
		return db.Model(&types.APIKey{}).Where("name=?", "personal integration").Update("expires_at", time.Now().Add(-time.Hour)).Error
	case "deleted":
		return db.Where("name=?", "personal integration").Delete(&types.APIKey{}).Error
	default:
		return nil
	}
}

func ingestAuthHTTPRouter(f *mcpFixture) *gin.Engine {
	router := gin.New()
	router.Use(middleware.ErrorHandler(), middleware.Auth(f.params.SpaceService, f.params.UserService, f.params.APIKeyService))
	entries := f.articles.(interfaces.EntryService)
	RegisterEntryRoutes(router.Group("/api/v1"), handler.NewEntryHandler(nil, entries, f.params.LibraryService, f.queue, nil), newAccessGuards(f.params.LibraryService, entries, nil))
	return router
}

func requireIngestAuthResponse(t *testing.T, response *httptest.ResponseRecorder, state string) {
	t.Helper()
	status, code := http.StatusUnauthorized, apperrors.ErrUnauthorized
	if state == "unavailable" {
		status, code = http.StatusServiceUnavailable, apperrors.ErrServiceUnavailable
		require.Equal(t, "5", response.Header().Get("Retry-After"))
	} else {
		require.Empty(t, response.Header().Get("Retry-After"))
	}
	require.Equal(t, status, response.Code, response.Body.String())
	var body struct {
		Success bool               `json:"success"`
		Error   apperrors.AppError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.False(t, body.Success)
	require.Equal(t, code, body.Error.Code)
	require.NotContains(t, response.Body.String(), "unavailable_expires_at")
	require.NotContains(t, response.Body.String(), "SQLSTATE")
}

func TestIngestServiceCredentialRecheckHTTPAndMCP(t *testing.T) {
	for _, protocol := range []string{"HTTP", "MCP"} {
		for _, state := range []string{"unavailable", "revoked", "expired", "deleted"} {
			t.Run(protocol+"/"+state, func(t *testing.T) {
				f := newMCPFixture(t)
				middlewareReads, serviceReads := 0, 0
				var mutationErr, middlewareErr, serviceErr error
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("ingest_auth_query_failure", func(tx *gorm.DB) {
					if state == "unavailable" && middlewareReads > 0 && tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
						// PostgreSQL executes this query and reports an undefined
						// column. Middleware's SELECT * remains valid.
						tx.Statement.Selects = append(tx.Statement.Selects, "unavailable_expires_at")
					}
				}))
				t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_query_failure")) })
				require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("ingest_auth_recheck", func(tx *gorm.DB) {
					if tx.Statement.Table != "api_keys" {
						return
					}
					if len(tx.Statement.Selects) == 0 {
						middlewareReads++
						middlewareErr = tx.Error
						if middlewareReads == 1 && tx.Error == nil {
							mutationErr = mutateIngestAuthKey(f.db, state)
						}
					} else {
						serviceReads++
						serviceErr = tx.Error
					}
				}))
				t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_recheck")) })
				var response *httptest.ResponseRecorder
				if protocol == "MCP" {
					response = ingestAuthMCPRequest(f.router, f.key, ingestAuthMCPBody)
				} else {
					response = personalRequest(t, ingestAuthHTTPRouter(f), http.MethodPost, "/api/v1/ingest/check", ingestAuthCheckBody, f.key, false)
				}
				require.NoError(t, mutationErr)
				require.NoError(t, middlewareErr)
				require.Equal(t, 1, middlewareReads)
				require.Equal(t, 1, serviceReads)
				if state == "unavailable" {
					var pgErr *pgconn.PgError
					require.True(t, errors.As(serviceErr, &pgErr))
					require.Equal(t, "42703", pgErr.Code)
				}
				requireIngestAuthResponse(t, response, state)
			})
		}
	}
}

func TestIngestHTTPBatchDoesNotHideItemCredentialFailures(t *testing.T) {
	for _, state := range []string{"unavailable", "revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			f := newMCPFixture(t)
			serviceReads := 0
			var mutationErr error
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("ingest_auth_batch_failure", func(tx *gorm.DB) {
				if state == "unavailable" && serviceReads > 0 && tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
					tx.Statement.Selects = append(tx.Statement.Selects, "unavailable_expires_at")
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_batch_failure")) })
			require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("ingest_auth_batch_recheck", func(tx *gorm.DB) {
				if tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
					serviceReads++
					if serviceReads == 1 && tx.Error == nil {
						mutationErr = mutateIngestAuthKey(f.db, state)
					}
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_batch_recheck")) })
			response := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodPost, "/api/v1/ingest", ingestAuthBatchBody, f.key, false)
			require.NoError(t, mutationErr)
			require.Equal(t, 2, serviceReads)
			requireIngestAuthResponse(t, response, state)
			require.NotContains(t, response.Body.String(), `"status":"invalid"`)
		})
	}
}

func TestIngestHTTPAssetAndCompletionCredentialFailures(t *testing.T) {
	f := newMCPFixture(t)
	router := ingestAuthHTTPRouter(f)
	article := f.seedArticle(t, "Auth completion fixture", types.ProcessingModeFull, "# Original measurements\n\nA complete source body.\n")
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("ingest_auth_asset_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
			tx.Statement.Selects = append(tx.Statement.Selects, "unavailable_expires_at")
		}
	}))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_asset_failure")) })
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/ingest", ingestAuthBatchBody},
		{http.MethodPost, "/api/v1/ingest/check", ingestAuthCheckBody},
		{http.MethodGet, "/api/v1/ingest/incomplete", ""},
		{http.MethodPost, "/api/v1/ingest/" + article.ID + "/accept-incomplete", ""},
		{http.MethodPost, "/api/v1/ingest/" + article.ID + "/discard", `{}`},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			response := personalRequest(t, router, endpoint.method, endpoint.path, endpoint.body, f.key, false)
			requireIngestAuthResponse(t, response, "unavailable")
		})
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "original.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("Complete original source."))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/assets", &body)
	request.Header.Set("Authorization", "Bearer "+f.key)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	requireIngestAuthResponse(t, response, "unavailable")
}

func TestMCPUsesOneSharedCredentialRecheckForToolCall(t *testing.T) {
	for _, tool := range []struct{ name, body, status string }{
		{"check_urls", ingestAuthMCPBody, "not_found"},
		{"ingest", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ingest","arguments":` + ingestAuthBatchBody + `}}`, "accepted"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			f := newMCPFixture(t)
			serviceReads := 0
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("ingest_auth_shared_failure", func(tx *gorm.DB) {
				if serviceReads > 0 && tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
					tx.Statement.Selects = append(tx.Statement.Selects, "unavailable_expires_at")
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_shared_failure")) })
			require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("ingest_auth_shared_check", func(tx *gorm.DB) {
				if tx.Statement.Table == "api_keys" && len(tx.Statement.Selects) != 0 {
					serviceReads++
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove("ingest_auth_shared_check")) })
			response := ingestAuthMCPRequest(f.router, f.key, tool.body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, 1, serviceReads)
			require.Contains(t, response.Body.String(), `"status":"`+tool.status+`"`)
			// The next request must perform its own durable check.
			response = ingestAuthMCPRequest(f.router, f.key, tool.body)
			require.Equal(t, 2, serviceReads)
			requireIngestAuthResponse(t, response, "unavailable")
		})
	}
}
