<template>
  <t-dialog v-model:visible="dialogVisible" :footer="false" :header="false" :close-btn="false" width="640px" top="10vh"
    destroy-on-close class="cmdk-dialog" @close="handleClose" @opened="onDialogOpened">
    <div class="cmdk" @keydown="onKeyDown">
      <!-- Input row -->
      <div class="cmdk__input-row">
        <t-icon name="search" class="cmdk__input-icon" />

        <input ref="inputRef" v-model="query" type="text" class="cmdk__input"
          :placeholder="t('commandPalette.placeholder')"
          :aria-label="t('knowledgeSearch.query')" autofocus spellcheck="false" @compositionstart="composing = true" @compositionend="composing = false" />
        <button v-if="query" type="button" class="cmdk__icon-btn" :aria-label="t('knowledgeSearch.clearQuery')" @click="query = ''; inputRef?.focus()"><t-icon name="close" size="16px" /></button>
        <span v-if="loading" class="cmdk__input-spinner">
          <t-loading size="small" />
        </span>
        <t-tooltip :content="t('commandPalette.retrieval')" placement="bottom">
          <button type="button" class="cmdk__icon-btn" :class="{ active: drawerVisible }" @click="drawerVisible = true">
            <t-icon name="setting" size="16px" />
          </button>
        </t-tooltip>
        <button type="button" class="cmdk__icon-btn" :aria-label="t('commandPalette.hotkey.esc')" @click="handleClose">
          <t-icon name="close" size="16px" />
        </button>
      </div>

      <!-- Results -->
      <div ref="scrollRef" class="cmdk__results">
        <!-- Empty idle state: recent + quick actions -->
        <template v-if="!query.trim()">
          <ResultGroup v-if="recentQueries.length" :label="t('commandPalette.group.recent')"
            :action="t('commandPalette.clearRecent')" @action="commandPaletteStore.clearRecent()">
            <ResultItem v-for="(q, i) in recentQueries" :key="'r-' + q" icon-name="history"
              :index="flatIndexFor('recent', i)" :selected="selectedIndex === flatIndexFor('recent', i)"
              :shortcut="shortcutFor(flatIndexFor('recent', i))" :title="q" @primary="query = q"
              @hover="selectItemAt($event)" />
          </ResultGroup>

          <ResultGroup :label="t('commandPalette.group.quickActions')">
            <ResultItem v-for="(c, i) in allCommands" :key="'cmd-' + c.id" :index="flatIndexFor('commands', i)"
              :selected="selectedIndex === flatIndexFor('commands', i)"
              :shortcut="shortcutFor(flatIndexFor('commands', i))" :icon-name="c.icon" :title="c.label" @primary="c.run"
              @hover="selectItemAt($event)" />
          </ResultGroup>
        </template>

        <!-- Active search results -->
        <template v-else>
          <t-button variant="text" @click="openFullSearch">{{ t('knowledgeSearch.fullSearch') }}</t-button>
          <!-- Knowledge messages (one representative match each) -->
          <ResultGroup v-if="isGroupVisible('chunks') && fileGroups.length" :label="t('commandPalette.group.chunks')"
            :count="fileGroups.length">
            <template v-for="(item, i) in flatChunkItems" :key="'c-' + item.file.knowledgeId">
              <ResultItem :index="flatIndexFor('chunks', i)" :selected="selectedIndex === flatIndexFor('chunks', i)"
                :shortcut="shortcutFor(flatIndexFor('chunks', i))" icon-name="file"
                :badge="item.chunk.image_path ? t('knowledgeSearch.imageMatch') : item.chunk.match_type === 'vector' ? t('commandPalette.match.vector') : t('commandPalette.match.keyword')"
                :badge-variant="item.chunk.image_path || item.chunk.match_type === 'vector' ? 'vector' : 'keyword'" :score="item.chunk.score"
                :source-unverified="item.file.source_unverified"
                @primary="openChunk(item)" @hover="selectItemAt($event)">
                <template #title>
                  <span class="cmdk-chunk-title">{{ item.file.title }}</span>
                </template>
                <template #subtitle>
                  <KnowledgeSearchImage v-if="item.file.imagePath" :article-id="item.file.knowledgeId" :path="item.file.imagePath" :title="item.file.title" compact />
                  <span v-html="highlight(item.chunk.matched_content || item.chunk.content)" />
                </template>
              </ResultItem>
            </template>
          </ResultGroup>

          <!-- Messages -->

          <!-- Commands matching the query -->
          <ResultGroup v-if="isGroupVisible('commands') && filteredCommands.length"
            :label="t('commandPalette.group.commands')">
            <ResultItem v-for="(c, i) in filteredCommands" :key="'fcmd-' + c.id" :index="flatIndexFor('commands', i)"
              :selected="selectedIndex === flatIndexFor('commands', i)"
              :shortcut="shortcutFor(flatIndexFor('commands', i))" :icon-name="c.icon" :title="c.label" @primary="c.run"
              @hover="selectItemAt($event)" />
          </ResultGroup>

          <div v-if="!loading && searchError" class="cmdk__empty" role="alert">
            <p>{{ searchError }}</p>
            <t-button variant="outline" size="small" @click="retry">{{ t('common.retry') }}</t-button>
          </div>
          <!-- No results -->
          <div v-if="!loading && !searchError && !hasAnyResults && hasSearched" class="cmdk__empty">
            <p>{{ t('commandPalette.empty.noResults') }}</p>
            <div class="cmdk__empty-actions">

              <t-button variant="outline" size="small" @click="drawerVisible = true">
                <template #icon><t-icon name="setting" size="14px" /></template>
                {{ t('commandPalette.empty.adjustRetrieval') }}
              </t-button>
            </div>
          </div>
        </template>
      </div>

      <!-- Hotkey footer -->
      <div class="cmdk__footer">
        <span class="cmdk__hotkey"><kbd>↑</kbd><kbd>↓</kbd> {{ t('commandPalette.hotkey.select') }}</span>
        <span class="cmdk__hotkey"><kbd>↵</kbd> {{ t('commandPalette.hotkey.enter') }}</span>
        <span class="cmdk__hotkey"><kbd>⌘</kbd><kbd>1</kbd>-<kbd>9</kbd> {{ t('commandPalette.hotkey.cmdNumber')
          }}</span>

        <span class="cmdk__hotkey"><kbd>Esc</kbd> {{ t('commandPalette.hotkey.esc') }}</span>
      </div>
    </div>

    <!-- Retrieval settings drawer (layered on top of the palette) -->
    <t-drawer v-model:visible="drawerVisible" :header="t('retrievalSettings.title')" size="420px" :footer="false"
      :close-on-overlay-click="true" class="cmdk-retrieval-drawer">
      <RetrievalSettings />
    </t-drawer>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useCommandPaletteStore } from '@/stores/commandPalette'
