package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

type managedNamesFixture struct {
	*mcpFixture
	t         *testing.T
	libraryID string
}

// Medium spec, product shelf task 01 / ADR 0023: names, aliases and codes are
// one namespace per kind, and confirmation never bypasses same-kind uniqueness.
func TestManagedNamesSameKindRejectsDuplicateSpellings(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var owner types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "Original Product", "kind": "model", "aliases": []string{"Legacy Product"}, "code": "PROD-401"}, http.StatusCreated, &owner)
	for _, field := range []string{"name", "aliases", "code"} {
		for _, spelling := range []string{"original product", "legacy product", "prod-401"} {
			t.Run(field+"/"+spelling, func(t *testing.T) {
				fixture := *f
				fixture.t = t
				f := &fixture
				input := map[string]any{"name": "Another Product", "kind": "model", "name_confirmations": []map[string]any{{"name": spelling, "other_entity_id": owner.ID, "other_kind": "model"}}}
				if field == "aliases" {
					input[field] = []string{spelling}
				} else {
					input[field] = spelling
				}
				f.request(http.MethodPost, path, input, http.StatusConflict, nil)
			})
		}
	}
	var listed []types.Entity
	f.request(http.MethodGet, "/entities?library_id="+f.libraryID, nil, http.StatusOK, &listed)
	require.Len(t, listed, 1, "all rejected creates roll back")
}

// Medium spec, product shelf task 01 / ADR 0023: every cross-field spelling
// collision is confirmable across kinds, without rewriting the submitted name.
func TestManagedNamesCrossKindSpellingsCanBeConfirmed(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	index := 0
	for _, field := range []string{"name", "aliases", "code"} {
		for _, tokenField := range []string{"name", "aliases", "code"} {
			index++
			t.Run(field+"/"+tokenField, func(t *testing.T) {
				fixture := *f
				fixture.t = t
				f := &fixture
				name, alias, code := fmt.Sprintf("  Original %d  ", index), fmt.Sprintf("  Legacy %d  ", index), fmt.Sprintf("PROD-%d", index)
				var owner, saved types.Entity
				f.request(http.MethodPost, path, map[string]any{"name": name, "aliases": []string{alias}, "code": code, "kind": "platform"}, http.StatusCreated, &owner)
				input := map[string]any{"name": fmt.Sprintf("Distinct %d", index), "kind": "model"}
				spelling := map[string]string{"name": name, "aliases": alias, "code": code}[tokenField]
				if field == "aliases" {
					input[field] = []string{spelling}
				} else {
					input[field] = spelling
				}
				f.request(http.MethodPost, path, input, http.StatusConflict, nil)
				input["name_confirmations"] = []map[string]any{{"name": spelling, "other_entity_id": owner.ID, "other_kind": "platform"}}
				f.request(http.MethodPost, path, input, http.StatusCreated, &saved)
				require.Equal(t, input["name"], saved.CanonicalName)
				if field == "aliases" {
					require.Contains(t, saved.Aliases, spelling)
					var detail types.EntityDetail
					f.request(http.MethodGet, "/entities/"+saved.ID, nil, http.StatusOK, &detail)
					require.Contains(t, detail.Entity.Aliases, spelling)
				}
			})
		}
	}
}

// Medium spec, product shelf task 01 / ADR 0023: confirmed pairs survive a fresh
// service instance, but cannot approve a third kind or a change in either kind.
func TestManagedNamesConfirmationSurvivesServiceRestart(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var owner, model types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "Shared Product", "kind": "platform"}, http.StatusCreated, &owner)
	f.request(http.MethodPost, path, map[string]any{"name": "Shared Product", "kind": "model", "name_confirmations": []map[string]any{{"name": "Shared Product", "other_entity_id": owner.ID, "other_kind": "platform"}}}, http.StatusCreated, &model)
	// Rebuild the public routes with a new entity service over the same database.
	r := gin.New()
	r.Use(middleware.ErrorHandler(), middleware.Auth(f.params.SpaceService, f.params.UserService, f.params.APIKeyService))
	RegisterEntityRoutes(r.Group("/api/v1"), handler.NewEntityHandler(service.NewEntityService(f.db, nil)))
	f.router = r
	f.request(http.MethodPatch, "/entities/"+model.ID, map[string]any{"revision": model.Revision, "name": "Shared Product"}, http.StatusOK, &model)
	f.request(http.MethodPatch, "/entities/"+model.ID, map[string]any{"revision": model.Revision, "kind": "component"}, http.StatusConflict, nil)
	f.request(http.MethodPatch, "/entities/"+model.ID, map[string]any{"revision": model.Revision, "kind": "platform"}, http.StatusConflict, nil)
	// Existing confirmation does not approve another entity with the same spelling.
	f.request(http.MethodPost, path, map[string]any{"name": "Shared Product", "kind": "component", "name_confirmations": []map[string]any{{"name": "Shared Product", "other_entity_id": owner.ID, "other_kind": "platform"}}}, http.StatusConflict, nil)
	f.request(http.MethodPatch, "/entities/"+model.ID, map[string]any{"revision": model.Revision, "kind": "component", "name_confirmations": []map[string]any{{"name": "Shared Product", "other_entity_id": owner.ID, "other_kind": "platform"}}}, http.StatusOK, &model)
	// Confirmation is symmetric for subsequent edits by either owner.
	f.request(http.MethodPatch, "/entities/"+owner.ID, map[string]any{"revision": owner.Revision, "description": "Changed"}, http.StatusOK, &owner)
}

