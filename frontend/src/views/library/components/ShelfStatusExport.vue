<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { PopoverRoot, PopoverTrigger, PopoverPortal, PopoverContent } from 'reka-ui'
import { Button } from '@/components/ui'
import { exportShelf, type ShelfQuery } from '@/api/shelf'

const props = defineProps<{ libraryId: string; productLineId: string; productLineName: string; query: ShelfQuery }>()
const { t } = useI18n()
const open = ref(false), scope = ref('current'), applyFilters = ref(true)
const busy = ref(false), error = ref(''), downloaded = ref(false)
let controller: AbortController | undefined

function cancel() {
  controller?.abort(); controller = undefined; busy.value = false; open.value = false
}
async function download() {
  if (busy.value) return
  const request = new AbortController()
  controller = request; busy.value = true; error.value = ''; downloaded.value = false
  try {
    const blob = await exportShelf(props.libraryId, {
      ...(scope.value === 'current' ? { product_line_id: props.productLineId } : {}),
      query: applyFilters.value ? props.query : {},
    }, request.signal)
    if (request.signal.aborted) return
    const url = URL.createObjectURL(blob)
    try {
      const link = document.createElement('a')
      link.href = url; link.download = 'product-shelf.xlsx'; link.click()
    } finally { URL.revokeObjectURL(url) }
    downloaded.value = true; open.value = false
  } catch {
    if (!request.signal.aborted) error.value = t('shelfStatus.exportFailed')
  } finally {
    if (controller === request) { busy.value = false; controller = undefined }
  }
}
watch(() => props.libraryId, () => { cancel(); downloaded.value = false; error.value = ''; scope.value = 'current'; applyFilters.value = true })
onBeforeUnmount(cancel)
</script>

<template>
  <div class="shelf-export">
    <PopoverRoot v-model:open="open">
      <PopoverTrigger as-child><Button :disabled="!productLineId" :loading="busy">{{ t('shelfStatus.exportExcel') }}</Button></PopoverTrigger>
      <PopoverPortal>
        <PopoverContent v-if="open" class="rq-menu shelf-export-panel" align="end" :side-offset="4" :aria-label="t('shelfStatus.exportExcel')" @escape-key-down="cancel" @interact-outside="busy && $event.preventDefault()">
          <strong>{{ t('shelfStatus.exportExcel') }}</strong>
          <label><input type="radio" name="shelf-export-scope" value="current" :checked="scope === 'current'" :disabled="busy" @change="scope = 'current'">{{ t('shelfStatus.exportCurrent', { name: productLineName }) }}</label>
          <label><input type="radio" name="shelf-export-scope" value="all" :checked="scope === 'all'" :disabled="busy" @change="scope = 'all'">{{ t('shelfStatus.exportAll') }}</label>
          <label><input type="checkbox" :checked="applyFilters" :disabled="busy" @change="applyFilters = ($event.target as HTMLInputElement).checked">{{ t('shelfStatus.exportFilters') }}</label>
          <p v-if="busy" role="status">{{ t('shelfStatus.exportPreparing') }}</p>
          <p v-if="error" role="alert">{{ error }}</p>
          <div class="shelf-export-actions"><Button variant="primary" :loading="busy" @click="download">{{ t('shelfStatus.export') }}</Button><Button variant="ghost" @click="cancel">{{ t('common.cancel') }}</Button></div>
        </PopoverContent>
      </PopoverPortal>
    </PopoverRoot>
    <span v-if="downloaded" class="shelf-export-feedback" role="status">{{ t('shelfStatus.exportStarted') }}</span>
  </div>
</template>

<style scoped>
.shelf-export { display: flex; align-items: center; gap: var(--space-2); }
.shelf-export-panel { display: flex; flex-direction: column; gap: var(--space-3); width: 320px; padding: var(--space-4); }
.shelf-export-panel label { display: flex; align-items: baseline; gap: var(--space-2); overflow-wrap: anywhere; }
.shelf-export-panel input { accent-color: var(--text); }
.shelf-export-panel p { margin: 0; font-size: var(--text-sm); }
.shelf-export-panel [role=alert] { color: var(--danger); }
.shelf-export-actions { display: flex; gap: var(--space-2); }
.shelf-export-feedback { font-size: var(--text-xs); color: var(--muted); }
</style>
