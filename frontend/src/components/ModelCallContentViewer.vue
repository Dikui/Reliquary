<script setup lang="ts">
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { read, callDetails, type ModelCall, type ModelCallContent } from '@/api/insights'
import { copyToClipboard } from '@/utils/clipboard'
import { contentBlocks, formatContent, contentCharacters } from '@/utils/modelCallContent'
const props = defineProps<{ callId?: string; entryId?: string; processingAttempt?: number; spanId?: string; direction?: 'input' | 'output' }>()
const { t } = useI18n()
const content = ref<ModelCallContent | null>(null), calls = ref<ModelCall[]>([]), selected = ref(''), loading = ref(false), error = ref(''), copied = ref('')
const tab = ref<'input' | 'output'>('input')
const direction = computed(() => props.direction ?? tab.value)
const raw = computed(() => content.value ? direction.value === 'input' ? content.value.request : content.value.response : '')
const blocks = computed(() => contentBlocks(raw.value))
let revision = 0
async function loadContent(id: string) {
  const ticket = ++revision
  content.value = null; error.value = ''; copied.value = ''; loading.value = true
  try { const value = await read<ModelCallContent>(`model-calls/${encodeURIComponent(id)}/content`); if (ticket === revision) content.value = value }
  catch { if (ticket === revision) error.value = t('ikb23.unavailable') }
  finally { if (ticket === revision) loading.value = false }
}
async function load() {
  ++revision; content.value = null; calls.value = []; selected.value = ''; error.value = ''; loading.value = true
  if (props.callId) { selected.value = props.callId; await loadContent(props.callId); return }
  const ticket = revision
  try {
    const rows: ModelCall[] = []
    let offset = 0
    while (true) {
      const page = await callDetails({ entry_id: props.entryId, processing_attempt: props.processingAttempt, span_id: props.spanId, limit: 1000, offset })
      if (ticket !== revision) return
      rows.push(...page.data); offset += page.data.length
      if (!page.data.length || offset >= (page.total ?? offset)) break
    }
    calls.value = rows
    if (rows[0]) { selected.value = rows[0].id; await loadContent(selected.value) }
    else { loading.value = false; error.value = t('ikb23.unavailable') }
  } catch { if (ticket === revision) { loading.value = false; error.value = t('ikb23.unavailable') } }
}
async function copy(text: string, key: string) {
  copied.value = await copyToClipboard(text) ? key : 'failed'
}
onBeforeUnmount(() => { ++revision })
watch(() => [props.callId, props.entryId, props.processingAttempt, props.spanId], load, { immediate: true })
</script>
<template>
  <section class="call-content" :aria-busy="loading">
    <label v-if="calls.length > 1">{{ t('ikb23.call') }}
      <select v-model="selected" @change="loadContent(selected)"><option v-for="call in calls" :key="call.id" :value="call.id">{{ call.model_name }} {{ call.requested_at }} #{{ call.attempt }} {{ call.id }}</option></select>
    </label>
    <nav v-if="!props.direction"><button type="button" :aria-pressed="tab === 'input'" @click="tab = 'input'">{{ t('ikb23.input') }}</button><button type="button" :aria-pressed="tab === 'output'" @click="tab = 'output'">{{ t('ikb23.output') }}</button></nav>
    <p v-if="loading" role="status">{{ t('ikb23.loading') }}</p>
    <p v-if="error" role="alert">{{ error }} <button type="button" @click="load">{{ t('ikb23.retry') }}</button></p>
    <template v-if="content">
      <p v-if="!content.complete && direction === 'output'" role="status">{{ t('ikb23.partial') }}</p>
      <p v-if="copied" role="status">{{ t(copied === 'failed' ? 'ikb23.copyFailed' : 'ikb23.copied') }}</p>
      <article v-for="(block, index) in blocks" :key="`${direction}-${index}`" class="message-block">
        <header><strong>{{ block.role }}</strong><button type="button" @click="copy(block.text, String(index))">{{ t('ikb23.copy') }}</button></header>
        <details :open="block.text.length < 2000 || block.role === 'system'"><summary>{{ t('ikb23.details') }} {{ t('ikb23.characters', { n: contentCharacters(block.text) }) }}</summary><pre>{{ block.text }}</pre></details>
        <details v-if="block.body !== undefined" :key="`${selected}-${direction}-${index}-body`" class="message-body"><summary>{{ t('ikb23.body') }} {{ t('ikb23.characters', { n: contentCharacters(block.body) }) }}</summary><button type="button" @click="copy(block.body, `body-${index}`)">{{ t('ikb23.copy') }}</button><pre>{{ block.body }}</pre></details>
      </article>
      <details :open="blocks.length === 0" :key="`${selected}-${direction}-raw`"><summary>{{ t('ikb23.raw') }} {{ t('ikb23.characters', { n: contentCharacters(raw) }) }}</summary><button type="button" @click="copy(raw, 'raw')">{{ t('ikb23.copy') }}</button><pre>{{ formatContent(raw) }}</pre></details>
    </template>
  </section>
</template>
<style scoped>
.call-content { color: var(--text); min-width: 0; width: 100%; font-size: 13px; overflow-wrap: anywhere; }
.call-content nav, .call-content header { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 8px; }
.call-content button, .call-content select { color: inherit; background: var(--surface); border: 1px solid var(--border); border-radius: 0; padding: 5px 9px; cursor: pointer; max-width: 100%; }
.call-content button[aria-pressed="true"] { color: var(--accent); }
.call-content select { width: 100%; }
.call-content pre { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 420px; overflow: auto; padding: 10px; background: var(--surface-soft); font-size: 12px; }
.call-content details { margin: 8px 0; }
.call-content summary { cursor: pointer; }
.message-block { border: 1px solid var(--border); padding: 10px; border-radius: 0; margin: 10px 0; }
</style>
