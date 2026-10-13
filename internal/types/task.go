package types

// Worker-pool names are part of the runtime observability API. Each pool is
// backed by an independent asynq server, so concurrency is hard-isolated
// between pools instead of being only a weighted dequeue preference.
const (
	WorkerPoolCore        = "core"
	WorkerPoolEnrichment  = "enrichment"
	WorkerPoolMaintenance = "maintenance"
	WorkerPoolShared      = "shared"

	// Upstream defaults are explicit guarantees plus an elastic pool. The
	// shared pool may consume core and enrichment queues, so idle capacity in
	// either stage can be borrowed without sacrificing the dedicated minimums.
	DefaultCoreWorkerConcurrency        = 8
	DefaultEnrichmentWorkerConcurrency  = 12
	DefaultMaintenanceWorkerConcurrency = 4
	DefaultSharedWorkerConcurrency      = 6
	DefaultUpstreamWorkerConcurrency    = DefaultCoreWorkerConcurrency + DefaultEnrichmentWorkerConcurrency +
		DefaultMaintenanceWorkerConcurrency + DefaultSharedWorkerConcurrency
)

// Queue names. QueueMaintenance keeps the name "low" for the runtime API.
const (
	QueueDefault     = "default"
	QueueEntity      = "entity"
	QueueMaintenance = "low"
)

// QueueDefinition is the single source of truth for queue topology. Worker
// servers and runtime inspection both consume this registry, preventing the
// scheduling weights shown to operators from drifting from the actual server
// configuration.
type QueueDefinition struct {
	Name         string
	Pool         string
	Weight       int
	SharedWeight int
	TaskTypes    []string
}

var queueDefinitions = []QueueDefinition{
	{Name: QueueDefault, Pool: WorkerPoolCore, Weight: 1, SharedWeight: 3, TaskTypes: []string{
		TypeDocumentProcess, TypeManualProcess,
	}},
	{Name: QueueEntity, Pool: WorkerPoolEnrichment, Weight: 1, SharedWeight: 1, TaskTypes: []string{TypeEntityNameEmbeddingRebuild, TypeEmbeddingRebuild, TypeEntryVector, TypeTopicBackfill}},
	{Name: QueueMaintenance, Pool: WorkerPoolMaintenance, Weight: 1, TaskTypes: []string{
		TypeEntryListDelete, TypeEntryListReparse,
		TypeIngestAssetCleanup, TypeReportIndex,
	}},
}

// QueueDefinitions returns a copy so callers cannot mutate global topology.
func QueueDefinitions() []QueueDefinition {
	definitions := make([]QueueDefinition, len(queueDefinitions))
	for i, definition := range queueDefinitions {
		definitions[i] = definition
		definitions[i].TaskTypes = append([]string(nil), definition.TaskTypes...)
	}
	return definitions
}

// QueueForTaskType returns the declared queue for a task type. Producers still
// pass the queue explicitly to queue, while tests and observability can use
// this mapping to detect drift.
func QueueForTaskType(taskType string) (string, bool) {
	for _, definition := range queueDefinitions {
		for _, declaredType := range definition.TaskTypes {
			if declaredType == taskType {
				return definition.Name, true
			}
		}
	}
	return "", false
}

// QueueWeightsForPool returns the queue configuration for one worker
// pool. An empty map indicates a programming error in the pool declaration.
func QueueWeightsForPool(pool string) map[string]int {
	weights := make(map[string]int)
	for _, definition := range queueDefinitions {
		if definition.Pool == pool {
			weights[definition.Name] = definition.Weight
		}
	}
	return weights
}

// QueueWeightsForSharedPool returns the core/enrichment queues eligible for
// elastic capacity. Long maintenance tasks are excluded so they cannot pin
// burst capacity intended for the user-facing pipeline.
func QueueWeightsForSharedPool() map[string]int {
	weights := make(map[string]int)
	for _, definition := range queueDefinitions {
		if definition.SharedWeight > 0 {
			weights[definition.Name] = definition.SharedWeight
		}
	}
	return weights
}

// WorkerPoolConcurrency contains the explicit per-instance pool capacities.
// Unlike the old ratio allocator, each field is independently configurable.
type WorkerPoolConcurrency struct {
	Core        int
	Enrichment  int
	Maintenance int
	Shared      int
}

func DefaultWorkerPoolConcurrency() WorkerPoolConcurrency {
	return WorkerPoolConcurrency{
		Core:        DefaultCoreWorkerConcurrency,
		Enrichment:  DefaultEnrichmentWorkerConcurrency,
		Maintenance: DefaultMaintenanceWorkerConcurrency,
		Shared:      DefaultSharedWorkerConcurrency,
	}
}

// ResolveWorkerPoolConcurrency centralizes setting keys, environment names,
// defaults, and invalid-value fallback for both server construction and the
// runtime API. The callback lets this package stay independent of the system
// setting service interface.
func ResolveWorkerPoolConcurrency(read func(key, env string, fallback int) int) WorkerPoolConcurrency {
	allocation := DefaultWorkerPoolConcurrency()
	if read == nil {
		return allocation
	}
	positive := func(key, env string, fallback int) int {
		value := read(key, env, fallback)
		if value < 1 {
			return fallback
		}
		return value
	}
	allocation.Core = positive("queue.core_concurrency", "RELIQUARY_QUEUE_CORE_CONCURRENCY", allocation.Core)
	allocation.Enrichment = positive("queue.enrichment_concurrency", "RELIQUARY_QUEUE_ENRICHMENT_CONCURRENCY", allocation.Enrichment)
	allocation.Maintenance = positive("queue.maintenance_concurrency", "RELIQUARY_QUEUE_MAINTENANCE_CONCURRENCY", allocation.Maintenance)
	allocation.Shared = positive("queue.shared_concurrency", "RELIQUARY_QUEUE_SHARED_CONCURRENCY", allocation.Shared)
	return allocation
}

