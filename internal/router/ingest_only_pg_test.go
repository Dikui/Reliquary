package router

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPIngestBatchSeparatesInvalidItemsAndEnforcesLimits(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	var batch types.IngestBatchResult
	mcpCall(t, client, "ingest", map[string]any{"items": []any{
		map[string]any{"url": "https://example.org/first-batch", "content": "# Complete batch\n\nMeasured original facts for the ingestion interface.", "library": "institutional", "acquisition": "extracted", "publisher": "Fixture Journal"},
		map[string]any{"url": "https://example.org/first-batch", "content": "# Complete batch\n\nMeasured original facts for the ingestion interface.", "library": "institutional", "acquisition": "extracted", "publisher": "Fixture Journal"},
		map[string]any{"url": "https://example.org/missing-acquisition", "content": "Original text"},
		map[string]any{"url": "https://example.org/wrong-type", "content": 42, "library": "institutional", "acquisition": "extracted"},
		map[string]any{"url": "https://example.org/unknown-field", "content": "Original text", "library": "institutional", "acquisition": "extracted", "unknown": true},
		map[string]any{"url": "https://example.org/rejectfixture-batch", "content": "rejectfixture source facts", "library": "institutional", "acquisition": "model_written", "publisher": "Fixture Journal"},
	}}, &batch)
	require.Len(t, batch.Items, 6)
	for index, status := range []string{"accepted", "exists", "invalid", "invalid", "invalid", "pending_review"} {
		require.Equal(t, index, batch.Items[index].Index)
		require.Equal(t, status, batch.Items[index].Status, "%+v", batch.Items[index])
		require.NotEmpty(t, batch.Items[index].Message)
	}
	require.Equal(t, batch.Items[0].EntryID, batch.Items[1].EntryID)
	largeFile := base64.StdEncoding.EncodeToString(make([]byte, types.MaxIngestInlineAssetBytes+1))
	mcpCall(t, client, "ingest", map[string]any{"items": []any{map[string]any{
		"url": "https://example.org/oversized-inline", "content": "A complete source body", "library": "institutional", "acquisition": "extracted",
		"images": []any{map[string]any{"source_url": "https://example.org/image.png", "data": largeFile}},
	}}}, &batch)
	require.Equal(t, "invalid", batch.Items[0].Status)
	require.Contains(t, batch.Items[0].Reason, "limit")
	oversized := map[string]any{"items": []any{map[string]any{
		"url": "https://example.org/oversized-json", "content": strings.Repeat("a", types.MaxIngestContentRunes+1), "library": "institutional", "acquisition": "extracted",
	}}}
	_, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "ingest", Arguments: oversized})
	require.ErrorContains(t, err, "Bad Request")
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "ingest", "arguments": oversized}})
	require.NoError(t, err)
	rejected := ingestAuthMCPRequest(f.router, f.key, string(raw))
	require.Equal(t, http.StatusBadRequest, rejected.Code)
	require.Contains(t, rejected.Body.String(), "5000000 Unicode code points")
	// A transport-level rejection closes the SDK client; reconnect for discovery.
	client = f.connect(t, "2025-11-25")
	tools, err := client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == "ingest" || tool.Name == "check_urls" || tool.Name == "list_incomplete" {
			require.Contains(t, tool.Description, "送入 Markdown 正文全文与图片文件")
			require.Contains(t, tool.Description, "补全时提供 library、article_id 与图片")
		}
		if tool.Name == "ingest" {
			require.Contains(t, tool.Description, "10 MiB")
			require.Contains(t, tool.Description, "20 MiB")
		}
	}
}

