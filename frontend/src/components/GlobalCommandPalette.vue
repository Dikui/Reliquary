<template>
  <Dialog v-model="dialogVisible" :title="t('commandPalette.title')" :layer="2400" :closable="false"
    content-class="rq-cmdk-dialog" @open-auto-focus="onOpenAutoFocus" @close-auto-focus="restoreFocus">
    <div class="rq-cmdk" @keydown="onKeyDown">
      <div class="rq-cmdk__input-row">
        <input ref="inputRef" v-model="query" type="text" class="rq-cmdk__input cmdk__input"
          :placeholder="t('commandPalette.placeholder')" :aria-label="t('commandPalette.placeholder')"
          :aria-busy="loading" role="combobox" aria-autocomplete="list" :aria-expanded="open" :aria-controls="resultsId"
          :aria-activedescendant="flatItems.length ? `${resultsId}-${selectedIndex}` : undefined" spellcheck="false" @compositionstart="composing = true" @compositionend="composing = false" />
        <span v-if="loading" class="rq-cmdk__loading" role="status">{{ t('commandPalette.searching') }}</span>
        <button v-if="query" type="button" class="rq-icon-button" :aria-label="t('knowledgeSearch.clearQuery')" @click="query = ''; inputRef?.focus()">×</button>
        <button type="button" class="rq-cmdk__close" :aria-label="t('common.close')" @click="handleClose"><kbd>Esc</kbd></button>
      </div>
      <div class="rq-cmdk__scope">
        <span>{{ t('commandPalette.scope') }}</span>
        <Chip :model-value="!libraryScope.length" @update:model-value="libraryScope = []">{{ t('commandPalette.allLibraries') }}</Chip>
        <Chip v-if="currentLibrary" :model-value="libraryScope.length === 1 && libraryScope[0] === currentLibrary.id"
          @update:model-value="libraryScope = [currentLibrary.id]">{{ t('commandPalette.currentLibrary', { name: currentLibrary.display_name || currentLibrary.name }) }}</Chip>
        <details class="rq-cmdk__libraries">
          <summary>{{ t('commandPalette.chooseLibraries') }}<span v-if="libraryScope.length"> ({{ libraryScope.length }})</span></summary>
          <LibraryScopeSelect v-model="libraryScope" swiss @loaded="libraryCatalog = $event" />
        </details>
        <button type="button" class="rq-cmdk__settings" @click="drawerVisible = true">{{ t('commandPalette.retrieval') }}</button>
      </div>
      <div :id="resultsId" ref="scrollRef" class="rq-cmdk__results" role="listbox" :aria-label="t('commandPalette.results')">
        <template v-if="!query.trim()">
          <ResultGroup v-if="recentItems.length" :label="t('commandPalette.group.recent')" :action="t('commandPalette.clearRecent')" @action="commandPaletteStore.clearRecent()">
            <ResultItem v-for="(recent, i) in recentItems" :key="recent.key" :id="`${resultsId}-${flatIndexFor('recent', i)}`" :index="flatIndexFor('recent', i)"
              :selected="selectedIndex === flatIndexFor('recent', i)" :shortcut="shortcutFor(flatIndexFor('recent', i))"
              :title="recent.title" @primary="recent.run" @hover="selectItemAt" />
          </ResultGroup>
          <ResultGroup :label="t('commandPalette.group.quickActions')">
            <ResultItem v-for="(c, i) in quickCommands" :key="c.id" :id="`${resultsId}-${flatIndexFor('quick', i)}`" :index="flatIndexFor('quick', i)"
              :selected="selectedIndex === flatIndexFor('quick', i)" :shortcut="shortcutFor(flatIndexFor('quick', i))"
              :title="c.label" @primary="c.run" @hover="selectItemAt" />
          </ResultGroup>
        </template>
        <template v-else>
          <ResultGroup v-if="visibleEntries.length" :label="t('commandPalette.group.entries')">
            <ResultItem v-for="(file, i) in visibleEntries" :key="file.entryId" :id="`${resultsId}-${flatIndexFor('entries', i)}`" :index="flatIndexFor('entries', i)"
              :selected="selectedIndex === flatIndexFor('entries', i)" :shortcut="shortcutFor(flatIndexFor('entries', i))"
              :title="file.title" :cannot-be-fact="file.cannot_be_fact" :source-unverified="file.source_unverified" :acquisition="file.acquisition"
              @primary="openEntry(file)" @hover="selectItemAt">
              <template #icon><LibMark v-if="libraryFor(file.libraryId)" :color="libraryFor(file.libraryId)!.color" /><span v-else class="rq-cmdk__icon" /></template>
              <template #meta><span class="rq-cmdk__id" :title="file.entryId">{{ shortId(file.entryId) }}</span></template>
              <template #subtitle>
                <span v-if="file.libraryName">{{ file.libraryName }}</span>
                <Tag v-if="file.contentType">{{ contentTypeLabel(file.contentType) }}</Tag>
                <span v-if="file.publishedAt">{{ file.publishedAt.slice(0, 10) }}</span>
                <EntryUpdateNotice :has-update="file.has_update" :latest-entry-id="file.latest_entry_id" />
                <template v-if="!hasLocatedBodyChunk(file)">
                  <span v-if="file.chunks[0]?.content" class="rq-cmdk__excerpt" v-html="highlight(file.chunks[0].content)" />
                  <span @click.stop @keydown.enter.stop @keydown.space.stop><SearchImage v-if="file.imagePath" :article-id="file.entryId" :path="file.imagePath" :title="file.title" compact interactive swiss :preview-layer="2500" /></span>
                </template>
              </template>
            </ResultItem>
          </ResultGroup>
          <ResultGroup v-if="flatChunkItems.length" :label="t('commandPalette.group.chunks')">
            <ResultItem v-for="(item, i) in flatChunkItems" :key="item.file.entryId" :id="`${resultsId}-${flatIndexFor('chunks', i)}`" :index="flatIndexFor('chunks', i)"
              :selected="selectedIndex === flatIndexFor('chunks', i)" :shortcut="shortcutFor(flatIndexFor('chunks', i))"
              :badge="item.chunk.image_path ? t('knowledgeSearch.imageMatch') : item.chunk.match_type === 'vector' ? t('commandPalette.match.vector') : t('commandPalette.match.keyword')"
              :score="item.chunk.score" :source-unverified="item.file.source_unverified"
              :cannot-be-fact="item.file.cannot_be_fact" :acquisition="item.file.acquisition"
              @primary="openChunk(item)" @hover="selectItemAt">
              <template #icon><LibMark v-if="libraryFor(item.file.libraryId)" :color="libraryFor(item.file.libraryId)!.color" /><span v-else class="rq-cmdk__icon" /></template>
              <template #title><span>{{ item.file.title }}</span><span v-if="item.chunk.locations?.[0]?.paragraph" class="rq-cmdk__id">¶{{ item.chunk.locations[0].paragraph }}</span></template>
              <template #subtitle>
                <span class="rq-cmdk__excerpt" v-html="highlight(item.chunk.matched_content || item.chunk.content)" />
                <span @click.stop @keydown.enter.stop @keydown.space.stop><SearchImage v-if="item.file.imagePath" :article-id="item.file.entryId" :path="item.file.imagePath" :title="item.file.title" compact interactive swiss :preview-layer="2500" /></span>
              </template>
            </ResultItem>
          </ResultGroup>
          <ResultGroup v-if="entities.length" :label="t('commandPalette.group.entities')">
            <ResultItem v-for="(entity, i) in entities" :key="entity.id" :id="`${resultsId}-${flatIndexFor('entities', i)}`" :index="flatIndexFor('entities', i)"
              :selected="selectedIndex === flatIndexFor('entities', i)" :shortcut="shortcutFor(flatIndexFor('entities', i))"
              :title="entity.canonical_name" @primary="openEntity(entity)" @hover="selectItemAt">
              <template #subtitle><span>{{ t(`knowledgeWorkbench.entities.types.${entity.entity_type}`) }}</span><span>{{ libraryFor(entity.library_id)?.display_name || libraryFor(entity.library_id)?.name }}</span></template>
            </ResultItem>
          </ResultGroup>
          <div v-if="!loading && (searchError || entityError)" class="rq-cmdk__empty" role="alert">
            <p v-if="searchError">{{ searchError }}</p><p v-if="entityError">{{ entityError }}</p>
            <Button @click="retry">{{ t('common.retry') }}</Button>
          </div>
          <div v-else-if="!loading && !visibleEntries.length && !entities.length && visibleCommands.length === 1 && hasSearched" class="rq-cmdk__empty">
            <p>{{ t('commandPalette.empty.noResults') }}</p>
            <Button @click="drawerVisible = true">{{ t('commandPalette.empty.adjustRetrieval') }}</Button>
          </div>
        </template>
        <ResultGroup v-if="visibleCommands.length" :label="t('commandPalette.group.commands')">
          <ResultItem v-for="(c, i) in visibleCommands" :key="c.id" :id="`${resultsId}-${flatIndexFor('commands', i)}`" :index="flatIndexFor('commands', i)"
            :selected="selectedIndex === flatIndexFor('commands', i)" :shortcut="c.id === 'full-search' ? '↵' : shortcutFor(flatIndexFor('commands', i))"
            :title="c.label" @primary="c.run" @hover="selectItemAt" />
        </ResultGroup>
      </div>
      <div class="rq-cmdk__footer">
        <span><kbd>↑</kbd><kbd>↓</kbd> {{ t('commandPalette.hotkey.select') }} · <kbd>↵</kbd> {{ t('commandPalette.hotkey.enter') }} · <kbd>⌘1</kbd>-<kbd>⌘9</kbd> {{ t('commandPalette.hotkey.cmdNumber') }} · <kbd>Esc</kbd> {{ t('commandPalette.hotkey.esc') }}</span>
        <a :href="fullSearchHref" @click.prevent="openFullSearch">{{ t('commandPalette.viewAll') }}</a>
      </div>
    </div>
    <Drawer v-model="drawerVisible" :layer="2450" :title="t('retrievalSettings.title')" content-class="rq-cmdk-retrieval">
      <RetrievalSettings swiss />
    </Drawer>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useCommandPaletteStore, type CmdkRecentTarget } from '@/stores/commandPalette'
