<script setup lang="ts">
import { requirementLabel } from '@/utils/shelfRequirement'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState } from '@/components/ui'
import type { Entity } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import { getShelfNode, queryShelf, submitShelfStage, type ShelfNode, type ShelfQueryItem, type ShelfCondition, type ShelfDocumentVersion, type ShelfRequirement } from '@/api/shelf'
import { shelfConditionText } from '@/utils/productShelf'
import { safeExternalUrl } from '@/utils/externalUrl'

const props = defineProps<{ library: LibraryCatalogItem; nodeId: string }>()
const { t } = useI18n()
const node = ref<ShelfNode>(), models = ref<ShelfQueryItem[]>([]), ancestors = ref<Entity[]>([])
const paths = ref<Record<string, Entity[]>>({}), identities = ref<Entity[]>([])
const loading = ref(false), error = ref(''), saving = ref(false), saveError = ref(''), stageDraft = ref('')
let generation = 0
const isPart = computed(() => node.value?.entity.kind === 'part')
const sections = computed(() => isPart.value ? ['certifications', 'part-checklist', 'node-files'] : ['summary', 'node-files', 'certifications'])
const href = (id: string) => `/platform/entities?${new URLSearchParams({ libraries: props.library.id, entity_id: id })}`
const fileHref = (id: string) => `/platform/entries?${new URLSearchParams({ libraries: props.library.id, tab: 'files', file: id })}`
const statusHref = computed(() => `/platform/entries?${new URLSearchParams({ libraries: props.library.id })}`)
const kind = (entity: Entity) => props.library.entity_schema?.kinds.find(item => item.key === entity.kind)?.name || t(`productShelf.kinds.${entity.kind}`)
const stage = (key?: string) => props.library.shelf_config?.stages?.find(item => item.key === key)?.name || key || t('shelfNode.noStage')
const moduleName = (key: unknown) => props.library.shelf_config?.module_types?.find(item => item.key === key)?.name || String(key || '')
const knownParts = computed(() => [{ module_type: '', optional: false, parts: identities.value }])
const condition = (value?: ShelfCondition[] | null) => shelfConditionText(value, props.library.shelf_config?.module_types, knownParts.value)
const owner = (id: string) => identities.value.find(item => item.id === id)
const requirements = computed(() => [...new Map(models.value.flatMap(model => model.shelf?.checklist || []).map(row => [row.requirement.id, row.requirement])).values()])
const requirementData = (entity: Entity) => (entity.attributes?.requirement || {}) as ShelfRequirement
const gapCount = computed(() => new Set(models.value.flatMap(model => model.shelf?.checklist || []).filter(row => row.status === 'missing').map(row => row.requirement.id)).size)
const rowsFor = (model: ShelfQueryItem, id: string) => model.shelf?.checklist?.filter(row => row.requirement.id === id) || []
const certGroups = computed(() => [
  { title: isPart.value ? t('shelfModel.sections.cert') : t('shelfNode.ownCerts'), rows: node.value?.shelf?.certifications?.filter(row => row.origin === 'self') || [] },
  { title: t('shelfNode.ancestorCerts'), rows: node.value?.shelf?.certifications?.filter(row => row.origin === 'ancestor') || [] },
])
const fields = computed(() => props.library.entity_schema?.fields.filter(field => !field.archived && field.key !== 'module_type' && (!field.kinds?.length || field.kinds.includes('part'))) || [])
const partRoles = (model: ShelfQueryItem) => model.shelf?.composition?.filter(group => group.parts.some(part => part.id === props.nodeId)) || []
const documentURL = (document: ShelfDocumentVersion) => document.attachment_id ? `/api/v1/entities/${encodeURIComponent(document.file_id)}/document-versions/${encodeURIComponent(document.id)}/download` : safeExternalUrl(document.cloud_url)
const valueText = (value: unknown): string => Array.isArray(value) ? value.map(valueText).join(' · ') : value == null || value === '' ? t('shelfStatus.noRecord') : typeof value === 'object' ? JSON.stringify(value) : String(value)
function unwrap<T>(response: { success: boolean; data: T; message?: string }): T {
  if (!response.success) throw new Error(response.message || t('workbench.requestFailed'))
  return response.data
}
async function load() {
  const current = ++generation, libraryID = props.library.id, id = props.nodeId
  loading.value = true; error.value = ''
  try {
    const result = unwrap(await getShelfNode(libraryID, id))
    if (current !== generation) return
    const cache = new Map<string, Promise<ShelfNode>>([[id, Promise.resolve(result)]])
    const read = (nodeID: string) => {
      if (!cache.has(nodeID)) cache.set(nodeID, getShelfNode(libraryID, nodeID, result.as_of).then(unwrap))
      return cache.get(nodeID)!
    }
    async function path(detail: ShelfNode) {
      const items: Entity[] = [], seen = new Set([detail.entity.id])
      let parent = (detail.facts.find(fact => fact.field === 'belongs_to')?.value as { object_id?: string } | undefined)?.object_id
      while (parent && !seen.has(parent) && items.length < 3) {
        seen.add(parent)
        const next = await read(parent)
        items.unshift(next.entity)
        parent = (next.facts.find(fact => fact.field === 'belongs_to')?.value as { object_id?: string } | undefined)?.object_id
      }
      return items
    }
    const nextModels: ShelfQueryItem[] = []
    if (result.recorded) {
      let total = 0
      do {
        const page = unwrap(await queryShelf(libraryID, { ...(result.entity.kind === 'part' ? { part_id: id } : { ancestor_id: id }), as_of: result.as_of, limit: 100, offset: nextModels.length }))
        if (current !== generation) return
        total = page.total
        if (!page.items.length && nextModels.length < total) throw new Error(t('shelfStatus.incomplete'))
        nextModels.push(...page.items)
      } while (nextModels.length < total)
    }
    const nextAncestors = await path(result), nextPaths: Record<string, Entity[]> = {}
    if (result.entity.kind === 'part') {
      for (const model of nextModels) { nextPaths[model.entity.id] = await path(model); if (current !== generation) return }
    }
    const parts = nextModels.flatMap(model => model.shelf?.composition?.flatMap(group => group.parts) || [])
    const known = new Map([result.entity, ...nextAncestors, ...Object.values(nextPaths).flat(), ...parts].map(entity => [entity.id, entity]))
    const conditions = [...(result.shelf?.certifications?.flatMap(row => [...(row.certification.conditions || []), ...(row.certification.cell_part_id ? [{ module_type: '', part_ids: [row.certification.cell_part_id] }] : [])]) || []), ...(result.shelf?.documents?.flatMap(row => row.document.conditions || []) || [])]
    for (const partID of new Set(conditions.flatMap(row => row.part_ids))) {
      if (!known.has(partID)) known.set(partID, (await read(partID)).entity)
      if (current !== generation) return
    }
    if (current === generation) {
      node.value = result; models.value = nextModels; ancestors.value = nextAncestors; paths.value = nextPaths; identities.value = [...known.values()]
      stageDraft.value = String(result.entity.attributes?.current_stage || '')
    }
  } catch (cause) { if (current === generation) error.value = (cause as { message?: string } | undefined)?.message || t('workbench.requestFailed') }
  finally { if (current === generation) loading.value = false }
}
async function changeStage(event: Event) {
  if (!node.value || saving.value) return
  const entity = node.value.entity, current = generation
  stageDraft.value = (event.target as HTMLSelectElement).value
  saving.value = true; saveError.value = ''
  try {
    const result = unwrap(await submitShelfStage(props.library.id, entity, stageDraft.value))
    if (current !== generation) return
    if (result.status !== 'confirmed') throw new Error(t('workbench.requestFailed'))
    await load()
  } catch (cause) {
    if (current === generation) { stageDraft.value = String(entity.attributes?.current_stage || ''); saveError.value = (cause as { message?: string } | undefined)?.message || t('workbench.requestFailed') }
  } finally { saving.value = false }
}
watch(() => [props.library.id, props.nodeId], () => {
  generation++; node.value = undefined; models.value = []; ancestors.value = []; paths.value = {}; identities.value = []; saveError.value = ''; void load()
}, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <article class="shelf-node" :aria-label="isPart ? t('shelfNode.partTitle') : t('shelfNode.title')">
    <a :href="statusHref">{{ t('shelfStatus.title') }}</a>
    <ErrorState v-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading" role="status">{{ t('productShelf.loading') }}</p>
    <template v-if="node && !loading && !error">
      <header>
        <nav class="crumb" :aria-label="t('shelfModel.hierarchy')">
          <template v-for="item in ancestors" :key="item.id"><span class="kind">{{ kind(item) }}</span><a :href="href(item.id)">{{ item.canonical_name }}</a><span aria-hidden="true">›</span></template>
          <span class="kind" aria-current="page">{{ kind(node.entity) }}</span>
          <template v-if="isPart"><span>{{ t('shelfModel.module') }}</span><b>{{ moduleName(node.entity.attributes?.module_type) }}</b></template>
        </nav>
        <h1>{{ node.entity.canonical_name }}</h1><p v-if="node.entity.description">{{ node.entity.description }}</p>
      </header>
      <EmptyState v-if="!node.recorded" :title="t('shelfModel.noRecord')" />
      <template v-else>
        <dl class="facts">
          <template v-if="isPart">
            <div><dt>{{ t('managed.aliases') }}</dt><dd>{{ node.entity.aliases?.join(' · ') || t('shelfStatus.noRecord') }}</dd></div>
            <div v-for="field in fields" :key="field.key"><dt>{{ field.name }}</dt><dd>{{ valueText(node.entity.attributes?.[field.key]) }} {{ node.entity.attributes?.[field.key] != null && node.entity.attributes?.[field.key] !== '' && field.unit ? field.unit : '' }}</dd></div>
          </template>
          <div v-if="node.entity.kind === 'product'"><dt><label for="node-stage">{{ t('shelfModel.currentStage') }}</label></dt><dd><select id="node-stage" :value="stageDraft" :disabled="saving" @change="changeStage"><option value="">{{ t('shelfNode.noStage') }}</option><option v-for="item in library.shelf_config?.stages" :key="item.key" :value="item.key">{{ item.name }}</option></select></dd></div>
          <div><dt>{{ isPart ? t('shelfNode.usedBy') : t('shelfNode.models') }}</dt><dd>{{ t('shelfStatus.total', { count: models.length }) }}</dd></div>
          <div v-if="!isPart"><dt>{{ t('shelfNode.gaps') }}</dt><dd>{{ t('shelfNode.gapCount', { count: gapCount, models: node.shelf?.checklist_summary?.models_with_gaps || 0 }) }}</dd></div>
          <div><dt>{{ t('shelfModel.sections.cert') }}</dt><dd>{{ t('shelfModel.certCount', { count: node.shelf?.certifications?.length || 0 }) }}</dd></div>
        </dl>
        <p v-if="saveError" role="alert">{{ saveError }}</p>
        <section id="models">
          <h2>{{ isPart ? t('shelfNode.usedBy') : t('shelfNode.models') }}</h2>
          <div v-if="models.length" class="table-wrap"><table>
            <thead><tr><th>{{ t('productShelf.kinds.model') }}</th><th>{{ isPart ? t('productShelf.kinds.product_line') : t('shelfModel.sections.sale') }}</th><th>{{ isPart ? t('shelfModel.sections.composition') : t('shelfModel.currentStage') }}</th></tr></thead>
            <tbody><tr v-for="model in models" :key="model.entity.id">
              <td><a :href="href(model.entity.id)" :title="model.entity.description">{{ model.entity.canonical_name }}</a></td>
              <template v-if="isPart">
                <td><template v-for="line in paths[model.entity.id]?.filter(item => item.kind === 'product_line')" :key="line.id"><a :href="href(line.id)">{{ line.canonical_name }}</a></template></td>
                <td><div v-for="group in partRoles(model)" :key="group.module_type">{{ moduleName(group.module_type) }} · {{ group.optional ? t('shelfNode.optional') : t('shelfNode.fixed') }}<small v-if="group.optional">{{ t('shelfNode.alternatives') }}: <template v-for="(part, index) in group.parts" :key="part.id"><span v-if="index"> · </span><a :href="href(part.id)">{{ part.canonical_name }}</a></template></small></div></td>
              </template>
              <template v-else>
                <td><div v-for="(status, index) in model.shelf?.sale_statuses" :key="index" class="sale"><b>{{ library.shelf_config?.regions?.find(item => item.key === status.region)?.name || status.region }}</b> <span class="st" :class="`st-${status.status}`">{{ status.status }}</span> <b v-if="status.planned_eom">{{ t('shelfStatus.planned') }}</b><small>{{ status.notes }}</small><small>{{ condition(status.conditions) }}</small></div><span v-if="!model.shelf?.sale_statuses?.length">{{ t('shelfStatus.noRecord') }}</span></td>
                <td>{{ stage(model.shelf?.current_stage) }}<small v-if="model.shelf?.stage_owner_id">{{ model.shelf.stage_owner_id === model.entity.id ? t('shelfNode.overridden') : t('shelfModel.inheritedStage') }}</small></td>
              </template>
            </tr></tbody>
          </table></div><p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <template v-for="section in sections" :key="section">
        <section v-if="section === 'summary'" id="checklist-summary">
          <h2>{{ t('shelfNode.summary') }}</h2>
          <div v-if="requirements.length" class="table-wrap matrix"><table>
            <thead><tr><th>{{ t('shelfModel.material') }}</th><th v-for="model in models" :key="model.entity.id"><a :href="href(model.entity.id)">{{ model.entity.canonical_name }}</a></th></tr></thead>
            <tbody><tr v-for="requirement in requirements" :key="requirement.id"><th scope="row">{{ requirementLabel(requirement) }}</th><td v-for="model in models" :key="model.entity.id"><a v-for="(row, index) in rowsFor(model, requirement.id)" :key="index" :href="`${href(model.entity.id)}#requirement-${encodeURIComponent(requirement.id)}`"><span class="doc" :class="row.status">{{ t(`shelfModel.status.${row.status}`) }}</span><small>{{ condition(row.conditions) }}</small><small v-if="row.ambiguous">{{ t('shelfModel.ambiguous') }}</small></a><span v-if="!rowsFor(model, requirement.id).length">{{ t('shelfNode.notApplicable') }}</span></td></tr></tbody>
          </table></div><p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section v-if="section === 'part-checklist'" id="part-checklist">
          <h2>{{ t('shelfModel.sections.docs') }}</h2>
          <div v-if="node.shelf?.checklist?.length" class="table-wrap"><table>
            <thead><tr><th>{{ t('shelfModel.material') }}</th><th>{{ t('shelfModel.draft') }}</th><th>{{ t('shelfModel.final') }}</th><th>{{ t('shelfModel.due') }}</th><th>{{ t('shelfModel.file') }}</th></tr></thead>
            <tbody><tr v-for="(row, index) in node.shelf.checklist" :key="index"><th scope="row">{{ requirementLabel(row.requirement) }}<small>{{ requirementData(row.requirement).name_en }}</small></th><td>{{ requirementData(row.requirement).draft_due_stage ? stage(requirementData(row.requirement).draft_due_stage) : t('shelfStatus.noRecord') }}</td><td>{{ stage(requirementData(row.requirement).final_due_stage) }}</td><td>{{ requirementData(row.requirement).due_text }}</td><td><span>{{ row.has_file ? t('shelfModel.status.has_file') : t('shelfModel.status.no_file') }}</span><template v-if="row.document"><a :href="fileHref(row.document.file_id)">{{ row.document_name }}</a> <a v-if="documentURL(row.document)" :href="documentURL(row.document)" target="_blank" rel="noopener noreferrer">{{ row.document.attachment_id ? t('shelfModel.download') : t('shelfModel.cloudLink') }}</a><small>{{ row.document.stage === 'draft' ? t('shelfModel.draft') : t('shelfModel.final') }} {{ row.document.version_label }}</small></template><small>{{ condition(row.conditions) }}</small><small v-if="row.ambiguous">{{ t('shelfModel.ambiguous') }}</small></td></tr></tbody>
          </table></div><p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section v-if="section === 'node-files'" id="node-files">
          <h2>{{ t('shelfNode.files') }}</h2>
          <div v-if="node.shelf?.documents?.length" class="table-wrap"><table>
            <thead><tr><th>{{ t('shelfModel.file') }}</th><th>{{ t('shelfModel.material') }}</th><th>{{ t('shelfModel.version') }}</th></tr></thead>
            <tbody><tr v-for="file in node.shelf.documents" :key="file.document.id"><td><a :href="fileHref(file.entity.id)">{{ file.entity.canonical_name }}</a> <a v-if="documentURL(file.document)" :href="documentURL(file.document)" target="_blank" rel="noopener noreferrer">{{ file.document.attachment_id ? t('shelfModel.download') : t('shelfModel.cloudLink') }}</a></td><td>{{ (file.requirements || (file.requirement ? [file.requirement] : [])).map(requirementLabel).join('; ') || t('shelfNode.noRequirement') }}<small>{{ condition(file.document.conditions) }}</small></td><td>{{ file.document.stage === 'draft' ? t('shelfModel.draft') : t('shelfModel.final') }} {{ file.document.version_label }}</td></tr></tbody>
          </table></div><p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section v-if="section === 'certifications'" id="node-certifications">
          <h2>{{ t('shelfModel.sections.cert') }}</h2>
          <template v-for="group in certGroups" :key="group.title"><div v-if="group.rows.length" class="cert-group"><h3 v-if="!isPart">{{ group.title }}</h3><div class="table-wrap"><table>
            <thead><tr><th>{{ t('shelfStatus.region') }}</th><th>{{ t('shelfModel.standard') }}</th><th>{{ t('shelfModel.from') }}</th><th>{{ t('shelfModel.progress') }}</th><th>{{ t('shelfModel.agency') }}</th><th>{{ t('shelfModel.issued_date') }}</th><th>{{ t('shelfModel.validity') }}</th><th>{{ t('shelfModel.number') }}</th></tr></thead>
            <tbody><tr v-for="cert in group.rows" :key="cert.entity.id"><td>{{ cert.certification.region }}</td><td><a :href="href(cert.entity.id)">{{ cert.standard_name }} {{ cert.certification.year || '' }}</a><small>{{ cert.certification.certification_object }}</small></td><td><template v-if="owner(cert.certification.owner_id)">{{ kind(owner(cert.certification.owner_id)!) }} <a :href="href(cert.certification.owner_id)">{{ owner(cert.certification.owner_id)!.canonical_name }}</a></template><small>{{ condition(cert.certification.conditions) }}</small><small v-if="cert.certification.cell_part_id">{{ t('shelfModel.part') }}: {{ owner(cert.certification.cell_part_id)?.canonical_name || cert.certification.cell_part_id }}</small></td><td>{{ cert.certification.progress }}</td><td>{{ cert.certification.agency }}</td><td>{{ cert.certification.issued_date }}</td><td>{{ cert.certification.validity }}</td><td><a v-if="safeExternalUrl(cert.certification.cloud_url)" :href="safeExternalUrl(cert.certification.cloud_url)" target="_blank" rel="noopener noreferrer">{{ cert.certification.number || t('shelfModel.cloudLink') }}</a><span v-else>{{ cert.certification.number }}</span></td></tr></tbody>
          </table></div></div></template><p v-if="!node.shelf?.certifications?.length">{{ t('productShelf.noRecords') }}</p>
        </section>
        </template>
      </template>
    </template>
  </article>
</template>

<style scoped>
.shelf-node { display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; padding: var(--space-5) var(--space-6) var(--space-7); color: var(--text); }
h1, h2, h3, p, dl { margin: 0; }
h1 { margin-top: var(--space-4); font-family: var(--mono); font-size: var(--text-2xl); font-weight: 800; letter-spacing: -0.03em; overflow-wrap: anywhere; }
h2 { font-size: var(--text-xl); font-weight: 800; } h3 { font-size: var(--text-sm); }
a { color: inherit; text-decoration: underline; cursor: pointer; } a:hover { color: var(--danger); } a:active { opacity: 0.7; }
a:focus-visible, select:focus-visible { outline: 2px solid var(--text); outline-offset: 3px; }
.crumb { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); font-size: var(--text-sm); }
.kind { border: 1px solid var(--text); padding-inline: 4px; font-size: var(--text-xs); font-weight: 700; }
.facts { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-top: 2px solid var(--rule); }
.facts > div { padding: var(--space-3) var(--space-3) var(--space-3) 0; overflow-wrap: anywhere; }
dt { font-size: var(--text-xs); font-weight: 700; } dd { margin: var(--space-1) 0 0; }
select { min-height: 40px; padding: 8px; border: 1px solid var(--border); background: var(--bg); color: var(--text); font: inherit; }
section, .cert-group { display: flex; flex-direction: column; gap: var(--space-3); min-width: 0; }
.table-wrap { overflow: auto; max-height: 560px; border-top: 2px solid var(--rule); }
table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: 8px 12px; border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; min-width: 100px; overflow-wrap: anywhere; }
thead th { position: sticky; top: 0; z-index: 1; background: var(--bg); font-size: var(--text-xs); border-bottom-color: var(--rule); }
.matrix td, .matrix thead th:not(:first-child) { text-align: center; min-width: 160px; }
.matrix td > a { display: block; } .matrix td > a + a, .sale + .sale { margin-top: 8px; }
small { display: block; font-size: var(--text-xs); color: var(--muted); white-space: pre-wrap; overflow-wrap: anywhere; }
.st, .doc { display: inline-block; padding: 1px 8px; font-size: var(--text-xs); font-weight: 800; }
.st-GA, .delivered { background: var(--success); color: var(--bg); } .st-DEV { background: var(--blue); color: var(--on-blue); }
.st-Planning, .draft_only { background: var(--surface-soft); } .st-EOM { background: var(--muted); color: var(--bg); }
.st-待定, .not_due { outline: 1px dashed var(--muted); outline-offset: -1px; }
.missing { background: var(--danger-soft); color: var(--danger); } [role=alert] { color: var(--danger); }
</style>
