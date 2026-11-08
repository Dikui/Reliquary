<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { shelfConditionText } from '@/utils/productShelf'
import ShelfStatusEditor from './ShelfStatusEditor.vue'
import { Button, EmptyState, ErrorState, Tabs } from '@/components/ui'
import type { Entity } from '@/api/entities'
import { listEntities } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import { SALE_STATUSES, findShelf, getShelfNode, queryShelf, type ShelfNode, type ShelfQuery, type ShelfQueryItem, type ShelfResponse, type ShelfChangeset } from '@/api/shelf'

const props = defineProps<{ library: LibraryCatalogItem }>()
const { t } = useI18n(), router = useRouter()
const lines = ref<Entity[]>([]), line = ref(''), rows = ref<ShelfQueryItem[]>([])
const nodes = ref<Record<string, ShelfNode>>({}), counts = ref<Record<string, number>>({})
const loading = ref(false), error = ref(''), total = ref(0)
const collapsed = ref(new Set<string>())
const selected = ref(new Set<string>()), editor = ref<{ models: ShelfQueryItem[]; region: string; bulk: boolean }>()
const savedDraft = ref('')
const queryText = ref(''), composing = ref(false), searching = ref(false), searchError = ref(''), searchTotal = ref<number>(), matches = ref<(Entity & { kind_label?: string })[]>([])
let searchGeneration = 0
function resetSearch() { searchGeneration++; matches.value = []; searchTotal.value = undefined; searchError.value = ''; searching.value = false }
async function search(more = false) {
  if (composing.value || !queryText.value.trim()) return
  const current = ++searchGeneration, libraryID = props.library.id, text = queryText.value.trim()
  searching.value = true; searchError.value = ''
  if (!more) { matches.value = []; searchTotal.value = undefined }
  try {
    const page = unwrap(await findShelf(libraryID, text, more ? matches.value.length : 0))
    if (current === searchGeneration) { matches.value = more ? [...matches.value, ...page.items] : page.items; searchTotal.value = page.total }
  } catch (cause) { if (current === searchGeneration) searchError.value = cause instanceof Error ? cause.message : t('workbench.requestFailed') }
  finally { if (current === searchGeneration) searching.value = false }
}
function editCell(row: ShelfQueryItem, region: string) { editor.value = { models: [row], region, bulk: false } }
function editBulk() { editor.value = { models: rows.value.filter(row => selected.value.has(row.entity.id)), region: regions.value[0]?.key || props.library.shelf_config?.regions?.[0]?.key || '', bulk: true } }
async function saved(changeset: ShelfChangeset) {
  editor.value = undefined
  if (changeset.status === 'pending') { savedDraft.value = changeset.id; return }
  selected.value.clear(); await load()
}
function checkRow(id: string, checked: boolean) { if (checked) selected.value.add(id); else selected.value.delete(id) }
function conditions(row: ShelfQueryItem, record: NonNullable<ShelfQueryItem['shelf']>['sale_statuses'][number]) { return shelfConditionText(record.conditions, props.library.shelf_config?.module_types, row.shelf?.composition) }