import { useTheme } from '@/composables/useTheme'
import { Button, Chip, Dialog, Drawer, LibMark, Tag } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import type { Entity } from '@/api/entities'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { useCmdkSearch, type CmdkFileGroup, type CmdkChunk } from './GlobalCommandPalette/useSearch'
import { visibleFileChunks } from './GlobalCommandPalette/visibleFileChunks'
import { highlightText } from './GlobalCommandPalette/useHighlight'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import { buildCommands, filterCommands } from './GlobalCommandPalette/commands'
import ResultGroup from './GlobalCommandPalette/ResultGroup.vue'
import ResultItem from './GlobalCommandPalette/ResultItem.vue'
import LibraryScopeSelect from './LibraryScopeSelect.vue'
import { entryDetailLocation } from '@/utils/bodyReference'
import EntryUpdateNotice from '@/components/EntryUpdateNotice.vue'
import SearchImage from './SearchImage.vue'
import RetrievalSettings from '@/views/settings/RetrievalSettings.vue'

const { t } = useI18n()
const route = useRoute(), router = useRouter()
const { currentTheme, setTheme } = useTheme()
const commandPaletteStore = useCommandPaletteStore()
const { open, initialQuery, recentQueries, recentTargets } = storeToRefs(commandPaletteStore)
const resultsId = useId()
const libraryCatalog = ref<LibraryCatalogItem[]>([])
const { libraryScope, query, composing, loading, hasSearched, fileGroups, entities, clearResults, searchError, entityError, retry } = useCmdkSearch({ libraries: libraryCatalog })
const drawerVisible = ref(false)
const inputRef = ref<HTMLInputElement | null>(null), scrollRef = ref<HTMLElement | null>(null)
const returnFocus = ref<HTMLElement | null>(null)
const selectedKey = ref<string | null>(null)
const dialogVisible = computed({ get: () => open.value, set: (value: boolean) => { if (!value) handleClose() } })
const currentLibrary = computed(() => {
  const ids = String(route.query?.libraries || '').split(',').filter(Boolean)
  return libraryCatalog.value.find(library => library.id === (ids.length === 1 ? ids[0] : undefined))
})
const libraryFor = (id: string) => libraryCatalog.value.find(library => library.id === id)
const contentTypeLabel = (value: string) => CONTENT_TYPES.includes(value as ContentType) ? t(`library.contentTypes.${value}`) : value
const shortId = (id: string) => id.length > 12 ? `${id.slice(0, 8)}…` : id
interface FlatChunkItem { file: CmdkFileGroup; chunk: CmdkChunk }
const visibleEntries = computed(() => fileGroups.value.slice(0, 5))
const flatChunkItems = computed<FlatChunkItem[]>(() => visibleFileChunks(fileGroups.value, 5))
const hasLocatedBodyChunk = (file: CmdkFileGroup) => visibleFileChunks([file], 1).length > 0
const commandRouter = { push: (path: string) => { commandPaletteStore.pushTarget({ title: quickTitle(path), path }); return router.push(path) } } as typeof router
function quickTitle(path: string) {
  const labels: Record<string, string> = { '/platform/entries': t('commandPalette.quick.knowledge'), '/platform/search': t('commandPalette.quick.search'), '/platform/follows': t('commandPalette.quick.follows'), '/platform/settings': t('commandPalette.quick.settings') }
  return labels[path] || path
}
const allCommands = computed(() => buildCommands({ router: commandRouter, t, close: handleClose, toggleTheme: () => setTheme((currentTheme.value === 'system' ? document.documentElement.getAttribute('theme-mode') : currentTheme.value) === 'dark' ? 'light' : 'dark') }))
const quickCommands = computed(() => allCommands.value.filter(command => command.kind !== 'command'))
const visibleCommands = computed(() => query.value.trim()
  ? [...filterCommands(allCommands.value, query.value), { id: 'full-search', label: t('commandPalette.searchQuery', { query: query.value.trim() }), run: openFullSearch }]
  : allCommands.value.filter(command => command.kind === 'command'))
