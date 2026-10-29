import type { LibraryColor } from '../api/library'

export function libraryColorVar(color: LibraryColor): string {
  return `var(--lib-${color})`
}
