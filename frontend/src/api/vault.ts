import { get, post } from '../utils/request'

export interface VaultDirectoryStatus {
  directory?: string
  last_scan_at?: string | null
  indexed_files: number
  last_error?: string
}

export interface VaultStatus {
  root: string
  mine: VaultDirectoryStatus
  reports: VaultDirectoryStatus
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
