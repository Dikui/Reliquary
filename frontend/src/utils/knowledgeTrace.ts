/** Whether GET /knowledge/:id/spans returned a real trace (not legacy placeholder-only). */
export function knowledgeSpansPayloadHasTrace(
  data: { trace?: { span_id?: string }; current_attempt?: number } | null | undefined,
): boolean {
  if (!data?.trace) return false
  return !!(data.trace.span_id || (data.current_attempt ?? 0) > 0)
}

export interface KnowledgeTraceNode {
  span_id?: string
  parent_span_id?: string
  name: string
  kind: string
  status: string
  started_at?: string | null
  finished_at?: string | null
  duration_ms?: number
  error_code?: string
  error_message?: string
  input?: unknown
  output?: unknown
  metadata?: unknown
  children?: KnowledgeTraceNode[]
}

export interface PostprocessTaskSummary {
  running: number
  failed: number
  completed: number
  other: number
  total: number
}

/**
 * Counts leaf postprocess spans so the UI can distinguish the five main
 * pipeline stages from asynchronous enrichment work.
 */
export function summarizePostprocessTasks(
  trace?: KnowledgeTraceNode,
): PostprocessTaskSummary {
  const summary: PostprocessTaskSummary = {
    running: 0,
    failed: 0,
    completed: 0,
    other: 0,
    total: 0,
  }
  if (!trace) return summary

  const postprocess = trace.name === 'postprocess'
    ? trace
    : (trace.children || []).find(child => child.name === 'postprocess')
  if (!postprocess) return summary

  const countLeaves = (node: KnowledgeTraceNode) => {
    const children = node.children || []
    if (children.length > 0) {
      children.forEach(countLeaves)
      return
    }

    summary.total++
    switch (node.status) {
      case 'running':
      case 'pending':
      case 'processing':
      case 'finalizing':
        summary.running++
        break
      case 'failed':
        summary.failed++
        break
      case 'done':
      case 'completed':
        summary.completed++
        break
      default:
        summary.other++
    }
  }

  ;(postprocess.children || []).forEach(countLeaves)
  return summary
}
