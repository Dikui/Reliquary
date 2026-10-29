<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SourceLocation } from '@/types/search'
import { bodyParagraphs, resolveBodyReference } from '@/utils/bodyReference'
import { getApiBaseUrl } from '@/utils/api-base'
const props = defineProps<{ entryId: string; location: SourceLocation }>()
const { t } = useI18n()
const paragraphs = ref<Array<{ before: string; highlight: string; after: string }>>([]), changed = ref(false), loading = ref(false), error = ref(''), root = ref<HTMLElement>()
let sequence = 0, controller: AbortController | undefined
async function load() {
  const current = ++sequence; controller?.abort(); controller = new AbortController()
  loading.value = true; paragraphs.value = []; changed.value = false; error.value = ''
  try {
    const response = await fetch(`${getApiBaseUrl()}/api/v1/entries/${encodeURIComponent(props.entryId)}/body`, { signal: controller.signal })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const body = await response.text(), reference = await resolveBodyReference(body, props.location)
    if (current !== sequence) return
    changed.value = !reference.matches || !reference.range
    paragraphs.value = bodyParagraphs(body).map(block => {
      const start = reference.range ? Math.max(block.start, reference.range.start) : block.end
      const end = reference.range ? Math.min(block.end, reference.range.end) : block.end
      return end > start ? { before: body.slice(block.start, start), highlight: body.slice(start, end), after: body.slice(end, block.end) } : { before: block.text, highlight: '', after: '' }
    })
    await nextTick()
    if (current === sequence) (root.value?.querySelector('mark') || root.value?.querySelector(`[data-paragraph="${reference.paragraph}"]`))?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  } catch (cause: any) { if (current === sequence && !controller.signal.aborted) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sequence) loading.value = false }
}
watch(() => [props.entryId, props.location], load, { immediate: true })
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
<template>
  <section ref="root" class="body-reference" :aria-label="t('ikb18.reference')">
    <h4>{{ t('ikb18.reference') }}</h4><t-alert v-if="changed" theme="warning" :message="t('ikb18.bodyChanged')" />
    <t-alert v-if="error" theme="error" :message="error"><template #operation><t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading"><div v-for="(paragraph, index) in paragraphs" :key="index" :data-paragraph="index + 1" class="reference-paragraph"><span>{{ paragraph.before }}</span><mark v-if="paragraph.highlight">{{ paragraph.highlight }}</mark><span>{{ paragraph.after }}</span></div></t-loading>
  </section>
</template>
<style scoped>
.body-reference { color: var(--td-text-color-primary); margin-block: 20px; }
.reference-paragraph { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; }
mark { color: var(--td-text-color-primary); background: var(--td-warning-color-light); border-radius: var(--td-radius-small); outline: 1px solid var(--td-warning-color); }
</style>
