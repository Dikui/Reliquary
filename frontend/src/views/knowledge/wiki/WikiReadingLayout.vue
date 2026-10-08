<template>
  <div class="wiki-reading-layout" :class="{ 'wiki-reading-layout--narrow': narrow }">
    <div v-if="narrow" class="wiki-reading-layout__toolbar">
      <t-button variant="outline" theme="default" :aria-expanded="directoryOpen" @click="directoryOpen = true">
        <template #icon><t-icon name="catalog" /></template>
        {{ t('knowledgeEditor.wikiBrowser.openDirectory') }}
      </t-button>
      <slot name="toolbar" />
    </div>
    <t-drawer v-if="narrow" v-model:visible="directoryOpen"
      :header="t('knowledgeEditor.wikiBrowser.directoryTitle')" placement="left"
      size="min(320px, 100vw)" :footer="false" :close-btn="true" destroy-on-close
      class="wiki-directory-drawer">
      <slot name="directory" />
    </t-drawer>
    <slot v-else name="directory" />
    <slot />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ selectionKey: string }>()
const { t } = useI18n()
const query = typeof window !== 'undefined' ? window.matchMedia('(max-width: 767px)') : null
const narrow = ref(query?.matches ?? false)
const directoryOpen = ref(false)
function closeDirectory() { directoryOpen.value = false }
defineExpose({ closeDirectory })

function updateWidth(event: MediaQueryListEvent) {
  narrow.value = event.matches
  directoryOpen.value = false
}

watch(() => props.selectionKey, () => { directoryOpen.value = false })
onMounted(() => query?.addEventListener('change', updateWidth))
onUnmounted(() => query?.removeEventListener('change', updateWidth))
</script>

<style scoped lang="less">
.wiki-reading-layout {
  display: flex;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
.wiki-reading-layout--narrow { flex-direction: column; }
.wiki-reading-layout__toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 0 12px 12px;
  flex-shrink: 0;
}
.wiki-directory-drawer :deep(.t-drawer__body) {
  padding: 12px;
  display: flex;
  min-height: 0;
}
.wiki-directory-drawer :deep(.wiki-sidebar) {
  width: 100%;
  min-width: 0;
  border-right: 0;
}
</style>
