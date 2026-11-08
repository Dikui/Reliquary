package router

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 17 / ADR 0024: node files include requirement files,
// retain current versions at the requested time and exclude files owned elsewhere.
func TestShelfNodePageOwnDocuments(t *testing.T) {
	f, l, n := checklistSetup(t)
	product, part := shelfNode(n, "product"), shelfNode(n, "a")
	r := checklistRequirement(t, f, l, "Node drawing", "", "early")
	checklistBind(f, l, r, shelfNode(n, "line"))
	before := time.Now().UTC().Format(time.RFC3339Nano)
	checklistFile(f, l, product, r.ID, "draft")
	previous := time.Now().UTC().Format(time.RFC3339Nano)
	checklistFile(f, l, product, r.ID, "final")
	checklistFile(f, l, part, r.ID, "final")
	read := func(id, at string) map[string]any {
		var node map[string]any
		suffix := ""
		if at != "" {
			suffix = "?as_of=" + url.QueryEscape(at)
		}
		f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+id+suffix, nil, http.StatusOK, &node)
		return node["shelf"].(map[string]any)
	}
	shelf := read(product, "")
	require.Contains(t, shelf, "documents")
	files := shelf["documents"].([]any)
	require.Len(t, files, 1)
	file := files[0].(map[string]any)
	require.Equal(t, product, file["document"].(map[string]any)["owner_id"])
	require.Equal(t, "final", file["document"].(map[string]any)["stage"])
	require.Equal(t, "Node drawing", file["requirement"].(map[string]any)["canonical_name"])
	require.Empty(t, read(product, before)["documents"])
	require.Equal(t, "draft", read(product, previous)["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["stage"])
	require.Len(t, read(part, "")["documents"], 1)
}

// Medium spec, task 17 / ADR 0023: a product shows its own and its platform's
// certifications, while a part only shows certifications owned by that part.
func TestShelfNodePageCertificationOrigins(t *testing.T) {
	f, l, n := shelfModelSetup(t)
	configureCertificationStandards(f, &l)
	operations := []any{}
	for _, key := range []string{"product", "platform", "a", "b"} {
		operations = append(operations, map[string]any{"id": key, "type": "create_certification", "certification": map[string]any{
			"name": "Certificate " + key, "owner_id": shelfNode(n, key), "standard": "standard_a", "certification_object": "Synthetic object", "region": "Original region",
		}})
	}
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": operations}, http.StatusCreated, nil)
	var node types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+shelfNode(n, "product"), nil, http.StatusOK, &node)
	origins := map[string]string{}
	for _, cert := range node.Shelf.Certifications {
		origins[cert.Entity.CanonicalName] = cert.Origin
	}
	require.Equal(t, map[string]string{"Certificate product": "self", "Certificate platform": "ancestor"}, origins)
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+shelfNode(n, "a"), nil, http.StatusOK, &node)
	require.Len(t, node.Shelf.Certifications, 1)
	require.Equal(t, "Certificate a", node.Shelf.Certifications[0].Entity.CanonicalName)
}

// Medium spec, task 17 / ADR 0024: a part's module requirements are listed
// without inferring model stages or regional sale statuses.
func TestShelfNodePagePartRequirements(t *testing.T) {
	f, l, n := checklistSetup(t)
	part := shelfNode(n, "a")
	f.request(http.MethodPost, "/libraries/"+l.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{
		"id": "part-module", "type": "update_entity", "entity_id": part, "revision": 1, "patch": map[string]any{"revision": 1, "attributes": map[string]any{"module_type": "module_a", "pack_model": "Original Pack"}},
	}}}, http.StatusCreated, nil)
	required := checklistRequirement(t, f, l, "Part requirement", "draft_due", "final_due")
	checklistBind(f, l, required, required.ID, "module_a")
	other := checklistRequirement(t, f, l, "Other module requirement", "", "final_due")
	checklistBind(f, l, other, other.ID, "module_b")
	var node types.ShelfNodeDetail
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+part, nil, http.StatusOK, &node)
	require.Len(t, node.Shelf.Checklist, 1)
	require.Equal(t, required.ID, node.Shelf.Checklist[0].Requirement.ID)
	require.Equal(t, "no_file", node.Shelf.Checklist[0].Status)
	checklistFile(f, l, part, required.ID, "draft", types.ShelfCondition{ModuleType: "module_a", PartIDs: []string{part}})
	f.request(http.MethodGet, "/libraries/"+l.ID+"/shelf/nodes/"+part, nil, http.StatusOK, &node)
	require.Equal(t, "has_file", node.Shelf.Checklist[0].Status)
	require.Equal(t, "draft", node.Shelf.Checklist[0].Document.Stage)
	require.Equal(t, []types.ShelfCondition{{ModuleType: "module_a", PartIDs: []string{part}}}, node.Shelf.Checklist[0].Conditions)
	require.Empty(t, node.Shelf.CurrentStage)
	require.Empty(t, node.Shelf.SaleStatuses)
}
