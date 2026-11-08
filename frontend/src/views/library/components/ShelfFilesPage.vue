<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, EmptyState, ErrorState, Input, Select, Tabs } from '@/components/ui'
import { toast } from '@/components/ui/feedback'
import type { LibraryCatalogItem } from '@/api/library'
import { listShelfFiles, type ShelfFileRow } from '@/api/shelf-files'
import ShelfDocumentEditor from './ShelfDocumentEditor.vue'
import ShelfFilePage from './ShelfFilePage.vue'
const props = defineProps<{ library: LibraryCatalogItem }>()
const route = useRoute(), router = useRouter(), { t } = useI18n()
const group = ref('requirements'), search = ref(''), ownerKind = ref(''), parseStatus = ref(''), history = ref(false), offset = ref(0)
const rows = ref<ShelfFileRow[]>([]), total = ref(0), loading = ref(false), error = ref(''), upload = ref(false)
let generation = 0
function saved(id: string) { upload.value = false; toast.success(t('shelfFiles.saved')); if (id) open(id); else void load() }
function certification(row: ShelfFileRow) { return (row.owner?.attributes?.certification || {}) as { standard?: string; year?: number; agency?: string; issued_date?: string } }
const fileId = computed(() => typeof route.query.file === 'string' ? route.query.file : '')
const tabs = computed(() => [{ value: 'requirements', label: t('shelfFiles.requirements') }, { value: 'certificates', label: t('shelfFiles.certificates') }, { value: 'other', label: t('shelfFiles.other') }])
const ownerKinds = computed(() => [{ key: 'model', label: t('productShelf.kinds.model') }, { key: 'product', label: t('productShelf.kinds.product') }, { key: 'platform', label: t('productShelf.kinds.platform') }, { key: 'part', label: t('productShelf.kinds.part') }, { key: 'certification', label: t('productShelf.kinds.certification') }, { key: 'unassigned', label: t('shelfFiles.unassigned') }])
const statuses = computed(() => ({ pending: t('shelfFiles.pending'), parsing: t('shelfFiles.parsingStatus'), retrying: t('shelfFiles.retrying'), failed: t('shelfFiles.failed'), completed: t('shelfFiles.completed'), not_parsed: t('shelfFiles.not_parsed') }))
async function load() {
  const current = ++generation; loading.value = true; error.value = ''
  try {
    const response = await listShelfFiles(props.library.id, { q: search.value, group: group.value, owner_kind: ownerKind.value, parse_status: parseStatus.value, include_history: history.value, offset: offset.value })
    if (!response.success) throw new Error(response.message)
    if (current === generation) { rows.value = response.data.items; total.value = response.data.total }
  } catch { if (current === generation) error.value = t('shelfFiles.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function open(id: string) { void router.replace({ query: { ...route.query, tab: 'files', file: id } }) }
watch(fileId, (value, previous) => { if (!value && previous) void load() })
watch([group, ownerKind, parseStatus, history], () => { offset.value = 0; void load() })
watch(() => props.library.id, () => { offset.value = 0; void load() }, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
<template>
  <ShelfFilePage v-if="fileId" :key="fileId" :library="library" :file-id="fileId" />
  <div v-else class="rq-shelf-files">
    <header class="rq-shelf-row"><h1>{{ t('shelfFiles.title') }}</h1><Button variant="primary" @click="upload = true">{{ t('shelfFiles.upload') }}</Button></header>
    <form class="rq-shelf-filters" @submit.prevent="offset = 0; load()">
      <Input v-model="search" type="search" :aria-label="t('shelfFiles.search')" :placeholder="t('shelfFiles.search')" />
      <Select v-model="ownerKind" :aria-label="t('shelfFiles.owner')"><option value="">{{ t('shelfFiles.owner') }}: {{ t('shelfFiles.all') }}</option><option v-for="kind in ownerKinds" :key="kind.key" :value="kind.key">{{ kind.label }}</option></Select>
      <Select v-model="parseStatus" :aria-label="t('shelfFiles.parsing')"><option value="">{{ t('shelfFiles.parsing') }}: {{ t('shelfFiles.all') }}</option><option v-for="(label, key) in statuses" :key="key" :value="key">{{ label }}</option></Select>
      <Checkbox v-model="history">{{ t('shelfFiles.includeHistory') }}</Checkbox><Button type="submit">{{ t('shelfStatus.searchButton') }}</Button>
    </form>
    <Tabs v-model="group" :items="tabs" :label="t('shelfFiles.title')">
      <template v-for="item in tabs" :key="item.value" #[item.value]>
        <p v-if="loading" role="status">{{ t('common.loading') }}</p>
        <ErrorState v-else-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
        <template v-else>
          <EmptyState v-if="!rows.length" :title="t('shelfFiles.empty')" />
          <div v-else class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfFiles.fileName') }}</th><th>{{ t('shelfFiles.owner') }}</th><th>{{ group === 'certificates' ? t('shelfModel.standard') : t('shelfFiles.requirement') }}</th><th v-if="group === 'certificates'">{{ t('shelfModel.agency') }}</th><th v-if="group === 'certificates'">{{ t('shelfModel.issued_date') }}</th><th>{{ t('shelfFiles.version') }}</th><th>{{ t('shelfFiles.text') }}</th><th>{{ t('shelfFiles.updated') }}</th></tr></thead>
            <tbody><tr v-for="row in rows" :key="row.document.id"><td><a :href="`/platform/entries?libraries=${encodeURIComponent(library.id)}&tab=files&file=${encodeURIComponent(row.entity.id)}`" @click.prevent="open(row.entity.id)">{{ row.entity.canonical_name }}</a></td><td><template v-if="row.owner"><span class="rq-shelf-kind">{{ library.entity_schema?.kinds.find(kind => kind.key === row.owner?.kind)?.name || row.owner.kind }}</span> <a :href="`/platform/entities?libraries=${encodeURIComponent(library.id)}&entity_id=${encodeURIComponent(row.owner.id)}`">{{ row.owner.canonical_name }}</a></template><template v-else>{{ t('shelfFiles.unassigned') }} <Button variant="ghost" @click="open(row.entity.id)">{{ t('shelfFiles.attach') }}</Button></template></td><td><template v-if="group === 'certificates'">{{ library.shelf_config?.standards?.find(s => s.key === certification(row).standard)?.name || certification(row).standard }} {{ certification(row).year }}<small>{{ row.document.certificate_number }}</small></template><template v-else>{{ row.requirement?.canonical_name || row.document.file_category || t('shelfFiles.none') }}</template></td><td v-if="group === 'certificates'">{{ certification(row).agency }}</td><td v-if="group === 'certificates'">{{ certification(row).issued_date }}</td><td>{{ row.document.version_label }} {{ row.document.stage === 'final' ? t('shelfFiles.final') : t('shelfFiles.draft') }} <span v-if="row.document.is_current" class="rq-shelf-kind">{{ t('shelfFiles.current') }}</span></td><td :class="{ 'rq-shelf-error': row.document.parse_status === 'failed' }">{{ statuses[row.document.parse_status as keyof typeof statuses] || row.document.parse_status }}</td><td>{{ (row.entity.updated_at || row.document.occurred_at)?.slice(0, 10) }}</td></tr></tbody>
          </table></div>
          <footer class="rq-shelf-row"><span>{{ t('shelfFiles.total', { count: total }) }}</span><div><Button :disabled="offset === 0" @click="offset -= 50; load()">{{ t('shelfFiles.previous') }}</Button><Button :disabled="offset + 50 >= total" @click="offset += 50; load()">{{ t('shelfFiles.next') }}</Button></div></footer>
        </template>
      </template>
    </Tabs>
    <ShelfDocumentEditor v-if="upload" :library="library" @close="upload = false" @saved="saved" />
  </div>
</template>
<style scoped>
.rq-shelf-files { padding: var(--space-5) var(--space-6); display: flex; flex-direction: column; gap: var(--space-4); min-width: 900px; }
h1 { font-size: var(--text-3xl); font-weight: 800; letter-spacing: -1px; }
.rq-shelf-row { display: flex; justify-content: space-between; align-items: center; gap: var(--space-3); }
.rq-shelf-filters { display: grid; grid-template-columns: minmax(240px, 2fr) 150px 150px auto auto; gap: var(--space-3); align-items: center; }
.rq-shelf-table { overflow: auto; border-top: 2px solid var(--rule); margin-bottom: var(--space-4); }
table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: var(--space-3); text-align: left; border-bottom: 1px solid var(--border); }
th { font-size: var(--text-xs); border-bottom-color: var(--rule); }
.rq-shelf-kind { display: inline-block; padding: 0 var(--space-1); border: 1px solid var(--border); font-size: var(--text-xs); white-space: nowrap; }
.rq-shelf-error { color: var(--danger); }
</style>
