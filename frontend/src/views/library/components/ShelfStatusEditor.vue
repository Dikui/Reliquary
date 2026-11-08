<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { shelfConditionText } from '@/utils/productShelf'
import { Button, Dialog } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import { SALE_STATUSES, submitShelfStatuses, type ShelfChangeset, type ShelfQueryItem, type ShelfSaleStatus, type ShelfStatusOperation } from '@/api/shelf'

const props = defineProps<{ library: LibraryCatalogItem; models: ShelfQueryItem[]; region: string; bulk?: boolean }>()
const emit = defineEmits<{ close: []; saved: [changeset: ShelfChangeset] }>()
const { t } = useI18n()
const region = ref(props.region), busy = ref(false), error = ref('')
const model = props.models[0]!
const existing = (model.shelf?.sale_statuses || []).filter(status => status.region === props.region)
const drafts = ref<ShelfSaleStatus[]>(JSON.parse(JSON.stringify(!props.bulk && existing.length ? existing : [{ region: props.region, status: '', planned_eom: false, notes: '', raw: '', conditions: [] }])))
const regionName = computed(() => props.library.shelf_config?.regions?.find(item => item.key === region.value)?.name || region.value)
function conditionLabel(record: ShelfSaleStatus) {
  return shelfConditionText(record.conditions, props.library.shelf_config?.module_types, model.shelf?.composition) || t('shelfStatus.allOptions')
}
function operations(): ShelfStatusOperation[] {
  return props.models.map((item, index) => {
    const previous = item.shelf?.sale_statuses || []
    let next = 0
    const replacements = drafts.value.filter(record => record.status !== 'no_record')
    const records = previous.flatMap(record => {
      if (record.region !== region.value) return [record]
      const replacement = props.bulk ? { ...record, status: drafts.value[0]!.status, planned_eom: drafts.value[0]!.planned_eom, notes: drafts.value[0]!.notes } : drafts.value[next++]!
      return replacement.status === 'no_record' ? [] : [replacement]
    })
    if (!previous.some(record => record.region === region.value)) records.push(...replacements)
    return { id: `status-${index}`, type: 'set_sale_statuses', entity_id: item.entity.id, revision: item.entity.revision!, sale_statuses: records }
  })
}
async function save() {
  if (busy.value) return
  if (!region.value || drafts.value.some(record => !record.status)) { error.value = t('shelfStatus.chooseStatus'); return }
  busy.value = true; error.value = ''
  try {
    const response = await submitShelfStatuses(props.library.id, operations(), !props.bulk)
    if (!response.success) throw new Error(response.message || t('common.operationFailed'))
    emit('saved', response.data)
  } catch (cause: any) {
    const conflicts = cause?.error?.details?.items?.flatMap((item: any) => item.conflicts?.map((conflict: any) => conflict.message || conflict.type) || []) || []
    error.value = [cause?.message || t('common.operationFailed'), ...conflicts].join(' · ')
  } finally { busy.value = false }
}
</script>
<template>
  <Dialog :model-value="true" :title="bulk ? t('shelfStatus.bulkEdit') : t('shelfStatus.edit')" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" @update:model-value="!busy && emit('close')">
    <div class="status-editor">
      <p v-if="!bulk"><strong>{{ model.entity.canonical_name }}</strong> · {{ regionName }}</p>
      <template v-else><p>{{ t('shelfStatus.selectedCount', { count: models.length }) }}</p><ul><li v-for="item in models" :key="item.entity.id">{{ item.entity.canonical_name }}</li></ul><label>{{ t('shelfStatus.region') }}<select :value="region" :disabled="busy" :aria-label="t('shelfStatus.region')" @change="region = ($event.target as HTMLSelectElement).value; drafts[0]!.region = region"><option v-for="item in library.shelf_config?.regions" :key="item.key" :value="item.key">{{ item.name }}</option></select></label><p>{{ t('shelfStatus.bulkHint') }}</p></template>
      <fieldset v-for="(record, index) in drafts" :key="index" :disabled="busy">
        <legend>{{ bulk ? t('shelfStatus.allOptions') : conditionLabel(record) }}</legend>
        <label>{{ t('shelfStatus.state') }}<select :value="record.status" :aria-label="`${t('shelfStatus.state')} ${index + 1}`" @change="record.status = ($event.target as HTMLSelectElement).value"><option value="" disabled>{{ t('shelfStatus.chooseStatus') }}</option><option v-for="status in SALE_STATUSES" :key="status" :value="status">{{ status }}</option><option value="no_record">{{ t('shelfStatus.noRecord') }}</option></select></label>
        <label class="check"><input type="checkbox" :checked="record.planned_eom" :disabled="record.status === 'no_record'" @change="record.planned_eom = ($event.target as HTMLInputElement).checked">{{ t('shelfStatus.planned') }}</label>
        <label>{{ t('shelfStatus.notes') }}<textarea :value="record.notes" :disabled="record.status === 'no_record'" @input="record.notes = ($event.target as HTMLTextAreaElement).value" /></label>
        <p v-if="record.raw" class="raw">{{ t('shelfStatus.raw') }}：{{ record.raw }}</p>
      </fieldset>
      <p v-if="error" role="alert">{{ error }}</p>
    </div>
    <template #footer><Button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="busy" @click="save">{{ bulk ? t('shelfStatus.saveDraft') : t('shelfStatus.confirm') }}</Button></template>
  </Dialog>
</template>
<style scoped>
.status-editor { display: grid; gap: 16px; } p { margin: 0; } fieldset { display: grid; gap: 12px; border: 1px solid var(--border); padding: 12px; }
legend { font-size: var(--text-xs); font-weight: 700; } label { display: grid; gap: 8px; font-weight: 700; font-size: var(--text-sm); }
select, textarea { width: 100%; border: 1px solid var(--border); background: var(--surface); color: var(--text); padding: 8px; font: inherit; } textarea { min-height: 70px; resize: vertical; }
.check { display: flex; align-items: center; } .raw { font-size: var(--text-xs); color: var(--muted); } [role=alert] { color: var(--danger); }
</style>
