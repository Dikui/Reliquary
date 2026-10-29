<script setup lang="ts">
import { contentTypeLabel } from '@/utils/contentTypeLabel'
import { formatDateTime } from '@/utils/dateTime'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteUpdate, onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, EmptyState, ErrorState, LibMark, Select, Skeleton, StatusSquare, Tabs, Tag, confirm, toast } from '@/components/ui'
import DocContent from '@/components/DocContent.vue'
import FollowToggle from '@/components/FollowToggle.vue'
import RepostSources from './components/RepostSources.vue'
import { getEntryDetails, getEntryEditableBody, getEntrySpans, getCatalogLibrary, reparseEntry, cancelEntryParse, delEntryDetails, deleteDocument } from '@/api/library'
import { getEntity } from '@/api/entities'
import { listEntityRelations } from '@/api/relations'
import { correctEntryContentType, setEntryStar } from '@/api/entry-judgments'
import { entryDetailLocation, parseReference } from '@/utils/bodyReference'
import { entryProcessingState } from '@/utils/entryProcessingStatus'
import { entryChannelLabel, entryFileFormatLabel, getExternalToolName } from '@/utils/entrySourceLabels'
import { safeExternalUrl } from '@/utils/externalUrl'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import { processingSpanLabel } from '@/utils/processingSpanLabel'
import { useEntryDetail } from './useEntryDetail'
import type { SourceLocation } from '@/types/search'
import type { ConfirmHandle, ConfirmOptions } from '@/components/ui'
import type { EntryTraceNode } from '@/utils/entryTrace'