import { useAuthStore } from '@/stores/auth'
import { useCmdkSearch, type CmdkFileGroup, type CmdkChunk } from './GlobalCommandPalette/useSearch'
import { visibleFileChunks } from './GlobalCommandPalette/visibleFileChunks'
import { highlightText } from './GlobalCommandPalette/useHighlight'
import { buildCommands, filterCommands } from './GlobalCommandPalette/commands'
import ResultGroup from './GlobalCommandPalette/ResultGroup.vue'
import ResultItem from './GlobalCommandPalette/ResultItem.vue'
import KnowledgeSearchImage from './KnowledgeSearchImage.vue'
import RetrievalSettings from '@/views/settings/RetrievalSettings.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const commandPaletteStore = useCommandPaletteStore()
const authStore = useAuthStore()
const { open, initialQuery, recentQueries } = storeToRefs(commandPaletteStore)

// Per-group display caps — keep the palette compact.
const CHUNK_LIMIT = 5

const { query, composing, loading, hasSearched, fileGroups, clearResults, searchError, retry } = useCmdkSearch({})

const drawerVisible = ref(false)
const inputRef = ref<HTMLInputElement | null>(null)
const scrollRef = ref<HTMLElement | null>(null)

// Selection is identity-based (by item key) rather than positional, so
// results arriving asynchronously (e.g. slow KB search) can't silently
// reassign the user's highlight to a different row. `selectedIndex` is
// derived: it's the current position of `selectedKey` in flatItems, or 0 if
// the selected item is gone.
const selectedKey = ref<string | null>(null)

