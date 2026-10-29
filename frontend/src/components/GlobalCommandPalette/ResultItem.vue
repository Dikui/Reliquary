<template>
  <div role="option" tabindex="0" :aria-selected="!!selected" :data-cmdk-index="index"
    :class="['rq-cmdk-item', 'cmdk-item', { 'cmdk-item--selected': selected }]"
    @keydown.enter.self.prevent="onEnter" @keydown.space.self.prevent.stop="$emit('primary')"
    @click="$emit('primary')" @mousemove="onHover" @focusin="onHover">
    <span class="rq-cmdk-item__icon"><slot name="icon"><span class="rq-cmdk-item__symbol" /></slot></span>
    <div class="rq-cmdk-item__body">
      <div class="rq-cmdk-item__title"><slot name="title">{{ title }}</slot></div>
      <div v-if="badge || cannotBeFact || sourceUnverified || acquisition === 'model_written'" class="rq-cmdk-item__badges">
        <Tag v-if="badge">{{ badge }}</Tag>
        <Tag v-if="cannotBeFact">{{ t('ingestStatus.cannotBeFact') }}</Tag>
        <Tag v-if="sourceUnverified">{{ t('review.sourceUnverified') }}</Tag>
        <Tag v-if="acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag>
      </div>
    </div>
    <div class="rq-cmdk-item__meta">
      <slot name="meta" />
      <span v-if="score != null" class="rq-cmdk-item__score">{{ (score * 100).toFixed(0) }}%</span>
      <kbd v-if="shortcut">⌘{{ shortcut }}</kbd>
    </div>
    <div v-if="$slots.subtitle || subtitle" class="rq-cmdk-item__subtitle"><slot name="subtitle">{{ subtitle }}</slot></div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Tag } from '@/components/ui'
const { t } = useI18n()
defineProps<{
  index: number
  selected?: boolean
  title?: string
  subtitle?: string
  badge?: string
  score?: number
  cannotBeFact?: boolean
  sourceUnverified?: boolean
  acquisition?: string
  shortcut?: number | string
}>()
const emit = defineEmits<{ primary: []; hover: [index: number] }>()
function onEnter(event: KeyboardEvent) {
  if (event.isComposing || event.keyCode === 229 || event.metaKey || event.ctrlKey) return
  event.stopPropagation()
  emit('primary')
}
function onHover(event: Event) {
  const index = Number((event.currentTarget as HTMLElement).dataset.cmdkIndex)
  if (Number.isFinite(index)) emit('hover', index)
}
</script>
<style scoped>
.rq-cmdk-item { display: grid; grid-template-columns: 12px minmax(0, 1fr) auto; align-items: center; gap: 2px var(--space-3); width: 100%; padding: var(--space-2) var(--space-4); border: 0; border-radius: 0; background: transparent; color: var(--text); cursor: pointer; text-align: left; font: inherit; }
.rq-cmdk-item[aria-selected="true"] { background: var(--select); color: var(--on-select); }
.rq-cmdk-item:focus-visible { outline: 2px solid var(--rule); outline-offset: -2px; }
.rq-cmdk-item__symbol { display: block; width: 12px; height: 12px; border: 2px solid currentColor; }
.rq-cmdk-item__body { min-width: 0; }
.rq-cmdk-item__title { display: flex; align-items: baseline; flex-wrap: wrap; gap: var(--space-2); font-size: var(--text-base); font-weight: 700; overflow-wrap: anywhere; }
.rq-cmdk-item__badges { display: flex; flex-wrap: wrap; gap: var(--space-1); margin-top: 2px; }
.rq-cmdk-item__meta { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; font: var(--text-xs)/1.5 var(--mono); color: var(--muted); }
.rq-cmdk-item__meta kbd { font: inherit; white-space: nowrap; }
.rq-cmdk-item__score { font-size: var(--text-xs); }
.rq-cmdk-item__subtitle { grid-column: 2 / 4; display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); min-width: 0; font-size: var(--text-sm); line-height: 1.5; overflow-wrap: anywhere; color: var(--muted); }
:deep(.search-highlight) { background: var(--surface-soft); color: inherit; text-decoration: underline; font-weight: 700; }
</style>