func (c WorkerPoolConcurrency) UpstreamTotal() int {
	return c.Core + c.Enrichment + c.Maintenance + c.Shared
}

// QueueStat is a read-only depth snapshot of a single queue, used
// by the System runtime dashboard. Field names mirror the counts
// exposed by queue.QueueInfo. Pool / Weight are static metadata (which
// worker pool drains the queue and its scheduling weight within that
// pool) so the UI can group and explain the lanes without the frontend
// hard-coding the topology.
type QueueStat struct {
	Name string `json:"name"`
	// Pool is the independent worker pool that drains this queue.
	Pool string `json:"pool"`
	// Weight is the queue's scheduling weight inside its pool.
	Weight int `json:"weight"`
	// Size is the total number of tasks in the queue (pending + active +
	// scheduled + retry + aggregating + archived).
	Size      int `json:"size"`
	Pending   int `json:"pending"`
	Active    int `json:"active"`
	Scheduled int `json:"scheduled"`
	Retry     int `json:"retry"`
	Archived  int `json:"archived"`
	Completed int `json:"completed"`
	Cancelled int `json:"cancelled"`
	// Processed / Failed are today's counters (reset daily).
	Processed int `json:"processed"`
	Failed    int `json:"failed"`
	// Paused reports whether the queue is paused (tasks not consumed).
	Paused bool `json:"paused"`
	// LatencyMs is the age of the oldest pending task, in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
}

// WorkerServerStat is one live asynq worker pool snapshot. Queue weights identify
// which logical pool the server belongs to; the runtime handler aggregates
// these records across pools so configured per-instance capacity is not
// confused with actual cluster capacity.
type WorkerServerStat struct {
	Concurrency int
	Active      int
	Status      string
	Queues      map[string]int
}

const (
	TypeDocumentProcess            = "document:process"       // 文档处理任务
	TypeEntryListDelete            = "knowledge:list_delete"  // 批量删除知识任务
	TypeEntryListReparse           = "knowledge:list_reparse" // 批量重解析知识任务
	TypeEntityNameEmbeddingRebuild = "entity:name_embedding_rebuild"
	TypeEntryVector                = "entry:vector"
	TypeEmbeddingRebuild           = "knowledge:embedding_rebuild"
	TypeManualProcess              = "manual:process" // 文章正文处理任务，兼容已有持久队列名称
	TypeIngestAssetCleanup         = "ingest:asset_cleanup"
	TypeReportIndex                = "report:index"
)

// DocumentProcessPayload represents the document process task payload
type DocumentProcessPayload struct {
	RequestId string   `json:"request_id"`
	SpaceID   uint64   `json:"tenant_id"`
	EntryID   string   `json:"knowledge_id"`
	LibraryID string   `json:"library_id"`
	FilePath  string   `json:"file_path,omitempty"` // 文件路径（文件导入时使用）
	FileName  string   `json:"file_name,omitempty"` // 文件名（文件导入时使用）
	FileType  string   `json:"file_type,omitempty"` // 文件类型（文件导入时使用）
	URL       string   `json:"url,omitempty"`       // URL（URL导入时使用）
	FileURL   string   `json:"file_url,omitempty"`  // 文件资源链接（file_url导入时使用）
	Passages  []string `json:"passages,omitempty"`  // 文本段落（文本导入时使用）
	Language  string   `json:"language,omitempty"`  // Request locale for {{language}} in prompt templates
	// Attempt is the per-entry attempt number this task belongs to.
	// Set on enqueue (initial parse → attempt 1; reparse → max+1) so
	// every span recorded by this task lands on the right attempt
	// branch. Queue retries within an attempt keep the same value so
	// retried spans overwrite the previous attempt's row rather than
	// fan out into a new attempt for every retry.
	Attempt int `json:"attempt,omitempty"`
}

// EntryListDeletePayload represents the batch entry delete task payload
type EntryListDeletePayload struct {
	SpaceID   uint64        `json:"tenant_id"`
	EntryIDs  []string      `json:"knowledge_ids"`
	Initiator TaskInitiator `json:"initiator,omitempty"`
}

// EntryListReparsePayload represents the batch entry reparse task payload
type EntryListReparsePayload struct {
	SpaceID       uint64                 `json:"tenant_id"`
	EntryIDs      []string               `json:"knowledge_ids"`
	ProcessConfig *EntryProcessOverrides `json:"process_config,omitempty"`
	Initiator     TaskInitiator          `json:"initiator,omitempty"`
}

// ManualProcessPayload carries supplied or already stored article text.
// Used by ingest processing and local article reprocessing.
type ManualProcessPayload struct {
	RequestId   string `json:"request_id"`
	SpaceID     uint64 `json:"tenant_id"`
	EntryID     string `json:"knowledge_id"`
	LibraryID   string `json:"library_id"`
	Content     string `json:"content"`      // cleaned markdown content
	NeedCleanup bool   `json:"need_cleanup"` // true for update, false for create
}

const TypeTopicBackfill = "topic:backfill"

type TopicBackfillPayload struct {
	SpaceID uint64 `json:"tenant_id"`
	JobID   string `json:"job_id"`
}

// ReportIndexPayload identifies the saved report to index without embedding its body.
type ReportIndexPayload struct {
	SpaceID   uint64 `json:"tenant_id"`
	LibraryID string `json:"library_id"`
	EntryID   string `json:"knowledge_id"`
}
