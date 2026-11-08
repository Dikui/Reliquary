package router

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 20 / ADR 0024. Synthetic files exercise the public HTTP
// boundary: unassigned originals stay available until explicitly classified.
func TestShelfFilesUnassignedListAndClassification(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": " Synthetic manual ", "unassigned": true, "cloud_url": "https://example.invalid/manual", "stage": "draft", "version_label": " Rev A "}}}}, http.StatusCreated, &draft)
	fileID := draft.Items[0].EntityID
	var page struct {
		Items []struct {
			Entity   types.Entity
			Document types.ShelfDocumentVersion
			Owner    *types.Entity
		}
		Total int
	}
	f.request(http.MethodGet, path+"/documents?group=other&owner_kind=unassigned&q=Synthetic", nil, http.StatusOK, &page)
	require.Equal(t, 1, page.Total)
	require.Equal(t, " Synthetic manual ", page.Items[0].Entity.CanonicalName)
	require.Nil(t, page.Items[0].Owner)
	var owner types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Explicit owner"}, http.StatusCreated, &owner)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	op := map[string]any{"id": "attach", "type": "classify_document", "entity_id": fileID, "revision": detail.Entity.Revision, "classification": map[string]any{"owner_id": owner.ID}}
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{op}}, http.StatusCreated, &draft)
	f.request(http.MethodGet, path+"/documents?owner_kind=unassigned", nil, http.StatusOK, &page)
	require.Equal(t, 1, page.Total, "a proposal does not attach the file")
	f.request(http.MethodPost, path+"/changesets/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "attach", "action": "apply"}}}, http.StatusOK, nil)
	f.request(http.MethodGet, path+"/documents?owner_kind=unassigned", nil, http.StatusOK, &page)
	require.Zero(t, page.Total)
	f.request(http.MethodGet, path+"/documents?owner_kind=model", nil, http.StatusOK, &page)
	require.Equal(t, owner.ID, page.Items[0].Owner.ID)
	require.Equal(t, " Rev A ", page.Items[0].Document.VersionLabel)
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{op}}, http.StatusConflict, nil)
}

// Medium spec, task 20. Selecting an old original changes the current file,
// preserves both versions, and is reflected by the shared node query.
func TestShelfFilesSelectHistoricalVersion(t *testing.T) {
	f, library := newProductShelfFixture(t)
	path := "/libraries/" + library.ID + "/shelf"
	var owner types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"kind": "model", "name": "Version owner"}, http.StatusCreated, &owner)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Versioned original", "owner_id": owner.ID, "cloud_url": "https://example.invalid/old", "stage": "draft"}}}}, http.StatusCreated, &draft)
	fileID := draft.Items[0].EntityID
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	oldID := versions[0].ID
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "new", "type": "add_document_version", "entity_id": fileID, "revision": detail.Entity.Revision, "document": map[string]any{"owner_id": owner.ID, "cloud_url": "https://example.invalid/new", "stage": "final"}}}}, http.StatusCreated, nil)
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "current", "type": "select_document_version", "entity_id": fileID, "revision": detail.Entity.Revision, "version_id": oldID}}}, http.StatusCreated, nil)
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 2)
	require.False(t, versions[0].IsCurrent)
	require.True(t, versions[1].IsCurrent)
	var node types.ShelfNodeDetail
	f.request(http.MethodGet, path+"/nodes/"+owner.ID, nil, http.StatusOK, &node)
	require.Equal(t, oldID, node.Shelf.OtherDocuments[0].Document.ID)
	selectedAt := node.AsOf.Format(time.RFC3339Nano)
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "third", "type": "add_document_version", "entity_id": fileID, "revision": detail.Entity.Revision, "document": map[string]any{"owner_id": owner.ID, "cloud_url": "https://example.invalid/third", "stage": "final", "version_label": "Third"}}}}, http.StatusCreated, nil)
	f.request(http.MethodGet, path+"/nodes/"+owner.ID, nil, http.StatusOK, &node)
	require.Equal(t, "Third", node.Shelf.OtherDocuments[0].Document.VersionLabel)
	f.request(http.MethodGet, path+"/nodes/"+owner.ID+"?as_of="+selectedAt, nil, http.StatusOK, &node)
	require.Equal(t, oldID, node.Shelf.OtherDocuments[0].Document.ID, "later uploads do not erase the earlier selection")

}

