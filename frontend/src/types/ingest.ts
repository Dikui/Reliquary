export type Acquisition = 'verbatim' | 'extracted' | 'model_written'

export interface MissingMaterial {
  type: 'image'
  source_url: string
  reason: string
  api_key_id?: number
  api_key_name?: string
  submitted_at?: string
}

export interface IncompleteArticle {
  article_id: string
  library_id: string
  library_template: string
  url: string
  title: string
  acquisition: Acquisition | ''
  missing: MissingMaterial[]
  external_completion_attempted: boolean
}
