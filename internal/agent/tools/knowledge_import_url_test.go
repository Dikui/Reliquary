package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func TestKnowledgeImportURLToolUsesOnlyScopedOriginalURL(t *testing.T) {
	var imported KnowledgeImportURLInput
	tool := NewKnowledgeImportURLTool(types.SearchTargets{&types.SearchTarget{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 1,
	}}, func(_ context.Context, input KnowledgeImportURLInput) error {
		imported = input
		return nil
	})
	args := json.RawMessage(`{"knowledge_base_id":"kb","url":"https://example.com/original","source_info":{"author":"invented"}}`)
	wrongTenant := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2))
	result, err := tool.Execute(wrongTenant, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Empty(t, imported.URL)

	owner := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err = tool.Execute(owner, args)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "https://example.com/original", imported.URL)
	require.NotContains(t, result.Output, "approved")
}

func TestKnowledgeImportURLToolReportsJavaScriptRenderingFailure(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="root"></div><script>render()</script></body></html>`))
	}))
	defer server.Close()
	tool := NewKnowledgeImportURLTool(types.SearchTargets{&types.SearchTarget{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 1,
	}}, func(ctx context.Context, input KnowledgeImportURLInput) error {
		_, err := rss.FetchArticle(ctx, input.URL)
		return err
	})
	args, err := json.Marshal(KnowledgeImportURLInput{KnowledgeBaseID: "kb", URL: server.URL})
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	result, err := tool.Execute(ctx, args)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "该页面需要 JavaScript 渲染，无法抓取正文", result.Error)
	require.Equal(t, "javascript_required", result.Data["error_code"])
	require.Equal(t, false, result.Data["retryable"])
}
