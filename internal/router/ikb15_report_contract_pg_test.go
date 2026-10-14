package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reliquary/internal/queue"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ikb15CountingChat struct{ calls atomic.Int64 }

func (c *ikb15CountingChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls.Add(1)
	return nil, fmt.Errorf("unexpected generation call")
}
func (c *ikb15CountingChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	c.calls.Add(1)
	return nil, fmt.Errorf("unexpected streaming call")
}
func (*ikb15CountingChat) GetModelID() string   { return "mcp-chat" }
func (*ikb15CountingChat) GetModelName() string { return "forbidden-report-generation" }

func TestIKB15ReportContractHTTPAndMCP(t *testing.T) {
	for _, transport := range []string{"HTTP", "MCP"} {
		t.Run(transport, func(t *testing.T) {
			spy := &ikb15CountingChat{}
			f := newMCPFixture(t, mcpProcessingFixture{chat: spy})
			f.seedProcessingModels(t)
			f.router.POST("/api/v1/reports", handler.NewVaultHandler(f.documents).CreateReport)
			client := f.connect(t, "2025-11-25")
			write := func(input map[string]any) (*service.ReportWriteResult, string) {
				if transport == "MCP" {
					result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "write_report", Arguments: input})
					require.NoError(t, err)
					if result.IsError {
						data, _ := json.Marshal(result)
						return nil, string(data)
					}
					var report service.ReportWriteResult
					require.NoError(t, json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report))
					return &report, ""
				}
				body, _ := json.Marshal(input)
				req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/reports", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+f.key)
				req.Header.Set("Content-Type", "application/json")
				resp, err := f.server.Client().Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()
				var raw json.RawMessage
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
				if resp.StatusCode == 400 {
					return nil, string(raw)
				}
				require.Contains(t, []int{200, 201}, resp.StatusCode, string(raw))
				var result struct {
					Data service.ReportWriteResult `json:"data"`
				}
				require.NoError(t, json.Unmarshal(raw, &result))
				return &result.Data, ""
			}
			source := f.seedArticle(t, "Related source", "", "# Evidence\n\nAn original publication.")
			deleted := f.seedArticle(t, "Deleted source", "", "# Deleted\n\nUnavailable original.")
			require.NoError(t, f.db.Delete(deleted).Error)
			e1 := types.Entity{ID: uuid.NewString(), SpaceID: 1, CanonicalName: "Shared", NormalizedName: "shared", EntityType: "company"}
			e2 := types.Entity{ID: uuid.NewString(), SpaceID: 1, CanonicalName: "Shared", NormalizedName: "shared", EntityType: "product"}
			require.NoError(t, f.db.Create(&[]types.Entity{e1, e2}).Error)
			require.NoError(t, f.db.Create(&types.EntityAlias{ID: uuid.NewString(), SpaceID: 1, EntityID: e1.ID, EntityType: e1.EntityType, Alias: "Alias", NormalizedAlias: "alias"}).Error)
			topic := types.Topic{ID: uuid.NewString(), SpaceID: 1, Name: "Known topic", LibraryIDs: []string{}}
			require.NoError(t, f.db.Create(&topic).Error)
			invalid := map[string]any{"title": " ", "markdown": " ", "related_article_ids": []string{"absent", deleted.ID}, "related_entities": []string{"absent", "Shared"}, "topics": []string{"absent"}, "author": "unsupported"}
			missing, missingIssue := write(map[string]any{})
			require.Nil(t, missing)
			require.Contains(t, missingIssue, "title")
			require.Contains(t, missingIssue, "markdown")
			report, issue := write(invalid)
			require.Nil(t, report)
			for _, field := range []string{"title", "markdown", "related_article_ids[0]", "related_article_ids[1]", "related_entities[0]", "related_entities[1]", "topics[0]", "author", e1.ID, e2.ID} {
				require.Contains(t, issue, field)
			}
			report, issue = write(map[string]any{"title": 23, "markdown": false, "related_entities": "Shared", "topics": nil})
			require.Nil(t, report)
			for _, field := range []string{"title", "markdown", "related_entities", "topics"} {
				require.Contains(t, issue, field)
			}
			var count int64
			require.NoError(t, f.db.Model(&types.Entry{}).Where("source_kind='report'").Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, f.db.Model(&types.OperationLog{}).Where("operation_type='write_report'").Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, f.db.Model(&queue.Job{}).Count(&count).Error)
			require.Zero(t, count)
			files, err := filepath.Glob(filepath.Join(f.root, "ai-reports", "*.md"))
			require.NoError(t, err)
			require.Empty(t, files)
			nul, nulIssue := write(map[string]any{"title": "\x00", "markdown": "\x00", "request_id": "\x00", "related_article_ids": []string{"\x00"}, "related_entities": []string{"\x00"}, "topics": []string{"\x00"}})
			require.Nil(t, nul)
			for _, field := range []string{"title", "markdown", "request_id", "related_article_ids[0]", "related_entities[0]", "topics[0]"} {
				require.Contains(t, nulIssue, field)
			}
			input := map[string]any{"title": "Report contract", "markdown": "# Report contract\n\nikb15searchfixture derived analysis.\n\n![chart](https://example.test/chart.png)", "related_article_ids": []string{source.ID, source.ID}, "related_entities": []string{"Alias", e1.ID}, "topics": []string{topic.ID, topic.Name}}
			report, issue = write(input)
			require.Empty(t, issue)
			require.False(t, report.Existing)
			require.True(t, report.CannotBeFact)
			require.Empty(t, report.ContentType)
			require.Nil(t, report.StructuredSummary)
			require.Equal(t, "completed", report.IndexStatus)
			file, err := os.ReadFile(filepath.Join(f.root, report.VaultPath))
			require.NoError(t, err)
			header, body, err := service.ParseVaultArticle(file)
			require.NoError(t, err)
			require.True(t, header.CannotBeFact)
			require.Len(t, header.RelatedEntities, 1)
			require.Contains(t, string(file), source.ID)
			require.Contains(t, string(file), "Known topic")
			require.Equal(t, input["markdown"], string(body))
			require.Empty(t, header.Entities)
			require.Empty(t, header.ContentType)
			require.Nil(t, header.Summary)
			retry, issue := write(map[string]any{"title": " Report contract ", "markdown": input["markdown"], "related_article_ids": []string{source.ID}, "related_entities": []string{e1.ID}, "topics": []string{topic.ID}})
			require.Empty(t, issue)
			require.True(t, retry.Existing)
			require.Equal(t, report.ID, retry.ID)
			input["request_id"] = "first-distinct-request"
			explicit, issue := write(input)
			require.Empty(t, issue)
			require.NotEqual(t, report.ID, explicit.ID)
			input["title"] = "Changed title"
			input["markdown"] = "Different valid body"
			priority, issue := write(input)
			require.Empty(t, issue)
			require.Equal(t, explicit.ID, priority.ID)
			require.True(t, priority.Existing)
			input["request_id"] = "second-distinct-request"
			second, issue := write(input)
			require.Empty(t, issue)
			require.NotEqual(t, explicit.ID, second.ID)
			// Persistence survives a service restart, and deleted reports do not block a fresh write.
			other, err := service.NewVaultDocumentService(f.db, mcpFixtureModels{}, nil, f.root)
			require.NoError(t, err)
			defer other.Close()
			restarted, err := other.CreateReportRequest(f.ctx, service.ReportInput{Title: "anything", Markdown: "anything", RequestID: "first-distinct-request"})
			require.NoError(t, err)
			require.Equal(t, explicit.ID, restarted.ID)
			require.NoError(t, f.db.Delete(explicit.Entry).Error)
			replaced, err := other.CreateReportRequest(f.ctx, service.ReportInput{Title: "anything", Markdown: "anything", RequestID: "first-distinct-request"})
			require.NoError(t, err)
			require.NotEqual(t, explicit.ID, replaced.ID)
			require.False(t, replaced.Existing)
			var search types.EntrySearchResponse
			mcpCall(t, client, "search", map[string]any{"libraries": []string{"ai-reports"}, "query": "ikb15searchfixture"}, &search)
			require.NotEmpty(t, search.Results)
			ids := []string{}
			for _, hit := range search.Results {
				require.True(t, hit.CannotBeFact)
				ids = append(ids, hit.EntryID)
			}
			require.Contains(t, ids, report.ID)
			var read service.EntryReadResult
			mcpCall(t, client, "get_article", map[string]any{"id": report.ID}, &read)
			require.True(t, read.CannotBeFact)
			require.True(t, read.Header.CannotBeFact)
			for _, table := range []string{"entry_entities", "entry_processing_outputs"} {
				require.NoError(t, f.db.Table(table).Where("entry_id=?", report.ID).Count(&count).Error)
				require.Zero(t, count)
			}
			var detail types.EntityDetail
			mcpCall(t, client, "get_entity", map[string]any{"id": e1.ID}, &detail)
			require.Empty(t, detail.Entries)
			require.NoError(t, f.db.Model(&types.Entity{}).Count(&count).Error)
			require.EqualValues(t, 2, count)
			require.NoError(t, f.db.Table("embeddings").Where("source_id IN (SELECT id FROM chunks WHERE entry_id=?)", report.ID).Count(&count).Error)
			require.Positive(t, count)
			inline, inlineIssue := write(map[string]any{"title": "Inline image report", "markdown": "# Inline image\n\nCaller analysis.\n\n![chart](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+cXwAAAABJRU5ErkJggg==)"})
			require.Empty(t, inlineIssue)
			require.Equal(t, "completed", inline.IndexStatus)
			imageFile, err := os.ReadFile(filepath.Join(f.root, inline.VaultPath))
			require.NoError(t, err)
			require.Contains(t, string(imageFile), "../attachments/"+inline.ID+"/")
			inlineRetry, retryIssue := write(map[string]any{"title": "Inline image report", "markdown": "# Inline image\n\nCaller analysis.\n\n![chart](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+cXwAAAABJRU5ErkJggg==)"})
			require.Empty(t, retryIssue)
			require.Equal(t, inline.ID, inlineRetry.ID)
			require.NoError(t, f.db.Model(&types.Chunk{}).Where("entry_id=? AND chunk_type=?", inline.ID, types.ChunkTypeImageDescription).Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, f.db.Model(&types.Entity{}).Where("name_embedding IS NOT NULL").Count(&count).Error)
			require.Zero(t, count)
			require.Zero(t, spy.calls.Load(), "report writing, images, references and indexing must not call generation models")
			t.Logf("%s: aggregate field errors, references, idempotency, search marker and generation calls=0", transport)
		})
	}
}

