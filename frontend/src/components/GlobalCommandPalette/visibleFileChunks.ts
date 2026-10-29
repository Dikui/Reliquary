import type { CmdkFileGroup } from './useSearch'

export function visibleFileChunks(files: CmdkFileGroup[], limit: number) {
  return files.flatMap(file => file.chunks[0] ? [{ file, chunk: file.chunks[0] }] : []).slice(0, limit)
}
