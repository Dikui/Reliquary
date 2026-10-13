import { get, post, put, del, patch, postUpload } from '@/utils/request'

export interface SystemInfo {
  version: string
  edition?: string
  commit_id?: string
  build_time?: string
  go_version?: string
  keyword_index_engine?: string
  db_version?: string
  /** Human-readable error message when the startup migration failed.
   *  When non-empty, the system info view should surface a troubleshooting
   *  banner (see docs/migration-troubleshooting.md). */
  db_migration_error?: string
  /** Server process boot time (RFC3339, UTC). */
  started_at?: string
  /** Seconds since process start. */
  uptime_seconds?: number
  credential_encryption_enabled: boolean
}

export interface DeploymentCapability {
  supported: boolean
  reason?: string
}

export interface DeploymentCapabilitiesResponse {
  edition: string
  capabilities: Record<string, DeploymentCapability>
}

export function getDeploymentCapabilities(): Promise<{ data: DeploymentCapabilitiesResponse }> {
  return get('/api/v1/system/capabilities')
}

export function getSystemInfo(): Promise<{ data: SystemInfo }> {
  return get('/api/v1/system/info')
}


export interface ParserEngineInfo {
  Name: string
  Description: string
  FileTypes: string[]
  Available?: boolean
  UnavailableReason?: string
}

/** 解析引擎配置（引擎连接参数存空间） */
export type MinerUParseMethod = 'auto' | 'ocr' | 'txt'

export interface ParserEngineConfig {
  mineru_endpoint?: string
  mineru_api_key?: string
  // MinerU 自建参数
  mineru_model?: string
  mineru_vlm_server_url?: string
  mineru_enable_formula?: boolean | null
  mineru_enable_table?: boolean | null
  mineru_parse_method?: MinerUParseMethod
  mineru_enable_ocr?: boolean | null
  mineru_language?: string
  // MinerU 云 API 参数
  mineru_cloud_model?: string
  mineru_cloud_enable_formula?: boolean | null
  mineru_cloud_enable_table?: boolean | null
  mineru_cloud_enable_ocr?: boolean | null
  mineru_cloud_language?: string
}

export interface ParserEnginesResponse {
  data: ParserEngineInfo[]
}

export function getParserEngines(): Promise<ParserEnginesResponse> {
  return get('/api/v1/system/parser-engines')
}

/** 使用当前填写的参数检测引擎可用性（不保存），用于填写新参数后即时测试 */
export function checkParserEngines(config: ParserEngineConfig): Promise<ParserEnginesResponse> {
  return post('/api/v1/system/parser-engines/check', config)
}

export function getParserEngineConfig(): Promise<{ data: ParserEngineConfig }> {
  return get('/api/v1/space/kv/parser-engine-config')
}

export function updateParserEngineConfig(config: ParserEngineConfig): Promise<{ data: ParserEngineConfig }> {
  return put('/api/v1/space/kv/parser-engine-config', config)
}
export interface SystemSettingItem {
  id: number
  key: string
  /** Raw JSON value — narrow via value_type before rendering. */
  value: unknown
  value_type: 'int' | 'string' | 'bool' | 'string_list'
  category: string
  description: string
  /** P3+ — currently always false. UI may surface a "redacted" state when true. */
  is_secret: boolean
  /** P3+ — currently always false. UI may show "needs restart to take effect" badge when true. */
  requires_restart: boolean
  last_modified_by: string
  /**
   * Display label resolved from last_modified_by (UUID) on the server —
   * username when known, email as a fallback. Empty/undefined for
   * virtual rows that were never persisted; UI then falls back to the
   * UUID prefix.
   */
   last_modified_by_name?: string
  created_at: string
  updated_at: string
  /**
   * Allowed values for `value` when this setting is constrained. Populated by
   * the service from the in-code registry; absent/empty means "free-form".
   * Frontend renders a t-select instead of t-input when this is non-empty.
   */
  enum?: string[]
}

/**
 * List every system setting row (system-scope, not space-scope).
 * Backend returns the array directly; we cast through `unknown` to match
 * the project-wide axios contract (see utils/request.ts:97).
 */
export async function listSystemSettings(): Promise<SystemSettingItem[]> {
  const response = await get('/api/v1/system/settings')
  return response as unknown as SystemSettingItem[]
}

/**
 * Fetch a single system setting by key. Throws (via the axios interceptor)
 * if the key is unknown to the registry, or if the row is not yet persisted.
 */
export async function getSystemSetting(key: string): Promise<SystemSettingItem> {
  const response = await get(`/api/v1/system/settings/${encodeURIComponent(key)}`)
  return response as unknown as SystemSettingItem
}

/**
 * Persist a new value for `key`. The backend validates the value against
 * the registry-declared value_type and rejects mismatches with 400; the
 * error message is surfaced via err.message (see utils/request.ts:209).
 *
 * Successful updates emit an audit row (action=system.setting_changed)
 * carrying old/new values for forensics.
 */
