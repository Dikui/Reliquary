package types

import "testing"

func TestEntityNameEmbeddingRebuildUsesEntityEnrichmentQueue(t *testing.T) {
	queue, ok := QueueForTaskType(TypeEntityNameEmbeddingRebuild)
	if !ok || queue != QueueEntity {
		t.Fatalf("name embedding rebuild queue = %q, want %q", queue, QueueEntity)
	}
	if QueueWeightsForPool(WorkerPoolEnrichment)[queue] <= 0 || QueueWeightsForSharedPool()[queue] <= 0 {
		t.Fatal("name embedding rebuild requires enrichment and shared workers")
	}
	if QueueWeightsForPool(WorkerPoolCore)[queue] != 0 {
		t.Fatal("name embedding rebuild must preserve parse pool capacity")
	}
}
