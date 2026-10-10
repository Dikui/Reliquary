package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func registerPersonalResourceRoutes(t *testing.T, f *personalAuthFixture) interfaces.KnowledgeBaseService {
	t.Helper()
	kbRepo := apprepo.NewKnowledgeBaseRepository(f.db)
	kb := service.NewKnowledgeBaseService(f.db, kbRepo, apprepo.NewKnowledgeRepository(f.db), nil, nil, nil,
		apprepo.NewTenantRepository(f.db), nil, nil, nil, nil, nil, nil, nil)
	v1 := f.router.Group("/api/v1")
	RegisterKnowledgeBaseRoutes(v1, handler.NewKnowledgeBaseHandler(kb, nil, nil), newAccessGuards(kb, nil, nil))
	favorites := service.NewResourceFavoriteService(apprepo.NewResourceFavoriteRepository(f.db))
	RegisterUserFavoriteRoutes(v1, handler.NewResourceFavoriteHandler(favorites), nil)
	return kb
}

func personalKnowledgeBaseList(t *testing.T, f *personalAuthFixture, apiKey bool) []*types.KnowledgeBase {
	t.Helper()
	credential := f.token
	if apiKey {
		credential = f.key
	}
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-bases", "", credential, apiKey)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body struct {
		Data []*types.KnowledgeBase `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	return body.Data
}

func personalKBIDs(list []*types.KnowledgeBase) []string {
	ids := make([]string, 0, len(list))
	for _, kb := range list {
		ids = append(ids, kb.ID)
	}
	return ids
}

func TestPersonalKnowledgeBasePinsSharedAcrossLoginAndAPIKey(t *testing.T) {
	f := newPersonalAuthFixture(t)
	kbService := registerPersonalResourceRoutes(t, f)
	domain := service.BuiltinAIKnowledgeDomain()
	require.NoError(t, f.db.Create(domain).Error)
	createdAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"pin-old", "pin-middle", "pin-new"} {
		kb := &types.KnowledgeBase{
			ID: id, TenantID: 1, Name: id, DomainID: domain.ID, CreatorID: f.userID,
			Type: types.KnowledgeBaseTypeDocument, CreatedAt: createdAt.Add(time.Duration(i) * time.Hour),
		}
		require.NoError(t, f.db.Create(kb).Error)
	}
	assertList := func(want []string) {
		t.Helper()
		login := personalKnowledgeBaseList(t, f, false)
		api := personalKnowledgeBaseList(t, f, true)
		require.Equal(t, want, personalKBIDs(login))
		require.Equal(t, login, api)
	}
	toggle := func(id string, apiKey, wantPinned bool) {
		t.Helper()
		credential := f.token
		if apiKey {
			credential = f.key
		}
		response := personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge-bases/"+id+"/pin", "", credential, apiKey)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body struct {
			Data types.KnowledgeBase `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, wantPinned, body.Data.IsPinned)
		if wantPinned {
			require.NotNil(t, body.Data.PinnedAt)
		} else {
			require.Nil(t, body.Data.PinnedAt)
		}
		var stored types.KnowledgeBase
		require.NoError(t, f.db.First(&stored, "id = ?", id).Error)
		require.Equal(t, wantPinned, stored.IsPinned)
		if wantPinned {
			require.NotNil(t, stored.PinnedAt)
			require.WithinDuration(t, *body.Data.PinnedAt, *stored.PinnedAt, time.Microsecond)
		} else {
			require.Nil(t, stored.PinnedAt)
		}
	}
	assertList([]string{"pin-new", "pin-middle", "pin-old"})
	toggle("pin-old", true, true)
	assertList([]string{"pin-old", "pin-new", "pin-middle"})
	toggle("pin-middle", false, true)
	assertList([]string{"pin-middle", "pin-old", "pin-new"})
	toggle("pin-old", false, false)
	assertList([]string{"pin-middle", "pin-new", "pin-old"})
	toggle("pin-middle", true, false)
	assertList([]string{"pin-new", "pin-middle", "pin-old"})

	// A caller carrying only the personal-space tenant can change the same pin.
	tenantOnly := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	loginContext := context.WithValue(tenantOnly, types.UserIDContextKey, f.userID)
	pinned, err := kbService.TogglePinKnowledgeBase(tenantOnly, "pin-old")
	require.NoError(t, err)
	require.True(t, pinned.IsPinned)
	assertList([]string{"pin-old", "pin-new", "pin-middle"})
	tenantList, err := kbService.ListKnowledgeBases(tenantOnly)
	require.NoError(t, err)
	loginList, err := kbService.ListKnowledgeBases(loginContext)
	require.NoError(t, err)
	require.Equal(t, tenantList, loginList)
	unpinned, err := kbService.TogglePinKnowledgeBase(loginContext, "pin-old")
	require.NoError(t, err)
	require.False(t, unpinned.IsPinned)
	require.Nil(t, unpinned.PinnedAt)
	assertList([]string{"pin-new", "pin-middle", "pin-old"})

	copyResponse := personalRequest(t, f.router, http.MethodPost, "/api/v1/knowledge-bases/copy", `{}`, f.key, true)
	require.Equal(t, http.StatusNotFound, copyResponse.Code, copyResponse.Body.String())
	progressResponse := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-bases/copy/progress/old-task", "", f.token, false)
	require.Equal(t, http.StatusNotFound, progressResponse.Code, progressResponse.Body.String())
}

