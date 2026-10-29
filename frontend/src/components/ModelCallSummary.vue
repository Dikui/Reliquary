<template>
  <section class="model-call-summary">
    <h4>{{ t('modelCalls.callsTitle') }}</h4>
    <p v-if="!calls.length">{{ t('modelCalls.noCalls') }}</p>
    <table v-else><caption class="sr-only">{{ t('modelCalls.callsTitle') }}</caption><thead><tr><th>{{ t('modelCalls.stage') }}</th><th>{{ t('modelCalls.tier') }}</th><th>{{ t('modelCalls.calls') }}</th></tr></thead><tbody><tr v-for="(call, index) in calls" :key="`${call.stage}:${call.tier}:${index}`"><td>{{ stageLabel(call.stage) }}</td><td>{{ label('modelTiers.names', call.tier) }}</td><td>{{ call.calls }}</td></tr></tbody><tfoot><tr><th colspan="2">{{ t('modelCalls.totalCalls') }}</th><td>{{ total }}</td></tr></tfoot></table>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EntryModelCall } from '@/utils/modelCalls'
const props = defineProps<{ calls: EntryModelCall[] }>()
const { t, te } = useI18n()
const total = computed(() => props.calls.reduce((sum, call) => sum + call.calls, 0))
function stageLabel(stage: string) {
  const key = `modelCalls.stages.${stage}`
  return te(key) ? t(key) : t('modelCalls.otherCall')
}
function label(root: string, value: string) { const key = `${root}.${value}`; return te(key) ? t(key) : value }
</script>
<style scoped lang="less">
.model-call-summary { font-size: 12px; color: var(--td-text-color-secondary); }
h4 { margin: 14px 0 8px; color: var(--td-text-color-primary); font-size: 13px; }
p { line-height: 1.6; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 8px; text-align: left; border-bottom: 1px solid var(--td-component-stroke); overflow-wrap: anywhere; }
th:last-child, td:last-child { text-align: right; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
</style>
