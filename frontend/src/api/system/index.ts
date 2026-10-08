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

export interface PlaceholderDefinition {
  name: string
  label: string
  description: string
}

export interface PromptTemplate {
  id: string
  name: string
  description: string
  content: string
  user?: string
  has_knowledge_base?: boolean
  has_web_search?: boolean
  default?: boolean
  mode?: string
}

export interface PromptTemplatesConfig {
  system_prompt: PromptTemplate[]
  context_template: PromptTemplate[]
  // Rewrite templates — each template contains both content (system) + user fields
  rewrite: PromptTemplate[]
  // Fallback templates — fixed responses + model fallback prompts (mode: "model")
  fallback: PromptTemplate[]

  generate_session_title?: PromptTemplate[]
  generate_summary?: PromptTemplate[]
  keywords_extraction?: PromptTemplate[]
  chat_summary?: PromptTemplate[]
  agent_system_prompt?: PromptTemplate[]
  intent_prompts?: PromptTemplate[]
}

export function getSystemInfo(): Promise<{ data: SystemInfo }> {
  return get('/api/v1/system/info')
}

export function getPromptTemplates(): Promise<{ data: PromptTemplatesConfig }> {
  return get('/api/v1/tenants/kv/prompt-templates')
}

export interface ParserEngineInfo {
  Name: string
  Description: string
  FileTypes: string[]
  Available?: boolean
  UnavailableReason?: string
}

/** 解析引擎配置（引擎连接参数存空间；聊天附件解析策略在智能体中配置） */
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
  return get('/api/v1/tenants/kv/parser-engine-config')
}

