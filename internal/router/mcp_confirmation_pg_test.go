package router

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"reliquary/internal/types"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func requireMCPConfirmation(t *testing.T, response *mcp.CallToolResult, entry types.Entry, risks ...string) {
	t.Helper()
	require.True(t, response.IsError)
	require.Len(t, response.Content, 1)
	content, ok := response.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	for _, want := range append(risks, "confirmation_required", entry.ID, entry.Title, "会写操作日志", "不能恢复", "先向用户展示目标并取得明确同意，再带 confirmed=true 重试；不得自行补上 confirmed") {
		require.Contains(t, content.Text, want)
	}
}

func mcpDestructiveRecordCounts(t *testing.T, f *mcpFixture, id string) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, table := range []string{"entries", "chunks", "embeddings", "article_embeddings", "image_embeddings", "entry_entities", "entry_topics", "entry_processing_spans", "ingest_assets", "ingest_discards", "operation_logs"} {
		query := f.db.Unscoped().Table(table)
		if table == "entries" {
			query = query.Where("id=?", id)
		} else if table != "operation_logs" {
			query = query.Where("entry_id=?", id)
		}
		var count int64
		require.NoError(t, query.Count(&count).Error, table)
		counts[table] = count
	}
	return counts
}

func TestMCPDestructiveConfirmationSchemas(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	listed, err := client.ListTools(context.Background(), nil)
	require.NoError(t, err)
	checked := []string{}
	for _, tool := range listed.Tools {
		if tool.Name != "delete_document" && tool.Name != "discard_pending" {
			continue
		}
		checked = append(checked, tool.Name)
		require.Contains(t, tool.Description, "confirmed=true")
		require.Contains(t, tool.Description, "明确同意")
		require.Contains(t, tool.Description, "confirmation_required")
		require.Contains(t, tool.Description, "不得自行补上 confirmed")
		data, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(data, &schema))
		confirmed := schema.Properties["confirmed"]
		require.Equal(t, "boolean", confirmed.Type)
		require.Contains(t, confirmed.Description, "明确同意")
		require.Contains(t, confirmed.Description, "省略或 false 不执行")
		require.NotContains(t, schema.Required, "confirmed")
	}
	require.ElementsMatch(t, []string{"delete_document", "discard_pending"}, checked)
}

func TestMCPDiscardPendingRequiresConfirmation(t *testing.T) {
	f := newMCPFixture(t)
	pending := f.seedPending(t, "待审确认测试")
	require.NoError(t, f.db.First(pending, "id=?", pending.ID).Error)
	client := f.connect(t, "2025-11-25")
	intake, err := pending.IntakeMetadata()
	require.NoError(t, err)
	readSource := func() []byte {
		t.Helper()
		reader, err := f.files.GetFile(f.ctx, intake.OriginalPath)
		require.NoError(t, err)
		defer reader.Close()
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		return body
	}
	bodyBefore := readSource()
	countsBefore := mcpDestructiveRecordCounts(t, f, pending.ID)
	for _, args := range []map[string]any{{"id": pending.ID}, {"id": pending.ID, "confirmed": false}} {
		response, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "discard_pending", Arguments: args})
		require.NoError(t, err)
		requireMCPConfirmation(t, response, *pending, "待审资料", "暂存来源")
		var unchanged types.Entry
		require.NoError(t, f.db.First(&unchanged, "id=?", pending.ID).Error)
		require.Equal(t, *pending, unchanged)
		require.Equal(t, countsBefore, mcpDestructiveRecordCounts(t, f, pending.ID))
		require.Equal(t, bodyBefore, readSource())
		var listed struct {
			Data []types.Entry `json:"data"`
		}
		mcpCall(t, client, "list_pending", map[string]any{}, &listed)
		require.Len(t, listed.Data, 1)
		require.Equal(t, pending.ID, listed.Data[0].ID)
	}
	var discarded types.Entry
	mcpCall(t, client, "discard_pending", map[string]any{"id": pending.ID, "confirmed": true}, &discarded)
	require.Equal(t, pending.ID, discarded.ID)
	countsAfter := mcpDestructiveRecordCounts(t, f, pending.ID)
	require.Zero(t, countsAfter["entries"])
	require.Equal(t, countsBefore["operation_logs"]+1, countsAfter["operation_logs"])
	require.Positive(t, countsAfter["ingest_discards"])
	reader, err := f.files.GetFile(f.ctx, intake.OriginalPath)
	if reader != nil {
		reader.Close()
	}
	require.Error(t, err)
	var listed struct {
		Data []types.Entry `json:"data"`
	}
	mcpCall(t, client, "list_pending", map[string]any{}, &listed)
	require.Empty(t, listed.Data)
	f.queue.mu.Lock()
	defer f.queue.mu.Unlock()
	require.Empty(t, f.queue.tasks)
}
