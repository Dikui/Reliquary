<script setup lang="ts">
import { requirementLabel } from '@/utils/shelfRequirement'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState, Field, Input, Select, Tabs, Textarea } from '@/components/ui'
import { confirm } from '@/components/ui/feedback'
import { getCatalogLibrary, type EntityField, type LibraryCatalogItem, type ShelfOption, type ShelfParser } from '@/api/library'
import type { Entity } from '@/api/entities'
import { listModels, type ModelConfig } from '@/api/model'
import { configureShelf, shelfEntities, shelfWriteError } from '@/api/shelf-files'
import ShelfRequirementEditor from './ShelfRequirementEditor.vue'
import ShelfFieldEditor from './ShelfFieldEditor.vue'
const props = defineProps<{ library: LibraryCatalogItem }>()
const emit = defineEmits<{ saved: [] }>(), { t } = useI18n()
const tab = ref('modules'), library = ref<LibraryCatalogItem>(), nodes = ref<Entity[]>([]), loading = ref(true), busy = ref(false), error = ref(''), saveError = ref(''), newName = ref(''), baseline = ref('')
const requirement = ref<Entity | null>(), field = ref<EntityField | null>(), line = ref(''), sourceList = ref('')
const models = ref<ModelConfig[]>([]), modelError = ref('')
const editingOptions = ref(new Set<string>())
let generation = 0
async function loadModels() {
 modelError.value = ''
 try { models.value = (await listModels('KnowledgeQA')).filter(model => model.parameters.supports_vision) }
 catch { modelError.value = t('shelfFiles.loadFailed') }
}
watch(tab, value => { if (value === 'parsers') void loadModels() })
const config = computed(() => library.value?.shelf_config), dirty = computed(() => !!library.value && JSON.stringify({ config: config.value, schema: library.value.entity_schema }) !== baseline.value)
const tabs = computed(() => [{ value: 'modules', label: t('shelfSettings.modules') }, { value: 'regions', label: t('shelfSettings.regions') }, { value: 'stages', label: t('shelfSettings.stages') }, { value: 'requirements', label: t('shelfSettings.requirements') }, { value: 'fields', label: t('shelfSettings.fields') }, { value: 'parsers', label: t('shelfSettings.parsers') }])
const optionKey = computed(() => tab.value === 'modules' ? 'module_types' : tab.value === 'regions' ? 'regions' : 'stages')
const options = computed(() => config.value?.[optionKey.value] || [])
const lines = computed(() => nodes.value.filter(n => n.kind === 'product_line'))
const requirements = computed(() => nodes.value.filter(n => n.kind === 'document_requirement'))
const sources = computed(() => [...new Set(requirements.value.map(n => (n.attributes?.requirement as { list_name?: string })?.list_name || ''))].filter(Boolean))
const fields = computed(() => library.value?.entity_schema?.fields.filter(f => f.product_line_ids?.includes(line.value)) || [])
const key = (prefix: string) => `${prefix}_${crypto.randomUUID().replaceAll('-', '')}`
async function load() {
  const request = ++generation; loading.value = true; error.value = ''
  try {
    const [response, entities] = await Promise.all([getCatalogLibrary(props.library.id), shelfEntities(props.library.id)])
    if (!response.success) throw new Error(response.message)
    if (request !== generation) return
    library.value = JSON.parse(JSON.stringify(response.data))
    const value = library.value!; value.shelf_config ||= {}; value.shelf_config.module_types ||= []; value.shelf_config.regions ||= []; value.shelf_config.stages ||= []; value.shelf_config.parsers ||= []; value.shelf_config.parser_rules ||= []; value.entity_schema ||= { kinds: [], fields: [] }
    nodes.value = entities; editingOptions.value.clear(); if (!line.value) line.value = lines.value[0]?.id || ''
    baseline.value = JSON.stringify({ config: config.value, schema: value.entity_schema })
  } catch { if (request === generation) error.value = t('shelfFiles.loadFailed') }
  finally { if (request === generation) loading.value = false }
}
async function allowLeave() { if (busy.value) return false; if (!dirty.value) return true; return confirm({ header: t('shelfSettings.unsaved'), confirmBtn: t('shelfSettings.leave'), cancelBtn: t('common.cancel') }).result }
onBeforeRouteLeave(allowLeave)
onBeforeRouteUpdate(async (to, from) => to.query.tab === from.query.tab && to.query.libraries === from.query.libraries || await allowLeave())
function addOption() { if (!newName.value.trim()) return; options.value.push({ key: key('option'), name: newName.value }); newName.value = '' }
function move(list: ShelfOption[] | unknown[], index: number, step: number) { const target = index + step; if (target < 0 || target >= list.length) return; [list[index], list[target]] = [list[target]!, list[index]!] }
async function save() {
  if (!library.value || busy.value) return
  if (!library.value.updated_at || [...config.value?.module_types || [], ...config.value?.regions || [], ...config.value?.stages || []].some(option => !option.name.trim())) { saveError.value = t('shelfSettings.invalid'); return }
  busy.value = true; saveError.value = ''
  try {
    const response = await configureShelf(library.value.id, library.value.updated_at, { shelf_config: config.value, entity_schema: library.value.entity_schema })
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    await load(); emit('saved')
  } catch (cause) { saveError.value = shelfWriteError(cause, t('shelfSettings.saveFailed')) }
  finally { busy.value = false }
}
function addParser() { config.value?.parsers?.push({ key: key('parser'), name: '', type: 'none' }) }
function parserType(parser: ShelfParser, type: string) {
  const position = config.value!.parsers!.indexOf(parser)
  config.value!.parsers![position] = { key: parser.key, name: parser.name, type: type as ShelfParser['type'], ...(type === 'http' ? { url: '', timeout_seconds: 120, concurrency: 1 } : type === 'vision' ? { model_id: '', render_dpi: 144, prompt: '' } : {}) }
}
function requirementMetadata(entity: Entity) { return (entity.attributes?.requirement || {}) as Record<string, string> }
async function refreshed() { requirement.value = undefined; field.value = undefined; await load(); emit('saved') }
watch(() => props.library.id, load, { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>
<template>
  <div class="rq-shelf-settings">
    <header class="rq-shelf-row"><h1>{{ t('shelfSettings.title') }}</h1><Button v-if="!loading && !error" variant="primary" :loading="busy" :disabled="!dirty" @click="save">{{ t('shelfSettings.save') }}</Button></header>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <ErrorState v-else-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
    <template v-else-if="library && config">
      <p v-if="saveError" role="alert" class="rq-shelf-error">{{ saveError }} <Button :disabled="busy" @click="load">{{ t('shelfFiles.reload') }}</Button></p>
      <Tabs v-model="tab" :items="tabs" :label="t('shelfSettings.title')">
        <template v-for="section in ['modules', 'regions', 'stages']" :key="section" #[section]>
          <p v-if="section === 'stages'" class="rq-shelf-statuses"><b>{{ t('shelfSettings.fixedStatuses') }}</b> GA · DEV · Planning · EOM · {{ t('shelfSettings.pendingStatus') }}</p>
          <div class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfSettings.name') }}</th><th v-if="section === 'modules'">{{ t('shelfSettings.partCount') }}</th><th v-if="section === 'modules'">{{ t('shelfSettings.extraFields') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody><tr v-for="(option, index) in options" :key="option.key"><td><Input v-if="editingOptions.has(option.key)" v-model="option.name" :aria-label="t('shelfSettings.name')" :disabled="busy" /><b v-else>{{ option.name }}</b></td><td v-if="section === 'modules'">{{ nodes.filter(n => n.kind === 'part' && n.attributes?.module_type === option.key).length }}</td><td v-if="section === 'modules'">{{ library.entity_schema?.fields.filter(f => f.kinds.includes('part') && !f.archived).map(f => f.name).join(' · ') }}</td><td><Button :disabled="busy || editingOptions.has(option.key)" @click="editingOptions.add(option.key)">{{ t('shelfSettings.edit') }}</Button><Button :disabled="busy || index === 0" @click="move(options, index, -1)">{{ t('shelfSettings.up') }}</Button><Button :disabled="busy || index + 1 === options.length" @click="move(options, index, 1)">{{ t('shelfSettings.down') }}</Button></td></tr></tbody></table></div>
          <form novalidate class="rq-shelf-row rq-shelf-add" @submit.prevent="addOption"><Input v-model="newName" :aria-label="t('shelfSettings.newName')" :placeholder="t('shelfSettings.newName')" :disabled="busy" /><Button type="submit" :disabled="busy || !newName.trim()">{{ t('shelfSettings.add') }}</Button></form>
        </template>
        <template #requirements>
          <div class="rq-shelf-row"><Select v-model="sourceList" :aria-label="t('shelfSettings.sourceList')"><option value="">{{ t('shelfSettings.allLists') }}</option><option v-for="source in sources" :key="source" :value="source">{{ source }}</option></Select><Button :disabled="dirty || busy" @click="requirement = null">{{ t('shelfSettings.newRequirement') }}</Button></div>
          <div class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfSettings.name') }}</th><th>{{ t('shelfSettings.draftDue') }}</th><th>{{ t('shelfSettings.finalDue') }}</th><th>{{ t('shelfSettings.owner') }}</th><th>{{ t('shelfSettings.sourceList') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody><tr v-for="node in requirements.filter(n => !sourceList || requirementMetadata(n).list_name === sourceList)" :key="node.id"><td :title="requirementMetadata(node).name_en">{{ node.canonical_name }}</td><td>{{ config.stages?.find(s => s.key === requirementMetadata(node).draft_due_stage)?.name || t('shelfFiles.none') }}</td><td>{{ config.stages?.find(s => s.key === requirementMetadata(node).final_due_stage)?.name || t('shelfFiles.untitled') }}</td><td>{{ requirementMetadata(node).owner }}</td><td>{{ requirementMetadata(node).list_name }}<small>{{ requirementMetadata(node).source_list }} {{ requirementMetadata(node).source_version }}</small></td><td><Button :disabled="dirty || busy" @click="requirement = node">{{ t('shelfSettings.edit') }}</Button></td></tr></tbody></table></div>
        </template>
        <template #fields>
          <div class="rq-shelf-row"><Select v-model="line" :aria-label="t('shelfSettings.line')"><option v-for="node in lines" :key="node.id" :value="node.id">{{ node.canonical_name }}</option></Select><Button :disabled="!line || dirty || busy" @click="field = null">{{ t('shelfSettings.newField') }}</Button></div>
          <div class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfSettings.name') }}</th><th>{{ t('shelfSettings.type') }}</th><th>{{ t('shelfSettings.unit') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody><tr v-for="value in fields" :key="value.key"><td>{{ value.name }} <span v-if="value.archived">{{ t('shelfSettings.archived') }}</span></td><td>{{ value.type }}</td><td>{{ value.unit }}</td><td><Button :disabled="dirty || busy" @click="field = value">{{ t('shelfSettings.edit') }}</Button></td></tr></tbody></table></div>
          <EmptyState v-if="!lines.length" :title="t('shelfSettings.empty')" />
        </template>
        <template #parsers>
          <p v-if="modelError" role="alert">{{ modelError }} <Button @click="loadModels">{{ t('common.retry') }}</Button></p>
          <div class="rq-shelf-parser-head"><Field :label="t('shelfSettings.defaultParser')" id="shelf-default-parser"><Select id="shelf-default-parser" v-model="config.default_parser" :disabled="busy"><option value="">{{ t('shelfFiles.none') }}</option><option v-for="parser in config.parsers" :key="parser.key" :value="parser.key">{{ parser.name }}</option></Select></Field><Field :label="t('shelfSettings.maxSize')" id="shelf-max-file" :hint="t('shelfSettings.useDefaultSize')"><Input id="shelf-max-file" type="number" :model-value="(config.max_file_bytes || 0) / 1048576" min="0" step="1" :disabled="busy" @update:model-value="config.max_file_bytes = Number($event) * 1048576" /></Field></div>
          <fieldset v-for="parser in config.parsers" :key="parser.key" class="rq-shelf-parser" :disabled="busy"><legend>{{ parser.name || t('shelfSettings.addParser') }}</legend><div class="rq-shelf-grid"><Field :label="t('shelfSettings.name')" :id="`parser-${parser.key}-name`"><Input :id="`parser-${parser.key}-name`" v-model="parser.name" required /></Field><Field :label="t('shelfSettings.type')" :id="`parser-${parser.key}-type`"><Select :id="`parser-${parser.key}-type`" :model-value="parser.type" @update:model-value="parserType(parser, $event)"><option value="none">{{ t('shelfSettings.none') }}</option><option value="http">{{ t('shelfSettings.http') }}</option><option value="vision">{{ t('shelfSettings.vision') }}</option></Select></Field>
            <template v-if="parser.type === 'http'"><Field :label="t('shelfSettings.url')" :id="`parser-${parser.key}-url`"><Input :id="`parser-${parser.key}-url`" v-model="parser.url" type="url" /></Field><Field :label="t('shelfSettings.authorization')" :id="`parser-${parser.key}-auth`"><Input :id="`parser-${parser.key}-auth`" v-model="parser.authorization" type="password" autocomplete="new-password" /></Field><Field :label="t('shelfSettings.timeout')" :id="`parser-${parser.key}-timeout`"><Input :id="`parser-${parser.key}-timeout`" type="number" :model-value="parser.timeout_seconds" min="1" max="3600" @update:model-value="parser.timeout_seconds = Number($event)" /></Field><Field :label="t('shelfSettings.concurrency')" :id="`parser-${parser.key}-concurrency`"><Input :id="`parser-${parser.key}-concurrency`" type="number" :model-value="parser.concurrency" min="1" max="100" @update:model-value="parser.concurrency = Number($event)" /></Field></template>
            <template v-if="parser.type === 'vision'"><Field :label="t('shelfSettings.model')" :id="`parser-${parser.key}-model`"><Select :id="`parser-${parser.key}-model`" v-model="parser.model_id"><option value="" disabled>{{ t('shelfSettings.model') }}</option><option v-for="model in models" :key="model.id" :value="model.id">{{ model.display_name || model.name }}</option></Select></Field><Field :label="t('shelfSettings.dpi')" :id="`parser-${parser.key}-dpi`"><Input :id="`parser-${parser.key}-dpi`" type="number" :model-value="parser.render_dpi" min="72" max="600" @update:model-value="parser.render_dpi = Number($event)" /></Field><Field :label="t('shelfSettings.prompt')" :id="`parser-${parser.key}-prompt`"><Textarea :id="`parser-${parser.key}-prompt`" v-model="parser.prompt" /></Field></template>
          </div><Button variant="danger" @click="config.parsers?.splice(config.parsers.indexOf(parser), 1)">{{ t('shelfFiles.remove') }}</Button></fieldset>
          <Button :disabled="busy" @click="addParser">{{ t('shelfSettings.addParser') }}</Button>
          <h2>{{ t('shelfSettings.rules') }}</h2><div class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfSettings.extension') }}</th><th>{{ t('shelfFiles.requirement') }}</th><th>{{ t('shelfFiles.category') }}</th><th>{{ t('shelfFiles.parser') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody><tr v-for="(rule, index) in config.parser_rules" :key="index"><td><Input v-model="rule.extension" :aria-label="t('shelfSettings.extension')" :disabled="busy" /></td><td><Select v-model="rule.requirement_id" :aria-label="t('shelfFiles.requirement')" :disabled="busy"><option value="">{{ t('shelfFiles.all') }}</option><option v-for="node in requirements" :key="node.id" :value="node.id">{{ requirementLabel(node) }}</option></Select></td><td><Input v-model="rule.file_category" :aria-label="t('shelfFiles.category')" :disabled="busy" /></td><td><Select v-model="rule.parser_key" :aria-label="t('shelfFiles.parser')" :disabled="busy"><option v-for="p in config.parsers" :key="p.key" :value="p.key">{{ p.name }}</option></Select></td><td><Button :disabled="busy || index === 0" @click="move(config.parser_rules!, index, -1)">{{ t('shelfSettings.up') }}</Button><Button :disabled="busy || index + 1 === config.parser_rules!.length" @click="move(config.parser_rules!, index, 1)">{{ t('shelfSettings.down') }}</Button><Button :disabled="busy" @click="config.parser_rules?.splice(index, 1)">{{ t('shelfFiles.remove') }}</Button></td></tr></tbody></table></div><Button :disabled="busy || !config.parsers?.length" @click="config.parser_rules?.push({ parser_key: config.default_parser || config.parsers![0]!.key, extension: '', requirement_id: '', file_category: '' })">{{ t('shelfSettings.addRule') }}</Button>
        </template>
      </Tabs>
      <ShelfRequirementEditor v-if="requirement !== undefined" :library="library" :entity="requirement || undefined" :nodes="nodes" @close="requirement = undefined" @saved="refreshed" />
      <ShelfFieldEditor v-if="field !== undefined" :library="library" :field="field || undefined" :line-id="line" @close="field = undefined" @saved="refreshed" />
    </template>
  </div>
</template>
<style scoped>
.rq-shelf-settings { display: flex; flex-direction: column; gap: var(--space-4); padding: var(--space-5) var(--space-6); min-width: 900px; }
h1 { font-size: var(--text-3xl); font-weight: 800; } h2 { font-size: var(--text-xl); margin-top: var(--space-4); }
.rq-shelf-row { display: flex; justify-content: space-between; gap: var(--space-3); align-items: center; } .rq-shelf-add { max-width: 480px; margin-top: var(--space-4); }
.rq-shelf-table { overflow: auto; border-top: 2px solid var(--rule); margin: var(--space-4) 0; } table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); } th, td { text-align: left; padding: var(--space-3); border-bottom: 1px solid var(--border); } th { font-size: var(--text-xs); } td input { min-width: 160px; }
.rq-shelf-statuses { padding: var(--space-4) 0; } .rq-shelf-parser-head, .rq-shelf-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-4); } .rq-shelf-parser { padding: var(--space-4); margin: var(--space-4) 0; border: 1px solid var(--border); } .rq-shelf-parser > button { margin-top: var(--space-4); }
.rq-shelf-error { color: var(--danger); }
</style>
