package service

import (
	"context"
	"errors"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type summaryStaleKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
	err       error
}

func (r *summaryStaleKnowledgeRepo) GetKnowledgeByID(
	_ context.Context, _ uint64, _ string,
) (*types.Knowledge, error) {
	if r.err != nil {
		return nil, r.err
	}
	clone := *r.knowledge
	return &clone, nil
}

type summaryStaleChunkRepo struct {
	interfaces.ChunkRepository
	chunks map[string]*types.Chunk
	err    error
}

func (r *summaryStaleChunkRepo) GetChunkByID(
	_ context.Context, _ uint64, chunkID string,
) (*types.Chunk, error) {
	if r.err != nil {
		return nil, r.err
	}
	chunk, ok := r.chunks[chunkID]
	if !ok {
		return nil, errors.New("chunk not found")
	}
	clone := *chunk
	return &clone, nil
}

func TestSummarySourceChangedDetectsMetadataDrift(t *testing.T) {
	t.Parallel()
	stale, err := summarySourceChanged(
		context.Background(),
		&summaryStaleKnowledgeRepo{knowledge: &types.Knowledge{CustomMetadata: types.JSON(`{"region":"Shanghai"}`)}},
		&summaryStaleChunkRepo{},
		1, "knowledge-1", `{"region":"Beijing"}`, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Fatal("expected metadata drift to be stale")
	}
}

func TestSummarySourceChangedDetectsChunkContentDrift(t *testing.T) {
	t.Parallel()
	stale, err := summarySourceChanged(
		context.Background(),
		&summaryStaleKnowledgeRepo{knowledge: &types.Knowledge{CustomMetadata: types.JSON(`{}`)}},
		&summaryStaleChunkRepo{chunks: map[string]*types.Chunk{
			"chunk-1": {ID: "chunk-1", Content: "latest body", IsEnabled: true},
		}},
		1, "knowledge-1", `{}`, []*types.Chunk{{ID: "chunk-1", Content: "earlier body", IsEnabled: true}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Fatal("expected chunk content drift to be stale")
	}
}

func TestSummarySourceChangedReturnsLookupErrors(t *testing.T) {
	t.Parallel()
	lookupErr := errors.New("database unavailable")
	_, err := summarySourceChanged(
		context.Background(),
		&summaryStaleKnowledgeRepo{err: lookupErr},
		&summaryStaleChunkRepo{},
		1, "knowledge-1", `{}`, nil,
	)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("expected lookup error, got %v", err)
	}

	_, err = summarySourceChanged(
		context.Background(),
		&summaryStaleKnowledgeRepo{knowledge: &types.Knowledge{CustomMetadata: types.JSON(`{}`)}},
		&summaryStaleChunkRepo{err: lookupErr},
		1, "knowledge-1", `{}`, []*types.Chunk{{ID: "chunk-1"}},
	)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("expected chunk lookup error, got %v", err)
	}
}

func TestSummarySourceChangedAcceptsCurrentSnapshot(t *testing.T) {
	t.Parallel()
	stale, err := summarySourceChanged(
		context.Background(),
		&summaryStaleKnowledgeRepo{knowledge: &types.Knowledge{CustomMetadata: types.JSON(`{"region":"Shanghai"}`)}},
		&summaryStaleChunkRepo{chunks: map[string]*types.Chunk{
			"chunk-1": {ID: "chunk-1", Content: "current body", IsEnabled: true},
		}},
		1, "knowledge-1", `{"region":"Shanghai"}`,
		[]*types.Chunk{{ID: "chunk-1", Content: "current body", IsEnabled: true}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale {
		t.Fatal("expected current snapshot to be accepted")
	}
}
