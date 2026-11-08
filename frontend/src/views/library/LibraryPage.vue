<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, ErrorState, Tabs } from '@/components/ui'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import Library from './Library.vue'
import ShelfStatusTable from './components/ShelfStatusTable.vue'
import ShelfReviewPage from './components/ShelfReviewPage.vue'

const route = useRoute(), router = useRouter(), { t } = useI18n()
const libraries = ref<LibraryCatalogItem[]>([]), loading = ref(true), error = ref('')
let generation = 0
const library = computed(() => {
  const ids = typeof route.query.libraries === 'string' ? route.query.libraries.split(',').filter(Boolean) : []
  return ids.length === 1 ? libraries.value.find(item => item.id === ids[0] && item.shelf_enabled) : undefined
})
const tab = computed(() => route.query.tab === 'review' ? 'review' : route.query.tab === 'documents' ? 'documents' : 'status')
const tabs = computed(() => [{ value: 'status', label: t('shelfStatus.status') }, { value: 'review', label: t('shelfReview.title') }, { value: 'documents', label: t('shelfStatus.documents') }])
function selectTab(value: string) { void router.replace({ query: { ...route.query, tab: value } }) }
async function load() {
  const current = ++generation; loading.value = true; error.value = ''
  try {
    const response = await listLibraries()
    if (!response.success) throw new Error(response.message)
    if (current === generation) libraries.value = response.data
  } catch { if (current === generation) error.value = t('libraryNavigation.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
onMounted(() => { void load(); window.addEventListener('reliquary:library-settings-saved', load) })
onBeforeUnmount(() => { generation++; window.removeEventListener('reliquary:library-settings-saved', load) })
</script>
<template>
  <div v-if="loading" class="shelf-page-loading" role="status">{{ t('common.loading') }}</div>
  <ErrorState v-else-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
  <section v-else-if="library && (!route.query.tab || ['status', 'documents', 'review'].includes(String(route.query.tab)))" class="shelf-library-page">
    <Tabs :model-value="tab" :items="tabs" :label="library.display_name || library.name" @update:model-value="selectTab">
      <template #status><ShelfStatusTable :key="library.id" :library="library" /></template>
      <template #review><ShelfReviewPage :key="library.id" :library="library" /></template>
      <template #documents><Library /></template>
    </Tabs>
  </section>
  <Library v-else />
</template>
<style scoped>
.shelf-library-page { flex: 1; min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow: auto; }
.shelf-library-page > :deep(.rq-tabs) { display: flex; flex-direction: column; flex: 1; min-height: 0; }
.shelf-library-page :deep(.rq-tabs__panel) { min-height: 0; overflow: auto; }
.shelf-library-page :deep(.rq-tabs__panel[data-state=active]) { display: flex; flex-direction: column; flex: 1; }
.shelf-page-loading { padding: var(--space-5); }
</style>
