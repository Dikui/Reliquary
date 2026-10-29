<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { LibMark, Tooltip } from '@/components/ui'
import PendingCountBadge from '@/components/PendingCountBadge.vue'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { listIngestFailures } from '@/api/ingest-diagnostics'
import { useMenuStore } from '@/stores/menu'
import { useUIStore } from '@/stores/ui'
import { useFollowStore } from '@/stores/follows'
import { usePendingCountsStore } from '@/stores/pendingCounts'
import { useCommandPaletteStore } from '@/stores/commandPalette'

const props = defineProps<{ mobileOverlay?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t, n } = useI18n()
const route = useRoute()
const ui = useUIStore()
const follows = useFollowStore()
const pending = usePendingCountsStore()
const commands = useCommandPaletteStore()
const { menuGroups } = storeToRefs(useMenuStore())
const sidebarCollapsed = computed(() => !props.mobileOverlay && ui.sidebarCollapsed)
const libraries = ref<LibraryCatalogItem[]>([])
const catalogLoading = ref(true), catalogError = ref(false)
const failureCount = ref<number | null>(null)
let request = 0
let timer: ReturnType<typeof setInterval> | undefined
const validCount = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
const entryCount = computed(() => {
  if (catalogLoading.value || catalogError.value || libraries.value.some(library => !validCount(library.entry_count))) return null
  const total = libraries.value.reduce((sum, library) => sum + library.entry_count!, 0)
  return validCount(total) ? total : null
})
const selectedLibraries = computed(() => String(route.query.libraries || '').split(',').filter(Boolean))
const isMaterials = computed(() => route.name === 'entryDetail' || (route.path === '/platform/entries' && !['entities', 'reviews'].includes(String(route.query.tab))))
function labelWithCount(label: string, count: unknown) {
  return validCount(count) ? t('menu.itemCount', { name: label, count: n(count) }) : label
}
function navigationLabel(key: string, title: string) {
  if (key === 'articles') return labelWithCount(title, entryCount.value)
  if (key === 'follows') return labelWithCount(title, follows.stateError ? null : follows.newCount)
  if (key === 'pending') return labelWithCount(title, pending.total)
  if (key === 'diagnostics') return labelWithCount(title, failureCount.value)
  return title
}
function active(path: string, tab?: string) {
  if (path === 'libraries') return route.path === '/platform/libraries' || (isMaterials.value && selectedLibraries.value.length !== 1)
  if (path === 'entries') return route.path === '/platform/entries' && route.query.tab === tab
  return route.path === `/platform/${path}`
}
function libraryActive(id: string) {
  return isMaterials.value && selectedLibraries.value.length === 1 && selectedLibraries.value[0] === id
}
function closeNavigation() { if (props.mobileOverlay) emit('close') }
async function openCommands() {
  closeNavigation()
  await nextTick()
  commands.openPalette()
}
async function refreshCatalog(current: number) {
  catalogLoading.value = true
  try {
    const response = await listLibraries()
    if (!response.success || !Array.isArray(response.data)) throw new Error('Library catalogue unavailable')
    if (current === request) { libraries.value = response.data; catalogError.value = false }
  } catch { if (current === request) catalogError.value = true }
  finally { if (current === request) catalogLoading.value = false }
}
async function refreshFailures(current: number) {
  failureCount.value = null
  try {
    const response = await listIngestFailures({ page: 1, page_size: 1 })
    if (current === request && response.success && validCount(response.data?.total)) failureCount.value = response.data.total
  } catch { /* An unavailable count stays hidden. */ }
}
function refreshNavigation() {
  if (document.visibilityState === 'hidden') return
  const current = ++request
  void refreshCatalog(current)
  void refreshFailures(current)
  void follows.refreshState()
}
function settingsSaved(event: Event) {
  const changed = (event as CustomEvent<Partial<LibraryCatalogItem>>).detail
  if (changed?.id) {
    libraries.value = libraries.value.map(library => library.id === changed.id ? { ...library, ...changed, display_name: changed.name || library.display_name } : library)
  }
  void refreshCatalog(++request)
}
function refreshAllCounts() { refreshNavigation(); if (document.visibilityState !== 'hidden') void pending.refresh() }
watch(() => route.fullPath, refreshAllCounts)
onMounted(() => {
  // PendingCountBadge owns its initial request.
  refreshNavigation()
  timer = setInterval(refreshAllCounts, 30000)
  document.addEventListener('visibilitychange', refreshAllCounts)
  window.addEventListener('reliquary:library-settings-saved', settingsSaved)
})
onUnmounted(() => {
  request++
  clearInterval(timer)
  document.removeEventListener('visibilitychange', refreshAllCounts)
  window.removeEventListener('reliquary:library-settings-saved', settingsSaved)
})
</script>