export function updateParserEngineConfig(config: ParserEngineConfig): Promise<{ data: ParserEngineConfig }> {
  return put('/api/v1/tenants/kv/parser-engine-config', config)
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
 * List every system setting row (system-scope, not tenant-scope).
 * Backend returns the array directly; we cast through `unknown` to match
 * the project-wide axios contract (see utils/request.ts:97).
 */
export async function listSystemSettings(): Promise<SystemSettingItem[]> {
  const response = await get('/api/v1/system/admin/settings')
  return response as unknown as SystemSettingItem[]
}

/**
 * Fetch a single system setting by key. Throws (via the axios interceptor)
 * if the key is unknown to the registry, or if the row is not yet persisted.
 */
export async function getSystemSetting(key: string): Promise<SystemSettingItem> {
  const response = await get(`/api/v1/system/admin/settings/${encodeURIComponent(key)}`)
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
    `/api/v1/system/admin/settings/${encodeURIComponent(key)}`,
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
  await del(`/api/v1/system/admin/settings/${encodeURIComponent(key)}`)
}

// ---- Runtime queue observability (system-scope) ----

/**
 * QueueStat mirrors types.QueueStat on the backend: a read-only depth
 * snapshot of one asynq queue. `pool` identifies the independent worker pool;
 * `weight` is the queue's scheduling weight within that pool.
 * Counts follow asynq.QueueInfo semantics — `active` is the number of
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
  memory_usage_bytes: number
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
 * concurrency and live cluster capacity/active workers aggregated from asynq
 * server heartbeats.
 */
export interface RuntimeQueuesResponse {
  available: boolean
  upstream_concurrency: number
  parse_concurrency: number
  wiki_concurrency: number
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
  tenant_id?: number
  knowledge_base_id?: string
  knowledge_id?: string
  task_id?: string
  source_id?: string
  target_id?: string
  source_kb_id?: string
  target_kb_id?: string
  data_source_id?: string
  sync_log_id?: string
  knowledge_count?: number
}

export interface RuntimeTasksResponse {
  available: boolean
  tasks: RuntimeTask[]
  page_size: number
  has_more: boolean
  next_cursor?: string
}

/**
 * Fetch the live asynq queue depths + worker-pool concurrency.
 * Returns the object directly — no {data: ...} wrapping (see
 * utils/request.ts interceptor).
 */
export async function getRuntimeQueues(): Promise<RuntimeQueuesResponse> {
  const response = await get('/api/v1/system/admin/runtime/queues')
  return response as unknown as RuntimeQueuesResponse
}

export async function getRuntimeTasks(
  queue: string,
  state: RuntimeTaskState,
  cursor = '',
  pageSize = 20,
): Promise<RuntimeTasksResponse> {
  return get(`/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/tasks`, {
    params: { state, ...(cursor ? { cursor } : {}), page_size: pageSize },
  })
}

export async function mutateRuntimeTask(
  queue: string,
  taskID: string,
  action: RuntimeTaskAction,
): Promise<void> {
  await post(
    `/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/tasks/${encodeURIComponent(taskID)}/actions/${encodeURIComponent(action)}`,
  )
}

/**
 * Clear every archived (finally-failed) task in one queue in a single call.
 * Only touches the archived dead-letter set — live tasks are never affected.
 * Backend: DELETE /api/v1/system/admin/runtime/queues/{queue}/archived.
 * Returns the object directly (no {data: ...} wrapping, see request.ts).
 */
export async function purgeArchivedRuntimeTasks(
  queue: string,
): Promise<{ success: boolean; deleted: number }> {
  const response = await del(
    `/api/v1/system/admin/runtime/queues/${encodeURIComponent(queue)}/archived`,
  )
  return response as unknown as { success: boolean; deleted: number }
}

// --- Sandbox backend configuration (per workspace) ---

export interface SandboxVolumeMountConfig {
  enabled: boolean
  mount_path?: string
  provider?: string
  volume_id?: string
  volume_name?: string
  volume_owner_fingerprint?: string
}

export interface SandboxSkillImage {
  snapshot_id?: string
  generation?: number
  built_at?: string
  base_template_id?: string
  owner_fingerprint?: string
}

export interface SandboxConfig {
  sandbox_type?: string
  default_timeout_sec?: number
  allow_private_endpoints?: boolean
  env_vars?: Record<string, string>
  volume_mount?: SandboxVolumeMountConfig
  skill_image?: SandboxSkillImage
  skill_rollout?: 'next_turn' | 'new_session'
  network?: SandboxNetworkPolicy
  docker?: SandboxDockerConfig
}

/** Docker backend: one daemon, one long-lived container per session. */
export interface SandboxDockerConfig {
  image?: string
  host?: string
  tls_cert_path?: string
  cpu_limit?: number
  memory_limit_mb?: number
  pids_limit?: number
  network_mode?: string
  runtime?: string
  idle_ttl_seconds?: number
  http_timeout_sec?: number
}

/**
 * Network policy for every sandbox created from this config. Absent fields
 * mean egress allowed. Inbound is always credential-required:
 * allow_public_inbound is accepted then ignored/cleared.
 */
export interface SandboxNetworkPolicy {
  deny_egress_by_default?: boolean
  /** Ignored. Inbound is always credential-required. */
}

/** `ok: null` means the probe was not executed in this run. */
export interface SandboxCheckItem {
  name: string
  ok: boolean | null
  message?: string
  /** Stable code for why a probe was skipped; localized by the caller. */
  reason?: string
  latency_ms?: number
}

export interface SandboxCheckResult {
  ok: boolean
  provider: string
  checks: SandboxCheckItem[]
  capabilities?: Record<string, boolean>
}

export interface SandboxTemplate {
  id: string
  name: string
  status?: string
  version?: string
  image?: string
  created_at?: string
  updated_at?: string
  standard: boolean
  /** The provider's own explanation for a failed build, when it reports one. */
  error?: string
  instance_type?: string
  network_type?: string
  allow_internet_access?: boolean
}

export interface SandboxTemplateCatalog {
  templates: SandboxTemplate[]
  standard_template_id?: string
  provisioned: boolean
}

/** One named sandbox backend config. Credentials arrive masked. */
export interface SandboxConfigRecord {
  id: string
  name: string
  description?: string
  sandbox_type: string
  config: SandboxConfig
  created_at: string
  updated_at: string
}

/** Create/update payload. */
export interface SandboxConfigUpsert {
  name: string
  description?: string
  config: SandboxConfig
}

/**
 * What a config currently holds. `sandbox_count` comes from the provider, so a
 * non-zero value is authoritative: identity edits and deletion are refused
 * until it reaches zero.
 *
 * `unverifiable` means the provider could not be reached, so the count is
 * UNKNOWN rather than zero — never render it as "0 sandboxes".
 */
export interface SandboxInventory {
  sandbox_count: number
  session_ids?: string[]
  agent_names?: string[]
  unverifiable?: boolean
}

/** Sandbox backends managed as named workspace configurations. */
export const NAMED_SANDBOX_BACKEND_TYPES = ['docker'] as const

export function isNamedSandboxBackend(type: string): boolean {
  return (NAMED_SANDBOX_BACKEND_TYPES as readonly string[]).includes(type)
}

/** Returns every sandbox config of the workspace. No config means disabled. */
export function listSandboxConfigs(): Promise<{
  data: SandboxConfigRecord[]
  workspace_scripts_disabled?: boolean
}> {
  return get('/api/v1/sandbox-configs') as unknown as Promise<{
    data: SandboxConfigRecord[]
    workspace_scripts_disabled?: boolean
  }>
}

export function setSandboxWorkspacePolicy(scriptsDisabled: boolean): Promise<{
  workspace_scripts_disabled: boolean
}> {
  return put('/api/v1/sandbox-configs/workspace-policy', {
    scripts_disabled: scriptsDisabled,
  }) as unknown as Promise<{ workspace_scripts_disabled: boolean }>
}

export function createSandboxConfig(
  payload: SandboxConfigUpsert,
): Promise<{ data: SandboxConfigRecord }> {
  return post('/api/v1/sandbox-configs', payload) as unknown as Promise<{
    data: SandboxConfigRecord
  }>
}

export function getSandboxConfigById(id: string): Promise<{ data: SandboxConfigRecord }> {
  return get(`/api/v1/sandbox-configs/${id}`) as unknown as Promise<{
    data: SandboxConfigRecord
  }>
}

export function updateSandboxConfigById(
  id: string,
  payload: SandboxConfigUpsert,
): Promise<{ data: SandboxConfigRecord }> {
  return put(`/api/v1/sandbox-configs/${id}`, payload) as unknown as Promise<{
    data: SandboxConfigRecord
  }>
}

/**
 * `force` only overrides an inventory the backend could not verify; it never
 * overrides sandboxes the backend can actually see. Ask for it exclusively in
 * response to a `sandbox_inventory_unverifiable` conflict.
 */
export function deleteSandboxConfig(id: string, force = false): Promise<void> {
  const query = force ? '?force=true' : ''
  return del(`/api/v1/sandbox-configs/${id}${query}`) as unknown as Promise<void>
}

export function getSandboxConfigInventory(id: string): Promise<{ data: SandboxInventory }> {
  return get(`/api/v1/sandbox-configs/${id}/sandboxes`) as unknown as Promise<{
    data: SandboxInventory
  }>
}

/**
 * Fetch templates using the connection currently entered in the drawer.
 * `ensure_standard` starts a provider-side build when no Reliquary template is
 * present. `replace_standard` rebuilds the Reliquary template so a new spec
 * (DNS, image) can take effect; it requires `config_id`. The returned
 * building item can be polled through the same endpoint.
 */
export function querySandboxTemplates(payload: {
  config: SandboxConfig
  config_id?: string
  ensure_standard?: boolean
  replace_standard?: boolean
}): Promise<{ data: SandboxTemplateCatalog }> {
  return post('/api/v1/sandbox-configs/templates/query', payload) as unknown as Promise<{
    data: SandboxTemplateCatalog
  }>
}

/**
 * Probe a sandbox configuration without saving it. Redacted secrets are
 * resolved server-side, so pass `config_id` alongside an edited `config` to
 * test unsaved changes without retyping an API key. Omit `config` to probe a
 * stored config as-is.
 *
 * `deep` additionally creates and destroys one sandbox, which is the only way
 * It consumes real sandbox time.
 */
export function checkSandboxConfig(payload: {
  config?: SandboxConfig
  config_id?: string
  deep?: boolean
}): Promise<{ data: SandboxCheckResult }> {
  return post('/api/v1/system/sandbox-check', payload) as unknown as Promise<{
    data: SandboxCheckResult
  }>
}

/**
 * The two refusals a save or a delete can hit. They mean opposite things:
 * `sandboxes_still_live` says the backend counted live sandboxes, so the only
 * ways forward are ending the owning sessions or creating a second config;
 * `sandbox_inventory_unverifiable` says the backend is unreachable, so nothing
 * could be counted — the one case a force delete may override.
 */
export type SandboxConflictCode =
  | 'sandboxes_still_live'
  | 'sandbox_inventory_unverifiable'
  | 'skill_snapshot_blocks_template'

export interface SandboxConflict {
  code: SandboxConflictCode
  message?: string
  /** Present for `sandboxes_still_live`; there is nothing to report otherwise. */
  inventory?: SandboxInventory
}

/**
 * Reads a sandbox-config conflict out of a rejected request, or returns null
 * when the failure is anything else.
 *
 * The interceptor spreads the response body onto the rejection, so the code
 * sits at `err.error.code`. Parsing it in one place keeps callers from
 * hard-coding that shape — and from confusing the two conflicts, which drive
 * different recovery paths in the UI.
 */
export function parseSandboxConflict(err: unknown): SandboxConflict | null {
  if (typeof err !== 'object' || err === null) return null
  const detail = (err as { error?: { code?: string; message?: string; data?: SandboxInventory } }).error
  if (!detail || typeof detail !== 'object') return null
  if (
    detail.code !== 'sandboxes_still_live' &&
    detail.code !== 'sandbox_inventory_unverifiable' &&
    detail.code !== 'skill_snapshot_blocks_template'
  ) {
    return null
  }
  return { code: detail.code, message: detail.message, inventory: detail.data }
}

// --- Agent skills installed onto a sandbox config's image ---

export type ConfigSkillStatus = 'installing' | 'ready' | 'failed' | 'removing' | 'removed'

/**
 * One environment variable the skill's installer declared. `is_set` reports
 * whether a workspace-wide value exists; the value itself is never returned,
 * so an editor can show that something is stored but not what.
 */
export interface ConfigSkillEnv {
  name: string
  description?: string
  required?: boolean
  is_set: boolean
}

export interface ConfigSkill {
  id: string
  name: string
  version?: string
  description?: string
  enabled: boolean
  status: ConfigSkillStatus | string
  error?: string
  bundle_sha256?: string
  installed_snapshot_id?: string
  // Locators for this skill's most recent install conversation. Absent for
  // skills installed before transcripts existed, which is how the drawer
  // decides whether to offer the "view install" entry point.
  install_session_id?: string
  install_message_id?: string
  created_at: string
  updated_at: string
  // Absent for a skill whose installer declared nothing, which is how the
  // panel decides whether to offer the environment variable editor at all.
  envs?: ConfigSkillEnv[]
}

export interface ConfigSkillInstallEvent {
  percent: number
  stage: string
  log?: string
  status?: string
  done: boolean
}

export function listConfigSkills(configId: string): Promise<{ data: ConfigSkill[] }> {
  return get(`/api/v1/sandbox-configs/${configId}/skills`) as unknown as Promise<{ data: ConfigSkill[] }>
}

export function uploadConfigSkill(
  configId: string, file: File, onProgress?: (percent: number) => void,
): Promise<{ data: { skill_id: string } }> {
  const form = new FormData()
  form.append('file', file)
  return postUpload(`/api/v1/sandbox-configs/${configId}/skills`, form, (e: any) => {
    if (e.total) onProgress?.(Math.round((e.loaded * 100) / e.total))
  }, { timeout: 5 * 60 * 1000 })
}

export function installConfigSkillFromSource(
  configId: string,
  payload: { source: string },
): Promise<{ data: { skill_id: string } }> {
  return post(`/api/v1/sandbox-configs/${configId}/skills`, payload, {
    timeout: 2 * 60 * 1000,
  }) as unknown as Promise<{ data: { skill_id: string } }>
}

// Retries an install from the archive the server already stores, so a failure
// that had nothing to do with the bundle does not send the operator looking
// for the original zip or registry URL.
export function reinstallConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: { skill_id: string } }> {
  return post(
    `/api/v1/sandbox-configs/${configId}/skills/${skillId}/reinstall`,
    {},
  ) as unknown as Promise<{ data: { skill_id: string } }>
}

