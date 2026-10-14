export function formatContent(value: unknown): string {
  if (typeof value !== 'string') return JSON.stringify(value, null, 2) ?? ''
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
export interface ContentBlock { role: string; text: string; body?: string }
export function contentBlocks(raw: string): ContentBlock[] {
  let data: any
  try { data = JSON.parse(raw) } catch { return [] }
  const messages = data.messages ?? (Array.isArray(data.input) ? data.input : data.choices?.map((c: any) => c.message)) ?? data.output
  if (!Array.isArray(messages)) return []
  const result: ContentBlock[] = []
  if (data.instructions) result.push({ role: 'system', text: formatContent(data.instructions) })
  for (const message of messages) {
    const content = message.content ?? message.text ?? message
    const parts = Array.isArray(content) ? content : [{ text: content }]
    for (const part of parts) {
      const text = typeof part === 'string' ? part : part.text ?? part
      let structured: any
      if (typeof text === 'string') { try { structured = JSON.parse(text) } catch { /* Plain prompt. */ } }
      else structured = text
      if (structured && typeof structured === 'object' && typeof structured.body === 'string') {
        const { body, ...metadata } = structured
        result.push({ role: message.role ?? message.type ?? 'message', text: formatContent(metadata), body })
      } else result.push({ role: message.role ?? message.type ?? 'message', text: formatContent(text) })
    }
  }
  return result
}
export const contentCharacters = (text: string) => [...text].length
