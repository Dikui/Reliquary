package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/redistest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func reportWriter(t *testing.T, f *mcpFixture, transport string) func(string, string) *service.ReportWriteResult {
	t.Helper()
	if transport == "MCP" {
		client := f.connect(t, "2025-11-25")
		return func(title, markdown string) *service.ReportWriteResult {
			var result service.ReportWriteResult
			mcpCall(t, client, "write_report", map[string]any{"title": title, "markdown": markdown}, &result)
			return &result
		}
	}
	h := handler.NewVaultHandler(f.documents)
	f.router.POST("/api/v1/reports", h.CreateReport)
	return func(title, markdown string) *service.ReportWriteResult {
		body, err := json.Marshal(map[string]string{"title": title, "markdown": markdown})
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/reports", bytes.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+f.key)
		request.Header.Set("Content-Type", "application/json")
		response, err := f.server.Client().Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Contains(t, []int{http.StatusCreated, http.StatusOK}, response.StatusCode)
		var output struct {
			Success bool                      `json:"success"`
			Data    service.ReportWriteResult `json:"data"`
		}
		require.NoError(t, json.NewDecoder(response.Body).Decode(&output))
		require.True(t, output.Success)
		if output.Data.Existing {
			require.Equal(t, http.StatusOK, response.StatusCode)
		} else {
			require.Equal(t, http.StatusCreated, response.StatusCode)
		}
		return &output.Data
	}
}

