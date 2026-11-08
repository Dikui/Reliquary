<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState, Select } from '@/components/ui'
import { confirm } from '@/components/ui/feedback'
import { entityHistory, getEntity, getEntityAttachment, type Entity, type EntityAttachment } from '@/api/entities'
import { getEntryEditableBody, type LibraryCatalogItem } from '@/api/library'
import { listShelfVersions, shelfVersionDownload, shelfWriteError, writeShelf } from '@/api/shelf-files'
import type { ShelfRequirement, ShelfDocumentVersion } from '@/api/shelf'
import type { OperationLog } from '@/api/operation-logs'
import { safeExternalUrl } from '@/utils/externalUrl'
import { requirementLabel } from '@/utils/shelfRequirement'
import ShelfDocumentEditor from './ShelfDocumentEditor.vue'
const props = defineProps<{ library: LibraryCatalogItem; fileId: string }>()
const route = useRoute(), router = useRouter(), { t } = useI18n()
const file = ref<Entity>(), owner = ref<Entity>(), requirements = ref<Entity[]>([]), versions = ref<ShelfDocumentVersion[]>([]), selected = ref(''), parser = ref('')
const loading = ref(false), busy = ref(false), error = ref(''), writeError = ref(''), body = ref(''), bodyError = ref(''), textLoading = ref(false), editor = ref<'' | 'upload' | 'classify'>('')
const originals = ref<Record<string, EntityAttachment>>({}), originalError = ref('')
const partNames = ref<Record<string, string>>({})
const requirementMetadata = (requirement: Entity) => (requirement.attributes?.requirement || {}) as ShelfRequirement
const dueStage = (key?: string) => props.library.shelf_config?.stages?.find(stage => stage.key === key)?.name || ''
const conditionText = computed(() => version.value?.conditions?.map(c => `${props.library.shelf_config?.module_types?.find(module => module.key === c.module_type)?.name || c.module_type} ∈ {${c.part_ids.map(id => partNames.value[id] || id).join(', ')}}`).join(' · ') || t('shelfFiles.allOptions'))
const operationLabels = computed(() => ({ update_entity: t('operationLogs.types.update_entity'), create_entity: t('operationLogs.types.create_entity'), reparse_document: t('operationLogs.types.reparse_document'), classify_document: t('operationLogs.types.classify_document'), select_document_version: t('operationLogs.types.select_document_version') }))
const logs = ref<OperationLog[]>([]), logPage = ref(1), logTotal = ref(0), logError = ref('')
let generation = 0, textGeneration = 0, historyGeneration = 0, controller: AbortController | undefined
const current = computed(() => versions.value.find(v => v.is_current)), version = computed(() => versions.value.find(v => v.id === selected.value))
const statuses = computed(() => ({ pending: t('shelfFiles.pending'), parsing: t('shelfFiles.parsingStatus'), retrying: t('shelfFiles.retrying'), failed: t('shelfFiles.failed'), completed: t('shelfFiles.completed'), not_parsed: t('shelfFiles.not_parsed') }))
const source = (value?: { type?: string; agent_name?: string; import_batch_id?: string }) => value?.type === 'agent' ? value.agent_name : value?.type === 'import' ? `${t('shelfImport.title')} ${value.import_batch_id || ''}` : t('shelfFiles.web')
async function load() {
  const request = ++generation; loading.value = true; error.value = ''
  try {
    const [detail, rows] = await Promise.all([getEntity(props.fileId), listShelfVersions(props.fileId)])
    if (!detail.success || !rows.success || detail.data.entity.library_id && detail.data.entity.library_id !== props.library.id) throw new Error()
    if (request !== generation) return
    file.value = detail.data.entity; versions.value = rows.data
    if (!versions.value.some(v => v.id === selected.value)) selected.value = current.value?.id || versions.value[0]?.id || ''
    const classification = current.value
    const idsForRequirements = classification?.requirement_ids || (classification?.requirement_id ? [classification.requirement_id] : [])
    const [ownerResponse, requirementResponses] = await Promise.all([classification?.owner_id ? getEntity(classification.owner_id) : undefined, Promise.all(idsForRequirements.map(id => getEntity(id)))])
    if (request !== generation) return
    if (ownerResponse && !ownerResponse.success || requirementResponses.some(response => !response.success)) throw new Error()
    const ids = [...new Set(versions.value.flatMap(v => v.conditions?.flatMap(c => c.part_ids) || []))]
    const attachmentIDs = [...new Set(versions.value.map(v => v.attachment_id).filter((id): id is string => !!id))]
    const [parts, attachments] = await Promise.all([Promise.all(ids.map(id => getEntity(id))), Promise.allSettled(attachmentIDs.map(id => getEntityAttachment(id)))])
    if (request !== generation) return
    originals.value = Object.fromEntries(attachments.flatMap(result => result.status === 'fulfilled' && result.value.success ? [[result.value.data.id, result.value.data]] : []))
    originalError.value = attachments.some(result => result.status === 'rejected' || !result.value.success) ? t('shelfFiles.loadFailed') : ''
    partNames.value = Object.fromEntries(parts.filter(response => response.success).map(response => [response.data.entity.id, response.data.entity.canonical_name]))
    owner.value = ownerResponse?.success ? ownerResponse.data.entity : undefined; requirements.value = requirementResponses.filter(response => response.success).map(response => response.data.entity)
    await loadText()
  } catch { if (request === generation) error.value = t('shelfFiles.loadFailed') }
  finally { if (request === generation) loading.value = false }
}
async function loadText() {
  const request = ++textGeneration; controller?.abort(); controller = new AbortController(); body.value = ''; bodyError.value = ''; textLoading.value = false
  parser.value = version.value?.parser_key || props.library.shelf_config?.default_parser || ''
  if (!version.value?.entry_id) return
  textLoading.value = true
  try { const text = await getEntryEditableBody(version.value.entry_id, controller.signal); if (request === textGeneration) body.value = text.content }
  catch { if (request === textGeneration) bodyError.value = t('shelfFiles.loadFailed') }
  finally { if (request === textGeneration) textLoading.value = false }
}
async function loadHistory() {
  const request = ++historyGeneration; logError.value = ''
  try { const response = await entityHistory(props.fileId, logPage.value); if (!response.success) throw new Error(); if (request === historyGeneration) { logs.value = response.data.items; logTotal.value = response.data.total } }
  catch { if (request === historyGeneration) logError.value = t('shelfFiles.loadFailed') }
}
async function write(type: 'reparse_document' | 'select_document_version', id: string) {
  if (!file.value || busy.value) return
  if (!await confirm({ header: type === 'reparse_document' ? t('shelfFiles.reparse') : t('shelfFiles.confirmCurrent'), body: type === 'reparse_document' ? t('shelfFiles.reparseHint') : undefined, confirmBtn: t('shelfFiles.save'), cancelBtn: t('common.cancel') }).result) return
  busy.value = true; writeError.value = ''
  try {
    const response = await writeShelf(props.library.id, [{ id: 'version', type, entity_id: file.value.id, revision: file.value.revision, ...(type === 'reparse_document' ? { reparse: { version_id: id, parser_key: parser.value } } : { version_id: id }) }])
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    await Promise.all([load(), loadHistory()])
  } catch (cause) { writeError.value = shelfWriteError(cause, t('shelfFiles.saveFailed')) }
  finally { busy.value = false }
}
function back() { const query = { ...route.query }; delete query.file; void router.replace({ query }) }
function saved() { editor.value = ''; void load(); void loadHistory() }
watch(selected, loadText)
watch(() => props.fileId, () => { selected.value = ''; logPage.value = 1; void load(); void loadHistory() }, { immediate: true })
onBeforeUnmount(() => { generation++; textGeneration++; historyGeneration++; controller?.abort() })
</script>
<template>
  <article class="rq-shelf-file">
    <nav><a :href="`/platform/entries?libraries=${encodeURIComponent(library.id)}&tab=files`" @click.prevent="back">{{ t('shelfFiles.back') }}</a><template v-if="owner"> · {{ t('shelfFiles.owner') }} <a :href="`/platform/entities?libraries=${encodeURIComponent(library.id)}&entity_id=${encodeURIComponent(owner.id)}`">{{ owner.canonical_name }}</a></template></nav>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <ErrorState v-else-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
    <template v-else-if="file">
      <header class="rq-shelf-row"><h1>{{ file.canonical_name }}</h1><div><Button variant="primary" :disabled="busy" @click="editor = 'upload'">{{ t('shelfFiles.newVersion') }}</Button><Button :disabled="busy" @click="editor = 'classify'">{{ t('shelfFiles.classify') }}</Button></div></header>
      <p v-if="writeError" role="alert" class="rq-shelf-error">{{ writeError }} <Button @click="load">{{ t('shelfFiles.reload') }}</Button></p>
      <dl class="rq-shelf-facts"><div><dt>{{ t('shelfFiles.currentVersion') }}</dt><dd>{{ current?.version_label }} {{ current?.stage === 'final' ? t('shelfFiles.final') : t('shelfFiles.draft') }}</dd></div><div><dt>{{ t('shelfFiles.owner') }}</dt><dd>{{ owner?.canonical_name || t('shelfFiles.unassigned') }}</dd></div></dl>
      <p v-if="!requirements.length">{{ t('shelfFiles.requirement') }}: {{ t('shelfFiles.none') }}</p>
      <dl v-for="requirement in requirements" :key="requirement.id" class="rq-shelf-facts">
        <div><dt>{{ t('shelfFiles.requirement') }}</dt><dd>{{ requirementLabel(requirement) }}</dd></div>
        <div><dt>{{ t('shelfFiles.due') }}</dt><dd><span v-if="requirementMetadata(requirement).draft_due_stage">{{ t('shelfFiles.draft') }} {{ dueStage(requirementMetadata(requirement).draft_due_stage) }} · </span>{{ t('shelfFiles.final') }} {{ dueStage(requirementMetadata(requirement).final_due_stage) }} {{ requirementMetadata(requirement).due_text }}</dd></div>
        <div v-if="requirementMetadata(requirement).markets"><dt>{{ t('shelfFiles.markets') }}</dt><dd v-for="(necessity, region) in requirementMetadata(requirement).markets" :key="region">{{ library.shelf_config?.regions?.find(value => value.key === region)?.name || region }} · {{ necessity === 'required' ? t('shelfSettings.required') : necessity === 'optional' ? t('shelfSettings.optional') : t('shelfSettings.not_needed') }}</dd></div>
      </dl>
      <section><h2>{{ t('shelfFiles.version') }}</h2><p v-if="originalError" role="alert">{{ originalError }} <Button @click="load">{{ t('common.retry') }}</Button></p><div class="rq-shelf-table"><table><thead><tr><th>{{ t('shelfFiles.version') }}</th><th>{{ t('shelfFiles.stage') }}</th><th>{{ t('shelfFiles.original') }}</th><th>{{ t('shelfFiles.text') }}</th><th>{{ t('shelfFiles.source') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody>
        <tr v-for="row in versions" :key="row.id"><td><Button variant="ghost" :aria-pressed="selected === row.id" @click="selected = row.id">{{ row.version_label }}</Button> <b v-if="row.is_current">{{ t('shelfFiles.current') }}</b></td><td>{{ row.stage === 'final' ? t('shelfFiles.final') : t('shelfFiles.draft') }}</td><td><a v-if="row.attachment_id" :href="shelfVersionDownload(file.id, row.id)">{{ originals[row.attachment_id]?.name || t('shelfFiles.download') }}</a><span v-if="row.attachment_id && originals[row.attachment_id]"> · {{ (originals[row.attachment_id]!.size / 1048576).toFixed(2) }} MiB</span> <a v-if="safeExternalUrl(row.cloud_url)" :href="safeExternalUrl(row.cloud_url)" target="_blank" rel="noopener noreferrer">{{ t('shelfFiles.openLink') }}</a></td><td>{{ library.shelf_config?.parsers?.find(p => p.key === row.parser_key)?.name }} · {{ statuses[row.parse_status as keyof typeof statuses] || row.parse_status }}<p v-if="row.parse_error" class="rq-shelf-error">{{ row.parse_error }}</p></td><td>{{ source(row.source) }} · {{ row.occurred_at?.slice(0, 19) }}<p>{{ row.origin }}</p></td><td><Button v-if="!row.is_current" :disabled="busy" @click="write('select_document_version', row.id)">{{ t('shelfFiles.makeCurrent') }}</Button></td></tr>
      </tbody></table></div></section>
      <p>{{ t('shelfFiles.conditions') }}: {{ conditionText }}</p>
      <section v-if="version"><h2>{{ t('shelfFiles.text') }} · {{ version.version_label }}</h2><p v-if="textLoading" role="status">{{ t('common.loading') }}</p><p v-else-if="bodyError" role="alert">{{ bodyError }} <Button @click="loadText">{{ t('common.retry') }}</Button></p><pre v-else-if="body" class="rq-shelf-markdown">{{ body }}</pre><EmptyState v-else :title="t('shelfFiles.noText')" />
        <div v-if="version.attachment_id && library.shelf_config?.parsers?.length" class="rq-shelf-parser"><Select v-model="parser" :aria-label="t('shelfFiles.parser')" :disabled="busy"><option value="" disabled>{{ t('shelfFiles.parser') }}</option><option v-for="p in library.shelf_config.parsers" :key="p.key" :value="p.key">{{ p.name }}</option></Select><Button :disabled="busy || !parser" @click="write('reparse_document', version.id)">{{ t('shelfFiles.reparse') }}</Button></div>
      </section>
      <section><h2>{{ t('shelfFiles.history') }}</h2><p v-if="logError" role="alert">{{ logError }} <Button @click="loadHistory">{{ t('common.retry') }}</Button></p><ul v-else class="rq-shelf-history"><li v-for="log in logs" :key="log.id"><time>{{ log.created_at?.slice(0, 19) }}</time><b>{{ source(log.source) }}</b><span>{{ operationLabels[log.operation_type as keyof typeof operationLabels] || log.operation_type }}</span></li></ul><div class="rq-shelf-row"><span>{{ t('shelfFiles.total', { count: logTotal }) }}</span><div><Button :disabled="logPage === 1" @click="logPage--; loadHistory()">{{ t('shelfFiles.previous') }}</Button><Button :disabled="logPage * 20 >= logTotal" @click="logPage++; loadHistory()">{{ t('shelfFiles.next') }}</Button></div></div></section>
      <ShelfDocumentEditor v-if="editor && current" :key="editor" :library="library" :file="file" :current="current" :classify="editor === 'classify'" @close="editor = ''" @saved="saved" />
    </template>
  </article>
</template>
<style scoped>
.rq-shelf-file { padding: var(--space-5) var(--space-6); display: flex; flex-direction: column; gap: var(--space-5); min-width: 900px; }
h1 { font: 800 var(--text-2xl)/1.2 var(--mono); overflow-wrap: anywhere; }
h2 { font-size: var(--text-xl); margin-bottom: var(--space-3); }
.rq-shelf-row, .rq-shelf-parser { display: flex; gap: var(--space-3); justify-content: space-between; align-items: center; }
.rq-shelf-parser { justify-content: flex-start; } .rq-shelf-parser select { width: 240px; }
.rq-shelf-facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border-top: 2px solid var(--rule); margin: 0; }
.rq-shelf-facts > div { padding: var(--space-3) 0; } dt { font-size: var(--text-xs); font-weight: 800; } dd { margin: var(--space-2) 0 0; }
.rq-shelf-table { overflow: auto; border-top: 2px solid var(--rule); } table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); } th, td { text-align: left; padding: var(--space-3); border-bottom: 1px solid var(--border); } th { font-size: var(--text-xs); }
.rq-shelf-markdown { max-height: 352px; padding: var(--space-4); overflow: auto; background: var(--surface-soft); font-family: var(--mono); font-size: var(--text-xs); white-space: pre-wrap; margin: 0 0 var(--space-3); }
.rq-shelf-history { list-style: none; padding: 0; border-top: 2px solid var(--rule); } .rq-shelf-history li { display: grid; grid-template-columns: 170px 150px 1fr; gap: var(--space-3); padding: var(--space-3) 0; border-bottom: 1px solid var(--border); font-size: var(--text-sm); }
.rq-shelf-error { color: var(--danger); }
</style>
