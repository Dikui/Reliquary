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

type renderingFailureEntryRepository struct {
	interfaces.EntryRepository
	updated *types.Entry
}

func (r *renderingFailureEntryRepository) UpdateEntry(_ context.Context, entry *types.Entry) error {
	copy := *entry
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
	repo := &renderingFailureEntryRepository{}
	svc := &entryService{repo: repo}
	kb := &types.KnowledgeBase{ID: "kb-1"}
	entry := &types.Entry{ID: "entry-1", ParseStatus: types.ParseStatusProcessing}
	result, err := svc.convert(context.Background(), types.DocumentProcessPayload{URL: server.URL},
		kb, entry, ResolveProcessConfig(kb, nil), false)
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
	repo := &renderingFailureEntryRepository{}
	svc := &entryService{repo: repo}
	kb := &types.KnowledgeBase{ID: "kb-1"}
	entry := &types.Entry{ID: "entry-1", ParseStatus: types.ParseStatusProcessing}
	result, err := svc.convert(context.Background(), types.DocumentProcessPayload{URL: server.URL},
		kb, entry, ResolveProcessConfig(kb, nil), false)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, repo.updated)
	require.Equal(t, types.ParseStatusFailed, repo.updated.ParseStatus)
	require.Contains(t, repo.updated.ErrorMessage, "该页面没有可抓取的正文")
}
