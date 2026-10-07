package types

import "testing"

func TestContextNameEmbeddingRebuildUsesGraphEnrichmentQueue(t *testing.T) {
	queue, ok := QueueForTaskType(TypeContextNameEmbeddingRebuild)
	if !ok || queue != QueueGraph {
		t.Fatalf("name embedding rebuild queue = %q, want %q", queue, QueueGraph)
	}
	if QueueWeightsForPool(WorkerPoolEnrichment)[queue] <= 0 || QueueWeightsForSharedPool()[queue] <= 0 {
		t.Fatal("name embedding rebuild requires enrichment and shared workers")
	}
	if QueueWeightsForPool(WorkerPoolCore)[queue] != 0 || QueueWeightsForPool(WorkerPoolWiki)[queue] != 0 {
		t.Fatal("name embedding rebuild must preserve parse and wiki pool capacity")
	}
}
