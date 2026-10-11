package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/chatgpt"
	"reliquary/internal/maintenance"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type modelMaintenanceTransport func(*http.Request) (*http.Response, error)

func (f modelMaintenanceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSnapshotChatGPTModelDebugAuditsRealQuotaIncludingFailedCalls(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("SSRF_WHITELIST", "api.openai.com,127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db := pgtest.Open(t)
	ctx := context.Background()
	_, err := repository.EnsureSpaceKnowledgeBase(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	credential, err := utils.EncryptAESGCM(`{"access_token":"debug-access-secret","refresh_token":"debug-refresh-secret","id_token":"debug-id-secret"}`, utils.GetAESKey())
	require.NoError(t, err)
	accountID := uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,credentials,status,expires_at) VALUES(?,'debug-client','https://auth.openai.com','debug-subject',?,'connected',?)`, accountID, credential, time.Now().Add(time.Hour)).Error)
	model := &types.Model{ID: "maintenance-debug", SpaceID: 1, Name: "fixture", Type: types.ModelTypeGeneration, Source: types.ModelSourceChatGPT, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{Provider: "chatgpt", InterfaceType: "responses", ExtraConfig: map[string]string{"chatgpt_account_id": accountID}}}
	require.NoError(t, db.Create(model).Error)
	limit := 10
	require.NoError(t, db.Create(&types.ChatGPTPolicy{ID: 1, DailyLimit: &limit, DailyUsed: 2, Day: time.Now().UTC().Format(time.DateOnly), UpdatedAt: time.Now().Add(-time.Hour)}).Error)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	var fail atomic.Bool
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer debug-access-secret", r.Header.Get("Authorization"))
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fail.Load() {
			_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"debug failure\"}}}\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"))
	}))
	defer upstream.Close()
	endpoint, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	models := service.NewModelServiceWithChatGPT(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, db, chatgpt.NewManager(db), service.NewChatGPTPolicyService(db))
	models.(interface{ SetChatGPTTransport(*http.Client) }).SetChatGPTTransport(&http.Client{Transport: modelMaintenanceTransport(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		address := *request.URL
		address.Scheme, address.Host = endpoint.Scheme, endpoint.Host
		copy.URL = &address
		return upstream.Client().Transport.RoundTrip(copy)
	})})
	handler := NewModelHandlerWithMaintenance(models, db)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1)))
	})
	router.POST("/models/:id/debug", handler.DebugModel)
	invoke := func() bool {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("input", "Maintenance test"))
		require.NoError(t, writer.Close())
		request := httptest.NewRequest(http.MethodPost, "/models/maintenance-debug/debug", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		for _, secret := range []string{"debug-access-secret", "debug-refresh-secret", "debug-id-secret", credential} {
			require.NotContains(t, response.Body.String(), secret)
		}
		var result struct {
			Data struct {
				OK bool `json:"ok"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result.Data.OK
	}
	before, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.True(t, invoke())
	fail.Store(true)
	require.False(t, invoke())
	after, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("4"), after[0]["daily_used"])
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2)
	encode := func(value any) string { data, err := json.Marshal(value); require.NoError(t, err); return string(data) }
	require.JSONEq(t, encode(before), string(logs[0].Before))
	require.JSONEq(t, string(logs[0].After), string(logs[1].Before))
	require.JSONEq(t, encode(after), string(logs[1].After))
	require.Equal(t, model.ID, logs[0].ObjectID)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	fail.Store(false)
	require.True(t, invoke())
	require.EqualValues(t, 3, calls.Load())
	after, err = maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("5"), after[0]["daily_used"])
	var count int64
	require.NoError(t, db.Model(&types.OperationLog{}).Where("operation_type='maintenance_model_probe'").Count(&count).Error)
	require.EqualValues(t, 2, count, "normal model tests retain their original accounting without maintenance audit rows")
}
