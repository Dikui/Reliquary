package tools

import (
	"context"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type scopeKnowledgeService struct {
	interfaces.KnowledgeService
	knowledge *types.Knowledge
	tags      map[string][]*types.KnowledgeTag
}

type scopeChunkService struct {
	interfaces.ChunkService
	chunk *types.Chunk
}

func (s *scopeChunkService) GetChunkByIDOnly(context.Context, string) (*types.Chunk, error) {
	return s.chunk, nil
}

func (s *scopeKnowledgeService) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return s.knowledge, nil
}

func (s *scopeKnowledgeService) GetKnowledgeTags(
	_ context.Context, ids []string,
) (map[string][]*types.KnowledgeTag, error) {
	out := make(map[string][]*types.KnowledgeTag, len(ids))
	for _, id := range ids {
		if tags, ok := s.tags[id]; ok {
			out[id] = tags
		}
	}
	return out, nil
}

func testKnowledgeTag(id string) *types.KnowledgeTag {
	return &types.KnowledgeTag{ID: id}
}

func TestKnowledgeIDsMatchingAnyTag(t *testing.T) {
	matches, err := knowledgeIDsMatchingAnyTag(
		context.Background(),
		[]string{"doc-1", "doc-2"},
		[]string{"tag-a"},
		func(_ context.Context, ids []string) (map[string][]*types.KnowledgeTag, error) {
			return map[string][]*types.KnowledgeTag{
				"doc-1": []*types.KnowledgeTag{testKnowledgeTag("tag-z")},
				"doc-2": []*types.KnowledgeTag{testKnowledgeTag("tag-a")},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("knowledgeIDsMatchingAnyTag() error = %v", err)
	}
	if matches["doc-1"] {
		t.Fatalf("doc-1 should not match tag-a")
	}
	if !matches["doc-2"] {
		t.Fatalf("doc-2 should match tag-a")
	}
}

func TestAuthorizeKnowledgeInSearchTargetsRejectsSiblingKB(t *testing.T) {
	service := &scopeKnowledgeService{knowledge: &types.Knowledge{ID: "doc-2", KnowledgeBaseID: "kb-2"}}
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledgeBase,
		KnowledgeBaseID: "kb-1",
	}}
	if _, err := authorizeKnowledgeInSearchTargets(context.Background(), targets, "doc-2", service); err == nil {
		t.Fatal("document from a sibling KB must be rejected")
	}
}

func TestAuthorizeKnowledgeInSearchTargetsAllowsBoundDocument(t *testing.T) {
	service := &scopeKnowledgeService{knowledge: &types.Knowledge{ID: "doc-1", KnowledgeBaseID: "kb-1"}}
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledge,
		KnowledgeBaseID: "kb-1",
		KnowledgeIDs:    []string{"doc-1"},
	}}
	got, err := authorizeKnowledgeInSearchTargets(context.Background(), targets, "doc-1", service)
	if err != nil || got == nil || got.ID != "doc-1" {
		t.Fatalf("bound document authorization failed: got=%+v err=%v", got, err)
	}
}

func TestReadSourceDocumentExcludesUnapprovedIntake(t *testing.T) {
	service := &scopeKnowledgeService{knowledge: &types.Knowledge{
		ID: "waiting-message", KnowledgeBaseID: "kb-1", ParseStatus: "rejected",
		Metadata: types.JSON(`{"intake":{"source":"manual","decision":"rejected","reason":"Unreasoned opinion"}}`),
	}}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1"}}
	_, err := authorizeKnowledgeInSearchTargets(context.Background(), targets, "waiting-message", service)
	if err == nil {
		t.Fatal("unapproved intake must remain unavailable to synthesis")
	}

}