const recentItems = computed(() => [
  ...recentTargets.value.map(target => ({ key: `target:${target.path}:${JSON.stringify(target.query)}`, title: target.title, run: () => openRecent(target) })),
  ...recentQueries.value.map(q => ({ key: `recent:${q}`, title: q, run: () => { query.value = q } })),
].slice(0, 4))
const groupOrder = computed(() => query.value.trim() ? ['entries', 'chunks', 'entities', 'commands'] : ['recent', 'quick', 'commands'])
const groupSizes = computed<Record<string, number>>(() => ({ recent: recentItems.value.length, quick: quickCommands.value.length, entries: visibleEntries.value.length, chunks: flatChunkItems.value.length, entities: entities.value.length, commands: visibleCommands.value.length }))
function flatIndexFor(group: string, index: number) {
  let base = 0
  for (const current of groupOrder.value) { if (current === group) return base + index; base += groupSizes.value[current] || 0 }
  return base + index
}
interface FlatItem { key: string; run: () => void }
const flatItems = computed<FlatItem[]>(() => {
  if (!query.value.trim()) return [
    ...recentItems.value,
    ...quickCommands.value.map(command => ({ key: `cmd:${command.id}`, run: command.run })),
    ...visibleCommands.value.map(command => ({ key: `cmd:${command.id}`, run: command.run })),
  ]
  return [
    ...visibleEntries.value.map(file => ({ key: `entry:${file.entryId}`, run: () => openEntry(file) })),
    ...flatChunkItems.value.map(item => ({ key: `chunk:${item.file.entryId}`, run: () => openChunk(item) })),
    ...entities.value.map(entity => ({ key: `entity:${entity.id}`, run: () => openEntity(entity) })),
    ...visibleCommands.value.map(command => ({ key: `cmd:${command.id}`, run: command.run })),
  ]
})
const selectedIndex = computed(() => Math.max(0, flatItems.value.findIndex(item => item.key === selectedKey.value)))
const shortcutFor = (index: number) => index < 9 ? index + 1 : undefined
const fullSearchLocation = computed(() => ({ path: '/platform/search', query: { ...(query.value.trim() ? { q: query.value.trim() } : {}), ...(libraryScope.value.length ? { libraries: libraryScope.value.join(',') } : {}) } }))
const fullSearchHref = computed(() => `${fullSearchLocation.value.path}?${new URLSearchParams(fullSearchLocation.value.query)}`)
function openRecent(target: CmdkRecentTarget) { handleClose(); void router.push({ path: target.path, query: target.query }) }
function openEntry(file: CmdkFileGroup) { navigateTo(entryDetailLocation(file.entryId), file.title) }
function openChunk(item: FlatChunkItem) { navigateTo(entryDetailLocation(item.file.entryId, item.chunk.locations?.[0]), item.file.title) }
function openEntity(entity: Entity) { navigateTo({ path: '/platform/entities', query: { entity_id: entity.id, libraries: entity.library_id } }, entity.canonical_name) }
function openFullSearch() { navigateTo(fullSearchLocation.value, t('commandPalette.searchQuery', { query: query.value.trim() })) }
function navigateTo(target: { path: string; query: Record<string, string | undefined> }, title: string) { commandPaletteStore.pushRecent(query.value); commandPaletteStore.pushTarget({ path: target.path, query: Object.fromEntries(Object.entries(target.query).filter((pair): pair is [string, string] => typeof pair[1] === 'string')), title }); handleClose(); void router.push(target) }
const highlight = (text: string) => highlightText(markdownToPlainText(text, `[${t('ingestStatus.missingTypes.image')}]`), query.value)
function selectItemAt(index: number) { selectedKey.value = flatItems.value[index]?.key || null }
function moveSelection(delta: number) {
  if (!flatItems.value.length) return
  selectItemAt((selectedIndex.value + delta + flatItems.value.length) % flatItems.value.length)
  const moveFocus = document.activeElement !== inputRef.value
  void nextTick(() => {
    const row = scrollRef.value?.querySelector<HTMLElement>(`[data-cmdk-index="${selectedIndex.value}"]`)
    row?.scrollIntoView({ block: 'nearest' })
    if (moveFocus) row?.focus()
  })
}
const primaryActionForSelected = () => flatItems.value[selectedIndex.value]?.run()
const onKeyDown = (e: KeyboardEvent) => {
  if (e.isComposing || composing.value || e.keyCode === 229) return
  if (drawerVisible.value) return
  if ((e.metaKey || e.ctrlKey) && /^[1-9]$/.test(e.key)) { e.preventDefault(); flatItems.value[Number(e.key) - 1]?.run(); return }
  if (e.key === 'Escape') { e.stopPropagation(); e.preventDefault(); handleClose(); return }
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); openFullSearch(); return }
  const target = e.target as HTMLElement | null
  if (target?.closest?.('button, a, select, summary, textarea')) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); moveSelection(e.key === 'ArrowDown' ? 1 : -1) }
  else if (e.key === 'Enter') { e.preventDefault(); primaryActionForSelected() }
}
function handleClose() { drawerVisible.value = false; commandPaletteStore.closePalette() }
function onOpenAutoFocus(event: Event) { event.preventDefault(); void nextTick(() => inputRef.value?.focus()) }
function restoreFocus(event: Event) { event.preventDefault(); if (returnFocus.value?.isConnected) returnFocus.value.focus() }
function isEditingElement(target: EventTarget | null) { const node = target as HTMLElement | null; return !!node && (/^(INPUT|TEXTAREA|SELECT)$/.test(node.tagName) || node.isContentEditable) }
function onGlobalKey(event: KeyboardEvent) {
  if (event.isComposing || composing.value || event.keyCode === 229) return
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); if (open.value) handleClose(); else commandPaletteStore.openPalette('') }
  else if (event.key === '/' && !open.value && !isEditingElement(event.target)) { event.preventDefault(); commandPaletteStore.openPalette('') }
}
watch(open, value => {
  if (value) { returnFocus.value = typeof document !== 'undefined' && document.activeElement instanceof HTMLElement ? document.activeElement : null; query.value = initialQuery.value || ''; selectedKey.value = null }
  else { clearResults(); query.value = ''; drawerVisible.value = false }
}, { flush: 'sync', immediate: true })
onMounted(() => window.addEventListener('keydown', onGlobalKey))
onUnmounted(() => window.removeEventListener('keydown', onGlobalKey))
</script>