// Medium spec, product shelf task 01 / ADR 0023: existing extracted identities
// keep their IDs on conversion, with the same confirmation rules as new entries.
func TestManagedNamesConversionUsesMappedKinds(t *testing.T) {
	f := newManagedNamesFixture(t)
	var extracted types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Conversion", "directory": "name-conversion"}, http.StatusCreated, &extracted)
	for _, entity := range []types.Entity{
		{ID: "converted-company", SpaceID: 1, LibraryID: extracted.ID, EntityType: "company", CanonicalName: "Shared Conversion", NormalizedName: "shared conversion"},
		{ID: "converted-product", SpaceID: 1, LibraryID: extracted.ID, EntityType: "product", CanonicalName: "Shared Conversion", NormalizedName: "shared conversion"},
	} {
		require.NoError(t, f.db.Create(&entity).Error)
	}
	input := map[string]any{"entity_mode": "managed", "entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "platform", Name: "平台"}, {Key: "model", Name: "型号"}}}, "entity_kind_mapping": map[string]string{"company": "platform", "product": "model"}}
	f.request(http.MethodPut, "/libraries/"+extracted.ID, input, http.StatusConflict, nil)
	input["name_confirmations"] = []map[string]any{{"entity_id": "converted-product", "name": "Shared Conversion", "other_entity_id": "converted-company", "other_kind": "platform"}}
	f.request(http.MethodPut, "/libraries/"+extracted.ID, input, http.StatusOK, &extracted)
	var listed []types.Entity
	f.request(http.MethodGet, "/entities?library_id="+extracted.ID, nil, http.StatusOK, &listed)
	require.Len(t, listed, 2)
	for _, entity := range listed {
		require.Contains(t, []string{"converted-company", "converted-product"}, entity.ID)
		require.Contains(t, []string{"platform", "model"}, entity.Kind)
	}
}

// Medium spec, product shelf task 01 / ADR 0023: code comparisons use the same
// normalized spelling as names and aliases, while preserving the supplied code.
func TestManagedNamesNormalizedCodesShareTheNameNamespace(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var owner types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "Coded Product", "kind": "model", "code": " ＰＲＯＤ  ８ "}, http.StatusCreated, &owner)
	require.Equal(t, " ＰＲＯＤ  ８ ", owner.Code)
	f.request(http.MethodPost, path, map[string]any{"name": "prod 8", "kind": "model"}, http.StatusConflict, nil)
	f.request(http.MethodPost, path, map[string]any{"name": "prod 8", "kind": "platform"}, http.StatusConflict, nil)
}

