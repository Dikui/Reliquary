<script setup lang="ts">
import { contentTypeLabel } from '@/utils/contentTypeLabel'
import { formatDateTime } from '@/utils/dateTime'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getEntryDetails, getEntrySpans, type LibraryCatalogItem } from '@/api/library'
import { Button, ErrorState, LibMark, Skeleton, StatusSquare, Tag } from '@/components/ui'
import { entryProcessingState, isEntryProcessingActive } from '@/utils/entryProcessingStatus'
import { entrySpansPayloadHasTrace, type EntryTraceNode } from '@/utils/entryTrace'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import { safeExternalUrl } from '@/utils/externalUrl'
import type { ClassifiedEntry } from '@/types/entryClassification'

interface PreviewEntry extends ClassifiedEntry { id: string; title?: string; file_name?: string; display_name?: string; parse_status?: string; created_at?: string }
const props = defineProps<{ entry: PreviewEntry; libraries: LibraryCatalogItem[] }>()
const emit = defineEmits<{ open: []; 'open-entity': [id: string]; action: [action: 'reparse' | 'cancel-parse' | 'delete' | 'view-trace', entry: PreviewEntry] }>()
const { t, locale } = useI18n()
const detail = ref<PreviewEntry>(), trace = ref<EntryTraceNode>(), loading = ref(false), error = ref(''), traceError = ref('')
let generation = 0, timer: ReturnType<typeof setTimeout> | undefined, disposed = false
const value = computed(() => ({ ...props.entry, ...detail.value, parse_status: props.entry.parse_status, summary_status: props.entry.summary_status }))
const library = computed(() => props.libraries.find(lib => lib.id === value.value.library_id))
const state = computed(() => entryProcessingState(value.value))
const source = computed(() => value.value.metadata?.intake?.source_info?.original_url || value.value.source || '')
const sourceURL = computed(() => safeExternalUrl(source.value))
const created = computed(() => formatDateTime(value.value.created_at, locale.value, { dateStyle: 'medium' }, t('entries.unknown')))
const stages = computed(() => (trace.value?.children || []).filter(node => node.kind === 'stage'))
const plainText = (text: string) => markdownToPlainText(text, `[${t('ingestStatus.missingTypes.image')}]`)
const stageLabel = (node: EntryTraceNode) => ['docreader', 'processing', 'chunking', 'embedding'].includes(node.name) ? t(`processingStages.stage.${node.name}`) : node.name
const stageStatus = (status: string) => ['pending', 'running', 'done', 'failed', 'skipped', 'cancelled', 'finalizing'].includes(status) ? t(`processingStages.status.${status}`) : t('entries.unknown')
async function load() {
  clearTimeout(timer)
  const request = ++generation, id = props.entry.id
  loading.value = !detail.value
  error.value = ''; traceError.value = ''
  const results = await Promise.allSettled([getEntryDetails(id), getEntrySpans(id)])
  if (disposed || request !== generation) return
  const [entryResult, traceResult] = results
  if (entryResult.status === 'fulfilled' && entryResult.value?.success && entryResult.value.data) detail.value = entryResult.value.data as PreviewEntry
  else error.value = t('entries.previewFailed')
  if (traceResult.status === 'fulfilled' && traceResult.value?.success) {
    trace.value = entrySpansPayloadHasTrace(traceResult.value.data) ? traceResult.value.data.trace : undefined
  } else traceError.value = t('entries.traceFailed')
  loading.value = false
  if (isEntryProcessingActive(value.value)) timer = setTimeout(load, 2000)
}
watch(() => props.entry.id, () => { detail.value = undefined; trace.value = undefined; void load() }, { immediate: true })
watch(() => [props.entry.parse_status, props.entry.summary_status], () => { void load() })
onBeforeUnmount(() => { disposed = true; generation++; clearTimeout(timer) })
</script>

