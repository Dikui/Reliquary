package router

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

func TestManagedLibraryEvolution(t *testing.T) {
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
	library := f.library
	localDoc := f.seedArticle(t, "Existing specification", "", "Original document body")
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"relation_types": []types.LibraryRelationType{{Name: "compatible", Enabled: true}}}, http.StatusOK, nil)
	parent := types.Entity{ID: "existing-platform", SpaceID: 1, LibraryID: library.ID, CanonicalName: "Existing platform", NormalizedName: "existing platform", EntityType: "product"}
	child := types.Entity{ID: "existing-model", SpaceID: 1, LibraryID: library.ID, CanonicalName: "Existing model", NormalizedName: "existing model", EntityType: "product"}
	require.NoError(t, f.db.Create(&parent).Error)
	require.NoError(t, f.db.Create(&child).Error)
	relation := types.Relation{ID: "existing-relation", SpaceID: 1, LibraryID: library.ID, SubjectID: child.ID, ObjectID: parent.ID, RelationType: "compatible", Version: 1}
	require.NoError(t, f.db.Create(&relation).Error)
	require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: localDoc.ID, EntityID: child.ID}).Error)
	schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "model", Name: "Model"}}, Fields: []types.EntityFieldDefinition{
		{Key: "protocols", Name: "Protocols", Type: "multiselect", Options: []string{"modbus", "can"}},
		{Key: "notes", Name: "Notes", Type: "list"}, {Key: "platform", Name: "Platform", Type: "entity_ref"},
		{Key: "website", Name: "Website", Type: "url"}, {Key: "manual", Name: "Manual", Type: "attachment"},
	}}
	var preview service.EntityConversionPreview
	request(http.MethodGet, "/libraries/"+library.ID+"/entity-conversion", nil, http.StatusOK, &preview)
	require.EqualValues(t, 1, preview.EntryCount)
	require.EqualValues(t, 2, preview.EntityCounts["product"])
	// Mapping is explicit; a rejected conversion must preserve the source catalog.
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_mode": "managed", "entity_schema": schema}, http.StatusBadRequest, nil)
	var unchanged types.Library
	require.NoError(t, f.db.First(&unchanged, "id=?", library.ID).Error)
	require.Equal(t, "extracted", unchanged.EntityMode)
	// Conversion cannot make previously distinct entity types ambiguous by name.
	duplicate := types.Entity{ID: "same-name-organization", SpaceID: 1, LibraryID: library.ID, CanonicalName: child.CanonicalName, NormalizedName: child.NormalizedName, EntityType: "organization"}
	require.NoError(t, f.db.Create(&duplicate).Error)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_mode": "managed", "entity_schema": schema, "entity_kind_mapping": map[string]string{"product": "model", "organization": "model"}}, http.StatusConflict, nil)
	require.NoError(t, f.db.Delete(&duplicate).Error)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"entity_mode": "managed", "entity_schema": schema, "entity_kind_mapping": map[string]string{"product": "model"}}, http.StatusOK, &library)
	var detail types.EntityDetail
	request(http.MethodGet, "/entities/"+child.ID, nil, http.StatusOK, &detail)
	require.Equal(t, "model", detail.Entity.Kind)
	require.Len(t, detail.Entries, 1)
	child = detail.Entity
	var localLinks []service.ManagedEntityLink
	request(http.MethodGet, "/entries/"+localDoc.ID+"/entity-links", nil, http.StatusOK, &localLinks)
	require.Equal(t, "manual", localLinks[0].Origin)

	// Real attachments use the configured file store and survive field snapshots.
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "fixture-manual.pdf")
	require.NoError(t, err)
	content := []byte("%PDF-1.4\nSynthetic manual\n")
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	upload := httptest.NewRequest(http.MethodPost, "/api/v1/libraries/"+library.ID+"/entity-attachments", &body)
	upload.Header.Set("Content-Type", form.FormDataContentType())
	upload.Header.Set("X-API-Key", f.key)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, upload)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var uploaded struct {
		Data types.EntityAttachment `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &uploaded))
	downloaded := personalRequest(t, f.router, http.MethodGet, "/api/v1/entity-attachments/"+uploaded.Data.ID+"/download", "", f.key, true)
	require.Equal(t, http.StatusOK, downloaded.Code)
	require.Equal(t, content, downloaded.Body.Bytes())
	require.Contains(t, downloaded.Header().Get("Content-Disposition"), "attachment")
	attrs := map[string]any{"protocols": []string{"modbus", "can"}, "notes": []string{"first", "second"}, "platform": parent.ID, "website": "https://example.org/manual", "manual": uploaded.Data.ID}
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "attributes": attrs}, http.StatusOK, &child)
	require.Equal(t, parent.ID, child.Attributes["platform"])
	request(http.MethodPatch, "/entities/"+child.ID, map[string]any{"revision": child.Revision, "attributes": map[string]any{"platform": "missing-product"}}, http.StatusBadRequest, nil)

	// Literature can live in its own library and carry a precise relationship type.
	var papers types.Library
	request(http.MethodPost, "/libraries", map[string]any{"name": "Papers", "directory": "evolution-papers", "product_library_ids": []string{library.ID}}, http.StatusCreated, &papers)
	external := types.Entry{ID: "external-specification", SpaceID: 1, LibraryID: papers.ID, Type: types.EntryTypeManual, Title: "External test report", VaultBody: "Existing model was tested", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, f.db.Create(&external).Error)
	localEntity := types.Entity{ID: "paper-local-entity", SpaceID: 1, LibraryID: papers.ID, CanonicalName: "Local extracted entity", NormalizedName: "local extracted entity", EntityType: "company"}
	require.NoError(t, f.db.Create(&localEntity).Error)
	require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: external.ID, EntityID: localEntity.ID}).Error)
	request(http.MethodPut, "/entries/"+external.ID+"/entity-links/"+child.ID, map[string]any{"excluded": false, "relation_type": "test_report"}, http.StatusOK, nil)
	var links []service.ManagedEntityLink
	request(http.MethodGet, "/entries/"+external.ID+"/entity-links", nil, http.StatusOK, &links)
	require.Len(t, links, 1)
	require.Equal(t, "test_report", links[0].RelationType)
	request(http.MethodGet, "/entities/"+child.ID, nil, http.StatusOK, &detail)
	require.Len(t, detail.Entries, 2)
	request(http.MethodPut, "/libraries/"+library.ID, map[string]any{"relation_types": []types.LibraryRelationType{{Name: "compatible", Enabled: true, Fields: []types.EntityFieldDefinition{{Key: "quantity", Name: "Quantity", Type: "number"}, {Key: "condition", Name: "Condition", Type: "text"}}}}}, http.StatusOK, nil)
	request(http.MethodPatch, "/relations/"+relation.ID, types.ManagedRelationInput{Version: relation.Version, SubjectID: child.ID, ObjectID: parent.ID, RelationType: "compatible", Attributes: map[string]any{"quantity": 2, "condition": "Firmware 2 or newer"}}, http.StatusOK, &relation)
	require.EqualValues(t, 2, relation.Attributes["quantity"])
	var stored types.Entry
	require.NoError(t, f.db.First(&stored, "id=?", localDoc.ID).Error)
	require.Equal(t, localDoc.VaultBody, stored.VaultBody)
}