// Medium spec, product shelf task 01 / ADR 0022 and 0023: archive releases
// names, retains code ownership, and restore rechecks current active owners.
func TestManagedNamesArchiveAndRestoreRespectCurrentOwners(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var archived, replacement, platform types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "Reusable Name", "kind": "model", "code": "RESERVED"}, http.StatusCreated, &archived)
	f.request(http.MethodPatch, "/entities/"+archived.ID, map[string]any{"revision": archived.Revision, "archived": true}, http.StatusOK, &archived)
	f.request(http.MethodPost, path, map[string]any{"name": "Other Name", "kind": "model", "code": "reserved"}, http.StatusConflict, nil)
	f.request(http.MethodPost, path, map[string]any{"name": "Reusable Name", "kind": "model"}, http.StatusCreated, &replacement)
	f.request(http.MethodPatch, "/entities/"+archived.ID, map[string]any{"revision": archived.Revision, "archived": false}, http.StatusConflict, nil)
	f.request(http.MethodDelete, "/entities/"+replacement.ID, map[string]any{"revision": replacement.Revision}, http.StatusOK, nil)
	f.request(http.MethodPost, path, map[string]any{"name": "Reusable Name", "kind": "platform"}, http.StatusCreated, &platform)
	f.request(http.MethodPatch, "/entities/"+archived.ID, map[string]any{"revision": archived.Revision, "archived": false}, http.StatusConflict, nil)
	f.request(http.MethodPatch, "/entities/"+archived.ID, map[string]any{"revision": archived.Revision, "archived": false, "name_confirmations": []map[string]any{{"name": "Reusable Name", "other_entity_id": platform.ID, "other_kind": "platform"}}}, http.StatusOK, &archived)
}

// Medium spec, product shelf task 01 / ADR 0023: split uses the same name
// confirmation contract and a rejected split leaves the source revision intact.
func TestManagedNamesSplitRequiresConfirmation(t *testing.T) {
	f := newManagedNamesFixture(t)
	var source, split types.Entity
	f.request(http.MethodPost, "/libraries/"+f.libraryID+"/entities", map[string]any{"name": "Shared Split", "kind": "platform"}, http.StatusCreated, &source)
	newEntity := map[string]any{"name": "Shared Split", "kind": "model", "attributes": map[string]any{}}
	input := map[string]any{"revision": source.Revision, "new_entity": newEntity}
	f.request(http.MethodPost, "/entities/"+source.ID+"/split", input, http.StatusConflict, nil)
	newEntity["name_confirmations"] = []map[string]any{{"name": "Shared Split", "other_entity_id": source.ID, "other_kind": "platform"}}
	f.request(http.MethodPost, "/entities/"+source.ID+"/split", input, http.StatusCreated, &split)
	require.NotEqual(t, source.ID, split.ID)
}

// Medium spec, product shelf task 01 / ADR 0023: aliases moved by a merge
// require confirmation against the target kind, with atomic rollback on refusal.
func TestManagedNamesMergeRechecksTransferredSpellings(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var peer, source, target types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "Component", "kind": "component", "code": "TRANSFERRED"}, http.StatusCreated, &peer)
	confirmation := []map[string]any{{"name": "TRANSFERRED", "other_entity_id": peer.ID, "other_kind": "component"}}
	f.request(http.MethodPost, path, map[string]any{"name": "Source", "kind": "platform", "code": "TRANSFERRED", "name_confirmations": confirmation}, http.StatusCreated, &source)
	f.request(http.MethodPost, path, map[string]any{"name": "Target", "kind": "model"}, http.StatusCreated, &target)
	input := map[string]any{"revision": target.Revision, "source_id": source.ID, "source_revision": source.Revision, "attributes": map[string]any{}}
	f.request(http.MethodPost, "/entities/"+target.ID+"/merge", input, http.StatusConflict, nil)
	var unchanged types.EntityDetail
	f.request(http.MethodGet, "/entities/"+source.ID, nil, http.StatusOK, &unchanged)
	require.Equal(t, source.ID, unchanged.Entity.ID)
	require.Equal(t, source.Revision, unchanged.Entity.Revision)
	input["name_confirmations"] = confirmation
	f.request(http.MethodPost, "/entities/"+target.ID+"/merge", input, http.StatusOK, &target)
	require.Contains(t, target.Aliases, "TRANSFERRED")
}

// Medium spec, product shelf task 01 / ADR 0023: a rejected edit cannot retain
// part of its confirmations, nor advance the entity revision.
func TestManagedNamesPartialConfirmationRollsBack(t *testing.T) {
	f := newManagedNamesFixture(t)
	path := "/libraries/" + f.libraryID + "/entities"
	var platform, component, model types.Entity
	f.request(http.MethodPost, path, map[string]any{"name": "First Alias", "kind": "platform"}, http.StatusCreated, &platform)
	f.request(http.MethodPost, path, map[string]any{"name": "Second Alias", "kind": "component"}, http.StatusCreated, &component)
	f.request(http.MethodPost, path, map[string]any{"name": "New Model", "kind": "model"}, http.StatusCreated, &model)
	first := map[string]any{"name": "First Alias", "other_entity_id": platform.ID, "other_kind": "platform"}
	second := map[string]any{"name": "Second Alias", "other_entity_id": component.ID, "other_kind": "component"}
	input := map[string]any{"revision": model.Revision, "aliases": []string{"First Alias", "Second Alias"}, "name_confirmations": []map[string]any{first}}
	f.request(http.MethodPatch, "/entities/"+model.ID, input, http.StatusConflict, nil)
	input["name_confirmations"] = []map[string]any{second}
	f.request(http.MethodPatch, "/entities/"+model.ID, input, http.StatusConflict, nil)
	input["name_confirmations"] = []map[string]any{first, second}
	f.request(http.MethodPatch, "/entities/"+model.ID, input, http.StatusOK, &model)
}

