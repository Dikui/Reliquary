<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getStory, listStories, type Story, type StoryDetail } from '@/api/stories'

const { t, locale } = useI18n()
const route = useRoute(), router = useRouter()
const pageSize = 20
const selectedID = computed(() => typeof route.params.storyId === 'string' ? route.params.storyId : '')
const page = computed(() => {
  const value = Number(route.query.page)
  return Number.isSafeInteger(value) && value > 0 && value <= 1000000 ? value : 1
})
const items = ref<Story[]>([]), total = ref(0), detail = ref<StoryDetail | null>(null)
const loading = ref(false), error = ref(''), notFound = ref(false)
let version = 0, disposed = false
const listLocation = computed(() => ({ path: '/platform/stories', query: route.query }))
const timestamp = (value: string) => new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })
const occurrenceTime = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : timestamp(value)
const entryLocation = (id: string) => ({ path: '/platform/entries', query: { entry_id: id } })
const entityLocation = (id: string) => ({ path: '/platform/entries', query: { tab: 'entities', entity_id: id } })
function changePage(value: number) {
  void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } })
}
async function load() {
  const ticket = ++version, id = selectedID.value
  loading.value = true; error.value = ''; notFound.value = false
  try {
    if (id) {
      const response = await getStory(id)
      if (!response.success) throw new Error(t('stories.loadFailed'))
      if (ticket === version && !disposed) detail.value = response.data
    } else {
      const response = await listStories(pageSize, (page.value - 1) * pageSize)
      if (!response.success) throw new Error(t('stories.loadFailed'))
      if (ticket !== version || disposed) return
      items.value = response.data.items; total.value = response.data.total
      const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
      if (page.value > lastPage) changePage(lastPage)
    }
  } catch (e: unknown) {
    if (ticket !== version || disposed) return
    notFound.value = (e as { status?: number })?.status === 404
    error.value = t(notFound.value ? 'stories.notFound' : 'stories.loadFailed')
  } finally {
    if (ticket === version && !disposed) loading.value = false
  }
}
watch(() => [selectedID.value, page.value], () => {
  detail.value = null; items.value = []
  void load()
}, { immediate: true })
watch(() => [locale.value, detail.value?.story.title, selectedID.value], () => {
  document.title = `${selectedID.value ? detail.value?.story.title || t('stories.detail') : t('stories.title')} · Reliquary`
}, { immediate: true })
onUnmounted(() => { disposed = true; version++ })
</script>

