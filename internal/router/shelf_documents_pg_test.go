package router

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

func shelfUpload(t *testing.T, f *managedNamesFixture, libraryID, content string, status int) types.EntityAttachment {
	t.Helper()
	return shelfUploadNamed(t, f, libraryID, "synthetic-manual.pdf", content, status)
}

func shelfUploadNamed(t *testing.T, f *managedNamesFixture, libraryID, name, content string, status int) types.EntityAttachment {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, form.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/libraries/"+libraryID+"/entity-attachments", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-API-Key", f.key)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, status, response.Code, response.Body.String())
	var uploaded struct{ Data types.EntityAttachment }
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &uploaded))
	}
	return uploaded.Data
}

// Medium spec, task 07 / ADR 0024: classification is confirmed before a
// version exists, and both replaced and current originals remain downloadable.
func TestShelfDocumentsOriginalVersionsRequireConfirmation(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var model, requirement types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Synthetic document model"}, http.StatusCreated, &model)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "document_requirement", "name": "Synthetic requirement"}, http.StatusCreated, &requirement)
	attachment := shelfUpload(t, f, library.ID, "%PDF synthetic first original", http.StatusCreated)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "agent", "agent_name": "Synthetic document assistant"}, "operations": []any{map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"name": " Customer manual ", "owner_id": model.ID, "requirement_id": requirement.ID, "attachment_id": attachment.ID, "version_label": " Rev A ", "stage": "draft", "origin": "Synthetic supplier", "occurred_at": "2026-11-01T00:00:00Z"}}}}, http.StatusCreated, &draft)
	fileID := draft.Items[0].EntityID
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusNotFound, nil)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "document", "action": "apply"}}}, http.StatusOK, &draft)
	var versions []map[string]any
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 1)
	require.Equal(t, " Rev A ", versions[0]["version_label"])
	require.Equal(t, "Synthetic supplier", versions[0]["origin"])
	require.Equal(t, "2026-11-01T00:00:00Z", versions[0]["occurred_at"])
	require.Equal(t, "not_parsed", versions[0]["parse_status"])
	require.Equal(t, "Synthetic document assistant", versions[0]["source"].(map[string]any)["agent_name"])
	firstID := versions[0]["id"].(string)
	second := shelfUpload(t, f, library.ID, "%PDF synthetic second original", http.StatusCreated)
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "replacement", "type": "add_document_version", "document": map[string]any{"owner_id": model.ID, "requirement_id": requirement.ID, "attachment_id": second.ID, "stage": "final"}}}}, http.StatusCreated, &draft)
	require.Equal(t, fileID, draft.Items[0].EntityID)
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 2)
	require.Equal(t, "2", versions[0]["version_label"])
	require.Equal(t, true, versions[0]["is_current"])
	require.Equal(t, false, versions[1]["is_current"])
	require.Equal(t, firstID, versions[1]["id"])
	for i, content := range []string{"%PDF synthetic second original", "%PDF synthetic first original"} {
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities/"+fileID+"/document-versions/"+versions[i]["id"].(string)+"/download", "", f.key, true)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, content, response.Body.String())
	}
	f.request(http.MethodDelete, "/entity-attachments/"+attachment.ID, nil, http.StatusConflict, nil)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	require.Equal(t, " Customer manual ", detail.Entity.CanonicalName)
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+fileID+"/history?source_type=agent&agent_name=Synthetic%20document%20assistant", nil, http.StatusOK, &history)
	require.NotZero(t, history.Total)
}

