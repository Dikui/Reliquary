<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ImagePreview from './ImagePreview.vue'
import { Button, Dialog } from '@/components/ui'
import { buildProtectedFileRequest } from '@/utils/protectedFileAccess'

const props = withDefaults(defineProps<{ articleId: string; path: string; title: string; compact?: boolean; interactive?: boolean; swiss?: boolean }>(), { compact: false, interactive: false, swiss: false })
const { t } = useI18n()
const imageURL = ref(''), loading = ref(true), failed = ref(false)
const previewVisible = ref(false)
const previewTrigger = ref<HTMLButtonElement | null>(null)
let request: AbortController | undefined, generation = 0, mounted = false
function release() { previewVisible.value = false; if (imageURL.value) URL.revokeObjectURL(imageURL.value); imageURL.value = '' }
function openPreview() { if (imageURL.value && !failed.value) previewVisible.value = true }
function restoreFocus(event: Event) { event.preventDefault(); previewTrigger.value?.focus({ preventScroll: true }) }
async function load() {
  request?.abort(); release()
  const current = ++generation
  const controller = new AbortController()
  request = controller
  loading.value = true; failed.value = false
  try {
    const source = `vault://${encodeURIComponent(props.articleId)}/${encodeURIComponent(props.path)}`
    const target = buildProtectedFileRequest(source, { mode: 'space' })
    if (!target) throw new Error('Invalid attachment')
    const response = await fetch(target.url, { method: 'GET', headers: target.headers, credentials: 'include', signal: controller.signal })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const blob = await response.blob()
    if (current === generation && mounted) imageURL.value = URL.createObjectURL(blob)
  } catch {
    if (current === generation && mounted && !controller.signal.aborted) failed.value = true
  } finally { if (current === generation && mounted) loading.value = false }
}
watch(() => [props.articleId, props.path], () => { if (mounted) void load() })
onMounted(() => { mounted = true; void load() })
onBeforeUnmount(() => { mounted = false; generation++; request?.abort(); release() })
</script>

<template>
  <component :is="compact ? 'span' : 'figure'" class="search-image" :class="{ compact, 'search-image--swiss': swiss }" :aria-busy="loading" :data-image-path="path">
    <component :is="compact ? 'span' : 'div'" class="search-image-preview">
      <button v-if="imageURL && (!compact || interactive)" ref="previewTrigger" type="button" class="search-image-open" :aria-label="t('knowledgeSearch.previewImage', { title })" @click.stop="openPreview">
        <img :src="imageURL" :alt="t('knowledgeSearch.imageAlt', { title })" loading="lazy" @error="failed = true; previewVisible = false" />
      </button>
      <img v-else-if="imageURL" :src="imageURL" :alt="t('knowledgeSearch.imageAlt', { title })" loading="lazy" @error="failed = true" />
      <span v-if="failed" class="search-image-state" role="alert"><span>{{ t('knowledgeSearch.imageLoadFailed') }}</span><Button v-if="swiss && !compact" @click="load">{{ t('common.retry') }}</Button><t-button v-else-if="!compact" size="small" variant="text" @click="load">{{ t('common.retry') }}</t-button></span>
      <span v-else-if="loading" class="search-image-state" role="status">{{ t('common.loading') }}</span>
    </component>
    <component :is="compact ? 'span' : 'figcaption'" class="search-image-caption">{{ t('knowledgeSearch.imageMatch') }}</component>
    <Dialog v-if="swiss" :model-value="previewVisible && !!imageURL && !failed" :title="t('knowledgeSearch.previewImage', { title })" @update:model-value="previewVisible = $event" @close-auto-focus="restoreFocus"><img class="search-image-full" :src="imageURL" :alt="t('knowledgeSearch.imageAlt', { title })" /></Dialog>
    <ImagePreview v-else :visible="previewVisible && !!imageURL && !failed" :images="imageURL ? [imageURL] : []" :return-focus="previewTrigger" @close="previewVisible = false" />
  </component>
</template>

<style scoped lang="less">
.search-image { display: block; margin: 0; width: 240px; max-width: 100%; }
.search-image.compact { width: 120px; }
.search-image-preview { position: relative; display: grid; place-items: center; width: 100%; aspect-ratio: 4 / 3; overflow: hidden; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); }
.search-image-open { position: absolute; inset: 0; padding: 0; border: 0; border-radius: inherit; background: transparent; cursor: zoom-in; }
.search-image-open:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -3px; }
img { position: absolute; inset: 0; display: block; width: 100%; height: 100%; max-width: 100%; max-height: 100%; object-fit: contain; }
.search-image-state { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; flex-wrap: wrap; gap: 4px; padding: 12px; font-size: 12px; color: var(--td-text-color-secondary); background: var(--td-bg-color-secondarycontainer); }
.search-image-caption { display: block; margin-top: 6px; font-size: 12px; color: var(--td-text-color-secondary); }
.search-image--swiss .search-image-preview { border-color: var(--border); border-radius: 0; background: var(--surface-soft); }
.search-image--swiss .search-image-state { color: var(--muted); background: var(--surface-soft); }
.search-image--swiss .search-image-caption { color: inherit; }
.search-image--swiss .search-image-open:focus-visible { outline-color: var(--rule); }
.search-image-full { position: static; width: 100%; height: auto; max-height: 70vh; object-fit: contain; }
</style>