const regionKeys = ref<string[]>([])
const filters = ref({ status: '', region: '', field: '', min: '', max: '', standard: '', gaps: false })
const applied = ref<ShelfQuery>({})
const filterError = ref('')
const numericFields = computed(() => (props.library.entity_schema?.fields || []).filter(field => field.type === 'number' && field.product_line_ids?.includes(line.value) && !field.archived))
const chips = computed(() => {
  const result: { key: keyof ShelfQuery; label: string }[] = []
  const status = applied.value.sale_statuses?.[0]
  if (status) result.push({ key: 'sale_statuses', label: `${props.library.shelf_config?.regions?.find(region => region.key === status.region)?.name || t('shelfStatus.anyRegion')} · ${status.status || (status.no_record ? t('shelfStatus.noRecord') : status.planned_eom ? t('shelfStatus.planned') : t('shelfStatus.all'))}` })
  const parameter = applied.value.parameters?.[0]
  if (parameter) result.push({ key: 'parameters', label: `${numericFields.value.find(field => field.key === parameter.field)?.name}: ${parameter.min ?? ''} ~ ${parameter.max ?? ''}` })
  if (applied.value.certification) result.push({ key: 'certification', label: props.library.shelf_config?.standards?.find(standard => standard.key === applied.value.certification?.standard)?.name || '' })
  if (applied.value.document_gaps) result.push({ key: 'document_gaps', label: t('shelfStatus.gaps') })
  return result
})
function applyFilters() {
  filterError.value = ''
  const f = filters.value
  if ((f.min !== '' || f.max !== '') && (!f.field || (f.min !== '' && !Number.isFinite(Number(f.min))) || (f.max !== '' && !Number.isFinite(Number(f.max))) || (f.min !== '' && f.max !== '' && Number(f.min) > Number(f.max)))) { filterError.value = t('shelfStatus.invalidRange'); return }
  const query: ShelfQuery = {}
  if (f.status || f.region) query.sale_statuses = [{ ...(f.region ? { region: f.region } : {}), ...(f.status === 'no_record' ? { no_record: true } : f.status === 'planned_eom' ? { planned_eom: true } : f.status ? { status: f.status } : {}) }]
  if (f.field && (f.min !== '' || f.max !== '')) query.parameters = [{ field: f.field, ...(f.min !== '' ? { min: Number(f.min) } : {}), ...(f.max !== '' ? { max: Number(f.max) } : {}) }]
  if (f.standard) query.certification = { standard: f.standard }
  if (f.gaps) query.document_gaps = true
  applied.value = query; void load()
}
function removeFilter(key: keyof ShelfQuery) {
  delete applied.value[key]
  if (key === 'sale_statuses') { filters.value.status = ''; filters.value.region = '' }
  if (key === 'parameters') { filters.value.field = ''; filters.value.min = ''; filters.value.max = '' }
  if (key === 'certification') filters.value.standard = ''
  if (key === 'document_gaps') filters.value.gaps = false
  void load()
}
let generation = 0, disposed = false
const tabs = computed(() => lines.value.map(item => ({ value: item.id, label: `${item.canonical_name}${counts.value[item.id] === undefined ? '' : ` (${counts.value[item.id]})`}` })))
const parentID = (node: ShelfNode) => (node.facts.find(fact => fact.field === 'belongs_to')?.value as { object_id?: string } | undefined)?.object_id
const regions = computed(() => (props.library.shelf_config?.regions || []).filter(region => regionKeys.value.includes(region.key)))
const groups = computed(() => {
  const platforms = new Map<string, { id: string; name: string; products: Map<string, { id: string; name: string; models: ShelfQueryItem[] }> }>()
  for (const row of rows.value) {
    const productID = parentID(row) || '', product = nodes.value[productID]
    const platformID = product ? parentID(product) || '' : '', platform = nodes.value[platformID]
    if (!platforms.has(platformID)) platforms.set(platformID, { id: platformID, name: platform?.entity.canonical_name || t('productShelf.unassigned'), products: new Map() })
    const group = platforms.get(platformID)!
    if (!group.products.has(productID)) group.products.set(productID, { id: productID, name: product?.entity.canonical_name || t('productShelf.unassigned'), models: [] })
    group.products.get(productID)!.models.push(row)
  }
  return [...platforms.values()].map(platform => ({ ...platform, products: [...platform.products.values()] }))
})
function toggle(id: string) { if (collapsed.value.has(id)) collapsed.value.delete(id); else collapsed.value.add(id) }
function openNode(id: string) { if (id) void router.push({ path: '/platform/entities', query: { libraries: props.library.id, entity_id: id } }) }
function unwrap<T>(response: ShelfResponse<T>): T { if (!response.success) throw new Error(response.message || t('workbench.requestFailed')); return response.data }
async function load() {
  const current = ++generation; loading.value = true; error.value = ''; rows.value = []; selected.value.clear(); total.value = 0
  const libraryID = props.library.id, lineID = line.value
  try {
    const items: ShelfQueryItem[] = []
    const query: ShelfQuery = { ...applied.value, ancestor_id: lineID, limit: 100, offset: 0 }
    let count = 0
    do {
      const page = unwrap(await queryShelf(libraryID, query))
      if (current !== generation) return
      items.push(...page.items); count = page.total
      if (!page.items.length && items.length < count) throw new Error(t('shelfStatus.incomplete'))
      query.offset = items.length; query.as_of = page.as_of
    } while (items.length < count)
    const cache: Record<string, Promise<ShelfNode>> = {}
    async function ancestors(node: ShelfNode, depth = 0): Promise<void> {
      const id = parentID(node)
      if (!id || depth >= 3) return
      cache[id] ||= getShelfNode(libraryID, id, query.as_of).then(unwrap)
      const parent = await cache[id]
      if (current !== generation) return
      nodes.value[id] = parent
      await ancestors(parent, depth + 1)
    }
    await Promise.all(items.map(item => ancestors(item)))
    if (current === generation) { rows.value = items; total.value = count; if (!Object.keys(applied.value).length) { counts.value[lineID] = count; regionKeys.value = [...new Set(items.flatMap(item => item.shelf?.sale_statuses?.map(status => status.region) || []))] } }
  } catch (cause) { if (current === generation) error.value = cause instanceof Error ? cause.message : t('workbench.requestFailed') }
  finally { if (current === generation) loading.value = false }
}
async function initialize() {
  generation++; resetSearch(); queryText.value = ''; editor.value = undefined; savedDraft.value = ''; selected.value.clear(); lines.value = []; line.value = ''; nodes.value = {}; counts.value = {}; collapsed.value.clear(); loading.value = true; error.value = ''
  const libraryID = props.library.id
  try {
    const items: Entity[] = []
    for (let offset = 0; ; offset += 100) {
      const response = await listEntities({ library_id: libraryID, kind: 'product_line', limit: 100, offset })
      if (!response.success) throw new Error(response.error)
      if (disposed || libraryID !== props.library.id) return
      items.push(...response.data)
      if (response.data.length < 100) break
    }
    lines.value = items; line.value = items[0]?.id || ''
  } catch (cause) { error.value = cause instanceof Error ? cause.message : t('workbench.requestFailed') }
  finally { if (!line.value) loading.value = false }
}
watch(line, value => { collapsed.value.clear(); regionKeys.value = []; applied.value = {}; filterError.value = ''; filters.value = { status: '', region: '', field: '', min: '', max: '', standard: '', gaps: false }; if (value) void load() })
watch(() => props.library.id, initialize, { immediate: true })
onBeforeUnmount(() => { disposed = true; generation++; searchGeneration++ })
</script>
<template>
  <section class="shelf-status" :aria-label="t('shelfStatus.title')">
    <header class="head-row"><div><h1>{{ t('shelfStatus.title') }}</h1><p>{{ library.display_name || library.name }}</p></div><div class="head-actions"><form class="shelf-search" @submit.prevent="search()"><input type="search" maxlength="512" :value="queryText" :aria-label="t('shelfStatus.search')" :placeholder="t('shelfStatus.search')" @input="queryText = ($event.target as HTMLInputElement).value; resetSearch()" @compositionstart="composing = true; resetSearch()" @compositionend="composing = false"><Button type="button" :disabled="searching || !queryText.trim() || composing" @click="search()">{{ t('shelfStatus.searchButton') }}</Button></form><a href="/platform/libraries">{{ t('menu.library') }}</a></div></header>
    <section v-if="searchTotal !== undefined || searchError || searching" class="search-results" :aria-label="t('shelfStatus.search')"><p v-if="searching" role="status">{{ t('common.loading') }}</p><p v-if="searchError" role="alert">{{ searchError }}</p><p v-if="searchTotal !== undefined" role="status">{{ t('shelfStatus.searchCount', { count: searchTotal }) }}</p><button v-for="item in matches" :key="item.id" type="button" class="search-match" @click="openNode(item.id)"><span class="kind">{{ item.kind_label || library.entity_schema?.kinds.find(kind => kind.key === item.kind)?.name || item.kind }}</span><span>{{ item.canonical_name }}</span><small>{{ item.code }}</small></button><Button v-if="searchTotal !== undefined && matches.length < searchTotal" :disabled="searching" @click="search(true)">{{ t('shelfStatus.more') }}</Button><Button @click="queryText = ''; resetSearch()">{{ t('shelfStatus.clearSearch') }}</Button></section>
    <form class="filters" @submit.prevent="applyFilters">
      <label>{{ t('shelfStatus.state') }}<select :value="filters.status" :aria-label="t('shelfStatus.state')" @change="filters.status = ($event.target as HTMLSelectElement).value"><option value="">{{ t('shelfStatus.all') }}</option><option v-for="status in SALE_STATUSES" :key="status" :value="status">{{ status }}</option><option value="planned_eom">{{ t('shelfStatus.planned') }}</option><option value="no_record">{{ t('shelfStatus.noRecord') }}</option></select></label>
      <label>{{ t('shelfStatus.region') }}<select :value="filters.region" :aria-label="t('shelfStatus.region')" @change="filters.region = ($event.target as HTMLSelectElement).value"><option value="">{{ t('shelfStatus.anyRegion') }}</option><option v-for="region in library.shelf_config?.regions" :key="region.key" :value="region.key">{{ region.name }}</option></select></label>
      <div v-if="numericFields.length" class="parameter"><label>{{ t('shelfStatus.parameter') }}<select :value="filters.field" :aria-label="t('shelfStatus.parameter')" @change="filters.field = ($event.target as HTMLSelectElement).value"><option value="">{{ t('shelfStatus.all') }}</option><option v-for="field in numericFields" :key="field.key" :value="field.key">{{ field.name }}{{ field.unit ? ` (${field.unit})` : '' }}</option></select></label><div class="range"><input type="number" step="any" :value="filters.min" :aria-label="t('shelfStatus.min')" :placeholder="t('shelfStatus.min')" @input="filters.min = ($event.target as HTMLInputElement).value"><span>~</span><input type="number" step="any" :value="filters.max" :aria-label="t('shelfStatus.max')" :placeholder="t('shelfStatus.max')" @input="filters.max = ($event.target as HTMLInputElement).value"></div></div>
      <label>{{ t('shelfStatus.standard') }}<select :value="filters.standard" :aria-label="t('shelfStatus.standard')" @change="filters.standard = ($event.target as HTMLSelectElement).value"><option value="">{{ t('shelfStatus.all') }}</option><option v-for="standard in library.shelf_config?.standards" :key="standard.key" :value="standard.key">{{ standard.name }}</option></select></label>
      <label class="check"><input type="checkbox" :checked="filters.gaps" :aria-label="t('shelfStatus.gaps')" @change="filters.gaps = ($event.target as HTMLInputElement).checked">{{ t('shelfStatus.gaps') }}</label>
      <Button type="button" :disabled="loading" @click="applyFilters">{{ t('shelfStatus.apply') }}</Button>
    </form>
    <p v-if="filterError" role="alert">{{ filterError }}</p>
    <div v-if="chips.length" class="applied"><Button v-for="chip in chips" :key="chip.key" variant="ghost" :aria-label="t('shelfStatus.removeFilter', { filter: chip.label })" @click="removeFilter(chip.key)">{{ chip.label }} ×</Button></div>
    <div class="legend"><span v-for="status in SALE_STATUSES" :key="status" class="st" :class="`st-${status}`">{{ status }}</span><span>· {{ t('shelfStatus.noRecord') }}</span><span class="st st-GA"><b class="exit-mark">{{ t('shelfStatus.exitMark') }}</b> {{ t('shelfStatus.planned') }}</span></div>
    <ErrorState v-if="error" :title="error"><Button @click="line ? load() : initialize()">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading" role="status">{{ t('productShelf.loading') }}</p>
    <p v-if="savedDraft" role="status">{{ t('shelfStatus.draftSaved', { id: savedDraft }) }}</p>
    <div v-if="rows.length" class="bulk-bar"><span>{{ t('shelfStatus.selectedCount', { count: selected.size }) }}</span><Button :disabled="!selected.size || !library.shelf_config?.regions?.length" @click="editBulk">{{ t('shelfStatus.bulk') }}</Button></div>
    <ShelfStatusEditor v-if="editor" :library="library" :models="editor.models" :region="editor.region" :bulk="editor.bulk" @close="editor = undefined" @saved="saved" />
    <Tabs v-if="lines.length" :model-value="line" :items="tabs" :label="t('shelfStatus.lines')" @update:model-value="line = $event">
      <template v-for="item in tabs" :key="item.value" #[item.value]>
        <p class="total" role="status">{{ t('shelfStatus.total', { count: total }) }}</p>
        <div class="table-wrap"><table :aria-label="t('shelfStatus.status')"><thead><tr><th class="c-model"><input type="checkbox" :checked="rows.length > 0 && selected.size === rows.length" :aria-label="t('shelfStatus.selectAll')" @change="selected = ($event.target as HTMLInputElement).checked ? new Set(rows.map(row => row.entity.id)) : new Set()"> {{ t('shelfStatus.hierarchy') }}</th><th v-for="region in regions" :key="region.key" class="c-reg">{{ region.name }}</th><th>{{ t('shelfStatus.notes') }}</th></tr></thead>
          <template v-for="platform in groups" :key="platform.id">
            <tbody class="g-platform"><tr><td :colspan="regions.length + 2"><button class="g-toggle" type="button" :aria-expanded="!collapsed.has(platform.id)" @click="toggle(platform.id)"><span>{{ collapsed.has(platform.id) ? '+' : '−' }}</span><span class="kind">{{ t('productShelf.kinds.platform') }}</span>{{ platform.name }}</button></td></tr></tbody>
            <template v-if="!collapsed.has(platform.id)"><template v-for="product in platform.products" :key="product.id">
              <tbody class="g-product"><tr><td :colspan="regions.length + 2"><button class="g-toggle sub" type="button" :aria-expanded="!collapsed.has(product.id)" @click="toggle(product.id)"><span>{{ collapsed.has(product.id) ? '+' : '−' }}</span><span class="kind">{{ t('productShelf.kinds.product') }}</span>{{ product.name }}</button><button class="node-link" type="button" @click="openNode(product.id)">{{ t('shelfStatus.openNode') }}</button><span class="count">{{ product.models.length }}</span></td></tr></tbody>
              <tbody v-if="!collapsed.has(product.id)" class="g-models"><tr v-for="row in product.models" :key="row.entity.id" :aria-selected="selected.has(row.entity.id)"><td class="c-model"><input type="checkbox" :checked="selected.has(row.entity.id)" :aria-label="t('shelfStatus.chooseModel', { name: row.entity.canonical_name })" @change="checkRow(row.entity.id, ($event.target as HTMLInputElement).checked)"> <button class="node-link" type="button" :title="row.entity.description" @click="openNode(row.entity.id)">{{ row.entity.canonical_name }}</button><small v-if="row.partial_options">{{ t('shelfStatus.partial') }}</small></td><td v-for="region in regions" :key="region.key" class="c-reg"><button type="button" class="cell-editor" :aria-label="t('shelfStatus.editCell', { model: row.entity.canonical_name, region: region.name })" @click="editCell(row, region.key)"><template v-for="(status, index) in row.shelf?.sale_statuses.filter(status => status.region === region.key)" :key="index"><span class="cell-record"><span class="st" :class="`st-${status.status}`" :title="[status.notes, status.raw].filter(Boolean).join(' · ')">{{ status.status }}<b v-if="status.planned_eom" class="exit-mark">{{ t('shelfStatus.exitMark') }}</b><i v-if="status.notes" class="note-dot" /></span><span v-if="status.conditions?.length" class="condition">{{ conditions(row, status) }}</span></span></template><span v-if="!row.shelf?.sale_statuses.some(status => status.region === region.key)" class="st-none" :title="t('shelfStatus.noRecord')">·</span></button></td><td class="c-note" :title="row.entity.description">{{ row.entity.description }}</td></tr></tbody>
            </template></template>
          </template>
        </table></div>
        <EmptyState v-if="!loading && !error && !rows.length" :title="t('productShelf.noRecords')" />
      </template>
    </Tabs>
    <EmptyState v-if="!loading && !error && !lines.length" :title="t('productShelf.noRecords')" />
  </section>