func newManagedNamesFixture(t *testing.T) *managedNamesFixture {
	t.Helper()
	f := &managedNamesFixture{mcpFixture: newMCPFixture(t), t: t}
	var library types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{
		"directory": "name-contracts", "name": "名称测试库", "entity_mode": "managed",
		"entity_schema": types.EntitySchema{Kinds: []types.EntityKind{{Key: "platform", Name: "平台"}, {Key: "model", Name: "型号"}, {Key: "component", Name: "部件"}}},
	}, http.StatusCreated, &library)
	f.libraryID = library.ID
	return f
}

func (f *managedNamesFixture) request(method, path string, input any, status int, output any) json.RawMessage {
	f.t.Helper()
	body := ""
	if input != nil {
		encoded, err := json.Marshal(input)
		require.NoError(f.t, err)
		body = string(encoded)
	}
	response := personalRequest(f.t, f.router, method, "/api/v1"+path, body, f.key, true)
	require.Equal(f.t, status, response.Code, response.Body.String())
	if output != nil {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(f.t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.NoError(f.t, json.Unmarshal(envelope.Data, output))
	}
	return response.Body.Bytes()
}

// Medium spec, product shelf task 01 / ADR 0023: conflict responses, entity
// lists and entity details distinguish same-name owners by their kind labels.
func TestManagedNamesResponsesIdentifyCrossKindOwners(t *testing.T) {
	f := newManagedNamesFixture(t)
	var platform, model types.Entity
	path := "/libraries/" + f.libraryID + "/entities"
	f.request(http.MethodPost, path, map[string]any{"name": "Shared Platform", "kind": "platform"}, http.StatusCreated, &platform)
	body := f.request(http.MethodPost, path, map[string]any{"name": "Shared Platform", "kind": "model"}, http.StatusConflict, nil)
	var conflict struct {
		Error struct {
			Details struct {
				Type      string `json:"type"`
				Conflicts []struct {
					Name           string `json:"name"`
					OtherEntityID  string `json:"other_entity_id"`
					OtherKind      string `json:"other_kind"`
					OtherKindLabel string `json:"other_kind_label"`
				} `json:"conflicts"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &conflict))
	require.Equal(t, "cross_kind_name_confirmation_required", conflict.Error.Details.Type)
	require.Len(t, conflict.Error.Details.Conflicts, 1)
	require.Equal(t, platform.ID, conflict.Error.Details.Conflicts[0].OtherEntityID)
	require.Equal(t, "platform", conflict.Error.Details.Conflicts[0].OtherKind)
	require.Equal(t, "平台", conflict.Error.Details.Conflicts[0].OtherKindLabel)
	f.request(http.MethodPost, path, map[string]any{
		"name": "Shared Platform", "kind": "model",
		"name_confirmations": []map[string]any{{"name": "Shared Platform", "other_entity_id": platform.ID, "other_kind": "platform"}},
	}, http.StatusCreated, &model)
	require.NotEqual(t, platform.ID, model.ID)
	var listed []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		KindLabel string `json:"kind_label"`
	}
	f.request(http.MethodGet, "/entities?library_id="+f.libraryID+"&entity=Shared", nil, http.StatusOK, &listed)
	require.Len(t, listed, 2)
	for _, item := range listed {
		require.Equal(t, map[string]string{"platform": "平台", "model": "型号"}[item.Kind], item.KindLabel)
		var detail struct {
			Entity struct {
				KindLabel string `json:"kind_label"`
			} `json:"entity"`
		}
		f.request(http.MethodGet, "/entities/"+item.ID, nil, http.StatusOK, &detail)
		require.Equal(t, item.KindLabel, detail.Entity.KindLabel)
	}
}
