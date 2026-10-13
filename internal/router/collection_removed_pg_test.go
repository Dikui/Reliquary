package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestRemovedCollectionAndManualRoutesReturnNotFound(t *testing.T) {
	fixture := newPersonalAuthFixture(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	fixture.router = NewRouter(fixture.params)
	for _, registered := range fixture.router.Routes() {
		require.False(t, strings.HasPrefix(registered.Path, "/api/v1/data-sources"), registered.Path)
		if registered.Method == http.MethodPost {
			for _, suffix := range []string{"file", "url", "manual"} {
				require.NotEqual(t, "/api/v1/knowledge-bases/:id/entries/"+suffix, registered.Path)
			}
		}
		if registered.Method == http.MethodPut {
			require.NotEqual(t, "/api/v1/entries/:id/manual", registered.Path)
		}
	}
	var baseID string
	require.NoError(t, fixture.db.Model(&types.KnowledgeBase{}).Where("space_id=?", types.SingleSpaceID).Select("id").Scan(&baseID).Error)
	if baseID == "" {
		baseID = uuid.NewString()
		require.NoError(t, fixture.db.Create(&types.KnowledgeBase{ID: baseID, SpaceID: types.SingleSpaceID, Name: "Articles", Type: types.KnowledgeBaseTypeDocument}).Error)
	}
	entryID := uuid.NewString()
	require.NoError(t, fixture.db.Create(&types.Entry{ID: entryID, SpaceID: types.SingleSpaceID, KnowledgeBaseID: baseID, Title: "Legacy article", Type: types.EntryTypeManual, Channel: types.ChannelWeb, SourceKind: "article", ParseStatus: types.ParseStatusCompleted}).Error)
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/knowledge-bases/kb/entries/file"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/entries/url"},
		{http.MethodPost, "/api/v1/knowledge-bases/kb/entries/manual"},
		{http.MethodPut, "/api/v1/entries/legacy/manual"},
		{http.MethodGet, "/api/v1/data-sources/types"},
		{http.MethodPost, "/api/v1/data-sources/validate-credentials"},
		{http.MethodPost, "/api/v1/data-sources"},
		{http.MethodGet, "/api/v1/data-sources"},
		{http.MethodGet, "/api/v1/data-sources/legacy"},
		{http.MethodPut, "/api/v1/data-sources/legacy"},
		{http.MethodDelete, "/api/v1/data-sources/legacy"},
		{http.MethodPut, "/api/v1/data-sources/legacy/credentials"},
		{http.MethodDelete, "/api/v1/data-sources/legacy/credentials/credentials"},
		{http.MethodPost, "/api/v1/data-sources/legacy/validate"},
		{http.MethodGet, "/api/v1/data-sources/legacy/resources"},
		{http.MethodPost, "/api/v1/data-sources/legacy/resource-ancestors"},
		{http.MethodPost, "/api/v1/data-sources/legacy/sync"},
		{http.MethodPost, "/api/v1/data-sources/legacy/pause"},
		{http.MethodPost, "/api/v1/data-sources/legacy/resume"},
		{http.MethodGet, "/api/v1/data-sources/legacy/logs"},
		{http.MethodGet, "/api/v1/data-sources/logs/legacy"},
	}
	for _, route := range routes {
		route.path = strings.ReplaceAll(route.path, "/knowledge-bases/kb/", "/knowledge-bases/"+baseID+"/")
		route.path = strings.ReplaceAll(route.path, "/entries/legacy/", "/entries/"+entryID+"/")
		for _, apiKey := range []bool{false, true} {
			credential := fixture.token
			if apiKey {
				credential = fixture.key
			}
			response := personalRequest(t, fixture.router, route.method, route.path, `{}`, credential, apiKey)
			require.Equal(t, http.StatusNotFound, response.Code, route.method+" "+route.path+" "+response.Body.String())
		}
	}
}
