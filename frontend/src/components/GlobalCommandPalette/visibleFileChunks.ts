import type { CmdkFileGroup } from './useSearch'

export function visibleFileChunks(files: CmdkFileGroup[], limit: number) {
  return files.flatMap(file => {
    const chunk = file.chunks.find(chunk => chunk.id.trim() && Number.isInteger(chunk.chunk_index)
      && chunk.chunk_index >= 0 && (chunk.locations?.length || 0) > 0)
    return chunk ? [{ file, chunk }] : []
  }).slice(0, limit)
}
