package types

import "testing"

func TestQueueDefinitionsAreUniqueAndConsumable(t *testing.T) {
	definitions := QueueDefinitions()
	if len(definitions) == 0 {
		t.Fatal("queue registry must not be empty")
	}

	validPools := map[string]bool{
		WorkerPoolCore:        true,
		WorkerPoolEnrichment:  true,
		WorkerPoolMaintenance: true,
	}
	seen := make(map[string]bool, len(definitions))
	seenTaskTypes := make(map[string]string)
	for _, definition := range definitions {
		if seen[definition.Name] {
			t.Fatalf("duplicate queue definition %q", definition.Name)
		}
		seen[definition.Name] = true
		if !validPools[definition.Pool] {
			t.Fatalf("queue %q references unknown pool %q", definition.Name, definition.Pool)
		}
		if definition.Weight <= 0 {
			t.Fatalf("queue %q has non-positive weight %d", definition.Name, definition.Weight)
		}
		if len(definition.TaskTypes) == 0 {
			t.Fatalf("queue %q declares no task types", definition.Name)
		}
		for _, taskType := range definition.TaskTypes {
			if previousQueue, exists := seenTaskTypes[taskType]; exists {
				t.Fatalf("task type %q is declared by both %q and %q", taskType, previousQueue, definition.Name)
			}
			seenTaskTypes[taskType] = definition.Name
			queue, ok := QueueForTaskType(taskType)
			if !ok || queue != definition.Name {
				t.Fatalf("task type %q resolves to queue %q, want %q", taskType, queue, definition.Name)
			}
		}
	}

	for pool := range validPools {
		if len(QueueWeightsForPool(pool)) == 0 {
			t.Fatalf("worker pool %q has no queues", pool)
		}
	}
	shared := QueueWeightsForSharedPool()
	if len(shared) == 0 || shared[QueueDefault] <= 0 || shared[QueueEntity] <= 0 {
		t.Fatalf("shared pool must cover core and enrichment queues: %+v", shared)
	}
	if shared[QueueMaintenance] != 0 {
		t.Fatalf("shared pool must not consume maintenance: %+v", shared)
	}
}

func TestQueueMaintenanceKeepsRuntimeName(t *testing.T) {
	if QueueMaintenance != "low" {
		t.Fatalf("maintenance queue must keep established runtime name, got %q", QueueMaintenance)
	}
}

func TestEveryTaskTypeHasADeclaredQueue(t *testing.T) {
	taskTypes := []string{
		TypeDocumentProcess,
		TypeEntryListReparse, TypeEntryListDelete,
		TypeManualProcess, TypeEntityNameEmbeddingRebuild, TypeEmbeddingRebuild, TypeTopicBackfill,
		TypeIngestAssetCleanup, TypeReportIndex,
	}
	for _, taskType := range taskTypes {
		if _, ok := QueueForTaskType(taskType); !ok {
			t.Fatalf("task type %q has no declared queue", taskType)
		}
	}
}

func TestDefaultWorkerPoolConcurrencyIsExplicitBudget(t *testing.T) {
	allocation := DefaultWorkerPoolConcurrency()
	if allocation.UpstreamTotal() != DefaultUpstreamWorkerConcurrency {
		t.Fatalf("upstream total = %d, want %d", allocation.UpstreamTotal(), DefaultUpstreamWorkerConcurrency)
	}
	if allocation.Core < 1 || allocation.Enrichment < 1 ||
		allocation.Maintenance < 1 || allocation.Shared < 1 {
		t.Fatalf("every pool must have positive explicit capacity: %+v", allocation)
	}
}

func TestResolveWorkerPoolConcurrencyFallsBackPerPool(t *testing.T) {
	allocation := ResolveWorkerPoolConcurrency(func(key, _ string, fallback int) int {
		if key == "queue.core_concurrency" {
			return 15
		}
		if key == "queue.shared_concurrency" {
			return 0
		}
		return fallback
	})
	if allocation.Core != 15 {
		t.Fatalf("core = %d, want override 15", allocation.Core)
	}
	if allocation.Shared != DefaultSharedWorkerConcurrency {
		t.Fatalf("shared = %d, want fallback %d", allocation.Shared, DefaultSharedWorkerConcurrency)
	}
}

func TestReplacedProcessingTasksHaveNoQueue(t *testing.T) {
	for _, taskType := range []string{
		"summary:generation", "question:generation", "image:multimodal", "knowledge:post_process", "datatable:summary",
		"graph:sync", "graph:check", "graph:rebuild",
	} {
		if queue, ok := QueueForTaskType(taskType); ok {
			t.Fatalf("removed task %q still routes to %q", taskType, queue)
		}
	}
}
