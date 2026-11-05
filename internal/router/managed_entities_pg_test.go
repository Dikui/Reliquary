package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

func TestManagedEntityLibraryLifecycle(t *testing.T) {
	f := newMCPFixture(t)
	request := func(method, path string, input any, status int, output any) {
		t.Helper()
		body := ""
		if input != nil {
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			body = string(encoded)
		}
		response := personalRequest(t, f.router, method, "/api/v1"+path, body, f.key, true)
		require.Equal(t, status, response.Code, response.Body.String())
		if output != nil {
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			require.NoError(t, json.Unmarshal(envelope.Data, output))
		}
	}
	var library types.Library
	request(http.MethodPost, "/libraries", map[string]any{
		"directory": "products", "name": "产品货架", "entity_mode": "managed",
		"entity_schema": types.EntitySchema{
			Kinds:  []types.EntityKind{{Key: "product", Name: "产品", ParentKinds: []string{"product"}}},
			Fields: []types.EntityFieldDefinition{{Key: "capacity", Name: "容量", Type: "number", Unit: "kWh"}},
		},
	}, http.StatusCreated, &library)
	require.Equal(t, "managed", library.EntityMode)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"relation_types": []types.LibraryRelationType{{Name: "belongs_to", Enabled: true, Hierarchical: true}}}, http.StatusOK, &library)
	var parent, child types.Entity
	request(http.MethodPost, "/libraries/"+library.ID+"/entities", types.ManagedEntityInput{Name: "Fixture Platform", Kind: "product", Code: "PLATFORM"}, http.StatusCreated, &parent)
	request(http.MethodPost, "/libraries/"+library.ID+"/entities", types.ManagedEntityInput{Name: "Fixture Model A", Kind: "product", Code: "MODEL-A", Aliases: []string{"样例型号 A"}, Attributes: map[string]any{"capacity": 5015}}, http.StatusCreated, &child)
	require.EqualValues(t, 1, child.Revision)
	var relation types.Relation
	request(http.MethodPost, "/libraries/"+library.ID+"/relations", types.ManagedRelationInput{SubjectID: child.ID, RelationType: "belongs_to", ObjectID: parent.ID}, http.StatusCreated, &relation)
	request(http.MethodPost, "/libraries/"+library.ID+"/relations", types.ManagedRelationInput{SubjectID: parent.ID, RelationType: "belongs_to", ObjectID: child.ID}, http.StatusBadRequest, nil)
	request(http.MethodPatch, "/entities/"+parent.ID, map[string]any{"revision": parent.Revision, "archived": true}, http.StatusBadRequest, nil)
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "attributes": map[string]any{"capacity": "wrong type"}}, http.StatusBadRequest, nil)
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "name": "Fixture Model A Updated", "attributes": map[string]any{"capacity": 5100}}, http.StatusOK, &child)
	require.EqualValues(t, 2, child.Revision)
	require.Contains(t, child.Aliases, "Fixture Model A")
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": 1, "name": "stale"}, http.StatusConflict, nil)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_mode": "extracted"}, http.StatusBadRequest, nil)
	request(http.MethodDelete, "/libraries/"+library.ID, nil, http.StatusBadRequest, nil)
	var history types.OperationLogPage
	request(http.MethodGet, "/entities/"+child.ID+"/history", nil, http.StatusOK, &history)
	require.GreaterOrEqual(t, history.Total, int64(3))
	entry := types.Entry{ID: "managed-manual", SpaceID: 1, LibraryID: library.ID, Type: types.EntryTypeManual, Title: "规格书", VaultBody: "Fixture Model A specifications", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, f.db.Create(&entry).Error)
	linkURL := "/entries/" + entry.ID + "/entity-links"
	request(http.MethodPut, linkURL+"/"+child.ID, map[string]any{"excluded": false}, http.StatusOK, nil)
	var links []service.ManagedEntityLink
	request(http.MethodGet, linkURL, nil, http.StatusOK, &links)
	require.Len(t, links, 1)
	require.Equal(t, "manual", links[0].Origin)
	require.False(t, links[0].Excluded)
	require.NoError(t, f.db.Model(&types.EntryEntity{}).Where("entry_id=?", entry.ID).Update("evidence", types.JSON(`[{"paragraph":1,"start":0,"end":5}]`)).Error)
	request(http.MethodGet, linkURL, nil, http.StatusOK, &links)
	require.Len(t, links[0].Evidence, 1)
	require.NoError(t, f.db.Model(&entry).Update("vault_body", "\n"+entry.VaultBody).Error)
	request(http.MethodGet, linkURL, nil, http.StatusOK, &links)
	require.Empty(t, links[0].Evidence, "even whitespace changes invalidate precise coordinates")
	request(http.MethodPut, linkURL+"/"+child.ID, map[string]any{"excluded": true}, http.StatusOK, nil)
	request(http.MethodGet, linkURL, nil, http.StatusOK, &links)
	require.True(t, links[0].Excluded)
	var detail types.EntityDetail
	request(http.MethodGet, "/entities/"+child.ID, nil, http.StatusOK, &detail)
	require.Empty(t, detail.Entries)
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "archived": true}, http.StatusOK, &child)
	require.NotNil(t, child.ArchivedAt)
	var entities []types.Entity
	request(http.MethodGet, "/entities?library_id="+library.ID, nil, http.StatusOK, &entities)
	require.Len(t, entities, 1)
	request(http.MethodGet, "/entities?library_id="+library.ID+"&include_archived=true", nil, http.StatusOK, &entities)
	require.Len(t, entities, 2)
	var restored types.Entity
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "archived": false}, http.StatusOK, &restored)
	require.Nil(t, restored.ArchivedAt)
	request(http.MethodDelete, "/relations/"+relation.ID, nil, http.StatusConflict, nil)
	request(http.MethodDelete, "/relations/"+relation.ID+"?version=1", nil, http.StatusOK, nil)
}
