package tools

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"reliquary/internal/types"
)

func TestAggregateByKnowledge_mergesChunksFromSameDocument(t *testing.T) {
	tool := &GrepChunksTool{}
	compiled := []*regexp.Regexp{regexp.MustCompile(`(?i)sample`)}
	results := []chunkWithTitle{
		{
			Chunk: types.Chunk{
				ID:              "chunk-a",
				KnowledgeID:     "doc-1",
				KnowledgeBaseID: "kb-1",
				Content:         "sample hit one",
			},
			KnowledgeTitle: "report-a.pdf",
		},
		{
			Chunk: types.Chunk{
				ID:              "chunk-b",
				KnowledgeID:     "doc-1",
				KnowledgeBaseID: "kb-1",
				Content:         "sample hit two",
			},
			KnowledgeTitle: "report-a.pdf",
		},
		{
			Chunk: types.Chunk{
				ID:              "chunk-c",
				KnowledgeID:     "doc-2",
				KnowledgeBaseID: "kb-1",
				Content:         "sample other doc",
			},
			KnowledgeTitle: "report-b.pdf",
		},
	}

	aggregated := tool.aggregateByKnowledge(results, []string{"sample"}, compiled)
	if len(aggregated) != 2 {
		t.Fatalf("want 2 documents, got %d", len(aggregated))
	}

	byID := make(map[string]knowledgeAggregation, len(aggregated))
	for _, row := range aggregated {
		byID[row.KnowledgeID] = row
	}
	if byID["doc-1"].ChunkHitCount != 2 {
		t.Fatalf("doc-1 chunk hits = %d, want 2", byID["doc-1"].ChunkHitCount)
	}
	if byID["doc-2"].ChunkHitCount != 1 {
		t.Fatalf("doc-2 chunk hits = %d, want 1", byID["doc-2"].ChunkHitCount)
	}
}

func TestGrepChunksIncludesRelevantSourceDispute(t *testing.T) {
	called := false
	tool := (&GrepChunksTool{}).WithSourceDisputes(func(_ context.Context, kbIDs, knowledgeIDs []string) ([]types.KnowledgeAnomaly, error) {
		called = true
		if len(kbIDs) != 1 || kbIDs[0] != "kb-1" || len(knowledgeIDs) != 1 || knowledgeIDs[0] != "doc-1" {
			t.Fatalf("unexpected dispute lookup scope: kb=%v docs=%v", kbIDs, knowledgeIDs)
		}
		return []types.KnowledgeAnomaly{{ID: "issue-1", Status: "pending", KnowledgeIDs: []string{"doc-1"}, Description: "sources disagree"}}, nil
	})
	got := tool.appendSourceDisputes(context.Background(), "<grep_results/>", []string{"kb-1"}, []chunkWithTitle{
		{Chunk: types.Chunk{KnowledgeID: "doc-1"}},
		{Chunk: types.Chunk{KnowledgeID: "doc-1"}},
	})
	if !called || !strings.Contains(got, `<source_disputes>`) || !strings.Contains(got, `sources disagree`) {
		t.Fatalf("expected one relevant source dispute after grep results, got %q", got)
	}
}