<template>
  <aside class="rq-sidebar" :class="{ 'rq-sidebar--collapsed': sidebarCollapsed, 'rq-sidebar--mobile': mobileOverlay }">
    <div class="rq-sidebar__brand-row">
      <RouterLink v-if="!sidebarCollapsed" class="rq-sidebar__brand" to="/platform/libraries" aria-current-value="false" @click="closeNavigation">
        <span class="rq-sidebar__brand-mark" aria-hidden="true" />Reliquary
      </RouterLink>
      <Tooltip v-if="!mobileOverlay" :content="t(sidebarCollapsed ? 'menu.expandSidebar' : 'menu.collapseSidebar')">
        <button type="button" class="rq-sidebar__toggle" :aria-label="t(sidebarCollapsed ? 'menu.expandSidebar' : 'menu.collapseSidebar')" @click="ui.toggleSidebar()">
          <span aria-hidden="true">{{ sidebarCollapsed ? '›' : '‹' }}</span>
        </button>
      </Tooltip>
    </div>
    <Tooltip :content="t('menu.commandSearch')" :disabled="!sidebarCollapsed">
      <button type="button" class="rq-sidebar__command" :aria-label="t('menu.commandSearch')" @click="openCommands">
        <span v-if="sidebarCollapsed" aria-hidden="true">⌕</span>
        <template v-else><span>{{ t('menu.commandSearch') }}</span><kbd>⌘K</kbd></template>
      </button>
    </Tooltip>
    <div class="rq-sidebar__scroll">
      <nav class="rq-sidenav" :aria-label="t('menu.mainNavigation')">
        <Tooltip v-for="item in menuGroups[0]?.items" :key="item.key" :content="item.title" :disabled="!sidebarCollapsed">
          <RouterLink class="rq-sidenav__link" :to="{ path: `/platform/${item.path}`, query: item.tab ? { tab: item.tab } : {} }"
            :aria-label="navigationLabel(item.key, item.title)" :aria-current="active(item.path, item.tab) ? 'page' : undefined" @click="closeNavigation">
            <span v-if="sidebarCollapsed" aria-hidden="true">{{ item.title.slice(0, 1) }}</span>
            <span v-else class="rq-sidenav__label">{{ item.title }}</span>
            <span v-if="item.key === 'articles' && entryCount !== null" class="rq-sidenav__count">{{ n(entryCount) }}</span>
            <span v-if="item.key === 'follows' && !follows.stateError && validCount(follows.newCount) && follows.newCount > 0" class="rq-sidenav__count">{{ n(follows.newCount) }}</span>
            <PendingCountBadge v-if="item.key === 'pending'" />
          </RouterLink>
        </Tooltip>
      </nav>
      <h2 class="rq-sidebar__group" :class="{ 'rq-sidebar__group--compact': sidebarCollapsed }">{{ t('menu.libraries') }}</h2>
      <nav class="rq-sidenav" :aria-label="t('menu.libraries')" :aria-busy="catalogLoading">
        <Tooltip v-for="library in libraries" :key="library.id" :content="library.display_name || library.name" :disabled="!sidebarCollapsed">
          <RouterLink class="rq-sidenav__link" :to="{ path: '/platform/entries', query: { libraries: library.id } }"
            :aria-label="labelWithCount(library.display_name || library.name, catalogLoading || catalogError ? null : library.entry_count)" :aria-current="libraryActive(library.id) ? 'page' : undefined" @click="closeNavigation">
            <LibMark :color="library.color" />
            <span v-if="!sidebarCollapsed" class="rq-sidenav__label">{{ library.display_name || library.name }}</span>
            <span v-if="!catalogLoading && !catalogError && validCount(library.entry_count)" class="rq-sidenav__count">{{ n(library.entry_count) }}</span>
          </RouterLink>
        </Tooltip>
      </nav>
      <p v-if="catalogError" class="rq-sidebar__status" role="alert">
        <span v-if="!sidebarCollapsed">{{ t('libraryNavigation.loadFailed') }}</span>
        <button type="button" :aria-label="t('common.retry')" @click="refreshAllCounts">{{ t('common.retry') }}</button>
      </p>
      <p v-else-if="!libraries.length && !sidebarCollapsed" class="rq-sidebar__status" role="status">{{ t(catalogLoading ? 'common.loading' : 'libraryNavigation.empty') }}</p>
      <h2 class="rq-sidebar__group" :class="{ 'rq-sidebar__group--compact': sidebarCollapsed }">{{ t('ingestDiagnostics.diagnostics') }}</h2>
      <nav class="rq-sidenav" :aria-label="t('ingestDiagnostics.diagnostics')">
        <Tooltip v-for="item in menuGroups[1]?.items" :key="item.key" :content="item.title" :disabled="!sidebarCollapsed">
          <RouterLink class="rq-sidenav__link" :to="`/platform/${item.path}`" :aria-label="navigationLabel(item.key, item.title)"
            :aria-current="active(item.path) ? 'page' : undefined" @click="closeNavigation">
            <span v-if="sidebarCollapsed" aria-hidden="true">{{ item.title.slice(0, 1) }}</span>
            <span v-else class="rq-sidenav__label">{{ item.title }}</span>
            <span v-if="item.key === 'diagnostics' && failureCount !== null" class="rq-sidenav__count rq-sidenav__count--bad">{{ n(failureCount) }}</span>
          </RouterLink>
        </Tooltip>
      </nav>
    </div>
    <nav class="rq-sidenav rq-sidebar__footer" :aria-label="t('menu.settings')">
      <Tooltip :content="t('menu.settings')" :disabled="!sidebarCollapsed">
        <RouterLink class="rq-sidenav__link" to="/platform/settings" :aria-label="t('menu.settings')"
          :aria-current="active('settings') ? 'page' : undefined" @click="closeNavigation">
          <svg v-if="sidebarCollapsed" aria-hidden="true" width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor">
            <path d="M8 2h4l.5 2 1.5.9 2-.6 2 3.4-1.5 1.4v1.8l1.5 1.4-2 3.4-2-.6-1.5.9-.5 2H8l-.5-2-1.5-.9-2 .6-2-3.4 1.5-1.4V9.1L2 7.7l2-3.4 2 .6L7.5 4Z" />
            <circle cx="10" cy="10" r="2.5" />
          </svg>
          <span v-else>{{ t('menu.settings') }}</span>
        </RouterLink>
      </Tooltip>
    </nav>
  </aside>