</template>
<style scoped>
.head-actions { display: flex; align-items: center; gap: 16px; } .shelf-search { display: flex; gap: 4px; } .shelf-search input { width: 250px; padding: 8px; border: 1px solid var(--border); color: var(--text); background: var(--surface); font: inherit; }
.search-results { align-self: flex-end; min-width: 400px; border: 2px solid var(--rule); background: var(--surface); padding: 12px; }
.search-match { display: flex; align-items: center; gap: 12px; width: 100%; padding: 8px; border: 0; border-bottom: 1px solid var(--border); background: none; color: inherit; text-align: left; font: inherit; cursor: pointer; } .search-match:hover, .search-match:focus-visible { background: var(--select); color: var(--on-select); }
.bulk-bar { display: flex; justify-content: space-between; align-items: center; font-size: var(--text-sm); }
.cell-editor { border: 0; padding: 0; background: none; font: inherit; color: inherit; width: 100%; min-height: 28px; cursor: pointer; } .cell-record { display: flex; flex-direction: column; align-items: center; gap: 2px; margin-block: 2px; } .condition { display: block; max-width: 190px; white-space: normal; font-size: var(--text-xs); color: var(--muted); } .c-model input { margin: 0 4px 0 0; }
.g-models tr[aria-selected=true] td { background: var(--surface-soft); } .g-models tr[aria-selected=true]:hover td, .g-models tr[aria-selected=true]:focus-within td { background: var(--select); }
.filters { display: flex; align-items: end; gap: 12px; padding-block: 12px; border-top: 2px solid var(--rule); }
.filters label { display: grid; gap: 8px; font-size: var(--text-xs); font-weight: 700; min-width: 115px; }
.filters input:not([type=checkbox]), .filters select { width: 100%; height: 36px; padding: 4px 8px; border: 1px solid var(--border); background: var(--surface); color: var(--text); font: inherit; }
.filters .check { display: flex; align-items: center; padding-bottom: 8px; white-space: nowrap; }
.parameter { width: 230px; } .range { display: flex; gap: 4px; align-items: center; margin-top: 4px; } .range input { min-width: 0; }
.legend, .applied { display: flex; align-items: center; gap: 8px 16px; font-size: var(--text-xs); } .applied :deep(button) { background: var(--text); color: var(--bg); }
.shelf-status { min-width: 0; padding: var(--space-5) var(--space-6) var(--space-7); display: flex; flex-direction: column; gap: var(--space-4); color: var(--text); }
.head-row { display: flex; align-items: end; justify-content: space-between; gap: var(--space-4); }
h1 { font-size: var(--type-page-size); font-weight: 800; letter-spacing: -0.035em; line-height: 1.05; margin: 0; }
p { margin: 0; } .head-row p { color: var(--muted); margin-top: var(--space-2); }
a { color: inherit; } .total { font-size: var(--text-lg); font-weight: 800; margin-block: var(--space-3); }
.table-wrap { max-height: 65vh; overflow: auto; border-top: 2px solid var(--rule); }
table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: 5px 12px; border-bottom: 1px solid var(--border); text-align: left; white-space: nowrap; }
th { position: sticky; top: 0; z-index: 2; background: var(--bg); border-bottom-color: var(--rule); font-size: var(--text-xs); }
.c-model { position: sticky; left: 0; z-index: 1; background: var(--bg); min-width: 280px; font-family: var(--mono); font-size: var(--text-xs); }
th.c-model { z-index: 3; font-family: var(--font); } .g-models .c-model { padding-left: 38px; }
.c-reg { text-align: center; padding-inline: 4px; } .c-note { max-width: 240px; overflow: hidden; text-overflow: ellipsis; color: var(--muted); font-size: var(--text-xs); }
.g-platform td { padding-top: 12px; border-bottom-color: var(--rule); } .g-product td { background: var(--surface-soft); }
.g-toggle { display: inline-flex; align-items: center; gap: 8px; font: inherit; font-weight: 700; border: 0; padding: 0; background: none; color: inherit; cursor: pointer; }
.g-toggle > span:first-child { display: inline-grid; place-items: center; width: 14px; height: 14px; border: 1px solid currentColor; font-family: var(--mono); } .g-toggle.sub { padding-left: 20px; }
.kind { padding: 0 4px; border: 1px solid var(--text); font-size: var(--text-xs); font-weight: 700; }
.node-link { border: 0; padding: 0; background: none; color: inherit; font: inherit; text-decoration: underline; cursor: pointer; } .g-product .node-link { margin-left: 12px; } .count { font-family: var(--mono); color: var(--muted); margin-left: 12px; }
.st { display: inline-flex; align-items: center; justify-content: center; min-width: 68px; padding: 1px 8px; font-size: var(--text-xs); font-weight: 800; line-height: 1.6; }
.st-GA { background: var(--success); color: var(--bg); } .st-DEV { background: var(--blue); color: var(--on-blue); } .st-Planning { background: var(--select); color: var(--on-select); } .st-EOM { background: var(--muted); color: var(--bg); } .st-待定 { outline: 1px dashed var(--muted); outline-offset: -1px; }
.exit-mark { margin-left: 4px; padding-inline: 3px; background: var(--accent); color: var(--on-accent); } .note-dot { width: 5px; height: 5px; background: currentColor; margin-left: 4px; } .st-none { color: var(--muted); }
.g-models tr:hover td, .g-models tr:focus-within td { background: var(--select); color: var(--on-select); }
button:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; } small { display: block; font-family: var(--font); color: var(--muted); }
</style>
