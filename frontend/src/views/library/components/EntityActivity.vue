<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getEntityFacts, type Entity } from '@/api/entities'
import type { StoryFact } from '@/api/stories'

const props = defineProps<{ entity: Entity }>()
const emit = defineEmits<{ 'open-source': [id: string] }>()
const { t, locale } = useI18n()
const range = ref<string[]>([])
const facts = ref<StoryFact[]>([]), total = ref(0), page = ref(1)
const factsLoading = ref(false), factsError = ref('')
const pageSize = 20
let factsVersion = 0
const timeFilter = () => ({ from: range.value[0] || undefined, to: range.value[1] || undefined })
const occurrenceTime = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })

async function loadFacts() {
  const version = ++factsVersion
  factsLoading.value = true; factsError.value = ''
  try {
    const result = await getEntityFacts(props.entity.id, { ...timeFilter(), limit: pageSize, offset: (page.value - 1) * pageSize })
    if (!result.success) throw new Error()
    if (version !== factsVersion) return
    facts.value = result.data.items; total.value = result.data.total
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (page.value > lastPage) page.value = lastPage
  } catch {
    if (version === factsVersion) factsError.value = t('entityActivity.factsFailed')
  } finally {
    if (version === factsVersion) factsLoading.value = false
  }
}
function refresh() { void loadFacts() }
watch(() => props.entity.id, () => {
  factsVersion++; facts.value = []; total.value = 0; page.value = 1; void loadFacts()
}, { immediate: true })
watch(range, () => {
  factsVersion++; facts.value = []; total.value = 0
  if (page.value !== 1) page.value = 1
  else void loadFacts()
}, { deep: true })
watch(page, () => { facts.value = []; void loadFacts() })
onBeforeUnmount(() => { factsVersion++ })
</script>

<template>
  <section class="entity-activity">
    <header class="activity-heading">
      <h4>{{ t('entityActivity.timeline') }}</h4>
      <t-button variant="text" :loading="factsLoading" @click="refresh">{{ t('common.refresh') }}</t-button>
    </header>
    <t-date-range-picker v-model="range" allow-input clearable :placeholder="[t('entityActivity.from'), t('entityActivity.to')]" :aria-label="t('entityActivity.period')" />
    <p class="muted">{{ t('entityActivity.description') }}</p>
    <p class="muted">{{ t('entityActivity.timeHint') }}</p>
    <div v-if="factsError" class="activity-error" role="alert">{{ factsError }} <t-button variant="text" :loading="factsLoading" @click="loadFacts">{{ t('common.retry') }}</t-button></div>
    <t-loading :loading="factsLoading">
      <ol v-if="facts.length" class="fact-list">
        <li v-for="fact in facts" :key="fact.id" class="fact-record">
          <p class="muted"><time v-if="fact.occurred_at" :datetime="fact.occurred_at">{{ occurrenceTime(fact.occurred_at) }}</time><span v-else>{{ t('stories.unknownTime') }}</span></p>
          <router-link :to="{ path: `/platform/stories/${encodeURIComponent(fact.story_id)}`, hash: `#fact-${fact.id}` }">{{ fact.title }}</router-link>
          <p>{{ fact.subject }} · {{ fact.action }} · {{ fact.object }}</p><p v-if="fact.conditions" class="muted">{{ fact.conditions }}</p>
          <p v-if="fact.manual_corrected" class="muted">{{ t('groupingCorrection.manual') }}</p>
          <p v-if="fact.no_source" class="muted">{{ t('stories.noSource') }}</p>
          <ul class="sources"><li v-for="source in fact.sources" :key="source.entry_id"><t-button variant="text" @click="emit('open-source', source.entry_id)">{{ source.title }}</t-button><blockquote>{{ source.quote }}</blockquote></li></ul>
        </li>
      </ol>
      <t-empty v-else-if="!factsLoading && !factsError" :description="t('entityActivity.noFacts')" />
    </t-loading>
    <t-pagination v-if="total" v-model="page" :total="total" :page-size="pageSize" :show-page-size="false" :disabled="factsLoading" size="small" />
  </section>
</template>

<style scoped lang="less">
.entity-activity { margin: 20px 0; min-width: 0; }
.activity-heading { display: flex; gap: 12px; align-items: center; justify-content: space-between; }
.muted { color: var(--td-text-color-secondary); font-size: 13px; }
.activity-error { color: var(--td-error-color); margin: 12px 0; }
.fact-list, .sources { list-style: none; padding: 0; }
.fact-record { padding: 16px 0; border-bottom: 1px solid var(--td-component-stroke); overflow-wrap: anywhere; }
.fact-record p { margin: 8px 0; }
.fact-record a { color: var(--td-brand-color); }
.fact-record a:hover { text-decoration: underline; }
blockquote { border-left: 2px solid var(--td-component-stroke); margin: 8px 0; padding: 0 12px; white-space: pre-wrap; }
</style>
