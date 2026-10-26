<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'

const { t } = useI18n()
const libraries = ref<LibraryCatalogItem[]>([])
const loading = ref(false)
const error = ref('')
let request = 0

async function load() {
  const current = ++request
  loading.value = true
  error.value = ''
  try {
    const response = await listLibraries()
    if (!response.success) throw new Error(response.message || t('library.loadingFailed'))
    if (current === request) libraries.value = response.data || []
  } catch (cause) {
    if (current === request) error.value = cause instanceof Error ? cause.message : t('library.loadingFailed')
  } finally {
    if (current === request) loading.value = false
  }
}

onMounted(load)
onUnmounted(() => { request++ })
</script>

<template>
  <section class="library-browser">
    <header class="library-browser-header">
      <h1>{{ t('library.title') }}</h1>
    </header>
    <t-alert v-if="error" theme="error" :message="error" role="alert">
      <template #operation><t-button variant="text" :loading="loading" @click="load">{{ t('common.retry') }}</t-button></template>
    </t-alert>
    <t-loading :loading="loading" show-overlay>
      <div v-if="libraries.length" class="library-grid">
        <RouterLink v-for="library in libraries" :key="library.id" class="library-card"
          :to="{ path: '/platform/entries', query: { libraries: library.id } }">
          <t-icon name="folder" class="library-card-icon" aria-hidden="true" />
          <h2>{{ library.display_name || library.name }}</h2>
          <p v-if="library.description">{{ library.description }}</p>
        </RouterLink>
      </div>
      <t-empty v-else-if="!loading && !error" :description="t('common.noData')" />
    </t-loading>
  </section>
</template>

<style scoped lang="less">
.library-browser { flex: 1; min-width: 0; height: 100%; overflow: auto; padding: 24px 32px; box-sizing: border-box; }
.library-browser-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; margin-bottom: 24px; }
h1 { margin: 0; font-size: 24px; color: var(--td-text-color-primary); }
.library-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(260px, 100%), 1fr)); gap: 16px; }
.library-card {
  display: flex; flex-direction: column; align-items: flex-start; gap: 12px; min-width: 0; min-height: 168px;
  padding: 24px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-large);
  background: var(--td-bg-color-container); color: var(--td-text-color-primary); text-decoration: none;
  &:hover { border-color: var(--td-brand-color); background: var(--td-bg-color-container-hover); }
  &:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
  h2 { margin: 0; font-size: 20px; overflow-wrap: anywhere; }
  p { margin: 0; line-height: 1.6; overflow-wrap: anywhere; color: var(--td-text-color-secondary); }
}
.library-card-icon { font-size: 28px; color: var(--td-brand-color); }
@media (max-width: 640px) { .library-browser { padding: 16px 12px; } }
</style>