<style>
.rq-cmdk-dialog { top: 12vh; transform: translateX(-50%); width: min(640px, calc(100vw - 32px)); max-height: min(72vh, calc(100dvh - 32px)); padding: 0; gap: 0; overflow: hidden; }
.rq-cmdk-dialog > .rq-dialog__header { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
.rq-cmdk-dialog > .rq-dialog__body { gap: 0; overflow: hidden; }
.rq-cmdk { display: flex; flex-direction: column; min-height: 0; color: var(--text); }
.rq-cmdk__input-row { display: flex; align-items: center; gap: var(--space-2); min-height: 56px; padding-inline: var(--space-4); border-bottom: 2px solid var(--rule); }
.rq-cmdk__input { flex: 1; min-width: 0; min-height: 54px; padding: 0; border: 0; border-radius: 0; outline: 0; font: 700 var(--text-lg)/1.4 var(--font); color: var(--text); background: transparent; }
.rq-cmdk__input::placeholder { color: var(--muted); }
.rq-cmdk__input:focus-visible { box-shadow: inset 0 -2px var(--rule); }
.rq-cmdk__close { padding: var(--space-1); border: 0; background: transparent; color: var(--muted); cursor: pointer; }
.rq-cmdk__scope { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); padding: var(--space-2) var(--space-4); border-bottom: 1px solid var(--border); font-size: var(--text-xs); color: var(--muted); }
.rq-cmdk__scope .rq-chip { min-height: 24px; padding: 2px 6px; font-size: var(--text-xs); }
.rq-cmdk__libraries { min-width: 0; }
.rq-cmdk__libraries[open] { flex: 1 1 100%; order: 1; }
.rq-cmdk__libraries summary { cursor: pointer; color: var(--text); }
.rq-cmdk__libraries .library-scope { max-width: 100%; margin-top: var(--space-2); }
.rq-cmdk__settings { margin-left: auto; border: 0; padding: 0; font: inherit; text-decoration: underline; color: var(--text); background: transparent; cursor: pointer; }
.rq-cmdk__settings:hover { color: var(--accent); }
.rq-cmdk__results { min-height: 0; overflow: auto; overscroll-behavior: contain; padding-bottom: var(--space-2); }
.rq-cmdk__icon { display: block; width: 12px; height: 12px; border: 2px solid currentColor; }
.rq-cmdk__id { flex-shrink: 0; font: var(--text-xs)/1.5 var(--mono); color: var(--muted); }
.rq-cmdk__excerpt { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.rq-cmdk__empty { display: grid; justify-items: start; gap: var(--space-2); padding: var(--space-5) var(--space-4); font-size: var(--text-sm); }
.rq-cmdk__empty p { margin: 0; }
.rq-cmdk__footer { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); padding: var(--space-2) var(--space-4); border-top: 2px solid var(--rule); font-size: var(--text-xs); color: var(--muted); }
.rq-cmdk__footer a { color: var(--text); font-weight: 700; text-decoration: underline; }
.rq-cmdk__footer a:hover { color: var(--accent); }
.rq-cmdk kbd { font: inherit; }
.rq-cmdk__loading { font-size: var(--text-xs); }
.rq-cmdk .rq-cmdk-item .entry-update-notice { color: var(--muted); }
.rq-cmdk .rq-cmdk-item[aria-selected="true"], .rq-cmdk .rq-cmdk-item[aria-selected="true"] * { color: var(--on-select); }
.rq-cmdk .rq-cmdk-item[aria-selected="true"] .rq-tag, .rq-cmdk .rq-cmdk-item[aria-selected="true"] .search-highlight { background: transparent; }
@media (max-width: 640px) {
  .rq-cmdk-dialog { top: 16px; max-height: calc(100dvh - 32px); }
  .rq-cmdk__input { font-size: var(--text-base); }
  .rq-cmdk__loading { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
}
</style>
