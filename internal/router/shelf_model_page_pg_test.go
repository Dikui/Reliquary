package router

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 16 / ADR 0024: other files share the model read scope,
// keep their current version and implicit component conditions, and exclude checklist files.
func TestShelfModelPageOtherFiles(t *testing.T) {
	f, library, nodes := checklistSetup(t)
	model, part := shelfNode(nodes, "model"), shelfNode(nodes, "a")
	addFile := func(owner, name, requirement string) string {
		var saved types.ShelfChangeset
		f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{
			"source": map[string]any{"type": "web"}, "confirmed": true,
			"operations": []any{map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{
				"name": name, "owner_id": owner, "requirement_id": requirement, "cloud_url": "https://example.invalid/file",
				"stage": "final", "version_label": " Original V2 ", "file_category": "Synthetic brochure",
			}}},
		}, http.StatusCreated, &saved)
		return saved.Items[0].EntityID
	}
	before := time.Now().UTC().Format(time.RFC3339Nano)
	fileID := addFile(model, "Model file", "")
	previous := time.Now().UTC().Format(time.RFC3339Nano)
	var file types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &file)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{
		"source": map[string]any{"type": "web"}, "confirmed": true,
		"operations": []any{map[string]any{"id": "replacement", "type": "add_document_version", "entity_id": fileID, "revision": file.Entity.Revision, "document": map[string]any{
			"owner_id": model, "cloud_url": "https://example.invalid/replacement", "stage": "final", "version_label": " Original V3 ",
		}}},
	}, http.StatusCreated, nil)
	addFile(part, "Part file", "")
	addFile(shelfNode(nodes, "platform"), "Platform file", "")
	addFile(shelfNode(nodes, "single"), "Other model file", "")
	requirement := checklistRequirement(t, f, library, "Required drawing", "", "early")
	checklistBind(f, library, requirement, shelfNode(nodes, "line"))
	addFile(model, "Required file", requirement.ID)
	var full map[string]any
	f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+model, nil, http.StatusOK, &full)
	require.Equal(t, "Required file", full["shelf"].(map[string]any)["checklist"].([]any)[0].(map[string]any)["document_name"])
	read := func(suffix string) []any {
		var response map[string]any
		f.request(http.MethodGet, "/libraries/"+library.ID+"/shelf/nodes/"+model+suffix, nil, http.StatusOK, &response)
		shelf := response["shelf"].(map[string]any)
		require.Contains(t, shelf, "other_documents")
		return shelf["other_documents"].([]any)
	}
	files := read("")
	require.Len(t, files, 3)
	origins := map[string]string{}
	for _, raw := range files {
		row := raw.(map[string]any)
		name := row["entity"].(map[string]any)["canonical_name"].(string)
		origins[name] = row["origin"].(string)
		wantVersion := " Original V2 "
		if name == "Model file" {
			wantVersion = " Original V3 "
		}
		require.Equal(t, wantVersion, row["document"].(map[string]any)["version_label"])
		if name == "Part file" {
			require.Equal(t, part, row["owner"].(map[string]any)["id"])
			require.Equal(t, []any{map[string]any{"module_type": "module_a", "part_ids": []any{part}}}, row["conditions"])
		}
	}
	require.Equal(t, map[string]string{"Model file": "self", "Part file": "component", "Platform file": "ancestor"}, origins)
	require.Len(t, read("?combination="+url.QueryEscape(`{"module_a":"`+shelfNode(nodes, "b")+`"}`)), 2)
	require.Empty(t, read("?as_of="+url.QueryEscape(before)), "future versions must not leak into older reads")
	oldFiles := read("?as_of=" + url.QueryEscape(previous))
	require.Len(t, oldFiles, 1)
	require.Equal(t, " Original V2 ", oldFiles[0].(map[string]any)["document"].(map[string]any)["version_label"])
	var page map[string]any
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/query", map[string]any{"ancestor_id": model}, http.StatusOK, &page)
	require.Len(t, page["items"].([]any)[0].(map[string]any)["shelf"].(map[string]any)["other_documents"], 3)
}

// Medium spec, task 16: model history includes its directly owned certification
// and document writes without including a sibling model's file writes.
func TestShelfModelPageHistoryIncludesOwnedRecords(t *testing.T) {
	f, library, nodes := shelfModelSetup(t)
	configureCertificationStandards(f, &library)
	model, other := shelfNode(nodes, "model"), shelfNode(nodes, "single")
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var saved types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{
		"source": map[string]any{"type": "agent", "agent_name": "Synthetic model assistant"}, "confirmed": true,
		"operations": []any{
			map[string]any{"id": "cert", "type": "create_certification", "certification": map[string]any{"name": "Model certificate", "owner_id": model, "standard": "standard_a", "certification_object": "Synthetic object", "region": "region_a"}},
			map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Model file", "owner_id": model, "cloud_url": "https://example.invalid/model", "stage": "final"}},
			map[string]any{"id": "other-file", "type": "add_document_version", "document": map[string]any{"name": "Sibling file", "owner_id": other, "cloud_url": "https://example.invalid/sibling", "stage": "final"}},
		},
	}, http.StatusCreated, &saved)
	var history types.OperationLogPage
	f.request(http.MethodGet, "/entities/"+model+"/history?page=1&page_size=100", nil, http.StatusOK, &history)
	found := map[string]bool{}
	for _, log := range history.Items {
		if log.OperationType == "create_entity" || log.OperationType == "update_entity" {
			found[log.ObjectID] = true
		}
	}
	require.True(t, found[shelfNode(saved, "cert")], "owned certification writes belong in model history")
	require.True(t, found[shelfNode(saved, "file")], "owned document version writes belong in model history")
	require.False(t, found[shelfNode(saved, "other-file")])
}