// Medium spec, task 07: an explicitly classified cloud link is a current
// draft/final version, without downloading or parsing the external content.
func TestShelfDocumentsCloudLinksClassifyOnEachAllowedNode(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var requirement types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "document_requirement", "name": "Cloud document requirement"}, http.StatusCreated, &requirement)
	for _, kind := range []string{"model", "part", "product", "platform"} {
		var owner types.Entity
		f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": kind, "name": "Link owner " + kind}, http.StatusCreated, &owner)
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "link", "type": "add_document_version", "document": map[string]any{"owner_id": owner.ID, "requirement_id": requirement.ID, "name": "Link file " + kind, "cloud_url": "https://example.org/cloud/manual", "stage": "final"}}}}, http.StatusCreated, &draft)
		var versions []map[string]any
		f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID+"/document-versions", nil, http.StatusOK, &versions)
		require.Equal(t, "https://example.org/cloud/manual", versions[0]["cloud_url"])
		require.Equal(t, requirement.ID, versions[0]["requirement_id"])
		require.Equal(t, "final", versions[0]["stage"])
		require.Equal(t, "not_parsed", versions[0]["parse_status"])
		require.NotContains(t, versions[0], "attachment_id")
		f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID+"/document-versions/"+versions[0]["id"].(string)+"/download", nil, http.StatusNotFound, nil)
	}
}

// Medium spec, task 07 / ADR 0024: invalid classification stays a correctable
// proposal; accepting it cannot persist a file or an invalid external link.
func TestShelfDocumentsRejectInvalidClassification(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var owner types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Validation owner"}, http.StatusCreated, &owner)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	for _, tc := range []struct {
		name, key string
		value     any
	}{
		{"unsafe link", "cloud_url", "javascript:alert(1)"},
		{"unknown owner", "owner_id", "missing"},
		{"unknown requirement", "requirement_id", "missing"},
		{"wrong requirement kind", "requirement_id", owner.ID},
		{"missing attachment", "attachment_id", "missing"},
		{"empty stage", "stage", ""},
		{"zero timestamp", "occurred_at", "0001-01-01T00:00:00Z"},
		{"unidentified certificate", "certificate_number", "Unknown number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *f
			copy.t = t
			f := &copy
			record := map[string]any{"name": "Invalid document", "owner_id": owner.ID, "cloud_url": "https://example.org/cloud", "stage": "final"}
			record[tc.key] = tc.value
			var draft types.ShelfChangeset
			f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "invalid", "type": "add_document_version", "document": record}}}, http.StatusCreated, &draft)
			require.NotEmpty(t, draft.Items[0].Conflicts)
			require.NotContains(t, []string{"new", "modified"}, draft.Items[0].Conflicts[0].Type)
			f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "invalid", "action": "apply"}}}, http.StatusConflict, nil)
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID, nil, http.StatusNotFound, nil)
		})
	}
}

// Medium spec, task 07: the library limit applies to actual upload bytes and
// is checked again when a staged original is confirmed after a settings change.
func TestShelfDocumentsConfiguredSizeLimitIsEnforced(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID
	f.request(http.MethodPut, path, map[string]any{"shelf_config": map[string]any{"max_file_bytes": 8}}, http.StatusOK, &library)
	attachment := shelfUpload(t, f, library.ID, "12345678", http.StatusCreated)
	shelfUpload(t, f, library.ID, "123456789", http.StatusBadRequest)
	var owner types.Entity
	f.request(http.MethodPost, path+"/entities", map[string]any{"kind": "model", "name": "Sized document owner"}, http.StatusCreated, &owner)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "sized", "type": "add_document_version", "document": map[string]any{"owner_id": owner.ID, "name": "Sized original", "attachment_id": attachment.ID, "stage": "final"}}}}, http.StatusCreated, &draft)
	f.request(http.MethodPut, path, map[string]any{"shelf_config": map[string]any{"max_file_bytes": 7}}, http.StatusOK, nil)
	f.request(http.MethodPost, path+"/shelf/changesets/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "sized", "action": "apply"}}}, http.StatusConflict, nil)
	f.request(http.MethodPut, path, map[string]any{"shelf_config": map[string]any{"max_file_bytes": -1}}, http.StatusBadRequest, nil)
	f.request(http.MethodPut, path, map[string]any{"shelf_config": map[string]any{"max_file_bytes": (32 << 20) + 1}}, http.StatusOK, nil)
	shelfUpload(t, f, library.ID, strings.Repeat("x", (32<<20)+1), http.StatusCreated)
}

