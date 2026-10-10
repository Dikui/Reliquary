import { get, post, put, del } from '../../utils/request'

// --- Types ---

export interface DataSource {
  id: string
  tenant_id: number
  knowledge_base_id: string
  name: string
  type: string
  config: any
  sync_schedule: string
  sync_mode: 'incremental' | 'full'
  status: 'active' | 'paused' | 'error'
  sync_deletions: boolean
  last_sync_at: string | null
  last_sync_result: any
  error_message: string
  // Single-field "credentials" map from the main response — DataSource
  // credentials are a per-connector atomic set.
  credentials?: { credentials: { configured: boolean } }
  created_at: string
  updated_at: string
  latest_sync_log?: SyncLog
}

/**
 * One user-facing failure sample. The backend sends a stable i18n `code`
 * (+ interpolation `params`) so the UI localises it to the user's language;
 * `message` is a fallback when no i18n key exists (old logs decode into it).
 */
export interface SyncItemError {
  title?: string
  code?: string
  params?: Record<string, string>
  message?: string
}

export interface SyncResultDetail {
  metrics?: Record<string, string | number>
  total?: number
  created?: number
  updated?: number
  deleted?: number
  skipped?: number
  failed?: number
  /** Per-item failure samples (capped); localised in the sync-log drawer. */
  errors?: SyncItemError[]
}

export interface SyncLog {
  id: string
  data_source_id: string
  status: 'running' | 'success' | 'partial' | 'failed' | 'canceled'
  started_at: string
  finished_at: string | null
  items_total: number
  items_created: number
  items_updated: number
  items_deleted: number
  items_skipped: number
  items_failed: number
  error_message: string
  result?: SyncResultDetail
}

export interface ConnectorMeta {
  type: string
  name: string
  description: string
  icon: string
  priority: number
  auth_type: string
  capabilities: string[]
}

export interface Resource {
  external_id: string
  name: string
  type: string
  description: string
  url: string
  parent_id?: string
  has_children?: boolean
}

// --- API calls ---

export function getConnectorTypes() {
  return get('/api/v1/data-sources/types')
}

export function listDataSources() {
  return get('/api/v1/data-sources')
}

export function getDataSource(id: string) {
  return get(`/api/v1/data-sources/${id}`)
}

export function createDataSource(data: Partial<DataSource>) {
  return post('/api/v1/data-sources', data)
}

export function updateDataSource(id: string, data: Partial<DataSource>) {
  return put(`/api/v1/data-sources/${id}`, data)
}

export function deleteDataSource(id: string) {
  return del(`/api/v1/data-sources/${id}`)
}

export function validateConnection(id: string) {
  return post(`/api/v1/data-sources/${id}/validate`, {})
}

// Validate credentials without persisting (for "Test Connection" during creation)
export function validateCredentials(type: string, credentials: Record<string, any>) {
  return post('/api/v1/data-sources/validate-credentials', { type, credentials })
}

// List selectable RSS feed resources.
export function listResources(id: string, parentId?: string) {
  const query = parentId ? `?parent_id=${encodeURIComponent(parentId)}` : ''
  return get(`/api/v1/data-sources/${id}/resources${query}`, { timeout: 120000 })
}

// resolveResourceAncestors returns the ExternalIDs of every parent that must be
// expanded to reveal the given (possibly deeply nested) selections in a lazily
// loaded picker. Used when editing a data source to restore an existing selection.
export function resolveResourceAncestors(id: string, resourceIds: string[]) {
  return post(`/api/v1/data-sources/${id}/resource-ancestors`, { resource_ids: resourceIds }, { timeout: 120000 })
}

export function triggerSync(id: string) {
  return post(`/api/v1/data-sources/${id}/sync`, {})
}

export function pauseDataSource(id: string) {
  return post(`/api/v1/data-sources/${id}/pause`, {})
}

export function resumeDataSource(id: string) {
  return post(`/api/v1/data-sources/${id}/resume`, {})
}

export function getSyncLogs(id: string, limit = 20, offset = 0) {
  return get(`/api/v1/data-sources/${id}/logs?limit=${limit}&offset=${offset}`)
}

// ----------------------------------------------------------------------------
// Data source credential subresource. Unlike the other three resources,
// DataSource exposes a single logical field "credentials" because connector
// auth is a per-connector atomic map. See internal/handler/dto/datasource.go.
// ----------------------------------------------------------------------------

export interface DataSourceCredentialsResponse {
  fields: {
    credentials: { configured: boolean }
  }
}

export async function putDataSourceCredentials(
  id: string,
  credentials: Record<string, unknown>,
): Promise<DataSourceCredentialsResponse> {
  const response: any = await put(`/api/v1/data-sources/${id}/credentials`, { credentials })
  return (response.data ?? response) as DataSourceCredentialsResponse
}

export async function deleteDataSourceCredentials(id: string): Promise<void> {
  await del(`/api/v1/data-sources/${id}/credentials/credentials`)
}
