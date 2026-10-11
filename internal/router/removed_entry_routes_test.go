package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemovedEntryResourcesReturnNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f.router = NewRouter(f.params)
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/entries/article/regenerate-summary"},
		{http.MethodPut, "/api/v1/chunks/by-id/chunk/questions"},
		{http.MethodDelete, "/api/v1/chunks/by-id/chunk/questions"},
		{http.MethodPost, "/api/v1/chunks/by-id/chunk/questions/regenerate"},
		{http.MethodPost, "/api/v1/initialization/multimodal/test"},

		{http.MethodPost, "/api/v1/entry-context/query"},
		{http.MethodPost, "/api/v1/entry-context/facts/fact/correction"},
		{http.MethodGet, "/api/v1/entry-context/events"},
		{http.MethodGet, "/api/v1/entry-context/evidence"},
		{http.MethodGet, "/api/v1/entry-context/focus"},
		{http.MethodPost, "/api/v1/entry-context/backfill"},
		{http.MethodGet, "/api/v1/entry-context/backfill/batches"},
		{http.MethodGet, "/api/v1/entry-domains"},
		{http.MethodPost, "/api/v1/entry-domains/domain/field-mappings"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/context/rebuild"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/context/events"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/workflows"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/workflows/workflow/run"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/reports"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/reports/report/download"},
		{http.MethodGet, "/api/v1/entry-workflows"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/tags"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/tags"},
		{http.MethodPut, "/api/v1/knowledge-bases/kb/tags/tag"},
		{http.MethodDelete, "/api/v1/knowledge-bases/kb/tags/tag"},
		{http.MethodPut, "/api/v1/entries/tags"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/strategies"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/strategies/version/activate"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/strategies/version/evaluate"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/strategies/reprocess"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/anomalies"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/anomalies/a/decision"},
		{http.MethodGet, "/api/v1/chunks/article/chunk/revisions"},
		{http.MethodPut, "/api/v1/chunks/article/chunk"},
		{http.MethodDelete, "/api/v1/chunks/article/chunk"},
		{http.MethodDelete, "/api/v1/chunks/article"},
		{http.MethodPost, "/api/v1/chunks/article/chunk/revert"},
		{http.MethodPut, "/api/v1/entries/image/article/chunk"},
		{http.MethodGet, "/api/v1/sessions"},
		{http.MethodPost, "/api/v1/sessions"},
		{http.MethodGet, "/api/v1/sessions/s/attachments"},
		{http.MethodPost, "/api/v1/sessions/s/generate_title"},
		{http.MethodGet, "/api/v1/sessions/s/messages/m/artifacts/0/download"},
		{http.MethodGet, "/api/v1/messages/s/load"},
		{http.MethodPost, "/api/v1/messages/search"},
		{http.MethodPost, "/api/v1/entry-chat/s"},
		{http.MethodPost, "/api/v1/agent-chat/s"},
		{http.MethodGet, "/api/v1/agents"},
		{http.MethodPost, "/api/v1/agents"},
		{http.MethodGet, "/api/v1/agents/a/suggested-questions"},
		{http.MethodGet, "/api/v1/mcp-services"},
		{http.MethodPost, "/api/v1/mcp-services"},
		{http.MethodGet, "/api/v1/mcp-services/m/tools"},
		{http.MethodPut, "/api/v1/mcp-services/m/credentials"},
		{http.MethodPut, "/api/v1/mcp-services/m/tool-approvals/tool"},
		{http.MethodPost, "/api/v1/agent/tool-approvals/pending"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/strategies"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/anomalies"},
		{http.MethodPost, "/api/v1/sessions/s/attachments"},
		{http.MethodPost, "/api/v1/sessions/s/stop"},
		{http.MethodPost, "/api/v1/sessions/s/pin"},
		{http.MethodPost, "/api/v1/agents/a/copy"},
		{http.MethodPost, "/api/v1/mcp-services/m/test"},
		{http.MethodGet, "/api/v1/messages/chat-history-stats"},
		{http.MethodGet, "/api/v1/sessions/s"},
		{http.MethodGet, "/api/v1/sessions/s/attachments/a"},
		{http.MethodGet, "/api/v1/sessions/s/attachments/a/preview"},
		{http.MethodGet, "/api/v1/sessions/continue-stream/s"},
		{http.MethodGet, "/api/v1/sessions/s/artifacts"},
		{http.MethodGet, "/api/v1/sessions/s/messages/m/artifacts"},
		{http.MethodGet, "/api/v1/agents/placeholders"},
		{http.MethodGet, "/api/v1/agents/type-presets"},
		{http.MethodGet, "/api/v1/agents/a"},
		{http.MethodGet, "/api/v1/mcp-services/m"},
		{http.MethodGet, "/api/v1/mcp-services/m/resources"},
		{http.MethodGet, "/api/v1/mcp-services/m/tool-approvals"},
		{http.MethodPut, "/api/v1/sessions/s"},
		{http.MethodPut, "/api/v1/agents/a"},
		{http.MethodPut, "/api/v1/mcp-services/m"},
		{http.MethodDelete, "/api/v1/messages/s/m"},
		{http.MethodDelete, "/api/v1/sessions/batch"},
		{http.MethodDelete, "/api/v1/sessions/s"},
		{http.MethodDelete, "/api/v1/sessions/s/messages"},
		{http.MethodDelete, "/api/v1/sessions/s/attachments/a"},
		{http.MethodDelete, "/api/v1/sessions/s/pin"},
		{http.MethodDelete, "/api/v1/agents/a"},
		{http.MethodDelete, "/api/v1/mcp-services/m"},
		{http.MethodDelete, "/api/v1/mcp-services/m/credentials/api_key"},
	}
	for _, tc := range cases {
		for _, apiKey := range []bool{false, true} {
			credential := f.token
			if apiKey {
				credential = f.key
			}
			response := personalRequest(t, f.router, tc.method, tc.path, `{}`, credential, apiKey)
			require.Equal(t, http.StatusNotFound, response.Code, tc.path+" "+response.Body.String())
		}
	}
}