<template>
  <article class="entry-preview" :aria-label="t('entries.preview')" :aria-busy="loading">
    <div class="entry-preview__top"><span class="entry-preview__id">{{ entry.id }}</span><span class="entry-preview__hint">{{ t('entries.keyboardHint') }}</span><Button variant="primary" @click="emit('open')">{{ t('entries.open') }} ↵</Button></div>
    <div class="entry-preview__body">
    <div class="entry-preview__main">
    <h2>{{ value.display_name || value.title || value.file_name || t('library.untitledDocument') }}</h2>
    <div class="entry-preview__marks">
      <Tag v-if="value.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag>
      <Tag v-if="value.cannot_be_fact || value.source_kind === 'report'">{{ t('ingestStatus.cannotBeFact') }}</Tag>
      <StatusSquare :status="state.failed ? 'danger' : state.active ? 'warning' : state.status === 'completed' ? 'success' : 'neutral'" :label="t(state.label)" />
    </div>
    <p v-if="value.source_unverified">{{ t('review.sourceUnverified') }}</p>
    <Skeleton v-if="loading" :label="t('common.loading')" />
    <ErrorState v-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
    <section><h3>{{ t('entries.summary') }}</h3><p>{{ plainText(value.structured_summary?.overview || value.description || t('entries.noSummary')) }}</p><ul v-if="value.structured_summary?.points?.length"><li v-for="(point, index) in value.structured_summary.points" :key="index">{{ plainText(point) }}</li></ul></section>
    </div>
    <div class="entry-preview__side">
    <dl class="entry-preview__facts">
      <div><dt>{{ t('entries.library') }}</dt><dd><LibMark v-if="library" :color="library.color" /> {{ library?.display_name || library?.name || value.library_id || t('entries.unknown') }}</dd></div>
      <div><dt>{{ t('library.contentTypeFilter') }}</dt><dd><Tag v-if="value.content_type">{{ contentTypeLabel(value.content_type, t) }}</Tag><span v-else>{{ t('entries.unknown') }}</span></dd></div>
      <div><dt>{{ t('entries.columns.created') }}</dt><dd>{{ created }}</dd></div>
      <div><dt>{{ t('entries.source') }}</dt><dd><a v-if="sourceURL" :href="sourceURL" target="_blank" rel="noopener noreferrer">{{ source }}</a><span v-else>{{ source || t('entries.unknown') }}</span></dd></div>
    </dl>
    <section><h3>{{ t('entries.entities') }}</h3><div class="entry-preview__entities"><Button v-for="entity in value.entities" :key="entity.id" variant="ghost" @click="emit('open-entity', entity.id)">{{ entity.canonical_name }}</Button><span v-if="!value.entities?.length">{{ t('entries.noEntities') }}</span></div></section>
    <section><h3>{{ t('entries.processing') }}</h3>
      <ErrorState v-if="traceError" :title="traceError"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
      <ul v-else-if="stages.length" class="entry-preview__steps"><li v-for="stage in stages" :key="stage.span_id || stage.name"><StatusSquare :status="stage.status === 'failed' ? 'danger' : stage.status === 'done' ? 'success' : stage.status === 'running' ? 'warning' : 'neutral'" :label="stageLabel(stage)" /><Tag>{{ stageStatus(stage.status) }}</Tag><span v-if="stage.duration_ms != null">{{ t('entries.duration', { seconds: (stage.duration_ms / 1000).toFixed(1) }) }}</span></li></ul>
      <p v-else>{{ t('entries.noTrace') }}</p>
      <p v-if="value.error_message" role="alert">{{ value.error_message }}</p>
    </section>
    <div class="entry-preview__actions">
      <Button @click="emit('action', 'view-trace', entry)">{{ t('processingStages.viewTrace') }}</Button>
      <Button :disabled="state.active" @click="emit('action', 'reparse', entry)">{{ t('library.reprocess') }}</Button>
      <Button v-if="state.active" @click="emit('action', 'cancel-parse', entry)">{{ t('library.cancelParse') }}</Button>
      <Button variant="danger" @click="emit('action', 'delete', entry)">{{ t('library.deleteDocument') }}</Button>
    </div>
    </div>
    </div>
  </article>
</template>

<style scoped>
.entry-preview { display: flex; flex-direction: column; gap: 24px; padding: 24px 32px; min-width: 0; overflow-wrap: anywhere; container-type: inline-size; }
.entry-preview__body, .entry-preview__main, .entry-preview__side { display: flex; flex-direction: column; gap: 24px; min-width: 0; }
/* 60rem fits the 42rem reading column, the 48px gap and an 18rem side column; narrower stacks them. */
@container (min-width: 60rem) { .entry-preview__body { display: grid; grid-template-columns: minmax(0, 42rem) minmax(18rem, 1fr); gap: 48px; align-items: start; } }
.entry-preview__top, .entry-preview__marks, .entry-preview__entities, .entry-preview__actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.entry-preview__id { font: var(--text-xs) var(--mono); margin-right: auto; }
.entry-preview__hint { font-size: var(--text-xs); color: var(--muted); }
.entry-preview h2 { margin: 0; font-size: var(--text-2xl); font-weight: 800; line-height: 1.15; letter-spacing: -0.03em; }
.entry-preview h3 { font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); text-transform: uppercase; letter-spacing: 0.08em; margin: 0 0 12px; }
.entry-preview p { margin: 0; white-space: pre-wrap; }
.entry-preview section, .entry-preview__facts { border-top: 2px solid var(--rule); padding-top: 16px; }
.entry-preview__facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(128px, 1fr)); gap: 16px; margin: 0; }
.entry-preview dt { font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); color: var(--muted); }
.entry-preview dd { margin: 4px 0 0; font-weight: 700; }
.entry-preview a { color: inherit; text-decoration: underline; }
.entry-preview a:hover { color: var(--accent); }
.entry-preview__steps { display: grid; gap: 8px; padding: 0; list-style: none; }
.entry-preview__steps li { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: var(--text-sm); }
.entry-preview__steps li > span:last-child { margin-left: auto; }
@media (max-width: 900px) { .entry-preview { display: none; } }
</style>
