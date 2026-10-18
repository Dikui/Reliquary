import { get, post } from '../utils/request'

export interface VaultLibraryStatus {
  library_id: string
  name: string
  system_role: string
  directory: string
  last_scan_at?: string | null
  archived_entries: number
  last_error?: string
}

export interface VaultStatus {
  root: string
  libraries: VaultLibraryStatus[]
}

export function getVaultStatus() {
  return get<{ success: boolean; data: VaultStatus }>('/api/v1/vault/status')
}

export interface VaultFileRebuildResult {
  rebuilt: boolean
  written: number
  skipped: number
  failed: number
}

export interface VaultFileRebuildStatus {
  status: 'idle' | 'running' | 'success' | 'failed'
  result?: VaultFileRebuildResult
  error?: string
}

export function rebuildVaultFiles() {
  return post<{ success: boolean; data: VaultFileRebuildStatus }>('/api/v1/vault/rebuild')
}

export function getVaultFileRebuildStatus() {
  return get<{ success: boolean; data: VaultFileRebuildStatus }>('/api/v1/vault/rebuild')
}