// Proxy the store's `open` to t-dialog v-model.
const dialogVisible = computed<boolean>({
  get: () => open.value,
  set: (v) => {
    if (!v) handleClose()
  },
})

// Flatten visible items into one indexable list for keyboard nav.
interface FlatChunkItem { kind: 'chunk'; file: CmdkFileGroup; chunk: CmdkChunk }

const flatChunkItems = computed<FlatChunkItem[]>(() =>
  visibleFileChunks(fileGroups.value, CHUNK_LIMIT).map(item => ({ kind: 'chunk', ...item })),
)

// ─── Commands ───
// All palette commands live in a shared module so quick-action (empty state)
// and the "commands" tab can use the same data.
const allCommands = computed(() => {
  const cmds = buildCommands({
    router,
    t,
    close: () => commandPaletteStore.closePalette(),
  })
  return cmds
})

const filteredCommands = computed(() => filterCommands(allCommands.value, query.value))

// Stable index layout — keep group order consistent so flatIndexFor is cheap.
// Groups always render in the same order; individual groups hide themselves
// when their backing list is empty, so users with nothing to show don't see
// empty headers.
const groupOrder = computed<readonly string[]>(() => {
  if (!query.value.trim()) {
    // Empty state: recents + full command list.
    return ['recent', 'commands'] as const
  }
  return ['chunks', 'commands'] as const
})

const groupSizes = computed<Record<string, number>>(() => ({
  recent: recentQueries.value.length,
  commands: query.value.trim() ? filteredCommands.value.length : allCommands.value.length,
  chunks: flatChunkItems.value.length,
}))

const flatIndexFor = (group: string, localIndex: number): number => {
  let base = 0
  for (const g of groupOrder.value) {
    if (g === group) return base + localIndex
    base += groupSizes.value[g] || 0
  }
  return base + localIndex
}

/**
 * Unified flattened list driving keyboard nav, ⌘N shortcuts and the primary
 * action. Each entry carries a stable `key` (so selection survives async
 * result arrivals) plus a `run` callback. Order follows `groupOrder`.
 */
interface FlatItem {
  key: string
  group: string
  run: (ev?: { cmd: boolean }) => void
}

const flatItems = computed<FlatItem[]>(() => {
  const out: FlatItem[] = []
  for (const g of groupOrder.value) {
    if (g === 'recent') {
      recentQueries.value.forEach((q, i) => {
        out.push({
          key: `recent:${i}:${q}`,
          group: g,
          run: () => { query.value = q },
        })
      })
    } else if (g === 'commands') {
      const list = query.value.trim() ? filteredCommands.value : allCommands.value
      list.forEach((cmd) => {
        out.push({ key: `cmd:${cmd.id}`, group: g, run: () => cmd.run() })
      })
    } else if (g === 'chunks') {
      flatChunkItems.value.forEach((item) => {
        out.push({
          key: `knowledge:${item.file.knowledgeId}`,
          group: g,
          run: () => openChunk(item),
        })
      })
    }
  }
  return out
})

const totalItems = computed(() => flatItems.value.length)

/** Derived: position of the currently-selected item, or 0 if it no longer exists. */
const selectedIndex = computed<number>(() => {
  if (!selectedKey.value) return 0
  const idx = flatItems.value.findIndex((it) => it.key === selectedKey.value)
  return idx >= 0 ? idx : 0
})

const hasAnyResults = computed(() => {
  // "Any result" means at least one of the groups visible under the current
  // state has items. The empty-state message should only appear when the user
  // really has nothing to click on.
  for (const g of groupOrder.value) {
    if ((groupSizes.value[g] || 0) > 0) return true
  }
  return false
})

