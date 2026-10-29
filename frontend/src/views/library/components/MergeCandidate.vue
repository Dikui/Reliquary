<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, LibMark } from '@/components/ui'
import { ENTITY_TYPES, getEntity, type EntityDetail, type EntityMergeSuggestion, type EntityType } from '@/api/entities'
import { getEntryEditableBody, type LibraryCatalogItem } from '@/api/library'
import { bodyParagraphs, entryDetailHref } from '@/utils/bodyReference'
import type { SourceLocation } from '@/types/search'

const props = withDefaults(defineProps<{ entityId: string; libraries?: LibraryCatalogItem[]; name?: string; entryId?: string; label: string; status?: EntityMergeSuggestion['status']; mergedIntoName?: string }>(), { status: 'pending' })
const { t } = useI18n()
const detail = ref<EntityDetail | null>(null), loading = ref(false), error = ref(''), evidenceError = ref('')
const evidence = ref<{ id: string; title?: string; text: string; location: SourceLocation } | null>(null)
let generation = 0, controller: AbortController | undefined
const historical = computed(() => props.status !== 'pending')
const displayName = computed(() => (historical.value ? props.name : detail.value?.entity.canonical_name || props.name) || props.entityId.slice(0, 8))
const entityType = computed(() => detail.value?.entity.entity_type || 'other')
const library = computed(() => props.libraries?.find(item => item.id === detail.value?.entity.library_id))
const typeLabel = computed(() => t(`workbench.entities.types.${ENTITY_TYPES.includes(entityType.value as EntityType) ? entityType.value : 'other'}`))
const pieces = computed(() => {
  const text = evidence.value?.text || ''
  const names = [displayName.value, ...(detail.value?.entity.aliases || [])].filter(Boolean).sort((a, b) => b.length - a.length)
  if (!names.length) return [{ text, marked: false }]
  const pattern = new RegExp(names.map(name => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'gi')
  const parts: { text: string; marked: boolean }[] = []
  let offset = 0
  for (const match of text.matchAll(pattern)) {
    parts.push({ text: text.slice(offset, match.index), marked: false }, { text: match[0], marked: true })
    offset = match.index! + match[0].length
  }
  parts.push({ text: text.slice(offset), marked: false })
  return parts
})
async function load() {
  const current = ++generation
  controller?.abort(); controller = new AbortController()
  loading.value = false; error.value = ''; evidenceError.value = ''; detail.value = null; evidence.value = null
  if (historical.value) return
  loading.value = true
  try {
    const response = await getEntity(props.entityId)
    if (!response.success) throw response
    if (current !== generation) return
    detail.value = response.data
    const entry = response.data.entries.find(item => item.id === props.entryId) || response.data.entries[0]
    if (entry) {
      try {
        const body = await getEntryEditableBody(entry.id, controller.signal)
        if (current !== generation) return
        const names = [response.data.entity.canonical_name, ...(response.data.entity.aliases || [])].filter(Boolean).map(name => name.toLowerCase())
        const paragraphs = bodyParagraphs(body.content)
        const index = paragraphs.findIndex(block => names.some(name => block.text.toLowerCase().includes(name)))
        const block = paragraphs[index]
        if (block) evidence.value = { id: entry.id, title: entry.title || entry.file_name, text: block.text.trim(), location: { heading_path: [], mapping: 'exact', paragraph: index + 1, start: Array.from(body.content.slice(0, block.start)).length, end: Array.from(body.content.slice(0, block.end)).length, body_hash: body.bodyHash } }
      } catch { if (current === generation) evidenceError.value = t('pendingPage.evidenceFailed') }
    }
  } catch { if (current === generation) error.value = t('pendingPage.candidateFailed') }
  finally { if (current === generation) loading.value = false }
}
watch(() => [props.entityId, props.entryId, props.status], load, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>

<template>
  <article class="merge-candidate">
    <div class="candidate-label">{{ label }}<template v-if="!historical"> · {{ typeLabel }}</template></div>
    <div v-if="library" class="candidate-library"><LibMark :color="library.color" />{{ library.display_name || library.name }}</div>
    <h3 :title="entityId">{{ displayName }}</h3>
    <p v-if="historical && mergedIntoName">{{ t('pendingPage.mergedInto', { name: mergedIntoName }) }}</p>
    <a v-if="historical && entryId" :href="entryDetailHref(entryId)">{{ t('workbench.entities.openArticle') }}</a>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert">{{ error }} <Button variant="ghost" @click="load">{{ t('common.retry') }}</Button></p>
    <dl v-if="detail"><dt>{{ t('entityPage.materials') }}</dt><dd class="mono">{{ detail.entries.length }}</dd><dt>{{ t('workbench.entities.aliasLabel') }}</dt><dd>{{ detail.entity.aliases?.join(' · ') || t('workbench.entities.noAliases') }}</dd><template v-if="detail.entity.description"><dt>{{ t('pendingPage.description') }}</dt><dd>{{ detail.entity.description }}</dd></template></dl>
    <blockquote v-if="evidence" class="candidate-evidence"><a :href="entryDetailHref(evidence.id, evidence.location)" :title="evidence.id">{{ evidence.title || evidence.id.slice(0, 8) }} · ¶{{ evidence.location.paragraph }}</a><p><template v-for="(piece, index) in pieces" :key="index"><mark v-if="piece.marked">{{ piece.text }}</mark><template v-else>{{ piece.text }}</template></template></p></blockquote>
    <a v-else-if="detail?.entries[0]" :href="entryDetailHref(detail.entries[0].id)" :title="detail.entries[0].id">{{ detail.entries[0].title || detail.entries[0].file_name || detail.entries[0].id.slice(0, 8) }}</a>
    <p v-if="evidenceError" role="alert">{{ evidenceError }} <Button variant="ghost" @click="load">{{ t('common.retry') }}</Button></p>
  </article>
</template>

<style scoped>
.merge-candidate { padding: var(--space-4) var(--space-5); display: flex; flex-direction: column; gap: var(--space-3); min-width: 0; overflow-wrap: anywhere; }
.candidate-library { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-xs); }
.candidate-label { font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); color: var(--muted); }
.merge-candidate h3 { margin: 0; font: 800 var(--text-2xl)/1.1 var(--font); color: var(--text); letter-spacing: -0.03em; }
.merge-candidate dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: var(--space-1) var(--space-3); margin: 0; font-size: var(--text-sm); }
.merge-candidate dt { font-size: var(--type-label-size); font-weight: var(--type-label-weight); color: var(--muted); }
.merge-candidate dd { margin: 0; }
.merge-candidate .mono { font-family: var(--mono); }
.merge-candidate p { margin: 0; font-size: var(--text-sm); line-height: 1.75; }
.candidate-evidence { margin: 0; border: 0; padding: var(--space-3); background: var(--surface-soft); font-size: var(--text-sm); }
.candidate-evidence p { white-space: pre-wrap; max-height: 280px; overflow: auto; }
.merge-candidate a { color: var(--text); text-decoration: underline; text-underline-offset: 3px; }
.merge-candidate a:hover { color: var(--accent); }
.candidate-evidence mark { color: var(--on-select); background: var(--select); }
.merge-candidate [role="alert"] { color: var(--danger); }
</style>
