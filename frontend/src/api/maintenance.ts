import { get } from '../utils/request'

export type MaintenanceResult = 'never' | 'running' | 'success' | 'failed' | 'skipped'

export interface MaintenanceStatus {
  paused: boolean
  pause_mode: string
  pause_reason: string
  backup_at?: string | null
  backup_status: MaintenanceResult
  backup_path: string
  backup_size_bytes: number
  backup_error: string
  drill_at?: string | null
  drill_status: MaintenanceResult
  drill_error: string
  backup_schedule: string
  drill_schedule: string
  schedule_timezone: string
  backup_directory: string
  automatic_enabled: boolean
}

export function getMaintenanceStatus() {
  return get<{ success: boolean; data: MaintenanceStatus }>('/api/v1/maintenance/status')
}
