package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

// Spec (medium), MEL-04: business time controls the relation timeline over HTTP.
func TestManagedRelationBusinessTime(t *testing.T) {
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
			var envelope struct{ Data json.RawMessage }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			require.NoError(t, json.Unmarshal(envelope.Data, output))
		}
	}
	var library types.Library
	request(http.MethodPost, "/libraries", map[string]any{
		"directory": "relation-time", "name": "Relation timeline", "entity_mode": "managed",
		"entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product", ParentKinds: []string{"product"}}}},
	}, http.StatusCreated, &library)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"relation_types": []types.LibraryRelationType{{Name: "belongs", Enabled: true, Hierarchical: true}}}, http.StatusOK, nil)
	createEntity := func(name string) types.Entity {
		var entity types.Entity
		request(http.MethodPost, "/libraries/"+library.ID+"/entities", types.ManagedEntityInput{Name: name, Kind: "product"}, http.StatusCreated, &entity)
		return entity
	}
	child, parent := createEntity("Model"), createEntity("Line A")
	start := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	var relation types.Relation
	request(http.MethodPost, "/libraries/"+library.ID+"/relations", map[string]any{"subject_id": child.ID, "object_id": parent.ID, "relation_type": "belongs", "valid_from": start}, http.StatusCreated, &relation)
	require.True(t, relation.ValidFrom.Equal(start), "valid_from must retain the supplied business time")
	var groups []service.RelationGroup
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(start.Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.Len(t, groups, 1)
	require.Equal(t, relation.ID, groups[0].Relations[0].ID)
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(start.Add(-time.Second).Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.Empty(t, groups)
	parentB := createEntity("Line B")
	change := start.Add(30 * 24 * time.Hour)
	end := change.Add(30 * 24 * time.Hour)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	for _, invalid := range []string{future, "0001-01-01T00:00:00Z", "2025-03-01", "invalid", ""} {
		request(http.MethodPost, "/libraries/"+library.ID+"/relations", map[string]any{"subject_id": parentB.ID, "object_id": parent.ID, "relation_type": "belongs", "valid_from": invalid}, http.StatusBadRequest, nil)
		request(http.MethodPatch, "/relations/"+relation.ID, map[string]any{"version": relation.Version, "subject_id": child.ID, "object_id": parentB.ID, "relation_type": "belongs", "valid_from": invalid}, http.StatusBadRequest, nil)
		request(http.MethodDelete, "/relations/"+relation.ID+"?version=1&valid_to="+url.QueryEscape(invalid), nil, http.StatusBadRequest, nil)
	}
	// A successor must leave the old row a nonempty interval.
	for _, invalid := range []time.Time{start.Add(-time.Second), start} {
		request(http.MethodPatch, "/relations/"+relation.ID, map[string]any{"version": relation.Version, "subject_id": child.ID, "object_id": parentB.ID, "relation_type": "belongs", "valid_from": invalid}, http.StatusBadRequest, nil)
		request(http.MethodDelete, "/relations/"+relation.ID+"?version=1&valid_to="+url.QueryEscape(invalid.Format(time.RFC3339)), nil, http.StatusBadRequest, nil)
	}
	oldID := relation.ID
	request(http.MethodPatch, "/relations/"+relation.ID, map[string]any{"version": relation.Version, "subject_id": child.ID, "object_id": parentB.ID, "relation_type": "belongs", "valid_from": change}, http.StatusOK, &relation)
	require.NotEqual(t, oldID, relation.ID)
	require.True(t, relation.ValidFrom.Equal(change))
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(start.Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Relations, 1)
	require.Equal(t, oldID, groups[0].Relations[0].ID)
	require.True(t, groups[0].Relations[0].ValidTo.Equal(change))
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(change.Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.Len(t, groups[0].Relations, 1)
	require.Equal(t, relation.ID, groups[0].Relations[0].ID)
	request(http.MethodDelete, "/relations/"+relation.ID+"?version=1&valid_to="+url.QueryEscape(end.Format(time.RFC3339)), nil, http.StatusOK, nil)
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(change.Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.True(t, groups[0].Relations[0].ValidTo.Equal(end))
	request(http.MethodGet, "/entities/"+child.ID+"/relations?as_of="+url.QueryEscape(end.Format(time.RFC3339)), nil, http.StatusOK, &groups)
	require.Empty(t, groups)
	// Both old rows are ended, but their parents still constrain backdated writes.
	for _, overlap := range []time.Time{start.Add(-time.Hour), start, change} {
		request(http.MethodPost, "/libraries/"+library.ID+"/relations", map[string]any{"subject_id": child.ID, "object_id": parent.ID, "relation_type": "belongs", "valid_from": overlap}, http.StatusBadRequest, nil)
	}
	request(http.MethodPost, "/libraries/"+library.ID+"/relations", map[string]any{"subject_id": child.ID, "object_id": parent.ID, "relation_type": "belongs", "valid_from": end}, http.StatusCreated, &relation)
}
