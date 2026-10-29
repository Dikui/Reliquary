<script setup lang="ts">
import { contentTypeLabel } from '@/utils/contentTypeLabel'
import { formatDateTime } from '@/utils/dateTime'
import EntryUpdateNotice from '@/components/EntryUpdateNotice.vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { DataTable, Tag, StatusSquare } from '@/components/ui'
import { entryProcessingState } from '@/utils/entryProcessingStatus'
import { entryChannelLabel, entryFileFormatLabel } from '@/utils/entrySourceLabels'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import type { ClassifiedEntry } from '@/types/entryClassification'

interface EntryItem extends ClassifiedEntry {
  id: string
  title?: string
  file_name?: string
  display_name?: string
  parse_status?: string
  created_at?: string
  file_type?: string
  channel?: string
  entity_count?: number
  relation_count?: number
}
const props = withDefaults(defineProps<{
  items: EntryItem[]
  mode?: 'list' | 'table'
  focused?: string
  selectedIds: Set<string>
}>(), { mode: 'list' })
const emit = defineEmits<{
  open: [item: EntryItem]
  'update:focused': [id: string]
  'update:selected': [ids: string[]]
}>()
const { t, locale } = useI18n()
const columns = computed(() => ['id', 'title', 'summary', 'type', 'entities', 'relations', 'status', 'created'].map(key => ({ key, label: t(`entries.columns.${key}`) })))
const title = (item: EntryItem) => item.display_name || item.title || item.file_name || t('library.untitledDocument')
const summary = (item: EntryItem) => markdownToPlainText(item.structured_summary?.overview || item.description || '', `[${t('ingestStatus.missingTypes.image')}]`)
const date = (value?: string) => formatDateTime(value, locale.value, { year: 'numeric', month: '2-digit', day: '2-digit' }, t('entries.unknown'))
const tone = (item: EntryItem) => { const state = entryProcessingState(item); return state.failed ? 'danger' : state.active ? 'warning' : state.status === 'completed' ? 'success' : 'neutral' }
function keydown(event: KeyboardEvent, item: EntryItem) {
  if (event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.target !== event.currentTarget) return
  const active = props.items.find(row => row.id === props.focused) || item
  if (event.key === 'Enter') { event.preventDefault(); emit('open', active); return }
  if (!['j', 'k', 'J', 'K', 'ArrowDown', 'ArrowUp'].includes(event.key)) return
  event.preventDefault()
  const index = props.items.findIndex(row => row.id === active.id)
  const next = props.items[Math.max(0, Math.min(props.items.length - 1, index + (['j', 'J', 'ArrowDown'].includes(event.key) ? 1 : -1)))]
  if (next) {
    emit('update:focused', next.id)
    const current = event.currentTarget as HTMLElement
    const row = current.closest?.('.entries-list')?.querySelector<HTMLElement>(`[data-select-id="${CSS.escape(next.id)}"]`)
    row?.querySelector('button')?.focus()
  }
}
</script>