/** Whether a named group should be rendered under the current state. */
const isGroupVisible = (name: string): boolean => groupOrder.value.includes(name as never)

// ─── Navigation helpers ───

const primaryActionForSelected = (ev?: { cmd: boolean }) => {
  const item = flatItems.value[selectedIndex.value]
  item?.run(ev)
}

const openChunk = (item: FlatChunkItem) => {
  commandPaletteStore.pushRecent(query.value)
  commandPaletteStore.closePalette()
  const alreadyViewingKnowledge = route.name === 'knowledge'
  // On the articles page, router.push to the same path+query
  // is a no-op (vue-router dedupes identical navigations), so the document
  // auto-open logic never fires. Dispatch a global event that KnowledgeBase.vue
  // listens for to open the matching article.
  if (alreadyViewingKnowledge) {
    window.dispatchEvent(
      new CustomEvent('reliquary:open-knowledge', {
        detail: { kbId: item.file.kbId, knowledgeId: item.file.knowledgeId },
      }),
    )
    return
  }
  router.push({
    path: '/platform/knowledge',
    query: { knowledge_id: item.file.knowledgeId },
  })
}

const openFullSearch = () => {
  commandPaletteStore.closePalette()
  router.push({ path: '/platform/knowledge-search', query: query.value.trim() ? { q: query.value.trim() } : {} })
}

const highlight = (text: string) => highlightText(text, query.value)

// ─── Keyboard navigation ───