func TestPersonalResourceFavoritesSharedAcrossLoginAndAPIKey(t *testing.T) {
	f := newPersonalAuthFixture(t)
	registerPersonalResourceRoutes(t, f)
	list := func(resourceType string, apiKey bool) []*types.ResourceFavorite {
		t.Helper()
		credential := f.token
		if apiKey {
			credential = f.key
		}
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/user/favorites?type="+resourceType, "", credential, apiKey)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body struct {
			Data []*types.ResourceFavorite `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body.Data
	}
	assertFavorites := func(resourceType string, wantIDs []string) {
		t.Helper()
		login := list(resourceType, false)
		api := list(resourceType, true)
		require.Equal(t, login, api)
		ids := make([]string, 0, len(login))
		for _, favorite := range login {
			ids = append(ids, favorite.ResourceID)
			require.EqualValues(t, 1, favorite.TenantID)
			require.Equal(t, resourceType, favorite.ResourceType)
		}
		require.Equal(t, wantIDs, ids)
	}
	change := func(method, resourceType, id string, apiKey bool) {
		t.Helper()
		credential := f.token
		if apiKey {
			credential = f.key
		}
		path, body := "/api/v1/user/favorites", fmt.Sprintf(`{"type":%q,"id":%q}`, resourceType, id)
		if method == http.MethodDelete {
			path += "/" + resourceType + "/" + id
			body = ""
		}
		response := personalRequest(t, f.router, method, path, body, credential, apiKey)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	assertFavorites(types.ResourceTypeKB, []string{})
	change(http.MethodPost, types.ResourceTypeKB, "favorite-1", true)
	assertFavorites(types.ResourceTypeKB, []string{"favorite-1"})
	change(http.MethodPost, types.ResourceTypeKB, "favorite-1", false)
	assertFavorites(types.ResourceTypeKB, []string{"favorite-1"})
	change(http.MethodPost, types.ResourceTypeKB, "favorite-2", false)
	assertFavorites(types.ResourceTypeKB, []string{"favorite-2", "favorite-1"})
	change(http.MethodPost, types.ResourceTypeAgent, "favorite-1", true)
	assertFavorites(types.ResourceTypeKB, []string{"favorite-2", "favorite-1"})
	assertFavorites(types.ResourceTypeAgent, []string{"favorite-1"})
	change(http.MethodDelete, types.ResourceTypeKB, "favorite-1", false)
	assertFavorites(types.ResourceTypeKB, []string{"favorite-2"})
	change(http.MethodDelete, types.ResourceTypeKB, "favorite-2", true)
	assertFavorites(types.ResourceTypeKB, []string{})
	change(http.MethodDelete, types.ResourceTypeAgent, "favorite-1", false)
	assertFavorites(types.ResourceTypeAgent, []string{})
	change(http.MethodDelete, types.ResourceTypeKB, "favorite-2", true)
	assertFavorites(types.ResourceTypeKB, []string{})

	tenantOnly := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
	loginContext := context.WithValue(tenantOnly, types.UserIDContextKey, f.userID)
	favorites := service.NewResourceFavoriteService(apprepo.NewResourceFavoriteRepository(f.db))
	require.NoError(t, favorites.Add(tenantOnly, 1, types.ResourceTypeKB, "favorite-3"))
	assertFavorites(types.ResourceTypeKB, []string{"favorite-3"})
	fromTenant, err := favorites.List(tenantOnly, 1, types.ResourceTypeKB)
	require.NoError(t, err)
	fromLogin, err := favorites.List(loginContext, 1, types.ResourceTypeKB)
	require.NoError(t, err)
	require.Equal(t, fromTenant, fromLogin)
	require.NoError(t, favorites.Remove(loginContext, 1, types.ResourceTypeKB, "favorite-3"))
	assertFavorites(types.ResourceTypeKB, []string{})
}

func TestPersonalKnowledgeBasePinPreservesUpdatedAtAndListOrder(t *testing.T) {
	f := newPersonalAuthFixture(t)
	registerPersonalResourceRoutes(t, f)
	domain := service.BuiltinAIKnowledgeDomain()
	require.NoError(t, f.db.Create(domain).Error)
	baseTime := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	beforeTimes := map[string]time.Time{}
	for i, id := range []string{"timestamp-old", "timestamp-middle", "timestamp-new"} {
		updated := baseTime.Add(time.Duration(i) * time.Hour)
		kb := &types.KnowledgeBase{ID: id, TenantID: 1, Name: id, DomainID: domain.ID,
			CreatorID: f.userID, Type: types.KnowledgeBaseTypeDocument,
			CreatedAt: baseTime.Add(-time.Duration(i) * time.Hour), UpdatedAt: updated}
		require.NoError(t, f.db.Create(kb).Error)
		beforeTimes[id] = updated
	}
	// The knowledge base page orders ordinary rows by their modification time.
	pageOrder := func(apiKey bool) []string {
		rows := personalKnowledgeBaseList(t, f, apiKey)
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].IsPinned != rows[j].IsPinned {
				return rows[i].IsPinned
			}
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		})
		return personalKBIDs(rows)
	}
	assertTimestamps := func() {
		t.Helper()
		for _, row := range personalKnowledgeBaseList(t, f, false) {
			require.True(t, beforeTimes[row.ID].Equal(row.UpdatedAt), "response updated_at changed for %s", row.ID)
			var stored types.KnowledgeBase
			require.NoError(t, f.db.First(&stored, "id = ?", row.ID).Error)
			require.True(t, beforeTimes[row.ID].Equal(stored.UpdatedAt), "stored updated_at changed for %s", row.ID)
		}
	}
	before := pageOrder(false)
	require.Equal(t, []string{"timestamp-new", "timestamp-middle", "timestamp-old"}, before)
	response := personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge-bases/timestamp-old/pin", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertTimestamps()
	require.Equal(t, []string{"timestamp-old", "timestamp-new", "timestamp-middle"}, pageOrder(false))
	response = personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge-bases/timestamp-old/pin", "", f.token, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertTimestamps()
	require.Equal(t, before, pageOrder(false))
	require.Equal(t, before, pageOrder(true))

	response = personalRequest(t, f.router, http.MethodPost, "/api/v1/user/favorites", `{"type":"kb","id":"timestamp-old"}`, f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertTimestamps()
	require.Equal(t, before, pageOrder(false))
	response = personalRequest(t, f.router, http.MethodDelete, "/api/v1/user/favorites/kb/timestamp-old", "", f.token, false)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertTimestamps()
	require.Equal(t, before, pageOrder(false))
}