// Medium spec, task 07: certificates attach to the uniquely numbered record;
// a mismatching explicit owner and an ambiguous number require correction.
func TestShelfDocumentsCertificateNumberIdentifiesRecord(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var cert types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Numbered certificate", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "number": "SYN-CERT-07"}}}}, http.StatusCreated, &cert)
	certID := cert.Items[0].EntityID
	record := map[string]any{"name": "Numbered report file", "certificate_number": "SYN-CERT-07", "cloud_url": "https://example.org/cert", "stage": "final"}
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "report", "type": "add_document_version", "document": record}}}, http.StatusCreated, &draft)
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, certID, *versions[0].OwnerID)
	require.Equal(t, "SYN-CERT-07", versions[0].CertificateNumber)
	var relations []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+certID+"/relations", nil, http.StatusOK, &relations)
	found := false
	for _, group := range relations {
		for _, r := range group.Relations {
			if r.RelationType == "has_document" && r.ObjectID == draft.Items[0].EntityID {
				found = true
			}
		}
	}
	require.True(t, found)
	firstFileID := draft.Items[0].EntityID
	record["owner_id"] = certID
	record["name"] = "Separate numbered report file"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "separate_report", "type": "add_document_version", "document": record}}}, http.StatusCreated, &draft)
	require.NotEqual(t, firstFileID, draft.Items[0].EntityID, "a certificate number identifies the certification, not the file")
	record["owner_id"] = certID
	record["certificate_number"] = "Wrong number"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "wrong", "type": "add_document_version", "document": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Other numbered certificate", "owner_id": shelfNode(nodes, "single"), "standard": "standard_a", "certification_object": "Container", "region": "Country", "number": "SYN-CERT-07"}}}}, http.StatusCreated, nil)
	delete(record, "owner_id")
	record["certificate_number"] = "SYN-CERT-07"
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "ambiguous", "type": "add_document_version", "document": record}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
}

// Medium regression, task 07: generic entity/relation edits cannot bypass the
// version operation and leave the recorded owner or requirement inconsistent.
func TestShelfDocumentsGenericWritesCannotReclassifyVersion(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"name": "Protected owner", "kind": "model"}},
		map[string]any{"id": "other", "type": "create_entity", "entity": map[string]any{"name": "Other owner", "kind": "model"}},
		map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"owner_id": "$owner", "name": "Protected document", "cloud_url": "https://example.org/protected", "stage": "final"}},
	}}, http.StatusCreated, &draft)
	fileID := shelfNode(draft, "document")
	otherID := shelfNode(draft, "other")
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPatch, "/entities/"+fileID, map[string]any{"revision": detail.Entity.Revision, "kind": "part"}, http.StatusBadRequest, nil)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "document_file", "name": "Bypass file"}, http.StatusBadRequest, nil)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/relations", map[string]any{"subject_id": otherID, "object_id": fileID, "relation_type": "has_document"}, http.StatusBadRequest, nil)
	var groups []service.RelationGroup
	f.request(http.MethodGet, "/entities/"+fileID+"/relations", nil, http.StatusOK, &groups)
	for _, g := range groups {
		for _, r := range g.Relations {
			if r.RelationType == "has_document" {
				f.request(http.MethodDelete, "/relations/"+r.ID+"?version=1", nil, http.StatusBadRequest, nil)
			}
		}
	}
}