</template>

<style scoped>
.rq-sidebar { display: flex; flex-direction: column; flex: 0 0 var(--sidebar); width: var(--sidebar); min-width: 0; height: 100%; padding: var(--space-5) var(--space-3); box-sizing: border-box; border-right: 2px solid var(--rule); background: var(--surface); color: var(--text); font: var(--type-body-weight) var(--type-body-size)/var(--leading) var(--font); }
.rq-sidebar__brand-row { display: flex; align-items: center; gap: var(--space-1); margin-bottom: var(--space-3); }
.rq-sidebar__brand { display: flex; align-items: center; gap: var(--space-2); min-width: 0; padding-left: var(--space-2); color: var(--text); text-decoration: none; font-size: 22.4px; font-weight: 800; letter-spacing: -0.035em; }
.rq-sidebar__brand-mark { width: 14.4px; height: 14.4px; flex-shrink: 0; background: var(--accent); }
.rq-sidebar button { border-radius: var(--radius); font: inherit; cursor: pointer; }
.rq-sidebar__toggle { display: grid; place-items: center; flex-shrink: 0; width: 28px; height: 32px; margin-left: auto; border: 0; background: transparent; color: var(--text); }
.rq-sidebar .rq-sidebar__command { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); width: 100%; margin-bottom: var(--space-3); padding: var(--space-2); border: 1px solid var(--border); background: var(--surface-soft); color: var(--muted); text-align: left; font-size: var(--text-sm); }
.rq-sidebar__command span { min-width: 0; overflow-wrap: anywhere; }
.rq-sidebar kbd { flex-shrink: 0; padding-inline: 4.8px; border: 1px solid var(--border); background: var(--surface); color: var(--muted); font: var(--text-xs)/var(--leading) var(--mono); }
.rq-sidebar__scroll { flex: 1; min-height: 0; overflow: auto; scrollbar-gutter: stable; }
.rq-sidenav { display: flex; flex-direction: column; gap: 1px; }
.rq-sidenav__link { display: flex; align-items: center; position: relative; gap: var(--space-2); min-width: 0; padding: 4.8px var(--space-2); color: var(--text); text-decoration: none; cursor: pointer; }
.rq-sidenav__label { min-width: 0; overflow-wrap: anywhere; }
.rq-sidenav__link:hover, .rq-sidebar__toggle:hover, .rq-sidebar__command:hover { background: var(--surface-soft); color: var(--text); }
.rq-sidenav__link[aria-current="page"] { background: var(--text); color: var(--bg); font-weight: 700; }
.rq-sidebar :is(a, button):focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.rq-sidenav__count { flex-shrink: 0; margin-left: auto; font: var(--text-xs)/var(--leading) var(--mono); opacity: 0.65; }
.rq-sidenav__count--bad { color: var(--danger); opacity: 1; font-weight: 700; }
.rq-sidenav__link[aria-current="page"] .rq-sidenav__count--bad { color: inherit; }
.rq-sidenav__link :deep(.pending-count) { margin-left: auto; }
.rq-sidebar__group { margin: var(--space-3) 0 0; padding: var(--space-2) var(--space-2) var(--space-1); border-top: 2px solid var(--rule); font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); letter-spacing: 0.04em; }
.rq-sidebar__status { margin: var(--space-2); color: var(--muted); font-size: var(--text-xs); overflow-wrap: anywhere; }
.rq-sidebar__status button { border: 0; padding: var(--space-1); background: transparent; color: var(--text); text-decoration: underline; }
.rq-sidebar__status button:hover { color: var(--accent); }
.rq-sidebar__footer { flex-shrink: 0; padding-top: var(--space-4); }
.rq-sidebar--collapsed { flex-basis: 72px; width: 72px; padding-inline: var(--space-2); }
.rq-sidebar--collapsed .rq-sidebar__toggle { margin-inline: auto; }
.rq-sidebar--collapsed .rq-sidebar__command, .rq-sidebar--collapsed .rq-sidenav__link { justify-content: center; min-height: 44px; padding-inline: var(--space-1); }
.rq-sidebar--collapsed :is(.rq-sidenav__count, :deep(.pending-count)) { position: absolute; top: 0; right: 0; font-size: 10.4px; opacity: 1; background: inherit; }
.rq-sidebar__group--compact { height: 0; padding: 0; overflow: hidden; color: transparent; }
.rq-sidebar--mobile { flex-basis: auto; width: 100%; border: 0; }
.rq-sidebar--mobile .rq-sidenav__link { min-height: 44px; }
</style>