func startReportQueue(t *testing.T, f *mcpFixture) *queue.Client {
	t.Helper()
	client := redistest.NewClient(t, f.db)
	mux := queue.NewServeMux()
	mux.HandleFunc(types.TypeReportIndex, f.documents.HandleReportIndex)
	require.NoError(t, client.Start(context.Background(), mux, queue.Config{
		Pools:         types.WorkerPoolConcurrency{Core: 1, Enrichment: 1, Maintenance: 1, Shared: 1},
		CheckInterval: 10 * time.Millisecond,
		RetryDelayFunc: func(int, error, *queue.Task) time.Duration {
			return 250 * time.Millisecond
		},
		OnDeadLetter: newDeadLetterEntryFailer(f.articles.(interfaces.EntryService), nil),
	}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func TestWriteReportHTTPAndMCPIndexRetryAndDuplicate(t *testing.T) {
	for _, transport := range []string{"HTTP", "MCP"} {
		t.Run(transport, func(t *testing.T) {
			f := newMCPFixture(t)
			f.seedProcessingModels(t)
			write := reportWriter(t, f, transport)
			f.library.ModelTiers.Embedding = "missing-embedding"
			require.NoError(t, f.db.Save(f.library).Error)
			require.NoError(t, f.db.Model(&types.Space{}).Where("id=?", f.library.SpaceID).Update("embedding_model_id", f.library.ModelTiers.Embedding).Error)
			markdown := "# Retried report\n\nreportqueueretryfixture preserves verified findings."
			report := write("Retried report", markdown)
			require.NotEmpty(t, report.ID)
			require.Equal(t, "pending_retry", report.IndexStatus)
			require.False(t, report.Existing)
			require.Equal(t, types.ParseStatusFailed, report.ParseStatus)
			content, err := os.ReadFile(filepath.Join(f.root, report.VaultPath))
			require.NoError(t, err)
			require.Contains(t, string(content), markdown)
			var jobs []queue.Job
			require.NoError(t, f.db.Find(&jobs).Error)
			require.Len(t, jobs, 1)
			require.Equal(t, types.TypeReportIndex, jobs[0].TaskType)
			client := startReportQueue(t, f)
			require.Eventually(t, func() bool {
				var job queue.Job
				return f.db.First(&job, "id=?", jobs[0].ID).Error == nil && job.State == queue.TaskStateRetry && job.Retried == 1
			}, 5*time.Second, 10*time.Millisecond)
			f.library.ModelTiers.Embedding = "mcp-embedding"
			require.NoError(t, f.db.Save(f.library).Error)
			require.NoError(t, f.db.Model(&types.Space{}).Where("id=?", f.library.SpaceID).Update("embedding_model_id", f.library.ModelTiers.Embedding).Error)
			require.Eventually(t, func() bool {
				var entry types.Entry
				return f.db.First(&entry, "id=?", report.ID).Error == nil && entry.ParseStatus == types.ParseStatusCompleted
			}, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, client.Close())
			var search types.EntrySearchResponse
			mcpCall(t, f.connect(t, "2025-11-25"), "search", map[string]any{"libraries": []string{"ai-reports"}, "query": "reportqueueretryfixture", "sources": []string{"report"}}, &search)
			require.Len(t, search.Results, 1)
			require.Equal(t, report.ID, search.Results[0].EntryID)
			completed := write("Retried report", markdown)
			require.NotEqual(t, report.ID, completed.ID)
			require.False(t, completed.Existing)
			require.Equal(t, "completed", completed.IndexStatus)
			var count int64
			require.NoError(t, f.db.Model(&types.Entry{}).Where("source_kind='report'").Count(&count).Error)
			require.EqualValues(t, 2, count)
			require.NoError(t, f.db.Model(&types.OperationLog{}).Where("operation_type='write_report'").Count(&count).Error)
			require.EqualValues(t, 2, count)
			t.Logf("%s saved report %s despite indexing failure; queued retry recovered search; identical write created an independent report", transport, report.ID)
		})
	}
}

func TestWriteReportExhaustedRetryVisibleInRuntimeQueue(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	f.library.ModelTiers.Embedding = "missing-embedding"
	require.NoError(t, f.db.Save(f.library).Error)
	require.NoError(t, f.db.Model(&types.Space{}).Where("id=?", f.library.SpaceID).Update("embedding_model_id", f.library.ModelTiers.Embedding).Error)
	write := reportWriter(t, f, "HTTP")
	report := write("Exhausted report", "# Exhausted report\n\nSaved source survives exhausted retries.")
	var job queue.Job
	require.NoError(t, f.db.First(&job).Error)
	require.NoError(t, f.db.Model(&job).Update("max_retry", 0).Error)
	client := startReportQueue(t, f)
	require.Eventually(t, func() bool {
		var current queue.Job
		return f.db.First(&current, "id=?", job.ID).Error == nil && current.State == queue.TaskStateArchived
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, client.Close())
	inspector := NewPostgresTaskInspector(client)
	system := handler.NewSystemHandler(nil, nil, nil, nil, nil, inspector, nil)
	router := gin.New()
	router.GET("/api/v1/system/runtime/queues/:queue/tasks", system.ListRuntimeTasks)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/queues/low/tasks?state=archived", nil))
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	var result handler.RuntimeTasksResponse
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &result))
	require.Len(t, result.Tasks, 1)
	require.Equal(t, report.ID, result.Tasks[0].EntryID)
	require.Equal(t, types.TypeReportIndex, result.Tasks[0].Type)
	require.Contains(t, result.Tasks[0].LastError, "embedding")
	require.True(t, result.Tasks[0].Allows(types.RuntimeTaskActionRunNow))
	require.True(t, result.Tasks[0].Allows(types.RuntimeTaskActionDelete))
	var deadLetters int64
	require.NoError(t, f.db.Model(&types.TaskDeadLetter{}).Count(&deadLetters).Error)
	require.EqualValues(t, 1, deadLetters)
	// Background scans leave retries and their exhausted state under queue control.
	f.library.ModelTiers.Embedding = "mcp-embedding"
	require.NoError(t, f.db.Save(f.library).Error)
	require.NoError(t, f.db.Model(&types.Space{}).Where("id=?", f.library.SpaceID).Update("embedding_model_id", f.library.ModelTiers.Embedding).Error)
	require.NoError(t, f.documents.Scan(f.ctx, false))
	var entry types.Entry
	require.NoError(t, f.db.First(&entry, "id=?", report.ID).Error)
	require.Equal(t, types.ParseStatusFailed, entry.ParseStatus)
	t.Logf("Archived report index task %s remains visible with entry %s, error and retry actions", job.ID, report.ID)
}

func TestWriteReportConcurrentDuplicateAndLegacyFingerprint(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	engine := f.documents
	other, err := service.NewVaultDocumentService(f.db, mcpFixtureModels{}, nil, f.root)
	require.NoError(t, err)
	var results [2]*service.ReportWriteResult
	var errs [2]error
	var wait sync.WaitGroup
	for i, documents := range []*service.VaultDocumentService{engine, other} {
		wait.Add(1)
		go func(i int, documents *service.VaultDocumentService) {
			defer wait.Done()
			results[i], errs[i] = documents.CreateReport(f.ctx, "Concurrent report", "# Different displayed heading\n\nconcurrentreportfixture", nil)
		}(i, documents)
	}
	wait.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.NotEqual(t, results[0].ID, results[1].ID)
	require.False(t, results[0].Existing)
	require.False(t, results[1].Existing)
	require.NoError(t, f.db.Model(&types.Entry{}).Where("id=?", results[0].ID).Update("metadata", types.JSON(`{"related_article_ids":[]}`)).Error)
	legacy, err := f.documents.CreateReport(f.ctx, "Concurrent report", "# Different displayed heading\n\nconcurrentreportfixture", nil)
	require.NoError(t, err)
	require.False(t, legacy.Existing)
	require.NotEqual(t, results[0].ID, legacy.ID)
}