const scrollSelectedIntoView = () => {
  nextTick(() => {
    const el = scrollRef.value?.querySelector<HTMLElement>(`[data-cmdk-index="${selectedIndex.value}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  })
}

const moveSelection = (delta: number) => {
  const total = totalItems.value
  if (total === 0) return
  const next = (selectedIndex.value + delta + total) % total
  selectedKey.value = flatItems.value[next]?.key || null
  scrollSelectedIntoView()
}

const selectItemAt = (idx: number) => {
  const item = flatItems.value[idx]
  if (!item) return
  selectedKey.value = item.key
}

/**
 * Return the ⌘N shortcut digit for the item at the given flat index, or
 * undefined if it's past the 9th slot. The ResultItem renders a kbd hint
 * iff this is defined, which is the only cue the user has that ⌘1-9 works.
 */
const shortcutFor = (flatIndex: number): number | undefined => {
  if (flatIndex < 0 || flatIndex > 8) return undefined
  return flatIndex + 1
}

const onKeyDown = (e: KeyboardEvent) => {
  if (e.isComposing || composing.value || e.keyCode === 229) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    moveSelection(1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    moveSelection(-1)
  } else if (
    (e.metaKey || e.ctrlKey) &&
    e.key >= '1' && e.key <= '9'
  ) {
    // ⌘1-9 — jump straight to the Nth visible item (shortcut badge on each
    // row reveals the binding). No modifier combos with ⌘Enter: digits take
    // precedence because ⌘+digit can't be confused with ⌘+enter.
    const n = parseInt(e.key, 10)
    const item = flatItems.value[n - 1]
    if (item) {
      e.preventDefault()
      item.run({ cmd: false })
    }
  } else if (e.key === 'Enter') {
    e.preventDefault()
    primaryActionForSelected({ cmd: e.metaKey || e.ctrlKey })
  } else if (e.key === 'Escape') {
    e.preventDefault()
    handleClose()
  }
}

const handleClose = () => {
  drawerVisible.value = false
  commandPaletteStore.closePalette()
}

// ─── Global ⌘K shortcut ───

const isEditingElement = (el: EventTarget | null): boolean => {
  if (!el) return false
  const node = el as HTMLElement
  const tag = (node.tagName || '').toUpperCase()
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (node.isContentEditable) return true
  return false
}

const onGlobalKey = (e: KeyboardEvent) => {
  const isCmd = e.metaKey || e.ctrlKey
  if (isCmd && e.key.toLowerCase() === 'k') {
    // Always allow ⌘K to open, even from an input.
    e.preventDefault()
    if (open.value) handleClose()
    else commandPaletteStore.openPalette('')
    return
  }
  // Plain "/" opens the palette when nothing is focused.
  if (e.key === '/' && !open.value && !isEditingElement(e.target)) {
    e.preventDefault()
    commandPaletteStore.openPalette('')
  }
}

// When the palette opens programmatically (from router redirect or elsewhere),
// sync the query. Focus is moved to the input by
// `onDialogOpened` once the dialog's enter animation has finished — doing it
// here in a plain `nextTick` is too early for t-dialog (the input may not be
// attached to the document yet, so `.focus()` silently no-ops).
watch(open, (val) => {
  if (val) {
    query.value = initialQuery.value || ''
    selectedKey.value = null
  } else {
    clearResults()
    query.value = ''
    drawerVisible.value = false
  }
})

// Fired by t-dialog after its open animation finishes — DOM is guaranteed
// attached, focus sticks. We also schedule a couple of retries because
// some browsers defer focus when the dialog is mid-layout; costs nothing.
const focusInputWithRetry = () => {
  const tryFocus = () => {
    const el = inputRef.value
    if (!el) return false
    el.focus()
    // Move cursor to end so an initialQuery is editable, not overwritten.
    if (typeof el.setSelectionRange === 'function' && el.value) {
      const len = el.value.length
      try { el.setSelectionRange(len, len) } catch { /* not all input types support this */ }
    }
    return document.activeElement === el
  }
  if (tryFocus()) return
  requestAnimationFrame(() => {
    if (tryFocus()) return
    setTimeout(tryFocus, 50)
  })
}

const onDialogOpened = () => {
  focusInputWithRetry()
}

onMounted(() => {
  window.addEventListener('keydown', onGlobalKey)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKey)
})
</script>

<style lang="less" scoped>
.cmdk {
  display: flex;
  flex-direction: column;
  gap: 0;
  min-height: 320px;
  max-height: 60vh;
}

.cmdk__input-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.cmdk__input-icon {
  color: var(--td-text-color-placeholder);
  font-size: 16px;
}

.cmdk__input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  font-size: 15px;
  color: var(--td-text-color-primary);
  font-family: inherit;

  &::placeholder {
    color: var(--td-text-color-placeholder);
  }
}

.cmdk__input-spinner {
  display: flex;
  align-items: center;
}

.cmdk__icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition: background 0.1s;

  &:hover,
  &.active {
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-primary);
  }
  &:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
}

.cmdk__results {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px 4px;
}

.cmdk__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 40px 20px 20px;
  color: var(--td-text-color-placeholder);
  font-size: 13px;

  p {
    margin: 0;
  }
}

.cmdk__empty-actions {
  display: flex;
  gap: 8px;
}

.cmdk__footer {
  display: flex;
  gap: 16px;
  padding: 8px 14px;
  border-top: 1px solid var(--td-component-stroke);
  font-size: 11px;
  color: var(--td-text-color-placeholder);
  flex-wrap: wrap;
}

.cmdk__hotkey {
  display: inline-flex;
  align-items: center;
  gap: 4px;

  kbd {
    display: inline-block;
    padding: 1px 5px;
    min-width: 16px;
    font-size: 10px;
    font-family: inherit;
    line-height: 14px;
    text-align: center;
    background: var(--td-bg-color-secondarycontainer);
    border: 1px solid var(--td-component-stroke);
    border-radius: 3px;
    color: var(--td-text-color-secondary);
  }
}



.cmdk-chunk-title {
  overflow: hidden;
  text-overflow: ellipsis;
}

.cmdk-msg-role {
  display: inline-block;
  margin-right: 6px;
  padding: 0 5px;
  font-size: 10px;
  font-weight: 600;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  border-radius: 3px;
}
</style>

<style lang="less">
/* Unscoped overrides — dialog renders in teleport. */
.cmdk-dialog {
  .t-dialog__body {
    padding: 0;
  }

  .t-dialog {
    padding: 0;
    overflow: hidden;
  }
}

.cmdk-retrieval-drawer {
  .section-header {
    font-weight: 600;
  }
}
</style>
