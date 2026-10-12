package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/datasource"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestRestoredDataSourcePageRequestsPreserveUneditedJSON(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer replacement", r.Header.Get("Authorization"))
		w.Write([]byte(`<rss version="2.0"><channel><title>Feed</title><link>https://example.test</link><description>Fixture</description></channel></rss>`))
	}))
	defer feed.Close()
	for _, resources := range []string{"null", "[]", "[\"original\"]", "absent", "sql-null-state"} {
		t.Run(resources, func(t *testing.T) {
			db := pgtest.Open(t)
			ctx := context.Background()
			repo := repository.NewDataSourceRepository(db)
			config := map[string]any{"credentials": nil, "settings": map[string]any{"feed_urls": feed.URL, "publisher": nil, "article_path_prefix": nil, "extra": map[string]any{"null": nil, "array": []any{}}}, "extension": nil, "_reliquary_maintenance_original_status": "active", "_reliquary_maintenance_required_fields": `["config.credentials.auth_headers"]`}
			if resources != "absent" && resources != "sql-null-state" {
				var value any
				require.NoError(t, json.Unmarshal([]byte(resources), &value))
				config["resource_ids"] = value
			}
			blob, err := json.Marshal(config)
			require.NoError(t, err)
			ds := &types.DataSource{ID: "restored", KnowledgeBaseID: "kb", SpaceID: 1, Name: "Private RSS", Type: "rss", Status: "paused", SyncSchedule: "", SyncMode: "incremental", SyncDeletions: false, Config: blob, LastSyncCursor: types.JSON(`{"null":null,"array":[]}`), LastSyncResult: types.JSON(`null`)}
			if resources == "sql-null-state" {
				ds.LastSyncCursor = nil
				ds.LastSyncResult = nil
			}
			require.NoError(t, repo.Create(ctx, ds))
			registry := datasource.NewConnectorRegistry()
			require.NoError(t, registry.Register(rss.NewConnector()))
			svc := service.NewDataSourceService(db, repo, nil, nil, nil, nil, registry, datasource.NewScheduler(repo, nil, nil), nil)
			kb := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				return &types.KnowledgeBase{ID: "kb", SpaceID: 1}, nil
			}}
			h := NewDataSourceHandler(svc, kb)
			credentials := NewDataSourceCredentialsHandler(svc, kb)
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(errorCapture())
			r.Use(func(c *gin.Context) { c.Set(types.SpaceIDContextKey.String(), uint64(1)); c.Next() })
			r.PUT("/api/v1/data-sources/:id", h.UpdateDataSource)
			r.PUT("/api/v1/data-sources/:id/credentials", credentials.Put)
			before, err := maintenance.BuildManifest(ctx, db, t.TempDir(), "")
			require.NoError(t, err)
			submit := func(path, body string) {
				request := httptest.NewRequest("PUT", path, bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)
				require.Equal(t, 200, response.Code, response.Body.String())
			}
			// Exact editor request when only the credential field is edited.
			submit("/api/v1/data-sources/restored/credentials", `{"credentials":{"auth_headers":"Authorization: Bearer replacement"}}`)
			stored, err := repo.FindByID(ctx, ds.ID)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, json.Unmarshal(stored.Config, &actual))
			for _, key := range []string{"type", "resource_ids", "settings", "extension"} {
				want, present := config[key]
				got, found := actual[key]
				require.Equal(t, present, found, key)
				require.Equal(t, want, got, key)
			}
			after, err := maintenance.BuildManifest(ctx, db, t.TempDir(), "")
			require.NoError(t, err)
			require.Empty(t, maintenance.CompareManifestWithSettings(before, after, maintenance.CompareOptions{AllowReconfigurationAppend: true}))
			// Actual editor patch for a separate deliberate name edit.
			submit("/api/v1/data-sources/restored", `{"name":"Renamed feed"}`)
			stored, err = repo.FindByID(ctx, ds.ID)
			require.NoError(t, err)
			require.Equal(t, "Renamed feed", stored.Name)
			require.False(t, stored.SyncDeletions)
			if resources == "sql-null-state" {
				var cursorNull, resultNull bool
				require.NoError(t, db.Raw(`SELECT last_sync_cursor IS NULL FROM data_sources WHERE id='restored'`).Scan(&cursorNull).Error)
				require.NoError(t, db.Raw(`SELECT last_sync_result IS NULL FROM data_sources WHERE id='restored'`).Scan(&resultNull).Error)
				require.True(t, cursorNull)
				require.True(t, resultNull)
			} else {
				require.JSONEq(t, `{"null":null,"array":[]}`, string(stored.LastSyncCursor))
				require.JSONEq(t, `null`, string(stored.LastSyncResult))
			}
			var renamed map[string]any
			require.NoError(t, json.Unmarshal(stored.Config, &renamed))
			require.Equal(t, actual, renamed)
			require.NoError(t, db.Exec(`UPDATE data_sources SET name='Private RSS' WHERE id='restored'`).Error)
			reverted, err := maintenance.BuildManifest(ctx, db, t.TempDir(), "")
			require.NoError(t, err)
			require.Empty(t, maintenance.CompareManifestWithSettings(before, reverted, maintenance.CompareOptions{AllowReconfigurationAppend: true}))
			// A real null-to-array modification is still a verification failure.
			replacement := `[]`
			if resources == `[]` {
				replacement = `null`
			}
			require.NoError(t, db.Exec(`UPDATE data_sources SET config=jsonb_set(config,'{resource_ids}',?::jsonb) WHERE id='restored'`, replacement).Error)
			bad, err := maintenance.BuildManifest(ctx, db, t.TempDir(), "")
			require.NoError(t, err)
			require.Contains(t, maintenance.CompareManifestWithSettings(before, bad, maintenance.CompareOptions{AllowReconfigurationAppend: true}), "table data_sources")
		})
	}
}
