<script setup lang="ts" generic="T extends object">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Checkbox from './Checkbox.vue'

const props = withDefaults(defineProps<{
  rows: T[]
  columns: { key: string; label: string }[]
  rowKey?: string
  label: string
  selectable?: boolean
  selected?: string[]
  focused?: string
}>(), { rowKey: 'id', selected: () => [] })
const emit = defineEmits<{ 'update:selected': [keys: string[]]; 'update:focused': [key: string]; activate: [row: T] }>()
const { t } = useI18n()
let anchor = ''
const shiftClick = ref(false)
const keyOf = (row: T) => String(row[props.rowKey as keyof T])
const selectedKeys = computed(() => new Set(props.selected))
const allChecked = computed(() => props.rows.length > 0 && props.rows.every(row => selectedKeys.value.has(keyOf(row))))
const partiallyChecked = computed(() => !allChecked.value && props.rows.some(row => selectedKeys.value.has(keyOf(row))))
const hasFocusedRow = computed(() => props.rows.some(row => keyOf(row) === props.focused))

function selectRow(key: string, checked: boolean) {
  const keys = new Set(props.selected)
  const from = props.rows.findIndex(row => keyOf(row) === anchor)
  const to = props.rows.findIndex(row => keyOf(row) === key)
  const affected = shiftClick.value && from >= 0 && to >= 0 ? props.rows.slice(Math.min(from, to), Math.max(from, to) + 1).map(keyOf) : [key]
  for (const id of affected) { if (checked) keys.add(id); else keys.delete(id) }
  anchor = key
  shiftClick.value = false
  emit('update:selected', [...keys])
}
function selectPage(checked: boolean) {
  const keys = new Set(props.selected)
  for (const row of props.rows) {
    if (checked) keys.add(keyOf(row))
    else keys.delete(keyOf(row))
  }
  emit('update:selected', [...keys])
}
function moveFocus(event: KeyboardEvent, index: number) {
  if (event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.target !== event.currentTarget) return
  if (event.key === 'Enter') { event.preventDefault(); emit('activate', props.rows[index]!); return }
  if (!['j', 'k', 'J', 'K', 'ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? props.rows.length - 1 : Math.max(0, Math.min(props.rows.length - 1, index + (['j', 'J', 'ArrowDown'].includes(event.key) ? 1 : -1)))
  const row = props.rows[target]
  if (!row) return
  emit('update:focused', keyOf(row))
  const tbody = (event.currentTarget as HTMLElement).parentElement
  ;(tbody?.children[target] as HTMLElement | undefined)?.focus()
}
</script>

<template>
  <div class="rq-table-wrap" tabindex="0" role="region" :aria-label="label">
    <table class="rq-table">
      <caption class="rq-sr-only">{{ label }}</caption>
      <thead><tr>
        <th v-if="selectable" scope="col" class="rq-table__check"><Checkbox :model-value="allChecked" :indeterminate="partiallyChecked" :disabled="rows.length === 0" :aria-label="t('ui.selectPage')" @update:model-value="selectPage" /></th>
        <th v-for="column in columns" :key="column.key" scope="col">{{ column.label }}</th>
      </tr></thead>
      <tbody>
        <tr v-for="(row, index) in rows" :key="keyOf(row)" :data-row-key="keyOf(row)" :data-select-id="keyOf(row)" class="rq-table__row"
          :class="{ 'rq-table__row--selected': selectedKeys.has(keyOf(row)), 'rq-table__row--focused': focused === keyOf(row) }"
          :tabindex="focused === keyOf(row) || (!hasFocusedRow && index === 0) ? 0 : -1"
          :aria-current="focused === keyOf(row) ? 'true' : undefined"
          @focusin="emit('update:focused', keyOf(row))" @keydown="moveFocus($event, index)">
          <td v-if="selectable" class="rq-table__check" @click.capture="shiftClick = $event.shiftKey"><Checkbox :model-value="selectedKeys.has(keyOf(row))" :aria-label="t('ui.selectRow', { label: keyOf(row) })" @update:model-value="selectRow(keyOf(row), $event)" /></td>
          <td v-for="column in columns" :key="column.key"><slot :name="`cell-${column.key}`" :row="row" :value="row[column.key as keyof T]">{{ row[column.key as keyof T] }}</slot></td>
        </tr>
        <tr v-if="rows.length === 0"><td :colspan="columns.length + (selectable ? 1 : 0)"><slot name="empty">{{ t('ui.noRows') }}</slot></td></tr>
      </tbody>
    </table>
  </div>
</template>