// Medium spec, task 07: a version draft captures the file revision, so the
// second reviewer cannot silently replace a version confirmed by the first.
func TestShelfDocumentsConcurrentConfirmationRejectsStaleVersion(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var initial types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"name": "Concurrent owner", "kind": "model"}},
		map[string]any{"id": "requirement", "type": "create_entity", "entity": map[string]any{"name": "Concurrent requirement", "kind": "document_requirement"}},
		map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"owner_id": "$owner", "requirement_id": "$requirement", "name": "Concurrent document", "cloud_url": "https://example.org/first", "stage": "draft"}},
	}}, http.StatusCreated, &initial)
	propose := func(link string) types.ShelfChangeset {
		var draft types.ShelfChangeset
		f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "next", "type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(initial, "owner"), "requirement_id": shelfNode(initial, "requirement"), "cloud_url": link, "stage": "final"}}}}, http.StatusCreated, &draft)
		return draft
	}
	first, second := propose("https://example.org/winner"), propose("https://example.org/stale")
	for i, draft := range []types.ShelfChangeset{first, second} {
		status := http.StatusOK
		if i == 1 {
			status = http.StatusConflict
		}
		f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "next", "action": "apply"}}}, status, nil)
	}
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+shelfNode(initial, "document")+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 2)
	require.Equal(t, "https://example.org/winner", versions[0].CloudURL)
}

// Medium regression, task 07: failure after demoting a current version rolls
// back that demotion, the new version, and the draft confirmation together.
// A temporary PostgreSQL trigger substitutes only the database failure boundary.
func TestShelfDocumentsConfirmationFailureKeepsCurrentOriginal(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	attachment := shelfUpload(t, f, library.ID, "Original before failed confirmation", http.StatusCreated)
	var initial types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"name": "Atomic owner", "kind": "model"}},
		map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"owner_id": "$owner", "name": "Atomic document", "attachment_id": attachment.ID, "stage": "draft"}},
	}}, http.StatusCreated, &initial)
	var detail types.EntityDetail
	fileID := shelfNode(initial, "document")
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "replacement", "type": "add_document_version", "entity_id": fileID, "revision": detail.Entity.Revision, "document": map[string]any{"owner_id": shelfNode(initial, "owner"), "cloud_url": "https://example.org/failed", "stage": "final"}}}}, http.StatusCreated, &draft)
	require.NoError(t, f.db.Exec(`CREATE SEQUENCE shelf_document_failure_attempt;
 CREATE FUNCTION shelf_document_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.ordinal=2 AND nextval('shelf_document_failure_attempt')=2 THEN RAISE EXCEPTION 'injected document confirmation failure'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER shelf_document_failure BEFORE INSERT ON shelf_document_versions FOR EACH ROW EXECUTE FUNCTION shelf_document_failure()`).Error)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "replacement", "action": "apply"}}}, http.StatusInternalServerError, nil)
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 1)
	require.True(t, versions[0].IsCurrent)
	require.Equal(t, "draft", versions[0].Stage)
	f.request(http.MethodGet, path+"/"+draft.ID, nil, http.StatusOK, &draft)
	require.Equal(t, "pending", draft.Status)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities/"+fileID+"/document-versions/"+versions[0].ID+"/download", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "Original before failed confirmation", response.Body.String())
}

// Medium spec, task 07: deferred version proposals freeze explicit new-node
// references and retain applicability and any separately supplied text entry.
func TestShelfDocumentsDeferredClassificationPreservesTextAndConditions(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	// A synthetic ingest creates the optional text through the public intake API.
	body, err := json.Marshal(map[string]any{"items": []any{map[string]any{"library": library.ID, "title": "Synthetic text version", "content": "Synthetic retained text without file parsing.", "acquisition": "verbatim"}}})
	require.NoError(t, err)
	response := personalRequest(t, ingestAuthHTTPRouter(f.mcpFixture), http.MethodPost, "/api/v1/ingest", string(body), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ingested struct{ Data types.IngestBatchResult }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ingested))
	entryID := ingested.Data.Items[0].EntryID
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{
		map[string]any{"id": "requirement", "type": "create_entity", "entity": map[string]any{"name": "Deferred document requirement", "kind": "document_requirement"}},
		map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(nodes, "model"), "requirement_id": "$requirement", "entry_id": entryID, "name": "Deferred text document", "cloud_url": "https://example.org/deferred", "stage": "final", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "a")}}}}},
	}}, http.StatusCreated, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "requirement", "action": "apply"}, map[string]any{"item_id": "document", "action": "defer"}}}, http.StatusOK, &draft)
	var deferred types.ShelfChangeset
	f.request(http.MethodGet, path+"/"+draft.DeferredChangesetID, nil, http.StatusOK, &deferred)
	f.request(http.MethodPost, path+"/"+deferred.ID+"/confirm", map[string]any{"revision": deferred.Revision, "decisions": []any{map[string]any{"item_id": "document", "action": "apply"}}}, http.StatusOK, &deferred)
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+deferred.Items[0].EntityID+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, entryID, *versions[0].EntryID)
	require.Equal(t, shelfNode(draft, "requirement"), *versions[0].RequirementID)
	require.Equal(t, "completed", versions[0].ParseStatus)
	require.Equal(t, []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{shelfNode(nodes, "a")}}}, versions[0].Conditions)
	var links []service.ManagedEntityLink
	f.request(http.MethodGet, "/entries/"+entryID+"/entity-links", nil, http.StatusOK, &links)
	require.Len(t, links, 2)
	for _, link := range links {
		require.Equal(t, "manual", link.Origin)
	}
}

