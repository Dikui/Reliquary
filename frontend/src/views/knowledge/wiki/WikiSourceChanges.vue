<template>
  <section v-if="changes.length" ref="noticeRef" tabindex="-1" class="wiki-source-changes" :aria-label="t('knowledgeEditor.wikiBrowser.sourceChangesTitle')">
    <h3 class="wiki-source-changes__title">
      <t-icon name="error-circle" aria-hidden="true" />
      {{ t('knowledgeEditor.wikiBrowser.sourceChangesTitle') }}
    </h3>
    <p class="wiki-source-changes__hint">{{ t('knowledgeEditor.wikiBrowser.sourceChangesHint') }}</p>
    <ul class="wiki-source-changes__list">
      <li v-for="change in changes" :key="change.chunkId">
        <span>{{ t('knowledgeEditor.wikiBrowser.sourceChangesChunk', { id: change.chunkId }) }}</span>
        <time :datetime="change.changedAt">{{ formatChangedAt(change.changedAt) }}</time>
      </li>
    </ul>
    <p class="wiki-source-changes__status" role="status" aria-live="polite">
      <template v-if="queued">{{ t('knowledgeEditor.wikiBrowser.sourceChangesQueued') }}</template>
      <template v-else-if="editing">{{ t('knowledgeEditor.wikiBrowser.sourceChangesEditing') }}</template>
      <template v-else-if="busy">{{ t('knowledgeEditor.wikiBrowser.sourceChangesBusy') }}</template>
    </p>
    <p v-if="error" class="wiki-source-changes__error" role="alert">{{ error }}</p>
    <div v-if="canEdit" class="wiki-source-changes__actions" :aria-busy="operation !== '' || queued">
      <t-popconfirm :disabled="actionsDisabled"
        :content="t('knowledgeEditor.wikiBrowser.sourceChangesRegenerateConfirm', { title: page.title })"
        @confirm="regenerate">
        <t-button size="small" theme="primary" variant="outline" :disabled="actionsDisabled"
          :loading="operation === 'regenerate'">
          {{ t('knowledgeEditor.wikiBrowser.sourceChangesRegenerate') }}
        </t-button>
      </t-popconfirm>
      <t-button size="small" theme="default" variant="outline" :disabled="actionsDisabled"
        :loading="operation === 'ignore'" @click="ignore">
        {{ t('knowledgeEditor.wikiBrowser.sourceChangesIgnore') }}
      </t-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { ignoreWikiSourceChanges, regenerateWikiSourceChanges, type WikiPage } from '@/api/wiki'

const props = defineProps<{
  page: WikiPage
  kbId: string
  canEdit?: boolean
  busy?: boolean
  editing?: boolean
}>()
const emit = defineEmits<{
  (event: 'updated', page: WikiPage): void
  (event: 'regenerate-queued'): void
}>()
const { t, locale } = useI18n()
const noticeRef = ref<HTMLElement | null>(null)
function reveal() {
  noticeRef.value?.scrollIntoView?.({ block: 'nearest' })
  noticeRef.value?.focus?.({ preventScroll: true })
}
defineExpose({ reveal })
const changes = computed(() => Object.entries(props.page.source_changes || {})
  .map(([chunkId, changedAt]) => ({ chunkId, changedAt }))
  .sort((a, b) => a.chunkId.localeCompare(b.chunkId)))
const operation = ref<'' | 'regenerate' | 'ignore'>('')
const queued = ref(false)
const error = ref('')
const actionsDisabled = computed(() => !!operation.value || queued.value || props.busy || props.editing)
let requestVersion = 0

watch(() => props.page, () => {
  requestVersion++
  operation.value = ''
  queued.value = false
  error.value = ''
})

function formatChangedAt(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short',
  }).format(date)
}

async function perform(action: 'regenerate' | 'ignore') {
  if (!props.canEdit || actionsDisabled.value || !changes.value.length) return
  const version = ++requestVersion
  const identity = `${props.kbId}/${props.page.id}`
  const slug = props.page.slug
  operation.value = action
  error.value = ''
  try {
    const response = await (action === 'regenerate' ? regenerateWikiSourceChanges : ignoreWikiSourceChanges)(props.kbId, slug)
    if (version !== requestVersion || identity !== `${props.kbId}/${props.page.id}`) return
    if (action === 'regenerate') {
      queued.value = true
      MessagePlugin.success(t('knowledgeEditor.wikiBrowser.sourceChangesRegenerateQueued'))
      emit('regenerate-queued')
    } else {
      emit('updated', (response.data || response) as WikiPage)
      MessagePlugin.success(t('knowledgeEditor.wikiBrowser.sourceChangesIgnored'))
    }
  } catch {
    if (version !== requestVersion || identity !== `${props.kbId}/${props.page.id}`) return
    error.value = t(action === 'regenerate'
      ? 'knowledgeEditor.wikiBrowser.sourceChangesRegenerateFailed'
      : 'knowledgeEditor.wikiBrowser.sourceChangesIgnoreFailed')
    MessagePlugin.error(error.value)
  } finally {
    if (version === requestVersion) operation.value = ''
  }
}

function regenerate() { return perform('regenerate') }
function ignore() { return perform('ignore') }
</script>

<style scoped>
.wiki-source-changes {
  margin: 0 0 20px;
  padding: 16px;
  min-width: 0;
  border: 1px solid var(--td-warning-color);
  border-radius: var(--td-radius-default);
  background: var(--td-warning-color-light);
  color: var(--td-text-color-primary);
}
.wiki-source-changes__title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.wiki-source-changes:focus { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.wiki-source-changes__title :deep(.t-icon) { color: var(--td-warning-color); }
.wiki-source-changes__hint { margin: 8px 0; font-size: 13px; line-height: 1.6; }
.wiki-source-changes__list { margin: 0; padding-left: 20px; font-size: 12px; line-height: 1.7; }
.wiki-source-changes__list li { overflow-wrap: anywhere; }
.wiki-source-changes__list time { display: block; color: var(--td-text-color-secondary); }
.wiki-source-changes__status { min-height: 20px; margin: 8px 0 0; font-size: 12px; line-height: 20px; }
.wiki-source-changes__error { margin: 8px 0 0; color: var(--td-error-color); font-size: 13px; }
.wiki-source-changes__actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
.wiki-source-changes__actions > * { flex-shrink: 0; max-width: 100%; }
</style>
