<script setup lang="ts">
import { Button, EmptyState, ErrorState, LibMark, Skeleton } from '@/components/ui'
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'

const { t } = useI18n()
const router = useRouter()
const libraries = ref<LibraryCatalogItem[]>([])
const loading = ref(true)
const error = ref('')
let request = 0

async function load() {
  const current = ++request
  loading.value = true
  error.value = ''
  try {
    const response = await listLibraries()
    if (!response.success) throw new Error(response.message)
    if (current === request) libraries.value = response.data || []
  } catch {
    if (current === request) error.value = t('libraryNavigation.loadFailed')
  } finally {
    if (current === request) loading.value = false
  }
}

function openLibrary(id: string) {
  void router.push({ path: '/platform/entries', query: { libraries: id } })
}

onMounted(load)
onBeforeUnmount(() => { request++ })
</script>

<template>
  <main class="library-list" :aria-busy="loading">
    <header>
      <h1>{{ t('menu.library') }}</h1>
      <p>{{ t('libraryNavigation.hint') }}</p>
    </header>
    <ErrorState v-if="error" :title="error">
      <Button variant="ghost" @click="load">{{ t('common.retry') }}</Button>
    </ErrorState>
    <Skeleton v-if="loading" :label="t('common.loading')" />
    <div v-else>
      <div v-if="libraries.length" class="library-cards">
        <button v-for="library in libraries" :key="library.id" type="button" class="library-card" @click="openLibrary(library.id)">
          <LibMark :color="library.color" />
          <h2>{{ library.display_name || library.name }}</h2>
          <p v-if="library.description">{{ library.description }}</p>
          <span v-if="library.entry_count != null">{{ t('libraryNavigation.count', { count: library.entry_count }) }}</span>
        </button>
      </div>
      <EmptyState v-else-if="!error" :title="t('libraryNavigation.empty')">
        <p>{{ t('libraryNavigation.emptyHint') }}</p>
      </EmptyState>
    </div>
  </main>
</template>

<style scoped>
.library-list { flex: 1; min-height: 0; overflow: auto; padding: 24px 32px; color: var(--text); }
header { margin-bottom: 24px; }
h1 { margin: 0 0 8px; font-size: var(--type-page-size); font-weight: var(--type-page-weight); }
header p, .library-card p, .library-card > span { color: var(--muted); }
.library-cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 260px), 1fr)); gap: 16px; }
.library-card { display: flex; flex-direction: column; gap: 12px; align-items: flex-start; min-width: 0; padding: 24px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); color: inherit; font: inherit; text-align: left; cursor: pointer; overflow-wrap: anywhere; }
.library-card h2, .library-card p { margin: 0; }
.library-card h2 { font-size: 18px; }
.library-card p { line-height: 1.6; }
.library-card > span { margin-top: auto; font-size: var(--type-note-size); }
.library-card:hover { border-color: var(--accent); background: var(--surface-soft); }
.library-card:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
@media (max-width: 640px) { .library-list { padding: 16px; } }
</style>