const route = useRoute(), router = useRouter(), { t, locale } = useI18n()
const id = computed(() => String(route.params.id || ''))
const state = useEntryDetail(id, { details: getEntryDetails, body: getEntryEditableBody, spans: getEntrySpans, library: getCatalogLibrary, entity: getEntity, relations: listEntityRelations })
const { entry, library, body, bodyHash, bodyReady, loading, error, bodyError, trace, traceError, entities, relations, relationsLoading, relationsError } = state
const content = ref<InstanceType<typeof DocContent>>()
const busy = ref(false), actionError = ref(''), typeOpen = ref(false), typeDraft = ref('')
const imagePreview = ref<{ src: string; alt: string }>()
const editing = ref(false), editButton = ref<InstanceType<typeof Button>>()
function editingChanged(value: boolean) {
  editing.value = value
  if (!value) void nextTick(() => editButton.value?.$el?.focus())
}
const reference = computed(() => parseReference(route.query.reference))
const tab = computed({ get: () => ['summary', 'entities', 'relations', 'processing'].includes(String(route.query.tab)) ? String(route.query.tab) : 'summary', set: value => { void router.replace({ query: { ...route.query, tab: value } }) } })
const tabs = computed(() => [
  { value: 'summary', label: t('entries.summary') },
  { value: 'entities', label: t('entries.entityCount', { count: entities.value.length }) },
  { value: 'relations', label: t('entries.relationCount', { count: relations.value.length }) },
  { value: 'processing', label: t('entries.processing') },
])
const processing = computed(() => entryProcessingState(entry.value || {}))
const renderDetails = computed(() => ({ ...entry.value, md: bodyReady.value ? [{ content: body.value, start_at: 0, end_at: body.value.length }] : [], total: bodyReady.value ? 1 : 0 }))
const title = computed(() => entry.value?.title || entry.value?.file_name || t('library.untitledDocument'))
const listLocation = computed(() => ({ path: '/platform/entries', query: entry.value?.library_id ? { libraries: entry.value.library_id } : {} }))
const source = computed(() => entry.value?.metadata?.intake?.source_info?.original_url || entry.value?.source || '')
const sourceURL = computed(() => safeExternalUrl(source.value))
const created = computed(() => {
  const value = entry.value?.created_at
  return formatDateTime(value, locale.value, { dateStyle: 'medium', timeStyle: 'short' }, t('entries.unknown'))
})
const plain = (value: string) => markdownToPlainText(value, `[${t('ingestStatus.missingTypes.image')}]`)
const typeOptions = computed(() => entry.value?.library_content_types?.filter(option => option.enabled) || [])
const steps = computed(() => {
  const result: Array<{ node: EntryTraceNode; depth: number }> = []
  function visit(node: EntryTraceNode, depth: number) {
    if (!['pipeline', 'root'].includes(node.kind)) result.push({ node, depth })
    for (const child of node.children || []) visit(child, ['pipeline', 'root'].includes(node.kind) ? depth : depth + 1)
  }
  if (trace.value) visit(trace.value, 0)
  return result
})
function stepLabel(node: EntryTraceNode) {
  const stageKeys: Record<string, string> = { docreader: 'processingStages.stage.docreader', processing: 'processingStages.stage.processing', chunking: 'processingStages.stage.chunking', embedding: 'processingStages.stage.embedding' }
  return stageKeys[node.name] ? t(stageKeys[node.name]!) : processingSpanLabel(node.name, t)
}
function stepStatus(status: string) {
  const keys: Record<string, string> = { pending: 'processingStages.status.pending', running: 'processingStages.status.running', done: 'processingStages.status.done', failed: 'processingStages.status.failed', skipped: 'processingStages.status.skipped', cancelled: 'processingStages.status.cancelled', finalizing: 'processingStages.status.finalizing' }
  return keys[status] ? t(keys[status]!) : status
}
const modelName = (node: EntryTraceNode) => {
  const metadata = node.metadata as Record<string, unknown> | undefined
  return typeof metadata?.model_name === 'string' ? metadata.model_name : typeof metadata?.model_id === 'string' ? metadata.model_id : ''
}
async function mutate(action: () => Promise<unknown>) {
  if (busy.value) return
  busy.value = true; actionError.value = ''
  try { await action() }
  catch (cause: any) { actionError.value = cause?.message || t('common.operationFailed'); throw cause }
  finally { busy.value = false }
}
async function toggleStar() {
  const target = entry.value
  if (!target || busy.value || editing.value) return
  try { await mutate(async () => {
    const response = await setEntryStar(target.id, !target.starred)
    if (!response.success) throw new Error(response.message)
    if (id.value !== target.id) return
    entry.value = { ...target, ...response.data, starred: !target.starred }
    toast.success(t(entry.value.starred ? 'articleJudgments.starred' : 'articleJudgments.unstarred'))
  }) } catch { /* Keep the inline error available for retry. */ }
}
async function saveType() {
  if (!entry.value || busy.value || !typeDraft.value) return
  const target = entry.value.id
  try { await mutate(async () => {
    const response = await correctEntryContentType(target, typeDraft.value)
    if (!response.success) throw new Error(response.message)
    if (id.value !== target) return
    entry.value = { ...entry.value!, ...response.data }
    typeOpen.value = false
    toast.success(t('articleJudgments.contentTypeSaved'))
  }) } catch { /* Retain the selection and error in the dialog. */ }
}
let activeConfirm: ConfirmHandle | undefined
function ask(options: ConfirmOptions) {
  activeConfirm?.destroy()
  activeConfirm = confirm(options)
}
function reprocess() {
  const target = entry.value
  if (!target || busy.value || editing.value || processing.value.active) return
  ask({ header: t('library.reprocess'), body: t('entryDetail.reprocessHint'), confirmBtn: t('library.reprocess'), onConfirm: () => mutate(async () => {
    const result: any = await reparseEntry(target.id)
    if (!result.success) throw new Error(result.message)
    if (id.value === target.id) { await state.refresh(); toast.success(t('library.reprocessQueued')) }
  }) })
}
function remove() {
  const target = entry.value
  if (!target || busy.value || editing.value) return
  ask({ header: t('library.deleteDocument'), body: t(target.source_kind === 'mine' ? 'personalDocument.deleteConfirm' : 'entryDetail.deleteHint', { title: title.value }), theme: 'danger', confirmBtn: t('library.deleteDocument'), onConfirm: () => mutate(async () => {
    const response: any = await (target.source_kind === 'mine' ? deleteDocument(target.id) : delEntryDetails(target.id))
    if (!response.success) throw new Error(response.message)
    toast.success(t('common.deleteSuccess'))
    busy.value = false
    await router.push(listLocation.value)
  }) })
}
async function cancelProcessing() {
  if (!entry.value || busy.value) return
  const target = entry.value.id
  ask({ header: t('library.cancelParse'), body: t('library.cancelParseConfirmBody', { title: title.value }), confirmBtn: t('library.cancelParse'), onConfirm: () => mutate(async () => {
    const response: any = await cancelEntryParse(target)
    if (!response.success) throw new Error(response.message)
    if (id.value === target) await state.refresh()
  }) })
}
function exportMarkdown() {
  if (!bodyReady.value || busy.value) return
  const url = URL.createObjectURL(new Blob([body.value], { type: 'text/markdown;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url; anchor.download = `${title.value.replace(/[\\/:*?"<>|\x00-\x1f]/g, '_')}.md`
  anchor.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
}
function locate(location: SourceLocation) { void router.replace({ query: { ...route.query, reference: JSON.stringify(location) } }) }
const entityLocation = (entityId: string) => ({ path: '/platform/entries', query: { tab: 'entities', entity_id: entityId, ...(entry.value?.library_id ? { libraries: entry.value.library_id } : {}) } })
watch(id, () => { actionError.value = ''; typeOpen.value = false; imagePreview.value = undefined; editing.value = false })
watch(() => entry.value?.library_id, libraryId => { if (libraryId && !route.query.libraries) void router.replace({ query: { ...route.query, libraries: libraryId } }) })
watch([title, loading, error], () => { document.title = `${loading.value ? t('common.loading') : error.value && !entry.value ? t('entryDetail.loadFailed') : title.value} · Reliquary` }, { immediate: true })
function leave() { if (busy.value) return false; activeConfirm?.destroy(); return true }
onBeforeRouteLeave(leave)
onBeforeRouteUpdate(leave)
onBeforeUnmount(() => { activeConfirm?.destroy(); document.title = 'Reliquary' })
</script>

<template>
  <div class="entry-detail" :aria-busy="loading">
    <header class="entry-detail__topbar">
      <nav class="entry-detail__crumbs" :aria-label="t('entryDetail.breadcrumb')">
        <LibMark v-if="library" :color="library.color" />
        <RouterLink :to="listLocation">{{ library?.display_name || library?.name || entry?.library_name || t('libraryEditor.document.title') }}</RouterLink>
        <span aria-hidden="true">/</span><span class="entry-detail__id" aria-current="page">{{ id }}</span>
      </nav>
      <div v-if="entry" class="entry-detail__actions">
        <Button variant="ghost" :disabled="editing" :aria-disabled="busy || undefined" :aria-busy="busy || undefined" :aria-pressed="Boolean(entry.starred)" @click="toggleStar">{{ entry.starred ? '★' : '☆' }} {{ t(entry.starred ? 'articleJudgments.unstar' : 'articleJudgments.star') }}</Button>
        <Button :disabled="busy || editing || processing.active || !typeOptions.length" @click="typeDraft = entry.content_type || ''; actionError = ''; typeOpen = true">{{ t('entryDetail.changeType') }}</Button>
        <Button :disabled="busy || editing || processing.active" @click="reprocess">{{ t('library.reprocess') }}</Button>
        <Button :disabled="busy || !bodyReady" @click="exportMarkdown">{{ t('entryDetail.exportMarkdown') }}</Button>
        <Button variant="danger" :disabled="busy || editing" @click="remove">{{ t('library.deleteDocument') }}</Button>
      </div>
    </header>
    <div v-if="loading" class="entry-detail__loading"><Skeleton v-for="n in 4" :key="n" :label="t('common.loading')" /></div>
    <ErrorState v-if="error" :title="t('entryDetail.loadFailed')"><Button @click="entry ? state.refresh() : state.load()">{{ t('common.retry') }}</Button></ErrorState>
    <div v-if="entry" class="entry-detail__layout">
      <article class="entry-detail__reading">
        <div v-if="processing.active || processing.failed" class="entry-detail__notice" :class="{ 'entry-detail__notice--failed': processing.failed }" :role="processing.failed ? 'alert' : 'status'">
          <strong>{{ t(processing.label) }}</strong><p>{{ entry.error_message || t('entryDetail.backgroundHint') }}</p>
          <Button v-if="processing.failed" :disabled="busy || editing" @click="reprocess">{{ t('entryStatus.retry') }}</Button>
          <Button v-if="processing.active" :disabled="busy" @click="cancelProcessing">{{ t('library.cancelParse') }}</Button>
        </div>
        <p v-if="actionError && !typeOpen" class="entry-detail__error" role="alert">{{ actionError }}</p>
        <div class="entry-detail__eyebrow">{{ contentTypeLabel(entry.content_type, t) || t('entries.unknown') }} · {{ created }}</div>
        <h1>{{ title }}</h1>
        <div class="entry-detail__markers">
          <Tag v-if="entry.acquisition === 'model_written'">{{ t('ingestStatus.modelWritten') }}</Tag>
          <Tag v-if="entry.source_kind === 'report' || entry.cannot_be_fact">{{ t('ingestStatus.cannotBeFact') }}</Tag>
          <Tag v-if="entry.source_unverified">{{ t('review.sourceUnverified') }}</Tag>
          <RouterLink v-if="entry.has_update && entry.latest_entry_id" :to="entryDetailLocation(entry.latest_entry_id)">{{ t('ikb18.hasUpdate') }}</RouterLink>
        </div>
        <div class="entry-detail__split">
        <dl class="entry-detail__facts">
          <div><dt>{{ t('entries.library') }}</dt><dd><LibMark v-if="library" :color="library.color" /> {{ library?.display_name || library?.name || entry.library_name || entry.library_id || t('entries.unknown') }}</dd></div>
          <div><dt>{{ t('library.contentTypeFilter') }}</dt><dd><Tag v-if="entry.content_type">{{ contentTypeLabel(entry.content_type, t) }}</Tag><span v-else>{{ t('entries.unknown') }}</span></dd></div>
          <div><dt>{{ t('entries.source') }}</dt><dd><a v-if="sourceURL" :href="sourceURL" target="_blank" rel="noopener noreferrer">{{ source }}</a><span v-else>{{ source || t('entries.unknown') }}</span></dd></div>
          <div><dt>{{ t('library.channelLabel') }}</dt><dd>{{ entryChannelLabel(entry, t) }}</dd></div>
          <div v-if="getExternalToolName(entry)"><dt>{{ t('library.externalTool') }}</dt><dd>{{ getExternalToolName(entry) }}</dd></div>
          <div><dt>{{ t('library.fileTypeFilter') }}</dt><dd>{{ entryFileFormatLabel(entry, t) }}</dd></div>
          <div v-if="entry.vault_path"><dt>{{ t('library.vaultPath') }}</dt><dd>{{ entry.vault_path }}</dd></div>
          <div><dt>{{ t('entryDetail.bodyLength') }}</dt><dd>{{ bodyReady ? t('entryDetail.characters', { count: Array.from(body).length }) : t('entries.unknown') }}</dd></div>
        </dl>
        <div class="entry-detail__main">
        <p v-if="entry.collect_reason">{{ entry.collect_reason }}</p>
        <RepostSources :sources="entry.repost_sources" />
        <div class="entry-detail__body-head"><h2>{{ t('entryEditor.markdown') }}</h2><Button v-if="entry.vault_path" ref="editButton" :disabled="busy || editing || processing.active || !bodyReady" @click="content?.startContentEdit()">{{ t('entryEditor.title') }}</Button></div>
        <p v-if="reference && bodyReady && reference.body_hash !== bodyHash" class="entry-detail__notice" role="status">{{ t('ikb18.bodyChanged') }}</p>
        <ErrorState v-if="bodyError" :title="t('entryEditor.loadFailed')"><Button @click="state.loadBody">{{ t('common.retry') }}</Button></ErrorState>
        <Skeleton v-else-if="!bodyReady" :label="t('common.loading')" />
        <DocContent v-if="bodyReady" ref="content" :visible="true" :details="renderDetails" :reference="reference" :library-id="entry.library_id" @get-doc="state.loadBody(); state.refresh()" @processing-state-change="state.refresh()" @preview-image="imagePreview = $event" @editing-change="editingChanged" />
        </div>
        </div>
      </article>
      <aside class="entry-detail__rail" :aria-label="t('entryDetail.information')">
        <Tabs v-model="tab" :label="t('entryDetail.information')" :items="tabs">
          <template #summary>
            <p class="entry-detail__overview">{{ plain(entry.structured_summary?.overview || entry.description || t('entries.noSummary')) }}</p>
            <ul v-if="entry.structured_summary?.points?.length"><li v-for="(point, index) in entry.structured_summary.points" :key="index">{{ plain(point) }}</li></ul>
            <section v-if="entry.structured_summary?.key_terms?.length"><h3>{{ t('entryDetail.keyTerms') }}</h3><div class="entry-detail__markers"><Tag v-for="term in entry.structured_summary.key_terms" :key="term">{{ plain(term) }}</Tag></div></section>
            <section v-if="entry.topics?.length"><h3>{{ t('articleJudgments.topics') }}</h3><p v-for="topic in entry.topics" :key="topic.id"><RouterLink :to="{ path: '/platform/topics', query: { topic_id: topic.id } }">{{ topic.name }}</RouterLink> <FollowToggle target-type="topic" :target-id="topic.id" :name="topic.name" /></p></section>
            <section v-if="entry.metadata?.related_entry_ids?.length"><h3>{{ t('library.relatedEntries') }}</h3><p v-for="relatedId in entry.metadata.related_entry_ids" :key="relatedId"><RouterLink :to="entryDetailLocation(relatedId)">{{ relatedId }}</RouterLink></p></section>
            <dl v-if="entry.custom_metadata && Object.keys(entry.custom_metadata).length" class="entry-detail__metadata"><template v-for="(value, key) in entry.custom_metadata" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></template></dl>
          </template>
          <template #entities>
            <Skeleton v-if="relationsLoading" :label="t('common.loading')" />
            <ErrorState v-if="relationsError" :title="t('entryDetail.relationsFailed')"><Button @click="state.loadRelations">{{ t('common.retry') }}</Button></ErrorState>
            <div v-for="entity in entities" :key="entity.id" class="entry-detail__entity"><div><RouterLink :to="entityLocation(entity.id)">{{ entity.canonical_name }}</RouterLink><span>{{ t(`workbench.entities.types.${entity.entity_type}`) }}</span></div><p v-if="entity.description">{{ entity.description }}</p><FollowToggle target-type="entity" :target-id="entity.id" :name="entity.canonical_name" /></div>
            <EmptyState v-if="!relationsLoading && !relationsError && !entities.length" :title="t('entries.noEntities')" />
          </template>
          <template #relations>
            <Skeleton v-if="relationsLoading" :label="t('common.loading')" />
            <ErrorState v-if="relationsError" :title="t('entryDetail.relationsFailed')"><Button @click="state.loadRelations">{{ t('common.retry') }}</Button></ErrorState>
            <div v-for="relation in relations" :key="relation.id" class="entry-detail__relation">
              <p><RouterLink :to="entityLocation(relation.subject.id)">{{ relation.subject.canonical_name }}</RouterLink> <strong>{{ relation.type }}</strong> <RouterLink :to="entityLocation(relation.object.id)">{{ relation.object.canonical_name }}</RouterLink></p>
              <div v-for="(evidence, index) in relation.evidence" :key="index"><p v-if="evidence.speaker">{{ t('ikb18.speaker') }}: {{ evidence.speaker.name }}</p><Button v-for="(location, i) in evidence.locations" :key="i" :aria-label="t('ikb18.paragraph', { number: location.paragraph })" :aria-pressed="reference?.paragraph === location.paragraph" @click="locate(location)">¶{{ location.paragraph }}</Button></div>
            </div>
            <EmptyState v-if="!relationsLoading && !relationsError && !relations.length" :title="t('ikb18.noRelations')" />
          </template>
          <template #processing>
            <ErrorState v-if="traceError" :title="t('entries.traceFailed')"><Button @click="state.loadTrace">{{ t('common.retry') }}</Button></ErrorState>
            <div v-for="({ node, depth }, index) in steps" :key="node.span_id || index" class="entry-detail__step" :style="{ paddingInlineStart: `${Math.min(depth, 3) * 12}px` }">
              <StatusSquare :status="node.status === 'failed' ? 'danger' : node.status === 'done' ? 'success' : node.status === 'running' ? 'warning' : 'neutral'" :label="stepLabel(node)" /><span>{{ stepStatus(node.status) }}</span><span v-if="node.duration_ms != null">{{ t('entries.duration', { seconds: new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(node.duration_ms / 1000) }) }}</span><p v-if="modelName(node)">{{ modelName(node) }}</p><p v-if="node.error_message" role="alert">{{ node.error_message }}</p>
            </div>
            <EmptyState v-if="!traceError && !steps.length" :title="t('entries.noTrace')" />
            <RouterLink :to="{ path: '/platform/model-calls', query: { entry_id: id } }">{{ t('entryDetail.modelCalls') }}</RouterLink>
          </template>
        </Tabs>
      </aside>
    </div>
    <Dialog :model-value="typeOpen" :title="t('entryDetail.changeType')" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" @update:model-value="!busy && (typeOpen = $event)">
      <form novalidate @submit.prevent="saveType"><label class="entry-detail__type-field">{{ t('articleJudgments.contentType') }}<Select v-model="typeDraft" :disabled="busy"><option value="" disabled>{{ t('articleJudgments.contentType') }}</option><option v-for="option in typeOptions" :key="option.name" :value="option.name">{{ option.name }}</option></Select></label><p>{{ t('articleJudgments.contentTypeHint') }}</p><p v-if="actionError" role="alert" class="entry-detail__error">{{ actionError }}</p></form>
      <template #footer><Button :disabled="busy" @click="typeOpen = false">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="busy" :disabled="!typeDraft || typeDraft === entry?.content_type" @click="saveType">{{ t('articleJudgments.saveContentType') }}</Button></template>
    </Dialog>
    <Dialog :model-value="Boolean(imagePreview)" :title="imagePreview?.alt || t('entryDetail.previewImage')" content-class="entry-image-dialog" @update:model-value="!$event && (imagePreview = undefined)">
      <img v-if="imagePreview" class="entry-detail__image" :src="imagePreview.src" :alt="imagePreview.alt" />
    </Dialog>
  </div>
</template>

<style scoped>
.entry-detail { flex: 1; height: 100%; min-height: 0; min-width: 0; overflow: auto; color: var(--text); background: var(--bg); font-family: var(--font); }
.entry-detail a { color: inherit; text-decoration: underline; text-underline-offset: 3px; }
.entry-detail a:hover { color: var(--accent); }
.entry-detail__topbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 16px; padding: 16px 24px; border-bottom: 2px solid var(--rule); }
.entry-detail__crumbs, .entry-detail__actions, .entry-detail__markers { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.entry-detail__crumbs { min-width: 0; font-size: var(--text-sm); font-weight: 700; }
.entry-detail__id { font-family: var(--mono); overflow-wrap: anywhere; }
.entry-detail__actions .rq-button { min-height: 32px; }
.entry-detail__layout { display: grid; grid-template-columns: minmax(0, 1fr) clamp(352px, 26vw, 480px); }
.entry-detail__reading { display: flex; flex-direction: column; gap: 24px; padding: 40px 48px 64px; min-width: 0; container-type: inline-size; }
.entry-detail__split, .entry-detail__main { display: flex; flex-direction: column; gap: 24px; min-width: 0; }
.entry-detail__reading > *, .entry-detail__main > * { margin-block: 0; }
.entry-detail__reading h1 { max-width: 22ch; font-size: var(--text-3xl); font-weight: 800; line-height: 1.08; letter-spacing: -0.04em; overflow-wrap: anywhere; }
.entry-detail__eyebrow { font-size: var(--text-xs); font-weight: 700; }
.entry-detail__facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(136px, 1fr)); gap: 16px; max-width: 704px; padding-top: 16px; border-top: 2px solid var(--rule); font-size: var(--text-sm); }
.entry-detail__facts dt { color: var(--muted); font-size: var(--text-xs); font-weight: 700; }
.entry-detail__facts dd { margin: 4px 0 0; overflow-wrap: anywhere; font-weight: 700; }
/* 60rem fits the 12rem fact column, the 48px gap and the 612px body at the 14px root; narrower keeps facts above the body. */
@container (min-width: 60rem) {
  .entry-detail__split { display: grid; grid-template-columns: 12rem minmax(0, 1fr); gap: 48px; align-items: start; }
  .entry-detail__facts { position: sticky; top: 16px; grid-template-columns: minmax(0, 1fr); gap: 12px; max-width: none; }
  .entry-detail__facts > div { padding-bottom: 8px; border-bottom: 1px solid var(--border); }
}
.entry-detail__body-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 8px; max-width: 612px; }
.entry-detail__body-head h2, .entry-detail__rail h3 { font-size: var(--text-sm); margin: 0; }
.entry-detail__reading :deep(.doc-reading) { max-width: 36em; font-size: 17px; line-height: 1.85; }
.entry-detail__notice { padding: 16px; background: var(--surface-soft); border-top: 2px solid var(--rule); max-width: 704px; }
.entry-detail__notice p { margin: 8px 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.entry-detail__notice--failed { background: var(--danger-soft); border-color: var(--danger); }
.entry-detail__error { color: var(--danger); overflow-wrap: anywhere; }
.entry-detail__rail { border-left: 2px solid var(--rule); min-width: 0; }
.entry-detail__rail :deep(.rq-tabs__list) { display: flex; }
.entry-detail__rail :deep(.rq-tabs__trigger) { flex: 1; min-width: 0; padding: 12px 4px; white-space: normal; }
.entry-detail__rail :deep(.rq-tabs__panel:not([hidden])) { display: flex; flex-direction: column; gap: 24px; padding: 24px 32px; overflow-wrap: anywhere; }
.entry-detail__rail p { margin: 0; }
.entry-detail__rail ul { padding-left: 20px; margin: 0; }
.entry-detail__rail li + li { margin-top: 8px; }
.entry-detail__overview { font-size: var(--text-md); font-weight: 700; }
.entry-detail__entity, .entry-detail__relation, .entry-detail__step { border-bottom: 1px solid var(--border); padding-bottom: 16px; font-size: var(--text-sm); }
.entry-detail__entity > div, .entry-detail__step { display: flex; flex-wrap: wrap; gap: 8px; justify-content: space-between; }
.entry-detail__entity p { margin-top: 8px; color: var(--muted); }
.entry-detail__relation .rq-button { min-height: 28px; padding: 2px 6px; margin: 8px 8px 0 0; font-family: var(--mono); }
.entry-detail__relation [aria-pressed="true"] { background: var(--text); color: var(--bg); }
.entry-detail__step > p { flex-basis: 100%; white-space: pre-wrap; }
.entry-detail__step > span { font-family: var(--mono); font-size: var(--text-xs); }
.entry-detail__metadata { display: grid; gap: 8px; margin: 0; }
.entry-detail__metadata dd { margin: 0; }
.entry-detail__loading { display: grid; gap: 24px; padding: 48px; }
.entry-detail__type-field { display: grid; gap: 8px; }
.entry-detail__image { display: block; max-width: 100%; max-height: 75vh; object-fit: contain; margin: auto; }
.entry-detail :deep(.repost-sources a) { color: var(--text); }
@media (max-width: 960px) { .entry-detail__layout { grid-template-columns: minmax(0, 1fr); } .entry-detail__reading { padding: 24px; } .entry-detail__rail { border-left: 0; border-top: 2px solid var(--rule); } }
@media (max-width: 480px) { .entry-detail__topbar, .entry-detail__reading { padding-inline: 16px; } .entry-detail__reading h1 { font-size: var(--text-2xl); } }
</style>