<template>
  <ul v-if="mode === 'list'" class="entries-list" :aria-label="t('entries.list')">
    <li v-for="(item, index) in items" :key="item.id" class="entries-list__row" :data-select-id="item.id"
      :class="{ 'rq-focus-row': focused === item.id }" :aria-current="focused === item.id ? 'true' : undefined"
      @mouseenter="emit('update:focused', item.id)" @focusin="emit('update:focused', item.id)">
      <button type="button" class="entries-list__select" :tabindex="focused === item.id || (!focused && index === 0) ? 0 : -1"
        @click="emit('update:focused', item.id)" @keydown="keydown($event, item)">
        <span class="entries-id" :title="item.id">{{ item.id }}</span>
        <strong class="entries-list__title">{{ title(item) }}</strong>
        <span v-if="summary(item)" class="entries-list__summary">{{ summary(item) }}</span>
        <Tag v-if="item.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag>
        <Tag v-if="item.cannot_be_fact || item.source_kind === 'report'">{{ t('ingestStatus.cannotBeFact') }}</Tag>
        <span class="entries-source">{{ entryFileFormatLabel(item, t) }} · {{ entryChannelLabel(item, t) }}</span>
        <span v-if="item.source_unverified" class="entries-source">{{ t('review.sourceUnverified') }}</span>
        <span class="entries-list__meta"><Tag v-if="item.content_type">{{ contentTypeLabel(item.content_type, t) }}</Tag><time>{{ date(item.created_at) }}</time>
          <span v-if="item.entity_count != null">{{ t('entries.entityCount', { count: item.entity_count }) }}</span>
          <span v-if="item.relation_count != null">{{ t('entries.relationCount', { count: item.relation_count }) }}</span>
          <StatusSquare :status="tone(item)" :label="t(entryProcessingState(item).label)" />
        </span>
      </button>
      <EntryUpdateNotice :has-update="item.has_update" :latest-entry-id="item.latest_entry_id" />
      <button type="button" class="entries-mobile-open" @click.stop="emit('open', item)">{{ t('entries.open') }}</button>
    </li>
  </ul>
  <DataTable v-else :rows="items" :columns="columns" :label="t('entries.table')" selectable :selected="[...selectedIds]" :focused="focused"
    @activate="emit('open', $event)" @update:selected="emit('update:selected', $event)" @update:focused="emit('update:focused', $event)">
    <template #cell-id="{ row }"><span class="entries-id" :title="row.id">{{ row.id }}</span></template>
    <template #cell-title="{ row }"><button type="button" class="entries-title-link" @click="emit('open', row)">{{ title(row) }}</button><EntryUpdateNotice :has-update="row.has_update" :latest-entry-id="row.latest_entry_id" /><small v-if="row.source_unverified">{{ t('review.sourceUnverified') }}</small><Tag v-if="row.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag><Tag v-if="row.cannot_be_fact || row.source_kind === 'report'">{{ t('ingestStatus.cannotBeFact') }}</Tag></template>
    <template #cell-summary="{ row }"><span class="entries-table-summary">{{ summary(row) }}</span></template>
    <template #cell-type="{ row }"><Tag v-if="row.content_type">{{ contentTypeLabel(row.content_type, t) }}</Tag></template>
    <template #cell-entities="{ row }">{{ row.entity_count ?? t('entries.unknown') }}</template>
    <template #cell-relations="{ row }">{{ row.relation_count ?? t('entries.unknown') }}</template>
    <template #cell-status="{ row }"><StatusSquare :status="tone(row)" :label="t(entryProcessingState(row).label)" /></template>
    <template #cell-created="{ row }"><time>{{ date(row.created_at) }}</time></template>
  </DataTable>
</template>

<style scoped>
.entries-list { list-style: none; margin: 0; padding: 0; border-right: 2px solid var(--rule); }
.entries-list__row { display: grid; gap: 4px; padding: 16px 24px; border-bottom: 1px solid var(--border); cursor: pointer; overflow-wrap: anywhere; }
.entries-list__row:focus-within { outline: 2px solid var(--accent); outline-offset: -2px; }
.entries-id { font-family: var(--mono); font-size: var(--text-xs); display: inline-block; max-width: 14ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: middle; }
.entries-list__select { display: grid; gap: 4px; padding: 0; border: 0; background: transparent; font: inherit; color: inherit; text-align: left; cursor: pointer; min-width: 0; }
.entries-list__title { font-size: var(--text-md); line-height: 1.4; }
.entries-list__summary { font-size: var(--text-sm); display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.entries-list__meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: var(--text-xs); }
.entries-source { font-size: var(--text-xs); }
.entries-list__meta :deep(.rq-status) { margin-left: auto; }
.entries-title-link, .entries-mobile-open { font: inherit; font-weight: 700; color: inherit; background: none; border: 0; padding: 0; text-decoration: underline; text-align: left; cursor: pointer; }
.entries-title-link { display: block; min-width: 266px; overflow-wrap: break-word; word-break: normal; }
.entries-title-link:hover { color: var(--accent); }
.entries-table-summary { display: block; min-width: 240px; max-width: 440px; white-space: normal; }
.entries-mobile-open { display: none; }
@media (max-width: 900px) { .entries-list { border-right: 0; } .entries-mobile-open { display: block; justify-self: start; } }
</style>
