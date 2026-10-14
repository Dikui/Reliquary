import { ref, watch } from 'vue'
import { readLibraryScope, saveLibraryScope } from '@/utils/libraryScope'
export function useLibraryScope() {
  const libraryScope = ref(readLibraryScope())
  watch(libraryScope, saveLibraryScope, { deep: true })
  return libraryScope
}
