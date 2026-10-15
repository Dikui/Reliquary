package types

import "testing"

func TestHasEntryRetrievalScope(t *testing.T) {
	tests := []struct {
		name          string
		searchTargets SearchTargets
		libraryIDs    []string
		entryIDs      []string
		want          bool
	}{
		{name: "empty", want: false},
		{name: "nil target", searchTargets: SearchTargets{nil}, want: false},
		{name: "empty document target", searchTargets: SearchTargets{{Type: SearchTargetTypeEntry, LibraryID: "library-1"}}, want: false},
		{name: "library IDs", libraryIDs: []string{"library-1"}, want: true},
		{name: "entry IDs", entryIDs: []string{"doc-1"}, want: true},
		{
			name: "library target",
			searchTargets: SearchTargets{
				{
					Type:      SearchTargetTypeLibrary,
					LibraryID: "library-1",
				},
			},
			want: true,
		},
		{
			name: "explicit document target",
			searchTargets: SearchTargets{
				{
					Type:                    SearchTargetTypeEntry,
					LibraryID:               "library-1",
					EntryIDs:                []string{"doc-1"},
					DisableRecallThresholds: true,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasEntryRetrievalScope(tt.searchTargets, tt.libraryIDs, tt.entryIDs)
			if got != tt.want {
				t.Fatalf("HasEntryRetrievalScope() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchTargetRecallThresholds(t *testing.T) {
	normal := &SearchTarget{}
	vector, keyword := normal.RecallThresholds(0.5, 0.24)
	if vector != 0.5 || keyword != 0.24 {
		t.Fatalf("normal thresholds = (%v, %v), want (0.5, 0.24)", vector, keyword)
	}

	explicit := &SearchTarget{DisableRecallThresholds: true}
	vector, keyword = explicit.RecallThresholds(0.5, 0.24)
	if vector != 0 || keyword != 0 {
		t.Fatalf("explicit thresholds = (%v, %v), want (0, 0)", vector, keyword)
	}
	if !(SearchTargets{explicit}).HasRecallThresholdOverride() {
		t.Fatal("expected explicit target to advertise recall threshold override")
	}
}
