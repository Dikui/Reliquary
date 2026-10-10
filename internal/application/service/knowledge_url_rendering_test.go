package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type renderingFailureKnowledgeRepository struct {
	interfaces.KnowledgeRepository
	updated *types.Knowledge
}

func (r *renderingFailureKnowledgeRepository) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	copy := *knowledge
	r.updated = &copy
	return nil
}

func TestURLImportJavaScriptShellFailsWithReadableReason(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="app">Loading...</div><script>render()</script></body></html>`))
	}))
	defer server.Close()
	repo := &renderingFailureKnowledgeRepository{}
	svc := &knowledgeService{repo: repo}
	kb := &types.KnowledgeBase{ID: "kb-1"}
	knowledge := &types.Knowledge{ID: "knowledge-1", ParseStatus: types.ParseStatusProcessing}
	result, err := svc.convert(context.Background(), types.DocumentProcessPayload{URL: server.URL},
		kb, knowledge, ResolveProcessConfig(kb, nil), false)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, repo.updated)
	require.Equal(t, types.ParseStatusFailed, repo.updated.ParseStatus)
	require.Equal(t, "该页面需要 JavaScript 渲染，无法抓取正文", repo.updated.ErrorMessage)
}

func TestURLImportEmptyPageFailsWithReadableReason(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body></body></html>`))
	}))
	defer server.Close()
	repo := &renderingFailureKnowledgeRepository{}
	svc := &knowledgeService{repo: repo}
	kb := &types.KnowledgeBase{ID: "kb-1"}
	knowledge := &types.Knowledge{ID: "knowledge-1", ParseStatus: types.ParseStatusProcessing}
	result, err := svc.convert(context.Background(), types.DocumentProcessPayload{URL: server.URL},
		kb, knowledge, ResolveProcessConfig(kb, nil), false)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, repo.updated)
	require.Equal(t, types.ParseStatusFailed, repo.updated.ParseStatus)
	require.Contains(t, repo.updated.ErrorMessage, "该页面没有可抓取的正文")
}
