package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Medium spec, task 27: imports preserve explicit names, including empty names;
// absent name columns use the original standard cell. A zero-match update can
// be explicitly changed to create without inventing unique names.
func TestShelfCertificationImportNamesNeedNoDisambiguation(t *testing.T) {
	for _, sample := range []struct {
		label, name string
		nameColumn  bool
	}{
		{"no name column", " Customer spelling:2022 ", false},
		{"explicit empty name", "", true},
		{"explicit display name", " Repeated original name ", true},
	} {
		t.Run(sample.label, func(t *testing.T) {
			f, library, nodes := shelfModelSetup(t)
			configureCertificationStandards(f, &library)
			columns := []types.ShelfImportColumn{{Column: "A", Field: "owner"}, {Column: "B", Field: "standard"}, {Column: "C", Field: "certification_object"}, {Column: "D", Field: "region"}}
			if sample.nameColumn {
				columns = append(columns, types.ShelfImportColumn{Column: "E", Field: "name"})
			}
			importRecordPlan(f, library, "certification_progress", columns, nil)
			rows := ""
			for i, region := range []string{"A", "B"} {
				values := []string{"Synthetic model", " Customer spelling:2022 ", "Container", region}
				if sample.nameColumn {
					values = append(values, sample.name)
				}
				rows += importRecordRow(i+3, values...)
			}
			draft := importUpload(t, f, library.ID, "cert_20261028.xlsx", "", importWorkbook(t, rows, ""), http.StatusCreated)
			require.Len(t, draft.Items, 2)
			decisions := []types.ShelfDecision{}
			for _, item := range draft.Items {
				require.Equal(t, "update_certification", item.Operation.Type)
				require.Equal(t, "uncertain", item.Conflicts[0].Type)
				require.Equal(t, sample.name, item.Operation.Certification.Name)
				op := item.Operation
				op.Type = "create_certification"
				decisions = append(decisions, types.ShelfDecision{ItemID: item.ID, Action: "replace", Replacement: &op})
			}
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets/"+draft.ID+"/confirm", types.ShelfChangesetReview{Revision: draft.Revision, Decisions: decisions}, http.StatusOK, &draft)
			var model types.EntityDetail
			f.request(http.MethodGet, "/entities/"+shelfNode(nodes, "model"), nil, http.StatusOK, &model)
			require.Len(t, model.Shelf.Certifications, 2)
			require.NotEqual(t, model.Shelf.Certifications[0].Entity.ID, model.Shelf.Certifications[1].Entity.ID)
			for _, certification := range model.Shelf.Certifications {
				require.Equal(t, sample.name, certification.Entity.CanonicalName)
				require.Equal(t, "standard_a", certification.Certification.Standard)
				require.Equal(t, 2022, certification.Certification.Year)
			}
		})
	}
}