// Medium regression, task 07 / ADR 0023: a part-owned file cannot select an
// alternative part instead of its own owner through applicability conditions.
func TestShelfDocumentsPartConditionsCannotSelectAnotherOwner(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "wrong_part", "type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(nodes, "a"), "name": "Contradictory part document", "cloud_url": "https://example.org/part", "stage": "final", "conditions": []any{map[string]any{"module_type": "module_a", "part_ids": []string{shelfNode(nodes, "b")}}}}}}}, http.StatusCreated, &draft)
	require.Equal(t, "uncertain", draft.Items[0].Conflicts[0].Type)
}

// Medium spec, task 07: originals must belong to the same shelf as the
// classified file, even when their identifiers are otherwise valid.
func TestShelfDocumentsRejectCrossLibraryMaterials(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var other types.Library
	f.request(http.MethodPost, "/libraries", map[string]any{"name": "Other material shelf", "directory": "other-material-shelf", "entity_mode": "managed", "shelf_enabled": true}, http.StatusCreated, &other)
	attachment := shelfUpload(t, f, other.ID, "Other library original", http.StatusCreated)
	var owner types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"name": "Local material owner", "kind": "model"}, http.StatusCreated, &owner)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "foreign", "type": "add_document_version", "document": map[string]any{"owner_id": owner.ID, "name": "Foreign material", "attachment_id": attachment.ID, "stage": "final"}}}}, http.StatusCreated, &draft)
	require.Equal(t, "unparseable", draft.Items[0].Conflicts[0].Type)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "foreign", "action": "apply"}}}, http.StatusConflict, nil)
}

// Medium regression, task 07: correcting a proposal from an existing file to
// a different new file preserves the original file and allocates a new identity.
func TestShelfDocumentsCorrectedProposalCanCreateDifferentFile(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var initial types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"name": "Correction owner", "kind": "model"}},
		map[string]any{"id": "requirement", "type": "create_entity", "entity": map[string]any{"name": "Correction requirement", "kind": "document_requirement"}},
		map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"owner_id": "$owner", "requirement_id": "$requirement", "name": "Original classified file", "cloud_url": "https://example.org/first", "stage": "draft"}},
	}}, http.StatusCreated, &initial)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{"id": "correct", "type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(initial, "owner"), "requirement_id": shelfNode(initial, "requirement"), "cloud_url": "https://example.org/corrected", "stage": "final"}}}}, http.StatusCreated, &draft)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "correct", "action": "replace", "replacement": map[string]any{"type": "add_document_version", "document": map[string]any{"owner_id": shelfNode(initial, "owner"), "name": "Corrected new file", "cloud_url": "https://example.org/corrected", "stage": "final"}}}}}, http.StatusOK, &draft)
	require.NotEqual(t, shelfNode(initial, "document"), draft.Items[0].EntityID)
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+shelfNode(initial, "document")+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 1)
	require.True(t, versions[0].IsCurrent)
}