func TestIKB15FiveLibraryStarsAndOperationLogs(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	client := f.connect(t, "2025-11-25")
	for _, library := range []string{"institutional", "opinions", "references", "mine", "ai-reports"} {
		t.Run(library, func(t *testing.T) {
			var id string
			if library == "ai-reports" {
				var report service.ReportWriteResult
				mcpCall(t, client, "write_report", map[string]any{"title": "Star report", "markdown": "# Star report\n\nDerived observations."}, &report)
				id = report.ID
			} else {
				var result types.IngestBatchResult
				mcpCall(t, client, "ingest", map[string]any{"items": []any{map[string]any{"library": library, "title": "Star " + library, "url": "https://example.test/" + library, "author": "Fixture author", "publisher": "Fixture publisher", "acquisition": "verbatim", "content": "# " + library + "\n\nOriginal measured observations for " + library + "."}}}, &result)
				require.Equal(t, "accepted", result.Items[0].Status)
				id = result.Items[0].EntryID
				f.queue.mu.Lock()
				task := f.queue.tasks[len(f.queue.tasks)-1]
				f.queue.mu.Unlock()
				require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, task))
				var completed types.Entry
				require.NoError(t, f.db.First(&completed, "id=?", id).Error)
				require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
			}
			for _, starred := range []bool{true, true, false} {
				var entry types.Entry
				mcpCall(t, client, "set_star", map[string]any{"id": id, "starred": starred}, &entry)
				require.Equal(t, starred, entry.Starred)
			}
			var logs []types.OperationLog
			require.NoError(t, f.db.Where("object_id=?", id).Order("created_at,id").Find(&logs).Error)
			require.Len(t, logs, 3)
			expected := "ingest"
			if library == "ai-reports" {
				expected = "write_report"
			}
			require.Equal(t, []string{expected, "star", "unstar"}, []string{logs[0].OperationType, logs[1].OperationType, logs[2].OperationType})
			for _, log := range logs {
				require.Contains(t, log.Actor, "api_key:")
			}
			{
				require.NoError(t, f.vault.RefreshHeader(f.ctx, id))
				var entry types.Entry
				require.NoError(t, f.db.First(&entry, "id=?", id).Error)
				file, err := os.ReadFile(filepath.Join(f.root, entry.VaultPath))
				require.NoError(t, err)
				header, _, err := service.ParseVaultArticle(file)
				require.NoError(t, err)
				require.False(t, header.Starred)
				require.Equal(t, library == "ai-reports", header.CannotBeFact)
			}
		})
	}
}

func TestIKB15ConcurrentRequestID(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	other, err := service.NewVaultDocumentService(f.db, mcpFixtureModels{}, nil, f.root)
	require.NoError(t, err)
	defer other.Close()
	var reports [2]*service.ReportWriteResult
	var errs [2]error
	var wg sync.WaitGroup
	for i, documents := range []*service.VaultDocumentService{f.documents, other} {
		wg.Add(1)
		go func(i int, documents *service.VaultDocumentService) {
			defer wg.Done()
			reports[i], errs[i] = documents.CreateReportRequest(f.ctx, service.ReportInput{Title: fmt.Sprint("Concurrent ", i), Markdown: fmt.Sprint("Concurrent body ", i), RequestID: "shared-request-id"})
		}(i, documents)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, reports[0].ID, reports[1].ID)
	require.NotEqual(t, reports[0].Existing, reports[1].Existing)
	var count int64
	require.NoError(t, f.db.Model(&types.Entry{}).Where("source_kind='report'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.OperationLog{}).Where("operation_type='write_report'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&queue.Job{}).Count(&count).Error)
	require.LessOrEqual(t, count, int64(1))
}