<template>
  <main class="stories-page">
    <header>
      <router-link v-if="selectedID" :to="listLocation">{{ t('stories.back') }}</router-link>
      <div class="heading">
        <h1>{{ selectedID ? detail?.story.title || t('stories.detail') : t('stories.title') }}</h1>
        <t-button variant="outline" :loading="loading" @click="load">{{ t('common.refresh') }}</t-button>
      </div>
      <p v-if="!selectedID" class="muted">{{ t('stories.description') }}</p>
    </header>
    <div v-if="error" class="error" role="alert">
      {{ error }}
      <t-button v-if="!notFound" variant="text" :disabled="loading" @click="load">{{ t('common.retry') }}</t-button>
    </div>
    <section :aria-busy="loading" :aria-label="t(selectedID ? 'stories.timeline' : 'stories.title')" class="content">
      <t-loading :loading="loading">
        <template v-if="!selectedID">
          <ol v-if="items.length" class="records">
            <li v-for="story in items" :key="story.id" class="record">
              <h2><router-link :to="{ path: `/platform/stories/${encodeURIComponent(story.id)}`, query: route.query }">{{ story.title }}</router-link></h2>
              <dl class="times">
                <dt>{{ t('stories.firstFact') }}</dt><dd><time :datetime="story.first_fact_at">{{ timestamp(story.first_fact_at) }}</time></dd>
                <dt>{{ t('stories.lastFact') }}</dt><dd><time :datetime="story.last_fact_at">{{ timestamp(story.last_fact_at) }}</time></dd>
              </dl>
            </li>
          </ol>
          <t-empty v-else-if="!loading && !error" :description="t('stories.empty')" />
        </template>
        <template v-else-if="detail">
          <dl class="times">
            <dt>{{ t('stories.firstFact') }}</dt><dd><time :datetime="detail.story.first_fact_at">{{ timestamp(detail.story.first_fact_at) }}</time></dd>
            <dt>{{ t('stories.lastFact') }}</dt><dd><time :datetime="detail.story.last_fact_at">{{ timestamp(detail.story.last_fact_at) }}</time></dd>
          </dl>
          <h2>{{ t('stories.timeline') }}</h2>
          <ol v-if="detail.facts.length" class="records timeline">
            <li v-for="fact in detail.facts" :key="fact.id" :id="`fact-${fact.id}`" class="record">
              <p class="muted"><time v-if="fact.occurred_at" :datetime="fact.occurred_at">{{ occurrenceTime(fact.occurred_at) }}</time><span v-else>{{ t('stories.unknownTime') }}</span></p>
              <h3>{{ fact.title }}</h3>
              <dl class="frame">
                <dt>{{ t('stories.subject') }}</dt><dd><router-link v-if="fact.subject_entity_id" :to="entityLocation(fact.subject_entity_id)">{{ fact.subject }}</router-link><template v-else>{{ fact.subject }}</template></dd>
                <dt>{{ t('stories.action') }}</dt><dd>{{ fact.action }}</dd>
                <dt v-if="fact.object">{{ t('stories.object') }}</dt><dd v-if="fact.object"><router-link v-if="fact.object_entity_id" :to="entityLocation(fact.object_entity_id)">{{ fact.object }}</router-link><template v-else>{{ fact.object }}</template></dd>
                <dt v-if="fact.conditions">{{ t('stories.conditions') }}</dt><dd v-if="fact.conditions">{{ fact.conditions }}</dd>
              </dl>
              <h4>{{ t('stories.sources') }}</h4>
              <ul class="sources">
                <li v-for="source in fact.sources" :key="source.entry_id">
                  <router-link :to="entryLocation(source.entry_id)">{{ source.title }}</router-link>
                  <blockquote>{{ source.quote }}</blockquote>
                </li>
              </ul>
            </li>
          </ol>
          <t-empty v-else :description="t('stories.noFacts')" />
        </template>
      </t-loading>
    </section>
    <t-pagination v-if="!selectedID && total > 0" :current="page" :page-size="pageSize" :total="total" :show-page-size="false" @current-change="changePage" />
  </main>
</template>

<style scoped lang="less">
.stories-page { height: 100%; overflow: auto; padding: 24px; box-sizing: border-box; color: var(--td-text-color-primary); }
header, .content { margin-bottom: 20px; }
.heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
h1 { font-size: 24px; margin: 12px 0; overflow-wrap: anywhere; }
h2 { font-size: 18px; margin: 0 0 16px; }
h3 { font-size: 16px; margin: 8px 0 16px; }
h4 { font-size: 14px; margin: 16px 0 8px; }
p { margin: 8px 0; }
.content { min-height: 180px; }
.records, .sources { list-style: none; padding: 0; margin: 0; }
.records { display: grid; gap: 16px; }
.record { padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); overflow-wrap: anywhere; }
.timeline .record { border-inline-start: 3px solid var(--td-brand-color); }
.times, .frame { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; font-size: 13px; }
dt, .muted { color: var(--td-text-color-secondary); }
dd { margin: 0; overflow-wrap: anywhere; }
blockquote { margin: 12px 0 20px; padding-inline-start: 16px; border-inline-start: 2px solid var(--td-component-stroke); white-space: pre-wrap; overflow-wrap: anywhere; }
.error { color: var(--td-error-color); padding-block: 12px; }
a { color: var(--td-brand-color); cursor: pointer; text-decoration: none; }
a:hover { text-decoration: underline; }
a:active { color: var(--td-brand-color-active); }
a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 640px) { .stories-page { padding: 16px; } .record { padding: 16px; } .times, .frame { grid-template-columns: minmax(0, 1fr); gap: 4px; } dd { margin-bottom: 8px; } }
</style>
