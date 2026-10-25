import { onBeforeUnmount, onMounted, ref, type ComputedRef, type Ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'

// Shared draft guard for directory forms and material association dialogs.
export function useDialogDraft(dirty: ComputedRef<boolean>, busy: Ref<boolean>, onClose: () => void) {
  const discardVisible = ref(false)
  let resolveNavigation: ((leave: boolean) => void) | undefined
  function close() { if (busy.value) return; if (dirty.value) discardVisible.value = true; else onClose() }
  function guardNavigation() {
    if (busy.value) return false
    if (!dirty.value) return true
    discardVisible.value = true
    return new Promise<boolean>(resolve => { resolveNavigation?.(false); resolveNavigation = resolve })
  }
  function cancelDiscard() { discardVisible.value = false; resolveNavigation?.(false); resolveNavigation = undefined }
  function discardChanges() { discardVisible.value = false; resolveNavigation?.(true); resolveNavigation = undefined; onClose() }
  function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
  onBeforeRouteLeave(guardNavigation)
  onBeforeRouteUpdate(guardNavigation)
  onMounted(() => window.addEventListener('beforeunload', beforeUnload))
  onBeforeUnmount(() => { window.removeEventListener('beforeunload', beforeUnload); resolveNavigation?.(false) })
  return { discardVisible, close, cancelDiscard, discardChanges }
}
