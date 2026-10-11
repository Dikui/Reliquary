<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ImageViewer } from 'tdesign-vue-next'

const props = defineProps<{ visible: boolean; images: string[]; returnFocus?: HTMLElement | null }>()
const emit = defineEmits<{ close: [] }>()
const viewerVisible = ref(false)
let trigger: HTMLElement | null = null
let visibilityChange = 0

watch(() => props.visible, async (visible, _previous, onCleanup) => {
  visibilityChange++
  let cancelled = false
  onCleanup(() => { cancelled = true })
  if (visible) {
    const active = typeof document === 'undefined' ? null : document.activeElement
    trigger = props.returnFocus ?? (typeof HTMLElement !== 'undefined' && active instanceof HTMLElement ? active : null)
    // ImageViewer focuses its keyboard container only after a visibility change.
    await nextTick()
    if (!cancelled) {
      trigger = props.returnFocus ?? trigger
      viewerVisible.value = true
    }
  } else {
    viewerVisible.value = false
    const target = trigger
    trigger = null
    await nextTick()
    if (!cancelled && target?.isConnected) target.focus({ preventScroll: true })
  }
}, { immediate: true })

async function closePreview() {
  if (!viewerVisible.value) return
  const current = visibilityChange
  viewerVisible.value = false
  await nextTick()
  if (current !== visibilityChange) return
  const target = trigger
  trigger = null
  if (target?.isConnected) target.focus({ preventScroll: true })
  emit('close')
}
onBeforeUnmount(() => { visibilityChange++; trigger = null })
</script>

<template>
  <ImageViewer :visible="viewerVisible" :images="images" close-on-overlay close-on-esc-keydown @close="closePreview">
    <template #trigger></template>
  </ImageViewer>
</template>