export async function updateSystemSetting(
  key: string,
  value: unknown,
): Promise<SystemSettingItem> {
  const response = await put(
    `/api/v1/system/settings/${encodeURIComponent(key)}`,
    { value },
  )
  return response as unknown as SystemSettingItem
}

/**
 * Reset a system setting back to its ENV / built-in default by deleting
 * the DB override row. Idempotent — resetting a key that was never
 * persisted resolves successfully.
 */
export async function resetSystemSetting(key: string): Promise<void> {
  await del(`/api/v1/system/settings/${encodeURIComponent(key)}`)
}

// ---- Runtime queue observability (system-scope) ----

/**
 * QueueStat mirrors types.QueueStat on the backend: a read-only depth
 * snapshot of one durable task queue. `pool` identifies the independent worker pool;
 * `weight` is the queue's scheduling weight within that pool.
 * The `active` count is the number of
 * tasks currently being processed (the closest thing to "workers busy"),
 * `pending` is the backlog waiting to be picked up.
 */
export interface QueueStat {
  name: string
  pool: string
  weight: number
  size: number
  pending: number
  active: number
  scheduled: number
  retry: number
  archived: number
  completed: number
  processed: number
  failed: number
  paused: boolean
  latency_ms: number
}

export interface RuntimeWorkerPool {
  name: string
  concurrency: number
  queue_count: number
  instances: number
  cluster_capacity: number
  active: number
  utilization: number
}

export interface ModelRuntimeStat {
  model_id: string
  name: string
  active: number
  waiting: number
  limit: number
}

/**
 * Runtime queue dashboard payload. Each pool includes configured per-process
 * concurrency and live capacity/active workers in the running process.
 */
export interface QueueConnectionStatus {
 state: 'active' | 'reconnecting' | 'stopped'
 last_error?: string
 since?: string
 next_retry_at?: string
 alert: boolean
 alert_after_seconds: number
}

export interface RuntimeQueuesResponse {
 queue_connection?: QueueConnectionStatus
  available: boolean
  upstream_concurrency: number
  parse_concurrency: number
  pools: RuntimeWorkerPool[]
  queues: QueueStat[]
  model_limiter_available: boolean
  models: ModelRuntimeStat[]
  timestamp: number
}

export type RuntimeTaskState = 'pending' | 'active' | 'scheduled' | 'retry' | 'archived' | 'completed'
export type RuntimeTaskAction = 'cancel' | 'run_now' | 'delete'

export interface RuntimeTask {
  id: string
  queue: string
  type: string
  state: RuntimeTaskState
  allowed_actions: RuntimeTaskAction[]
  last_error?: string
  last_failed_at?: string
  next_process_at?: string
  started_at?: string
  completed_at?: string
  deadline?: string
  enqueued_at?: string
  retried: number
  max_retry: number
  is_orphaned?: boolean
  worker?: string
  space_id?: number
  library_id?: string
  entry_id?: string
  task_id?: string
  source_library_id?: string
  target_library_id?: string
  entry_count?: number
}

export interface RuntimeTasksResponse {
  available: boolean
  tasks: RuntimeTask[]
  page_size: number
  has_more: boolean
  next_cursor?: string
}

/**
 * Fetch durable queue depths and worker-pool concurrency.
 * Returns the object directly — no {data: ...} wrapping (see
 * utils/request.ts interceptor).
 */
export async function getRuntimeQueues(): Promise<RuntimeQueuesResponse> {
  const response = await get('/api/v1/system/runtime/queues')
  return response as unknown as RuntimeQueuesResponse
}

export async function getRuntimeTasks(
  queue: string,
  state: RuntimeTaskState,
  cursor = '',
  pageSize = 20,
): Promise<RuntimeTasksResponse> {
  return get(`/api/v1/system/runtime/queues/${encodeURIComponent(queue)}/tasks`, {
    params: { state, ...(cursor ? { cursor } : {}), page_size: pageSize },
  })
}

export async function mutateRuntimeTask(
  queue: string,
  taskID: string,
  action: RuntimeTaskAction,
): Promise<void> {
  await post(
    `/api/v1/system/runtime/queues/${encodeURIComponent(queue)}/tasks/${encodeURIComponent(taskID)}/actions/${encodeURIComponent(action)}`,
  )
}

/**
 * Clear every archived (finally-failed) task in one queue in a single call.
 * Only touches the archived dead-letter set — live tasks are never affected.
 * Backend: DELETE /api/v1/system/runtime/queues/{queue}/archived.
 * Returns the object directly (no {data: ...} wrapping, see request.ts).
 */
export async function purgeArchivedRuntimeTasks(
  queue: string,
): Promise<{ success: boolean; deleted: number }> {
  const response = await del(
    `/api/v1/system/runtime/queues/${encodeURIComponent(queue)}/archived`,
  )
  return response as unknown as { success: boolean; deleted: number }
}

/** Local health remains readable while database-backed authentication is offline. */
export async function getQueueConnectionStatus(): Promise<QueueConnectionStatus | undefined> {
 const response = await get('/health') as { queue_connection?: QueueConnectionStatus }
 return response.queue_connection
}