func TestMCPAndHTTPIncompleteListAcceptAndAcquisition(t *testing.T) {
	f := newMCPFixture(t)
	first := f.seedArticle(t, "Acquisitionfixture first", "full", "# Original\n\nUnchanged source text.\n")
	attempted := f.seedArticle(t, "Acquisitionfixture attempted", "full", "# Other\n\nAnother original source text.\n")
	for _, article := range []*types.Entry{first, attempted} {
		article.Incomplete = true
		article.Acquisition = "model_written"
		article.NormalizedURL = article.Source
		article.ExternalCompletionAttempted = article.ID == attempted.ID
		require.NoError(t, article.SetIngestMissing([]types.IngestMissing{{Type: "image", SourceURL: "https://example.org/chart.png", Reason: "未提供图片文件"}}))
		require.NoError(t, f.db.Save(article).Error)
	}
	client := f.connect(t, "2025-11-25")
	var list types.IncompleteListResult
	mcpCall(t, client, "list_incomplete", map[string]any{"page": 1, "page_size": 1}, &list)
	require.EqualValues(t, 1, list.Total)
	require.Len(t, list.Items, 1)
	require.Equal(t, first.ID, list.Items[0].EntryID)
	require.Equal(t, "model_written", list.Items[0].Acquisition)
	require.Equal(t, "未提供图片文件", list.Items[0].Missing[0].Reason)
	mcpCall(t, client, "list_incomplete", map[string]any{"page": 2, "page_size": 1}, &list)
	require.Empty(t, list.Items)
	var check types.IngestCheckResponse
	mcpCall(t, client, "check_urls", map[string]any{"library": "institutional", "urls": []string{first.Source, "https://example.org/no-record"}}, &check)
	require.Equal(t, "in_library", check.Items[0].Status)
	require.True(t, check.Items[0].Incomplete)
	require.Equal(t, "not_found", check.Items[1].Status)
	for _, query := range []string{"", "?include_attempted=false", "?page=2&page_size=1"} {
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/ingest/incomplete"+query, "", f.key, false)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result struct {
			Data  []types.IncompleteArticle `json:"data"`
			Total int64                     `json:"total"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		if query == "?include_attempted=false" {
			require.EqualValues(t, 1, result.Total)
		} else {
			require.EqualValues(t, 2, result.Total)
		}
	}
	var read service.EntryReadResult
	mcpCall(t, client, "get_article", map[string]any{"id": first.ID}, &read)
	require.Equal(t, "model_written", read.Acquisition)
	var search types.EntrySearchResponse
	mcpCall(t, client, "search", map[string]any{"query": "Acquisitionfixture", "collapse_similar": false}, &search)
	require.Len(t, search.Results, 2)
	for _, result := range search.Results {
		require.Equal(t, "model_written", result.Acquisition)
	}
	var accepted types.IngestResult
	mcpCall(t, client, "accept_incomplete", map[string]any{"id": first.ID}, &accepted)
	require.Equal(t, first.ID, accepted.EntryID)
	require.NoError(t, f.db.First(first, "id=?", first.ID).Error)
	require.False(t, first.Incomplete)
	require.Len(t, first.IngestMissing(), 1)
	response := personalRequest(t, f.router, http.MethodPost, "/api/v1/ingest/"+attempted.ID+"/accept-incomplete", "", f.key, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("operation_type=?", "accept_incomplete").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "personal integration", log.APIKeyName)
	}
}

func TestHTTPIngestCheckDiscardAndRevokedKey(t *testing.T) {
	f := newMCPFixture(t)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), middleware.Auth(f.params.SpaceService, f.params.UserService, f.params.APIKeyService))
	entries := f.articles.(interfaces.EntryService)
	RegisterEntryRoutes(router.Group("/api/v1"), handler.NewEntryHandler(nil, entries, f.params.LibraryService, f.queue, nil), newAccessGuards(f.params.LibraryService, entries, nil))
	library := &types.Entry{ID: "11111111-1111-4111-8111-111111111111", SpaceID: types.SingleSpaceID, LibraryID: f.library.ID, Type: "manual", SourceKind: "article", Source: "https://example.org/library", NormalizedURL: "https://example.org/library", Title: "In library", ParseStatus: types.ParseStatusCompleted, Incomplete: true, Acquisition: "extracted"}
	require.NoError(t, library.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "ingest", Decision: "approved", SourceInfo: types.SourceInfo{OriginalURL: library.Source}}))
	require.NoError(t, f.db.Create(library).Error)
	pending := &types.Entry{ID: "22222222-2222-4222-8222-222222222222", SpaceID: types.SingleSpaceID, LibraryID: f.library.ID, Type: "manual", SourceKind: "article", Source: "https://example.org/pending", NormalizedURL: "https://example.org/pending", Title: "Pending", ParseStatus: types.ParseStatusRejected, Acquisition: "extracted"}
	require.NoError(t, pending.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "ingest", Decision: "rejected", Reason: "正文缺失", SourceInfo: types.SourceInfo{OriginalURL: pending.Source}}))
	require.NoError(t, f.db.Create(pending).Error)
	discardedID := "33333333-3333-4333-8333-333333333333"
	fingerprint := sha256.Sum256([]byte("url:https://example.org/discarded"))
	require.NoError(t, f.db.Create(&types.IngestDiscard{SpaceID: types.SingleSpaceID, LibraryID: f.library.ID, Fingerprint: hex.EncodeToString(fingerprint[:]), EntryID: discardedID, NormalizedURL: "https://example.org/discarded", Reason: "旧记录已被丢弃"}).Error)
	require.NoError(t, f.db.Create(&types.EntryRepostSource{ID: "44444444-4444-4444-8444-444444444444", SpaceID: types.SingleSpaceID, LibraryID: f.library.ID, EntryID: library.ID, OriginalURL: "https://example.org/repost", NormalizedURL: "https://example.org/repost", SourceFingerprint: strings.Repeat("a", 64), MatchMethod: "content_hash"}).Error)
	response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest/check", `{"library":"institutional","urls":["https://example.org/library?utm_campaign=fixture","https://example.org/pending","https://example.org/discarded","https://example.org/not-found","https://example.org/repost"]}`, f.key, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var checked struct {
		Data types.IngestCheckResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &checked))
	require.Len(t, checked.Data.Items, 5)
	for index, status := range []string{"in_library", "pending_review", "discarded", "not_found", "in_library"} {
		require.Equal(t, status, checked.Data.Items[index].Status)
	}
	require.True(t, checked.Data.Items[0].Incomplete)
	require.Equal(t, library.ID, checked.Data.Items[4].EntryID)
	response = personalRequest(t, router, http.MethodPost, "/api/v1/ingest/"+pending.ID+"/discard", `{"reason":"用户丢弃原因"}`, f.key, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ledger types.IngestDiscard
	require.NoError(t, f.db.First(&ledger, "space_id=? AND normalized_url=?", types.SingleSpaceID, pending.Source).Error)
	require.Equal(t, "用户丢弃原因", ledger.Reason)
	response = personalRequest(t, router, http.MethodPost, "/api/v1/ingest/check", `{"library":"institutional","urls":["https://example.org/pending"]}`, f.key, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &checked))
	require.Equal(t, "discarded", checked.Data.Items[0].Status)
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("object_id=?", pending.ID).Find(&logs).Error)
	require.NotEmpty(t, logs)
	for _, log := range logs {
		require.Equal(t, "personal integration", log.APIKeyName)
	}
	require.NoError(t, f.db.Model(&types.APIKey{}).Where("name=?", "personal integration").Update("revoked_at", time.Now()).Error)
	response = personalRequest(t, router, http.MethodPost, "/api/v1/ingest/check", `{"library":"institutional","urls":["https://example.org/library"]}`, f.key, false)
	require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
}
