package types

import "testing"

func TestHasKnowledgeRetrievalScope(t *testing.T) {
	tests := []struct {
		name             string
		searchTargets    SearchTargets
		knowledgeBaseIDs []string
		knowledgeIDs     []string
		want             bool
	}{
		{name: "empty", want: false},
		{name: "nil target", searchTargets: SearchTargets{nil}, want: false},
		{name: "empty document target", searchTargets: SearchTargets{{Type: SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1"}}, want: false},
		{name: "knowledge base IDs", knowledgeBaseIDs: []string{"kb-1"}, want: true},
		{name: "knowledge IDs", knowledgeIDs: []string{"doc-1"}, want: true},
		{
			name: "knowledge base target",
			searchTargets: SearchTargets{
				{
					Type:            SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: "kb-1",
				},
			},
			want: true,
		},
		{
			name: "explicit document target",
			searchTargets: SearchTargets{
				{
					Type:                    SearchTargetTypeKnowledge,
					KnowledgeBaseID:         "kb-1",
					KnowledgeIDs:            []string{"doc-1"},
					DisableRecallThresholds: true,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasKnowledgeRetrievalScope(tt.searchTargets, tt.knowledgeBaseIDs, tt.knowledgeIDs)
			if got != tt.want {
				t.Fatalf("HasKnowledgeRetrievalScope() = %v, want %v", got, tt.want)
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
