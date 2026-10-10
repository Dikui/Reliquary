import { get } from '../utils/request'

export interface VaultDirectoryStatus {
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
