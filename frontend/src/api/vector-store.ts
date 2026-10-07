import { get } from '@/utils/request'

export interface VectorStoreEntity {
  id: string
  name: string
  engine_type: 'postgres'
  source: 'env'
  readonly: true
  status: 'available' | 'unavailable'
  connection_config?: {
    version?: string
  }
}

export function listVectorStores(): Promise<{ success: boolean; data: VectorStoreEntity[] }> {
  return get('/api/v1/vector-stores')
}
