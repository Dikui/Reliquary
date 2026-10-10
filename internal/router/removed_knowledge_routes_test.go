package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemovedKnowledgeResourcesReturnNotFound(t *testing.T) {
	f := newPersonalAuthFixture(t)
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/knowledge-context/query"},
		{http.MethodPost, "/api/v1/knowledge-context/facts/fact/correction"},
		{http.MethodGet, "/api/v1/knowledge-context/events"},
		{http.MethodGet, "/api/v1/knowledge-context/evidence"},
		{http.MethodGet, "/api/v1/knowledge-context/focus"},
		{http.MethodPost, "/api/v1/knowledge-context/backfill"},
		{http.MethodGet, "/api/v1/knowledge-context/backfill/batches"},
		{http.MethodGet, "/api/v1/knowledge-domains"},
		{http.MethodPost, "/api/v1/knowledge-domains/domain/field-mappings"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/context/rebuild"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/context/events"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/workflows"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/workflows/workflow/run"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/reports"},
		{http.MethodGet, "/api/v1/knowledge-bases/kb/reports/report/download"},
		{http.MethodGet, "/api/v1/knowledge-workflows"},
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
