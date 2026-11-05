package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// MEL-02: small regression/spec; relation configuration always exposes arrays.
func TestLibraryRelationTypeJSONCollections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value LibraryRelationType
		want  string
	}{
		{
			name:  "unset",
			value: LibraryRelationType{},
			want: `{"fields":[],"hierarchical":false,"subject_kinds":[],"object_kinds":[],
				"name":"","position":0,"enabled":false}`,
		},
		{
			name: "empty",
			value: LibraryRelationType{
				Fields: []EntityFieldDefinition{}, SubjectKinds: []string{}, ObjectKinds: []string{},
			},
			want: `{"fields":[],"hierarchical":false,"subject_kinds":[],"object_kinds":[],
				"name":"","position":0,"enabled":false}`,
		},
		{
			name: "configured",
			value: LibraryRelationType{
				LibraryID: "products", Name: "belongs_to", Position: 2, Enabled: true, Hierarchical: true,
				Fields: []EntityFieldDefinition{
					{Key: "note", Name: "Note", Type: "text", Kinds: []string{"model"}},
				},
				SubjectKinds: []string{"model"}, ObjectKinds: []string{"product"},
			},
			want: `{"fields":[{"key":"note","name":"Note","type":"text","kinds":["model"],"archived":false}],
				"hierarchical":true,"subject_kinds":["model"],"object_kinds":["product"],
				"name":"belongs_to","position":2,"enabled":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(data))
		})
	}
}