// Medium spec, task 20: file search uses each version's own text, applies
// filters before paging, and reclassification cannot silently merge files.
func TestShelfFilesTextHistoryAndClassificationConflicts(t *testing.T) {
	f, library, nodes := newShelfSearchFixture(t)
	path := "/libraries/" + library.ID + "/shelf"
	first := shelfSearchText(t, f, library.ID, "Synthetic historical text", "Historical needle in original text")
	fileID := shelfSearchVersion(t, f, library.ID, shelfNode(nodes, "model"), "", first, "Old")
	second := shelfSearchText(t, f, library.ID, "Synthetic current text", "Current words in original text")
	shelfSearchVersion(t, f, library.ID, shelfNode(nodes, "model"), fileID, second, "New")
	var page types.ShelfDocumentList
	f.request(http.MethodGet, path+"/documents?q=Historical+needle", nil, http.StatusOK, &page)
	require.Zero(t, page.Total)
	f.request(http.MethodGet, path+"/documents?q=Historical+needle&include_history=true&limit=1", nil, http.StatusOK, &page)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, "Old", page.Items[0].Document.VersionLabel)
	f.request(http.MethodGet, path+"/documents?include_history=true&limit=1", nil, http.StatusOK, &page)
	require.EqualValues(t, 2, page.Total)
	require.True(t, page.Truncated)
	f.request(http.MethodGet, path+"/documents?parse_status=failed", nil, http.StatusOK, &page)
	require.Zero(t, page.Total)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	var result types.ShelfChangeset
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "move", "type": "classify_document", "entity_id": fileID, "revision": detail.Entity.Revision, "classification": map[string]any{"owner_id": shelfNode(nodes, "a")}}}}, http.StatusCreated, &result)
	f.request(http.MethodGet, path+"/documents?owner_kind=part&include_history=true", nil, http.StatusOK, &page)
	require.EqualValues(t, 2, page.Total)
	for _, row := range page.Items {
		require.Equal(t, shelfNode(nodes, "a"), row.Owner.ID)
	}
	// An invalid correction cannot change the confirmed classification.
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "bad", "type": "classify_document", "entity_id": fileID, "revision": detail.Entity.Revision, "classification": map[string]any{"owner_id": "unknown"}}}}, http.StatusConflict, nil)
	f.request(http.MethodGet, path+"/documents?owner_kind=part", nil, http.StatusOK, &page)
	require.EqualValues(t, 1, page.Total)
}

// Medium spec, task 20: certificate ownership determines the certificate tab,
// even when the file also satisfies a document requirement.
func TestShelfFilesCertificateGroup(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	path := "/libraries/" + library.ID + "/shelf"
	var result types.ShelfChangeset
	f.request(http.MethodPost, path+"/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Synthetic certificate", "owner_id": shelfNode(nodes, "model"), "standard": "standard_a", "certification_object": "Container", "region": "Synthetic region", "number": "SYN-GROUP"}},
		map[string]any{"id": "req", "type": "create_entity", "entity": map[string]any{"kind": "document_requirement", "name": "Synthetic certificate requirement"}},
		map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Synthetic certificate file", "owner_id": "$cert", "requirement_id": "$req", "certificate_number": "SYN-GROUP", "cloud_url": "https://example.invalid/cert", "stage": "final"}},
	}}, http.StatusCreated, &result)
	var page types.ShelfDocumentList
	f.request(http.MethodGet, path+"/documents?group=certificates", nil, http.StatusOK, &page)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, shelfNode(result, "cert"), page.Items[0].Owner.ID)
	f.request(http.MethodGet, path+"/documents?group=requirements", nil, http.StatusOK, &page)
	require.Zero(t, page.Total)
}