// Aborts an in-flight install so retry/uninstall become available.
// After a process restart the row may still say installing with nothing running.
export function stopConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkill }> {
  return post(
    `/api/v1/sandbox-configs/${configId}/skills/${skillId}/stop`,
    {},
  ) as unknown as Promise<{ data: ConfigSkill }>
}

/**
 * Partial update: an absent field is left alone. `envs` names only the
 * variables to write — an entry with an empty string clears the stored value
 * while keeping the declaration, and undeclared names are ignored server-side.
 */
export function patchConfigSkill(
  configId: string,
  skillId: string,
  payload: { enabled?: boolean; envs?: Record<string, string> },
): Promise<{ data: ConfigSkill }> {
  return patch(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`, payload) as unknown as Promise<{
    data: ConfigSkill
  }>
}

export function deleteConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: { skill_id: string } }> {
  return del(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`) as unknown as Promise<{
    data: { skill_id: string }
  }>
}

export function getConfigSkill(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkill }> {
  return get(`/api/v1/sandbox-configs/${configId}/skills/${skillId}`) as unknown as Promise<{
    data: ConfigSkill
  }>
}

export function configSkillInstallEventsUrl(configId: string, skillId: string): string {
  return `/api/v1/sandbox-configs/${configId}/skills/${skillId}/install-events`
}

// The installer agent's own transcript: its prompt, thinking, commands and
// their output, replayed from the start and then followed live. Answers 404
// once the event log has expired, which is the signal to read the durable
// message history instead.
export function configSkillTranscriptUrl(configId: string, skillId: string): string {
  return `/api/v1/sandbox-configs/${configId}/skills/${skillId}/transcript`
}

export interface ConfigSkillFileEntry {
  path: string
  size: number
}

export interface ConfigSkillFileContent {
  path: string
  size: number
  encoding: 'utf-8' | 'base64' | 'binary' | string
  content?: string
  media_type?: string
  truncated?: boolean
  binary?: boolean
}

export function listConfigSkillFiles(
  configId: string,
  skillId: string,
): Promise<{ data: ConfigSkillFileEntry[] }> {
  return get(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/files`) as unknown as Promise<{
    data: ConfigSkillFileEntry[]
  }>
}

export function getConfigSkillFile(
  configId: string,
  skillId: string,
  path: string,
): Promise<{ data: ConfigSkillFileContent }> {
  return get(`/api/v1/sandbox-configs/${configId}/skills/${skillId}/files/content`, {
    params: { path },
  }) as unknown as Promise<{ data: ConfigSkillFileContent }>
}