func TestAuthorizeChunkInSearchTargetsRejectsDisabledDocument(t *testing.T) {
	chunkService := &scopeChunkService{chunk: &types.Chunk{
		ID: "document-1", KnowledgeID: "document-source", KnowledgeBaseID: "kb-1",
		ChunkType: types.ChunkTypeText, IsEnabled: false,
	}}
	knowledgeService := &scopeKnowledgeService{knowledge: &types.Knowledge{
		ID: "document-source", KnowledgeBaseID: "kb-1",
	}}
	targets := types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1",
	}}

	if _, err := authorizeChunkInSearchTargets(
		context.Background(), targets, "document-1", chunkService, knowledgeService,
	); err == nil {
		t.Fatal("disabled document chunk must not be authorized for Agent tools")
	}
}

// A tag-scoped mention is resolved into KnowledgeIDs (intersected with any
// explicitly mentioned documents) while the tag is retained as the logical
// scope record. Re-admitting every document carrying that tag would undo the
// intersection and leak documents the user never mentioned.
func TestAuthorizeKnowledgeInSearchTargetsDoesNotWidenResolvedScopeByTag(t *testing.T) {
	service := &scopeKnowledgeService{
		knowledge: &types.Knowledge{ID: "doc-tagged", KnowledgeBaseID: "kb-1"},
		tags:      map[string][]*types.KnowledgeTag{"doc-tagged": {testKnowledgeTag("tag-a")}},
	}
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledge,
		KnowledgeBaseID: "kb-1",
		KnowledgeIDs:    []string{"doc-mentioned"},
		ScopeTagIDs:     []string{"tag-a"},
	}}
	if _, err := authorizeKnowledgeInSearchTargets(
		context.Background(), targets, "doc-tagged", service,
	); err == nil {
		t.Fatal("a document outside the resolved whitelist must not be authorized by its tag alone")
	}

	got, err := authorizeKnowledgeInSearchTargets(
		context.Background(),
		targets,
		"doc-mentioned",
		&scopeKnowledgeService{knowledge: &types.Knowledge{ID: "doc-mentioned", KnowledgeBaseID: "kb-1"}},
	)
	if err != nil || got == nil {
		t.Fatalf("the resolved document must stay authorized: got=%+v err=%v", got, err)
	}
}

// The intersection is per target. Two separate mentions of the same KB remain
// alternatives, so a tag-only target still authorizes on its own.
func TestAuthorizeKnowledgeInSearchTargetsKeepsCrossTargetUnion(t *testing.T) {
	service := &scopeKnowledgeService{
		knowledge: &types.Knowledge{ID: "doc-tagged", KnowledgeBaseID: "kb-1"},
		tags:      map[string][]*types.KnowledgeTag{"doc-tagged": {testKnowledgeTag("tag-a")}},
	}
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", KnowledgeIDs: []string{"doc-mentioned"}},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TagIDs: []string{"tag-a"}},
	}
	got, err := authorizeKnowledgeInSearchTargets(context.Background(), targets, "doc-tagged", service)
	if err != nil || got == nil {
		t.Fatalf("a tag-only alternative target must still authorize: got=%+v err=%v", got, err)
	}
}

func TestValidateKnowledgeBaseIDsInSearchTargetsRejectsMixedScope(t *testing.T) {
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledgeBase,
		KnowledgeBaseID: "kb-1",
	}}
	if err := validateKnowledgeBaseIDsInSearchTargets(targets, []string{"kb-1", "kb-2"}); err == nil {
		t.Fatal("mixed valid and out-of-scope KB IDs must be rejected")
	}
}

func TestFilterContextResultsHonorsDocumentScope(t *testing.T) {
	targets := types.SearchTargets{{
		Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", KnowledgeIDs: []string{"doc-allowed"},
	}}
	results := []*types.SearchResult{
		{ID: "chunk-1", KnowledgeID: "doc-allowed", KnowledgeBaseID: "kb-1"},
		{ID: "chunk-2", KnowledgeID: "doc-outside", KnowledgeBaseID: "kb-1"},
	}
	filtered, err := filterSearchResultsInSearchTargets(context.Background(), targets, "kb-1", results, nil)
	if err != nil {
		t.Fatalf("filterSearchResultsInSearchTargets() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].KnowledgeID != "doc-allowed" {
		t.Fatalf("context result scope leaked: %+v", filtered)
	}
}
