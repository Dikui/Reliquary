package container

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestMaintenanceModelProbeUsesLiveCredentialsAndBothModalities(t *testing.T) {
	db := pgtest.Open(t)
	_, err := repository.EnsureSpaceLibrary(context.Background(), db, types.SingleSpaceID, "")
	require.NoError(t, err)
	t.Setenv("OLLAMA_OPTIONAL", "true")
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer model-probe-fixture", r.Header.Get("Authorization"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		call := calls.Add(1)
		if call == 2 {
			require.Contains(t, string(mustJSON(payload["input"])), "data:image/png;base64,")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0,0]}],"usage":{"total_tokens":1}}`))
	}))
	defer upstream.Close()
	model := &types.Model{ID: "maintenance-probe", SpaceID: types.SingleSpaceID, Name: "fixture", Type: types.ModelTypeEmbedding,
		Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{APIKey: "model-probe-fixture", BaseURL: upstream.URL + "/v1", InterfaceType: "openai", Provider: "generic",
			EmbeddingParameters: types.EmbeddingParameters{Dimension: 4, SupportsImages: true, MultimodalFormat: "jina"}}}
	require.NoError(t, db.Create(model).Error)
	result, err := CheckMaintenanceModels(context.Background(), db, model.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Contains(t, string(mustJSON(result)), `"ready":true`)
	require.NotContains(t, string(mustJSON(result)), model.Parameters.APIKey)
	require.NoError(t, db.Model(model).Update("status", "needs_credentials").Error)
	result, err = CheckMaintenanceModels(context.Background(), db, model.ID)
	require.Error(t, err)
	require.Contains(t, string(mustJSON(result)), "paused")
	require.EqualValues(t, 2, calls.Load(), "paused model must never call its endpoint")
}

func TestMaintenanceModelProbeRejectsInvalidVectorsAndRedactsProviderErrors(t *testing.T) {
	for _, vector := range [][]float32{{}, {0, 0}, {float32(math.Inf(1)), 1}, {float32(math.NaN()), 1}, {1}} {
		require.Error(t, validateMaintenanceVector(vector, 2))
	}
	require.NoError(t, validateMaintenanceVector([]float32{1, 0}, 2))
	model := &types.Model{Parameters: types.ModelParameters{APIKey: "sensitive-fixture", AppSecret: "signing-fixture", CustomHeaders: map[string]string{"Authorization": "custom-fixture"}}}
	message := maintenanceModelError(errors.New("sensitive-fixture signing-fixture custom-fixture "+strings.Repeat("x", 800)), model)
	require.NotContains(t, message, "sensitive-fixture")
	require.NotContains(t, message, "signing-fixture")
	require.NotContains(t, message, "custom-fixture")
	require.LessOrEqual(t, len(message), 600)
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

type maintenanceChatGPTTransport func(*http.Request) (*http.Response, error)

func (f maintenanceChatGPTTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestMaintenanceChatGPTProbePreservesRealQuotaAndAppendsExactAuditChain(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	t.Setenv("SSRF_WHITELIST", "api.openai.com,127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db := pgtest.Open(t)
	ctx := context.Background()
	_, err := repository.EnsureSpaceLibrary(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	credential, err := utils.EncryptAESGCM(`{"access_token":"probe-access-token","refresh_token":"probe-refresh-token","id_token":"probe-id-token"}`, utils.GetAESKey())
	require.NoError(t, err)
	accountID := "00000000-0000-0000-0000-000000000015"
	require.NoError(t, db.Exec(`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,credentials,status,expires_at) VALUES(?,?,?,?,?,'connected',?)`, accountID, "probe-client", "https://auth.openai.com", "probe-subject", credential, time.Now().Add(time.Hour)).Error)
	model := &types.Model{ID: "maintenance-chatgpt-probe", SpaceID: types.SingleSpaceID, Name: "fixture", Type: types.ModelTypeGeneration,
		Source: types.ModelSourceChatGPT, Status: types.ModelStatusActive, Parameters: types.ModelParameters{Provider: "chatgpt", InterfaceType: "responses", ExtraConfig: map[string]string{"chatgpt_account_id": accountID}}}
	require.NoError(t, db.Create(model).Error)
	limit := 10
	require.NoError(t, db.Create(&types.ChatGPTPolicy{ID: 1, DailyLimit: &limit, DailyUsed: 2, Day: time.Now().UTC().Format(time.DateOnly), UpdatedAt: time.Now().Add(-time.Hour)}).Error)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	var fail atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer probe-access-token", r.Header.Get("Authorization"))
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fail.Load() {
			_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"probe failure\"}}}\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := &http.Client{Transport: maintenanceChatGPTTransport(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		address := *request.URL
		address.Scheme, address.Host = endpoint.Scheme, endpoint.Host
		copy.URL = &address
		return server.Client().Transport.RoundTrip(copy)
	})}
	before, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	_, err = checkMaintenanceModels(ctx, db, model.ID, client)
	require.NoError(t, err)
	after, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, "maintenance", logs[0].Actor)
	require.Equal(t, model.ID, logs[0].ObjectID)
	require.JSONEq(t, string(mustJSON(before)), string(logs[0].Before))
	require.JSONEq(t, string(mustJSON(after)), string(logs[0].After))
	require.Equal(t, json.Number("3"), after[0]["daily_used"], "the actual model call must consume quota")
	for _, secret := range []string{"probe-access-token", "probe-refresh-token", "probe-id-token", credential} {
		require.NotContains(t, string(mustJSON(logs)), secret)
	}
	fail.Store(true)
	_, err = checkMaintenanceModels(ctx, db, model.ID, client)
	require.Error(t, err)
	final, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("4"), final[0]["daily_used"], "failed real calls also retain consumed quota")
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2)
	require.JSONEq(t, string(logs[0].After), string(logs[1].Before))
	require.JSONEq(t, string(mustJSON(final)), string(logs[1].After))
	require.True(t, logs[1].CreatedAt.After(logs[0].CreatedAt))
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	fail.Store(false)
	_, err = checkMaintenanceModels(ctx, db, model.ID, client)
	require.NoError(t, err)
	final, err = maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("5"), final[0]["daily_used"])
	require.EqualValues(t, 3, calls.Load())
	var auditCount int64
	require.NoError(t, db.Model(&types.OperationLog{}).Where("operation_type='maintenance_model_probe'").Count(&auditCount).Error)
	require.EqualValues(t, 2, auditCount, "normal probes preserve existing accounting without maintenance audit rows")
}

func TestMaintenancePolicyProbeAuditsCreationAndCancellationWithoutReversingQuota(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	policy := service.NewChatGPTPolicyService(db)
	err := maintenance.WithChatGPTPolicyProbeAudit(ctx, db, "probe-model", func() error {
		_, err := policy.Reserve(ctx)
		return err
	})
	require.NoError(t, err)
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.JSONEq(t, `[]`, string(logs[0].Before), "a previously absent policy must have an explicit empty before array")
	after, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.JSONEq(t, string(mustJSON(after)), string(logs[0].After))
	unchanged := errors.New("probe failed before reserving quota")
	err = maintenance.WithChatGPTPolicyProbeAudit(ctx, db, "probe-model", func() error { return unchanged })
	require.ErrorIs(t, err, unchanged)
	var count int64
	require.NoError(t, db.Model(&types.OperationLog{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "no policy change means no invented audit transition")
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	err = maintenance.WithChatGPTPolicyProbeAudit(probeCtx, db, "probe-model", func() error {
		_, err := policy.Reserve(probeCtx)
		cancel()
		return errors.Join(err, probeCtx.Err())
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2, "cancellation after a real reservation must still record its accounting")
	require.JSONEq(t, string(logs[0].After), string(logs[1].Before))
	after, err = maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("2"), after[0]["daily_used"])
	require.JSONEq(t, string(mustJSON(after)), string(logs[1].After))
}

func TestOfflineChatGPTModelProbeWithoutMaintenanceStateAuditsRealQuotaWithoutMigration(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	t.Setenv("SSRF_WHITELIST", "api.openai.com,127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP TABLE maintenance_state").Error)
	t.Cleanup(func() {
		sourceErr, databaseErr := migration.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	ctx := context.Background()
	_, err = repository.EnsureSpaceLibrary(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	credential, err := utils.EncryptAESGCM(`{"access_token":"offline-probe-access","refresh_token":"offline-probe-refresh","id_token":"offline-probe-id"}`, utils.GetAESKey())
	require.NoError(t, err)
	accountID := "00000000-0000-0000-0000-000000000016"
	require.NoError(t, db.Exec(`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,credentials,status,expires_at) VALUES(?,?,?,?,?,'connected',?)`, accountID, "offline-probe-client", "https://auth.openai.com", "offline-probe-subject", credential, time.Now().Add(time.Hour)).Error)
	model := &types.Model{ID: "offline-probe-model", SpaceID: 1, Name: "fixture", Type: types.ModelTypeGeneration, Source: types.ModelSourceChatGPT, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{Provider: "chatgpt", InterfaceType: "responses", ExtraConfig: map[string]string{"chatgpt_account_id": accountID}}}
	require.NoError(t, db.Create(model).Error)
	var calls atomic.Int32
	client := &http.Client{Transport: maintenanceChatGPTTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer offline-probe-access", request.Header.Get("Authorization"))
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n")), Request: request}, nil
	})}
	_, err = checkMaintenanceModels(ctx, db, model.ID, client)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type='maintenance_model_probe'").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.JSONEq(t, `[]`, string(logs[0].Before))
	after, err := maintenance.CaptureChatGPTPolicy(ctx, db)
	require.NoError(t, err)
	require.Equal(t, json.Number("1"), after[0]["daily_used"])
	require.JSONEq(t, string(mustJSON(after)), string(logs[0].After))
	for _, secret := range []string{"offline-probe-access", "offline-probe-refresh", "offline-probe-id", credential} {
		require.NotContains(t, string(mustJSON(logs)), secret)
	}
	version, dirty, err := migration.Version()
	require.NoError(t, err)
	require.EqualValues(t, 9, version)
	require.False(t, dirty)
	require.False(t, db.Migrator().HasTable("maintenance_state"))
}
