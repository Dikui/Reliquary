<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, reactive, computed, nextTick } from "vue";
import { DialogPlugin, MessagePlugin } from "tdesign-vue-next";
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue';
import { useLibraryScope } from '@/composables/useLibraryScope';
import { parseReference } from '@/utils/bodyReference';
import type { SourceLocation } from '@/types/search';
import DocContent from "@/components/DocContent.vue";
import useLibrary from '@/composables/useLibrary';
import { useRoute, useRouter } from 'vue-router';
import EmptyEntry from '@/components/EmptyEntry.vue';
import LibraryInfoPopover from '@/components/LibraryInfoPopover.vue';
import { useUIStore } from '@/stores/ui';
import { useCommandPaletteStore } from '@/stores/commandPalette';
import { useResourcesStore } from '@/stores/resources';
import { useEditorResourcesStore } from '@/stores/editorResources';
const uiStore = useUIStore();
const commandPalette = useCommandPaletteStore();
const resources = useResourcesStore();
const editorResources = useEditorResourcesStore();
const router = useRouter();
import {
  batchQueryEntry,
  reparseEntry,
  cancelEntryParse,
  batchDeleteEntry,
  batchReparseEntry,
  rebuildCatalogLibrary,
  getEntrySpans,
  getEntryDetails,

} from "@/api/library";
import { entrySpansPayloadHasTrace } from '@/utils/entryTrace';
import DocumentListView from './components/DocumentListView.vue';
import DocumentCardView from './components/DocumentCardView.vue';
import DocumentBatchBar from './components/DocumentBatchBar.vue';
import EntryWorkbench from './components/EntryWorkbench.vue';
import PendingReviews from './components/PendingReviews.vue';
import PendingCountBadge from '@/components/PendingCountBadge.vue';
import { usePendingCountsStore } from '@/stores/pendingCounts';
import LibraryReviewAction from '@/components/LibraryReviewAction.vue';
import type { LibraryContentType, LibraryRebuildScope } from '@/api/library';
import { CONTENT_TYPES } from '@/types/entryClassification';
import { SEARCH_SOURCES } from '@/types/search';
import {
  documentListFiltersFromQuery,
  documentListFiltersToQuery,
  documentListFilterQueriesEqual,
  documentListFiltersEqual,
  type DocumentListFilters,
} from './documentListFilters';
import type { EntryJudgment } from '@/api/entry-judgments';
import type { EntryProcessOverrides } from '@/types/entryProcess';
import { useReparseConfirmStore } from '@/stores/reparseConfirm';
import {
  isEntryParseInFlight,
  entryNeedsStatusPolling,
} from './entryStatusPolling';
import { useI18n } from 'vue-i18n';
import { useMarqueeSelect } from '@/composables/useMarqueeSelect';
const route = useRoute();
const { t } = useI18n();
const pendingCounts = usePendingCountsStore();
const libraryId = computed(() => resources.library?.id || '');
const libraryInfo = ref<any>(null);
const libraryScope = useLibraryScope();
const libraryCatalog = ref<import('@/api/library').LibraryCatalogItem[]>([]);
const selectedLibrary = computed(() => libraryScope.value.length === 1
  ? libraryCatalog.value.find(item => item.id === libraryScope.value[0])
  : undefined);
const libraryRebuilding = ref(false);
const libraryRebuildScope = ref<LibraryRebuildScope>('failed');
const libraryRebuildError = ref('');
const libraryRebuildNotice = ref('');
const libraryRebuildConfirming = ref(false);
let libraryRebuildDialog: ReturnType<typeof DialogPlugin.confirm> | undefined;
const referencedEntryId = ref(typeof route.query.entry_id === 'string' ? route.query.entry_id : '');
const activeReference = ref<SourceLocation | undefined>(parseReference(route.query.reference));
const libraryLoading = ref(false);
const docListLoading = ref(true);
const validTabs = ['documents', 'reviews', 'entities'] as const
type KbTab = typeof validTabs[number]
const initTab = validTabs.includes(route.query.tab as any) ? (route.query.tab as KbTab) : 'documents'
const activeLibraryTab = ref<KbTab>(initTab);

let { cardList, total, moreIndex, details, loadEntries, delEntry, openMore, onVisibleChange: _onVisibleChange, getCardDetails, getfDetails } = useLibrary(libraryId.value)

/** Per-entry cache: whether /spans has a real trace (see entrySpansPayloadHasTrace). */
const traceAvailableById = reactive<Record<string, boolean>>({});
const traceProbeInflight = new Set<string>();

function clearTraceAvailabilityCache() {
  for (const key of Object.keys(traceAvailableById)) {
    delete traceAvailableById[key];
  }
  traceProbeInflight.clear();
}

// Parse phases where the backend pipeline is still actively running
// (primary parse OR post-process fan-out). Trace data exists and the
// UI should treat the row as "in flight" rather than terminal.
function isParseInFlight(status?: string): boolean {
  return isEntryParseInFlight(status);
}

function isTraceMenuVisible(item: EntryCard): boolean {
  if (!item?.id) return false;
  if (isParseInFlight(item.parse_status)) {
    return true;
  }
  return traceAvailableById[item.id] === true;
}

async function probeTraceAvailable(item: EntryCard) {
  const id = item.id;
  if (!id || traceProbeInflight.has(id)) return;
  if (isParseInFlight(item.parse_status)) {
    traceAvailableById[id] = true;
    return;
  }
  if (Object.prototype.hasOwnProperty.call(traceAvailableById, id)) return;
  traceProbeInflight.add(id);
  try {
    const res: any = await getEntrySpans(id);
    traceAvailableById[id] = !!(res?.success && entrySpansPayloadHasTrace(res.data));
  } catch {
    traceAvailableById[id] = false;
  } finally {
    traceProbeInflight.delete(id);
  }
}

const onCardMoreVisibleChange = (visible: boolean, item: EntryCard) => {
  _onVisibleChange(visible);
  if (visible) {
    probeTraceAvailable(item);
  }
};
let isCardDetails = ref(false);
let timeout: ReturnType<typeof setTimeout> | null = null;
let entryScroll = ref()
let page = 1;
let pageSize = 35;
let scrollLoading = false;
const resetPage = () => { page = 1; scrollLoading = false; };

// View mode (grid / list) — persisted per browser
type DocViewMode = 'grid' | 'list';
const VIEW_MODE_KEY = 'reliquary.library.docs.viewMode';
const initViewMode = (): DocViewMode => {
  try {
    return localStorage.getItem(VIEW_MODE_KEY) === 'list' ? 'list' : 'grid';
  } catch { return 'grid'; }
};
const viewMode = ref<DocViewMode>(initViewMode());
watch(viewMode, (v) => {
  try { localStorage.setItem(VIEW_MODE_KEY, v); } catch { /* ignore */ }
});

// Multi-select state — shared between grid and list views.
// Vue 3.5 tracks Set#add/delete natively, so direct mutation is reactive.
const selectedIds = ref<Set<string>>(new Set());
let lastSelectedIndex = -1;
const batchDeleting = ref(false);
const batchReparsing = ref(false);
// IDs submitted for async batch reparse; hold optimistic pending until the worker updates DB.
const pendingReparseAck = ref<Set<string>>(new Set());

const applyOptimisticBatchReparse = (ids: string[]) => {
  const idSet = new Set(ids);
  for (const card of cardList.value) {
    if (!idSet.has(card.id)) continue;
    pendingReparseAck.value.add(card.id);
    card.parse_status = 'pending';
    card.description = '';
    delete traceAvailableById[card.id];
    traceAvailableById[card.id] = true;
  }
};

const syncReparseAckFromServer = (ids: string[]) => {
  for (const id of ids) {
    if (!pendingReparseAck.value.has(id)) continue;
    const card = cardList.value.find((c) => c.id === id);
    if (card && isParseInFlight(card.parse_status)) {
      pendingReparseAck.value.delete(id);
    }
  }
};

const awaitBatchReparseReflection = async (ids: string[]) => {
  const maxPolls = 30;
  const delayMs = 400;
  for (let i = 0; i < maxPolls && pendingReparseAck.value.size > 0; i++) {
    await loadEntryFiles(libraryId.value);
    syncReparseAckFromServer(ids);
    applyOptimisticBatchReparse(Array.from(pendingReparseAck.value));
    await new Promise<void>((r) => setTimeout(r, delayMs));
  }
  pendingReparseAck.value.clear();
};

const confirmBatchReparse = async () => {
  if (!selectedDocumentsReprocessable.value || batchReparsing.value || batchDeleting.value || selectedIds.value.size === 0) return;
  const allIds = Array.from(selectedIds.value);
  const ids = allIds.filter((id) => {
    const item = cardList.value.find((c) => c.id === id);
    return !item || !isParseInFlight(item.parse_status);
  });
  const skipped = allIds.length - ids.length;
  if (ids.length === 0) {
    MessagePlugin.info(t('library.rebuildInProgress'));
    return;
  }
  if (skipped > 0) {
    MessagePlugin.warning(t('library.batchReparseSkippedInFlight', { count: skipped }));
  }
  batchReparsing.value = true;
  try {
    const res: any = await batchReparseEntry(ids);
    if (res?.success) {
      MessagePlugin.success(t('library.batchReparseSuccess', { count: ids.length }));
      applyOptimisticBatchReparse(ids);
      clearSelection();
      batchMode.value = false;
      void awaitBatchReparseReflection(ids);
    } else {
      MessagePlugin.error(res?.message || t('library.batchReparseFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('library.batchReparseFailed'));
  } finally {
    batchReparsing.value = false;
  }
};
let docSearchDebounce: number | null = null;
const initialDocumentFilters = documentListFiltersFromQuery(route.query);
const docSearchKeyword = ref(initialDocumentFilters.keyword);
const selectedFileType = ref(initialDocumentFilters.fileType);
const selectedContentType = ref(initialDocumentFilters.contentType);
const selectedStarred = ref(initialDocumentFilters.starred);
const starredOptions = computed(() => [
  { value: '', label: t('articleJudgments.allStars') },
  { value: 'true', label: t('articleJudgments.starredOnly') },
  { value: 'false', label: t('articleJudgments.unstarredOnly') },
]);
const contentTypeOptions = computed(() => {
  if (libraryCatalog.value.length) {
    const names = new Set(libraryCatalog.value.filter(item => !libraryScope.value.length || libraryScope.value.includes(item.id)).flatMap(item => (item.content_types || []).filter(row => row.enabled).map(row => row.name)));
    return [...names].map(value => ({ value, label: value }));
  }
  return libraryInfo.value?.content_types?.filter((row: LibraryContentType) => row.enabled).map((row: LibraryContentType) => ({ value: row.name, label: row.name })) || CONTENT_TYPES.map(value => ({ value, label: t(`library.contentTypes.${value}`) }));
});
const selectedEntityId = ref('');
watch(() => route.query.entity_id, id => { if (typeof id === 'string') selectedEntityId.value = id; }, { immediate: true });
const openArticleEntity = (id: string) => { selectedEntityId.value = id; activeLibraryTab.value = 'entities'; closeDoc(); };
const fileTypeOptions = computed(() => [
  { label: t('library.allFileTypes'), value: '' },
  { label: 'PDF', value: 'pdf' },
  { label: 'DOCX', value: 'docx' },
  { label: 'DOC', value: 'doc' },
  { label: 'PPTX', value: 'pptx' },
  { label: 'PPT', value: 'ppt' },
  { label: 'EPUB', value: 'epub' },
  { label: 'TXT', value: 'txt' },
  { label: 'Markdown', value: 'md' },
  { label: 'MP3', value: 'mp3' },
  { label: 'WAV', value: 'wav' },
  { label: 'M4A', value: 'm4a' },
  { label: 'FLAC', value: 'flac' },
  { label: 'OGG', value: 'ogg' },
]);
const selectedParseStatus = ref(initialDocumentFilters.parseStatus);
const parseStatusOptions = computed(() => [
  { label: t('library.allParseStatuses'), value: '' },
  { label: t('library.parseStatusPending'), value: 'pending' },
  { label: t('library.parseStatusProcessing'), value: 'processing' },
  { label: t('library.parseStatusCompleted'), value: 'completed' },
  { label: t('library.parseStatusFailed'), value: 'failed' },
  { label: t('library.parseStatusCancelled'), value: 'cancelled' },
  { label: t('library.parseStatusFinalizing'), value: 'finalizing' },
  { label: t('library.parseStatusDraft'), value: 'draft' },
  { label: t('library.intakeWaiting'), value: 'rejected' },
]);
const selectedSource = ref(initialDocumentFilters.source);
const selectedSourceKind = ref(initialDocumentFilters.sourceKind);
const sourceKindOptions = computed(() => [
  { label: t('library.allMaterialCategories'), value: '' },
  ...SEARCH_SOURCES.map(value => ({ value, label: t(`knowledgeSearch.sources.${value}`) })),
]);
// Submission channels are independent of file format and material category.
const sourceOptions = computed(() => [
  { label: t('library.allSources'), value: '' },
  { label: t('library.channelApi'), value: 'api' },
  { label: t('library.channelWeb'), value: 'web' },
  { label: t('library.channelRSS'), value: 'rss' },
  { label: t('library.channelBrowserExtension'), value: 'browser_extension' },
]);
// Date range as [start, end] in "YYYY-MM-DD" form (t-date-range-picker default).
const updatedTimeRange = ref<string[]>(
  initialDocumentFilters.startDate || initialDocumentFilters.endDate
    ? [initialDocumentFilters.startDate, initialDocumentFilters.endDate] : [],
);
const documentFilters = computed<DocumentListFilters>(() => ({
  keyword: docSearchKeyword.value.trim(),
  fileType: selectedFileType.value || '',
  contentType: selectedContentType.value || '',
  starred: selectedStarred.value || '',
  parseStatus: selectedParseStatus.value || '',
  source: selectedSource.value || '',
  sourceKind: selectedSourceKind.value || '',
  startDate: updatedTimeRange.value?.[0] || '',
  endDate: updatedTimeRange.value?.[1] || '',
}));
// Disable any date after today so users cannot filter into the future.
const disableFutureDate = { after: new Date(new Date().setHours(23, 59, 59, 999)) };

const filterParams = computed(() => {
  const [start, end] = updatedTimeRange.value || [];
  return {
    libraries: libraryScope.value,
    keyword: docSearchKeyword.value ? docSearchKeyword.value.trim() : undefined,
    file_type: selectedFileType.value || undefined,
    content_type: selectedContentType.value || undefined,
    starred: selectedStarred.value === '' ? undefined : selectedStarred.value === 'true',
    parse_status: selectedParseStatus.value || undefined,
    source: selectedSource.value || undefined,
    source_kind: selectedSourceKind.value || undefined,
    start_time: start ? `${start} 00:00:00` : undefined,
    end_time: end ? `${end} 23:59:59` : undefined,
  };
});
const getPageSize = () => {
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
  const itemHeight = 148;
  let itemsInView = Math.floor(viewportHeight / itemHeight) * 5;
  pageSize = Math.max(35, itemsInView);
}
getPageSize()

const loadEntryFiles = (libraryIdValue: string): Promise<void> => {
  if (!libraryIdValue) return Promise.resolve();

    docListLoading.value = true;

  return loadEntries(
    {
      page: 1,
      page_size: pageSize,
      ...filterParams.value,
    },
    libraryIdValue,
  ).finally(() => {
    if (isCurrentLibrary(libraryIdValue)) {
      docListLoading.value = false;
    }
  });
};

const isCurrentLibrary = (targetLibraryId: string) => targetLibraryId === libraryId.value;

const loadLibraryInfo = async (targetLibraryId: string, force = false) => {
  if (!targetLibraryId) {
    libraryInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  libraryLoading.value = true;
  try {
    const data = await resources.fetchLibrary(force);
    if (!isCurrentLibrary(targetLibraryId)) return;

    libraryInfo.value = data;

      loadEntryFiles(targetLibraryId);

  } catch (error) {
    if (!isCurrentLibrary(targetLibraryId)) return;

    console.error('Failed to load library info:', error);
    libraryInfo.value = null;
    cardList.value = [];
    total.value = 0;
  } finally {
    if (isCurrentLibrary(targetLibraryId)) {
      libraryLoading.value = false;
    }
  }
};

// 唯一库加载后获取文章内容
// Sync activeLibraryTab to URL query so it survives page refresh
watch(() => route.query.tab, (tab) => {
  activeLibraryTab.value = tab === 'entities' ? 'entities' : tab === 'reviews' ? 'reviews' : 'documents'
})
watch(activeLibraryTab, (tab) => {
  if (route.query.tab === (tab === 'documents' ? undefined : tab)) return
  const query = { ...route.query }
  if (tab === 'documents') {
    delete query.tab
  } else {
    query.tab = tab
  }
  router.replace({ query })
})

watch(() => libraryId.value, (newLibraryId, oldLibraryId) => {
  if (newLibraryId !== oldLibraryId)
  if (!newLibraryId) {
    libraryInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  if (newLibraryId === oldLibraryId && libraryInfo.value) return;

  if (newLibraryId !== oldLibraryId) {
    clearTraceAvailabilityCache();
    cardList.value = [];
    total.value = 0;
    docListLoading.value = true;
    resetPage();
  }
  loadLibraryInfo(newLibraryId);
}, { immediate: true });

watch(libraryScope, () => { selectedIds.value.clear(); resetPage(); void loadEntryFiles(libraryId.value); }, { deep: true });

// Restore all filters together before requesting the list after history navigation.
let documentFiltersRestoredFromRoute = false;
watch(() => route.query, query => {
  const restored = documentListFiltersFromQuery(query);
  if (!documentListFiltersEqual(documentFilters.value, restored)) {
    documentFiltersRestoredFromRoute = true;
    docSearchKeyword.value = restored.keyword;
    selectedFileType.value = restored.fileType;
    selectedContentType.value = restored.contentType;
    selectedStarred.value = restored.starred;
    selectedParseStatus.value = restored.parseStatus;
    selectedSource.value = restored.source;
    selectedSourceKind.value = restored.sourceKind;
    updatedTimeRange.value = restored.startDate || restored.endDate
      ? [restored.startDate, restored.endDate] : [];
  }
  const canonical = documentListFiltersToQuery(query, restored);
  if (!documentListFilterQueriesEqual(query, canonical)) void router.replace({ query: canonical });
}, { immediate: true });

watch(documentFilters, (filters, previous) => {
  if (documentListFiltersEqual(filters, previous)) return;
  const restoredFromRoute = documentFiltersRestoredFromRoute;
  documentFiltersRestoredFromRoute = false;
  const query = documentListFiltersToQuery(route.query, filters);
  if (!documentListFilterQueriesEqual(route.query, query)) void router.replace({ query });
  if (docSearchDebounce !== null) {
    window.clearTimeout(docSearchDebounce);
    docSearchDebounce = null;
  }
  resetPage();
  if (!libraryId.value) return;
  const keywordOnly = documentListFiltersEqual({ ...filters, keyword: previous.keyword }, previous);
  if (keywordOnly && filters.keyword && !restoredFromRoute) {
    docSearchDebounce = window.setTimeout(() => {
      docSearchDebounce = null;
      void loadEntryFiles(libraryId.value);
    }, 300);
  } else {
    void loadEntryFiles(libraryId.value);
  }
});

// Auto-open document detail when navigated with ?entry_id=xxx.
// Note: this runs both when the Library page mounts with a query param AND when a
// subsequent in-page navigation (e.g. from the global command palette) only
// changes the query without re-mounting the component; the internal ID is
// stable and cardList may already be populated, so relying solely on the
// cardList watcher misses the trigger.
const pendingEntryId = ref<string | null>(
  (route.query.entry_id as string) || null
);

let autoOpenRequest = 0;

const tryAutoOpenDocument = async () => {
  if (!pendingEntryId.value) return;
  activeReference.value = parseReference(route.query.reference);
  const targetId = pendingEntryId.value;
  referencedEntryId.value = targetId;
  pendingEntryId.value = null;
  const request = ++autoOpenRequest;
  const card = cardList.value.find((c: EntryCard) => c.id === targetId);

  let target = card || ({ id: targetId } as EntryCard);
  try {
    const response: any = await getEntryDetails(targetId);
    if (request !== autoOpenRequest) return;
    const detail = response?.data || response;
    if (detail && typeof detail === 'object') {
      target = { ...target, ...detail, id: targetId } as EntryCard;
    }
  } catch (error) {
    // Keep the previous ID-only fallback: getCardDetails will surface the
    // normal detail loading error, while links to root-level files still work.
    console.error('Failed to load referenced material', error);
  }

  if (request !== autoOpenRequest) return;
  await nextTick();
  openCardDetails(target);
};

// React to later ?entry_id= changes on the same Library route (no remount).
watch(
  () => [route.query.entry_id, route.query.reference],
  ([newId]) => {
    if (typeof newId !== 'string' || !newId) return;
    pendingEntryId.value = newId;
    tryAutoOpenDocument();
  },
);

// Dispatched by the global command palette when the user picks a chunk that
// lives in the Library they are already viewing — vue-router dedupes identical
// navigations, so we rely on this event instead of a URL change.
const handleOpenEntryEvent = async (e: Event) => {
  const detail = (e as CustomEvent<{ libraryId: string; entryId: string; location?: SourceLocation }>).detail;
  if (!detail || !detail.entryId) return;
  activeReference.value = detail.location;
  pendingEntryId.value = detail.entryId;
  await router.replace({ query: { ...route.query, entry_id: detail.entryId, reference: detail.location ? JSON.stringify(detail.location) : undefined } });
  tryAutoOpenDocument();
};

onMounted(() => {
  void resources.fetchLibrary().catch(() => {});

  window.addEventListener('reliquary:open-entry', handleOpenEntryEvent as EventListener);
});

onUnmounted(() => {
  if (docSearchDebounce !== null) {
    window.clearTimeout(docSearchDebounce);
    docSearchDebounce = null;
  }
  window.removeEventListener('reliquary:open-entry', handleOpenEntryEvent as EventListener);
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
});
watch(() => cardList.value, (newValue) => {

  docListLoading.value = false;

  // Auto-open document if navigated with ?entry_id=xxx.
  if (pendingEntryId.value) {
    tryAutoOpenDocument();
  }

  let analyzeList = [];
  // Filter articles whose processing is still active.
  analyzeList = newValue.filter(needsStatusPolling);
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
  if (analyzeList.length) {
    updateStatus(analyzeList)
  }

}, { deep: true })
type EntryCard = {
  id: string;
  starred?: boolean;
  source_kind?: string;
  vault_path?: string;
  library_id?: string;
  parse_status: string;
  description?: string;
  file_name?: string;
  original_file_name?: string;
  display_name?: string;
  title?: string;
  type?: string;
  updated_at?: string;
  file_type?: string;
  isMore?: boolean;
  metadata?: any;
  error_message?: string;
};
// Poll while article processing is active.
const needsStatusPolling = (item: EntryCard) => {
  return entryNeedsStatusPolling(item);
};

const updateStatus = (analyzeList: EntryCard[]) => {
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
  if (!analyzeList.length) return;

  let query = ``;
  for (let i = 0; i < analyzeList.length; i++) {
    query += `ids=${analyzeList[i].id}&`;
  }
  timeout = setTimeout(() => {
    batchQueryEntry(query).then((result: any) => {
      let hasChanges = false;
      if (result.success && result.data) {
        (result.data as EntryCard[]).forEach((item: EntryCard) => {
          const index = cardList.value.findIndex(card => card.id == item.id);
          if (index == -1) return;

          let parseStatus = item.parse_status;
          if (pendingReparseAck.value.has(item.id)) {
            if (isParseInFlight(item.parse_status)) {
              pendingReparseAck.value.delete(item.id);
            } else {
              parseStatus = 'pending';
            }
          }

          if (cardList.value[index].parse_status !== parseStatus ||
            cardList.value[index].description !== item.description) {

            // Always update the card data
            cardList.value[index].parse_status = parseStatus;
            Object.assign(cardList.value[index], item, { parse_status: parseStatus });
            delete traceAvailableById[item.id];
            hasChanges = true;
          }
        });
      }
      // If there are no changes, the watch won't trigger, so we must manually poll again
      // Even if there are changes, we can manually poll again just to be safe.
      // The watch will clear this timeout if it triggers.
      const stillPending = cardList.value.filter(needsStatusPolling);
      if (stillPending.length > 0) {
        updateStatus(stillPending);
      }
    }).catch((_err) => {
      // 错误处理
      const stillPending = cardList.value.filter(needsStatusPolling);
      if (stillPending.length > 0) {
        updateStatus(stillPending);
      }
    });
  }, 1500);
};

// 恢复文档处理状态（用于刷新后恢复）

const closeDoc = () => {
  isCardDetails.value = false;
};
const openCardDetails = (item: EntryCard) => {
  isCardDetails.value = true;
  getCardDetails(item);
};

// Open source document preview
const openSourceDoc = (entryId: string, location?: SourceLocation) => {
  activeReference.value = location;
  referencedEntryId.value = entryId;
  isCardDetails.value = true;
  getCardDetails({ id: entryId });
};

const openReportTopic = (topicId: string) => {
  closeDoc();
  void router.push({ path: '/platform/topics', query: { topic_id: topicId } });
};

const closeCardMoreMenu = (index: number) => {
  if (cardList.value?.[index]) {
    cardList.value[index].isMore = false;
  }
  moreIndex.value = -1;
};

const confirmDeleteEntry = (index: number, item: EntryCard) => {
  const deletedId = item?.id;
  return delEntry(index, item, async () => {
    closeCardMoreMenu(index);
    resetPage();
    const maxPolls = 30;
    const delayMs = 400;
    for (let i = 0; i < maxPolls; i++) {
      await loadEntryFiles(libraryId.value);
      const stillPresent = (cardList.value || []).some((c: EntryCard) => c.id === deletedId);
      if (!stillPresent) break;
      await new Promise<void>((r) => setTimeout(r, delayMs));
    }
    await pendingCounts.refresh();
  });
};

const onReparseMenuClick = (index: number, item: EntryCard) => {
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('library.rebuildInProgress'));
  }
};

const handleOpenLibrarySettings = () => uiStore.openSettings('library');
const reparseConfirmStore = useReparseConfirmStore();

const confirmLibraryRebuild = (scope: LibraryRebuildScope = 'failed') => {
  const target = selectedLibrary.value;
  if (!target || target.template === 'generated' || libraryRebuilding.value || libraryRebuildConfirming.value) return;
  libraryRebuildConfirming.value = true;
  const close = () => {
    libraryRebuildDialog?.destroy();
    libraryRebuildDialog = undefined;
    libraryRebuildConfirming.value = false;
  };
  libraryRebuildDialog = DialogPlugin.confirm({
    header: t(scope === 'all' ? 'library.rebuildLibraryAll' : 'library.rebuildLibrary'),
    body: t(scope === 'all' ? 'library.rebuildLibraryAllConfirm' : 'library.rebuildLibraryConfirm', { name: target.display_name || target.name }),
    confirmBtn: t(scope === 'all' ? 'library.rebuildLibraryAll' : 'library.rebuildLibrary'),
    cancelBtn: t('common.cancel'),
    onConfirm: () => { close(); void submitLibraryRebuild(target.id, scope); },
    onCancel: close,
    onClose: close,
  });
};

const submitLibraryRebuild = async (targetId: string, scope: LibraryRebuildScope) => {
  if (libraryRebuilding.value) return;
  libraryRebuildScope.value = scope;
  libraryRebuilding.value = true;
  libraryRebuildError.value = '';
  libraryRebuildNotice.value = '';
  try {
    const result = await rebuildCatalogLibrary(targetId, scope);
    if (!result.success) throw new Error(result.message || t('library.rebuildFailed'));
    libraryRebuildNotice.value = t(scope === 'all' ? 'library.rebuildLibraryAllSubmitted' : 'library.rebuildLibrarySubmitted', { count: result.data.total });
    MessagePlugin.success(libraryRebuildNotice.value);
  } catch (error: any) {
    libraryRebuildError.value = error?.message || t('library.rebuildFailed');
  } finally {
    libraryRebuilding.value = false;
  }
};
onUnmounted(() => libraryRebuildDialog?.destroy());

// Opens ONLY the trace drawer for this card — does NOT pop the
// document detail drawer behind it. The trace drawer attaches to
// body so it renders independent of its host's visibility; we just
// need `details` populated so the timeline component knows which
// entry_id to fetch. getCardDetails resets details synchronously
// then fills asynchronously, so we re-stamp the id/parse_status
// right after the call to avoid the brief empty-id window that
// would otherwise prevent the drawer from mounting.
const docContentRef = ref<any>(null);
const handleViewTrace = (index: number, item: EntryCard) => {
  if (cardList.value[index]) {
    cardList.value[index].isMore = false;
  }
  moreIndex.value = -1;
  getCardDetails(item);
  details.id = item.id;
  details.parse_status = item.parse_status;
  nextTick(() => {
    docContentRef.value?.openTimeline?.();
  });
};

const confirmRebuildEntry = async (index: number, item: EntryCard) => {
  if (!item?.id) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return;
  }
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('library.rebuildInProgress'));
    return;
  }
  closeCardMoreMenu(index);

  // No Library context to seed the dialog defaults — fall back to a direct reparse
  // that reuses the overrides stored at upload time.
  if (!libraryInfo.value) {
    await submitReparse(item.id);
    return;
  }

  // Prefill the confirm dialog with the overrides this doc was last parsed with.
  let processOverrides: EntryProcessOverrides | null = item.metadata?.process_overrides ?? null;
  let fileName = item.file_name || item.title || '';
  let fileType = item.file_type || '';
  try {
    const detail: any = await getEntryDetails(item.id);
    if (detail?.success && detail.data) {
      processOverrides = detail.data.metadata?.process_overrides ?? processOverrides;
      fileName = detail.data.file_name || detail.data.title || fileName;
      fileType = detail.data.file_type || fileType;
    }
  } catch {
    // fall back to the list item's fields
  }

  try {
    const result = await reparseConfirmStore.open({
      libraryInfo: libraryInfo.value,
      reparse: { entryId: item.id, fileName, fileType, processOverrides, sourceKind: item.source_kind },
    });
    await submitReparse(result.reparse.entryId, result.processConfig);
  } catch {
    // cancelled
  }
};

const submitReparse = async (id: string, processConfig?: EntryProcessOverrides) => {
  try {
    await reparseEntry(id, processConfig ? { process_config: processConfig } : undefined);
    delete traceAvailableById[id];
    traceAvailableById[id] = true;
    MessagePlugin.success(t('library.rebuildSubmitted'));
    resetPage();
    loadEntryFiles(libraryId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('library.rebuildFailed'));
  }
};

const handleScroll = () => {

  if (docListLoading.value) return;
  if (scrollLoading) return;
  const currentLibraryId = libraryId.value;
  if (!currentLibraryId) return;
  const element = entryScroll.value;
  if (element) {
    let pageNum = Math.ceil(total.value / pageSize)
    const { scrollTop, scrollHeight, clientHeight } = element;
    if (scrollTop + clientHeight >= scrollHeight - 10) {
      if (cardList.value.length < total.value && page < pageNum) {
        page++;
        scrollLoading = true;
        loadEntries({ page, page_size: pageSize, ...filterParams.value }, currentLibraryId).finally(() => {
          if (isCurrentLibrary(currentLibraryId)) {
            scrollLoading = false;
          }
        });
      }
    }
  }
};
const getDoc = (page: number) => {
  getfDetails(details.id, page || 1)
};

const syncDocumentProcessingState = (state: { id?: string; [key: string]: unknown }) => {
  if (!state?.id) return;
  const card = cardList.value.find((item: EntryCard) => item.id === state.id);
  if (card) Object.assign(card, state);
  if (selectedStarred.value !== '' && typeof state.starred === 'boolean' && String(state.starred) !== selectedStarred.value) {
    selectedIds.value.delete(state.id);
    resetPage();
    void loadEntryFiles(libraryId.value);
  }
};
const syncEntryJudgment = (state: EntryJudgment) => {
  syncDocumentProcessingState({ ...state });
  if (details.id === state.id) Object.assign(details, state);
  void pendingCounts.refresh();
};

const toggleSelectRow = (id: string, checked: boolean, shiftKey?: boolean) => {
  const items = cardList.value || [];
  const idx = items.findIndex((i: EntryCard) => i.id === id);
  if (shiftKey && lastSelectedIndex >= 0 && idx >= 0) {
    const [s, e] = idx < lastSelectedIndex
      ? [idx, lastSelectedIndex]
      : [lastSelectedIndex, idx];
    for (let i = s; i <= e; i++) {
      if (checked) selectedIds.value.add(items[i].id);
      else selectedIds.value.delete(items[i].id);
    }
  } else {
    if (checked) selectedIds.value.add(id);
    else selectedIds.value.delete(id);
  }
  lastSelectedIndex = idx;
};

const onCardGridCheckboxChange = (id: string, checked: boolean, ctx?: { e?: Event }) => {
  if (!ctx?.e && shouldSuppressDocClick()) return;
  const me = ctx?.e as MouseEvent | undefined;
  toggleSelectRow(id, checked, !!me?.shiftKey);
};

const toggleSelectAll = (checked: boolean) => {
  if (checked) {
    for (const item of cardList.value || []) selectedIds.value.add(item.id);
  } else {
    for (const item of cardList.value || []) selectedIds.value.delete(item.id);
  }
};

const clearSelection = () => {
  selectedIds.value.clear();
  lastSelectedIndex = -1;
};

// Batch selection keeps checkboxes hidden while off,
// no checkbox is rendered so the title doesn't jitter on hover; while on,
// checkboxes are persistent and clicking a card toggles its selection.
const batchMode = ref(false);
const toggleBatchMode = () => {
  batchMode.value = !batchMode.value;
  if (!batchMode.value) clearSelection();
};
// "取消选择" / 退出批量管理：清空选择，并退出 grid 视图下的批量模式。
const handleBatchCancel = () => {
  clearSelection();
  batchMode.value = false;
};
// 切到卡片视图时，如果列表视图里已经勾选过文档，需要自动开启批量管理模式，
// 否则卡片视图默认不渲染 checkbox，会看不到勾选态。
watch(viewMode, (mode) => {
  if (mode === 'grid' && selectedIds.value.size > 0) {
    batchMode.value = true;
  }
});
// Triggered from a card / row "..." menu. Use the batch selection flow where
// the menu item simply opens batch mode (no auto-selection).
const handleEnterBatchFromCard = (item: any) => {
  if (item) item.isMore = false;
  moreIndex.value = -1;
  clearSelection();
  batchMode.value = true;
};
const {
  onContainerMouseDown: onDocMarqueeMouseDown,
  marqueeVisible: docMarqueeVisible,
  marqueeMode: docMarqueeMode,
  boxStyle: docMarqueeBoxStyle,
  shouldSuppressClick: shouldSuppressDocClick,
} = useMarqueeSelect({
  containerRef: entryScroll,
  itemSelector: '.entry-card[data-select-id], .doc-list-row[data-select-id]',
  selectedIds,
  getItemId: (el) => el.dataset.selectId || null,
  enabled: computed(() => cardList.value.length > 0),
  onSelectionStart: () => {
    batchMode.value = true;
  },
});

const selectedDocumentsReprocessable = computed(() => Array.from(selectedIds.value).every(id => {
  const item = cardList.value.find((card: EntryCard) => card.id === id);
  return Boolean(item);
}));

const openEntryItem = (item: EntryCard) => {
  if (shouldSuppressDocClick()) return;
  openCardDetails(item);
};

const pendingReviewsRef = ref<InstanceType<typeof PendingReviews>>();
const handlePendingChanged = async () => {
  await Promise.all([loadEntryFiles(libraryId.value), pendingCounts.refresh()]);
};
const handleReviewChanged = async (_value: unknown, removed: boolean) => {
  if (removed) { closeDoc(); resetPage(); }
  else if (details.id) getCardDetails({ id: details.id });
  await Promise.all([loadEntryFiles(libraryId.value), pendingReviewsRef.value?.refresh(), pendingCounts.refresh()]);
};
const handlePersonalDocumentDeleted = async (id: string) => {
  if (details.id === id) closeDoc();
  selectedIds.value.delete(id);
  resetPage();
  await Promise.all([loadEntryFiles(libraryId.value), pendingReviewsRef.value?.refresh(), pendingCounts.refresh()]);
};

const confirmBatchDelete = async () => {
  if (batchDeleting.value || batchReparsing.value || selectedIds.value.size === 0) return;
  const ids = Array.from(selectedIds.value);
  const deletedIdSet = new Set(ids);
  batchDeleting.value = true;
  try {
    const res: any = await batchDeleteEntry(ids);
    if (res?.success) {
      MessagePlugin.success(t('library.batchDeleteSuccess', { count: ids.length }));
      clearSelection();
      batchMode.value = false;
      resetPage();
      // 后端将批量删除放入异步队列，立刻拉列表仍可能包含待删项；短轮询直到列表与后端一致或超时
      const maxPolls = 30;
      const delayMs = 400;
      for (let i = 0; i < maxPolls; i++) {
        await loadEntryFiles(libraryId.value);
        const stillPresent = (cardList.value || []).some((c: EntryCard) => deletedIdSet.has(c.id));
        if (!stillPresent) break;
        await new Promise<void>((r) => setTimeout(r, delayMs));
      }
      await pendingCounts.refresh();
    } else {
      MessagePlugin.error(res?.message || t('library.batchDeleteFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('library.batchDeleteFailed'));
  } finally {
    batchDeleting.value = false;
  }
};

const confirmCancelParseEntry = async (item: EntryCard) => {
  if (!item?.id) return;
  try {
    await cancelEntryParse(item.id);
    MessagePlugin.success(t('library.cancelParseSubmitted'));
    loadEntryFiles(libraryId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('library.cancelParseFailed'));
  }
};

// Bridge card-view actions back to existing per-card handlers.
const handleCardAction = (
  action: 'reparse' | 'cancel-parse' | 'delete' | 'view-trace' | 'batch-manage',
  item: EntryCard,
) => {
  const idx = (cardList.value || []).findIndex((i: EntryCard) => i.id === item.id);
  if (action === 'reparse') {
    if (isParseInFlight(item.parse_status)) return onReparseMenuClick(idx, item);
    return confirmRebuildEntry(idx, item);
  }
  if (action === 'cancel-parse') return confirmCancelParseEntry(item);
  if (action === 'delete') return confirmDeleteEntry(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Bridge list-view actions back to existing per-card handlers.
const handleListAction = (
  action: 'reparse' | 'cancel-parse' | 'delete' | 'view-trace' | 'batch-manage',
  item: EntryCard,
) => {
  const idx = (cardList.value || []).findIndex((i: EntryCard) => i.id === item.id);
  if (action === 'reparse') return confirmRebuildEntry(idx, item);
  if (action === 'cancel-parse') return confirmCancelParseEntry(item);
  if (action === 'delete') return confirmDeleteEntry(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Clear selection on filter/library change to avoid acting on hidden items.
watch(
  [docSearchKeyword, selectedFileType, selectedContentType, selectedStarred, selectedParseStatus, selectedSource, selectedSourceKind, updatedTimeRange, libraryId],
  () => {
    clearSelection();
  },
);

// After cardList reloads: stable keys rely on correct indices for shift-range; clamp anchor index.
watch(cardList, () => {
  const items = cardList.value || [];
  const n = items.length;
  if (lastSelectedIndex >= n) {
    lastSelectedIndex = n > 0 ? n - 1 : -1;
  }
  if (moreIndex.value >= n) {
    moreIndex.value = -1;
  }
  if (selectedIds.value.size === 0) return;
  const visible = new Set(items.map((i: EntryCard) => i.id));
  for (const id of selectedIds.value) {
    if (!visible.has(id)) selectedIds.value.delete(id);
  }
}, { deep: false });

// 处理库编辑成功后的回调
onMounted(() => window.addEventListener('reliquary:library-settings-saved', refreshLibrarySettings));
onUnmounted(() => window.removeEventListener('reliquary:library-settings-saved', refreshLibrarySettings));
const refreshLibrarySettings = () => { if (libraryId.value) void loadLibraryInfo(libraryId.value, true); };
</script>

<template>
  <div class="entry-layout">
      <div class="document-header">
        <div class="document-header-title">
          <div class="document-title-row">
            <h2 class="document-breadcrumb">
              <span>{{ $t('library.title') }}</span>
              <t-icon name="chevron-right" class="breadcrumb-separator" />
                <button type="button" :class="['breadcrumb-tab', { active: activeLibraryTab === 'documents' }]" :aria-pressed="activeLibraryTab === 'documents'"
                  @click="activeLibraryTab = 'documents'">{{ $t('knowledgeEditor.document.title') }}</button>
                <span class="breadcrumb-tab-sep">/</span><button type="button" :class="['breadcrumb-tab', { active: activeLibraryTab === 'reviews' }]" :aria-pressed="activeLibraryTab === 'reviews'" @click="activeLibraryTab = 'reviews'">{{ $t('knowledgeReview.title') }} <PendingCountBadge /></button>
                <span class="breadcrumb-tab-sep">/</span><button type="button" :class="['breadcrumb-tab', { active: activeLibraryTab === 'entities' }]" :aria-pressed="activeLibraryTab === 'entities'"
                    @click="activeLibraryTab = 'entities'">{{ $t('knowledgeWorkbench.tabLabel') }}</button>
            </h2>
            <!-- 标题行右侧的动作锚点：聚拢"信息"和"设置"两个圆形按钮。 -->
            <div class="library-title-actions">

              <LibraryInfoPopover v-if="libraryInfo" :library-info="libraryInfo"
                />
              <t-tooltip :content="$t('library.settings')" placement="top">
                <button type="button" class="library-settings-button" :disabled="!libraryId" @click="handleOpenLibrarySettings">
                  <t-icon name="setting" size="16px" />
                </button>
              </t-tooltip>
            </div>
          </div>
          <p v-if="activeLibraryTab === 'documents'" class="document-subtitle">{{ $t('knowledgeEditor.document.subtitle') }}</p>
        </div>
      </div>

      <t-alert v-if="libraryRebuildError" theme="error" :message="libraryRebuildError" role="alert" />
      <div v-if="libraryRebuildNotice" role="status">
        {{ libraryRebuildNotice }}
        <t-button variant="text" size="small" @click="uiStore.openSettings('runtime-queues')">{{ $t('settings.taskQueue') }}</t-button>
      </div>

      <div v-if="activeLibraryTab === 'entities'" class="entity-workbench-area">
        <EntryWorkbench :initial-entity-id="selectedEntityId"
          @open-source="openSourceDoc" />
      </div>

      <PendingReviews v-if="activeLibraryTab === 'reviews' && libraryId" ref="pendingReviewsRef" @open-source="openSourceDoc" @changed="handlePendingChanged" @refreshed="pendingCounts.refresh()" />

      <template v-if="activeLibraryTab === 'documents'">
        <div class="entry-main">

          <div class="documents-content">
            <div class="doc-card-area">

              <div class="doc-filter-bar">
                <t-input v-model.trim="docSearchKeyword" :placeholder="$t('library.docSearchPlaceholder')"
                  clearable class="doc-search-input" @clear="loadEntryFiles(libraryId)"
                  @enter="loadEntryFiles(libraryId)">
                  <template #prefix-icon>
                    <t-icon name="search" size="16px" />
                  </template>
                </t-input>
                <div class="doc-filter-bar__filters">
                <LibraryScopeSelect v-model="libraryScope" @loaded="libraryCatalog = $event" />
                <LibraryReviewAction :library="selectedLibrary" scope="all" :disabled="libraryRebuilding || libraryRebuildConfirming" @submitted="pendingCounts.refresh()" />

                <div class="doc-filter-field">
                  <t-select v-model="selectedContentType" :options="contentTypeOptions" clearable :placeholder="$t('library.contentTypeFilter')" :aria-label="$t('library.contentTypeFilter')" class="doc-type-select doc-filter-field__control" />
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedStarred" :options="starredOptions" :aria-label="$t('articleJudgments.starLabel')" class="doc-type-select doc-filter-field__control" />
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedFileType" :options="fileTypeOptions"
                    :placeholder="$t('library.fileTypeFilter')" :aria-label="$t('library.fileTypeFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="file" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedParseStatus" :options="parseStatusOptions"
                    :placeholder="$t('library.parseStatusFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="check-circle" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedSourceKind" :options="sourceKindOptions"
                    :placeholder="$t('library.materialCategory')" :aria-label="$t('library.materialCategory')"
                    class="doc-type-select doc-filter-field__control" clearable />
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedSource" :options="sourceOptions"
                    :placeholder="$t('library.channelLabel')" :aria-label="$t('library.channelLabel')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="link" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field doc-filter-field--wide">
                  <t-date-range-picker v-model="updatedTimeRange"
                    :placeholder="[$t('library.updatedTimeFrom'), $t('library.updatedTimeTo')]"
                    :disable-date="disableFutureDate" class="doc-date-range doc-filter-field__control" clearable
                    allow-input>
                    <template #prefixIcon>
                      <t-icon name="time" size="16px" />
                    </template>
                  </t-date-range-picker>
                </div>
                </div>
                <div class="doc-filter-bar__trailing">
                  <t-tooltip v-if="selectedLibrary?.template !== 'generated'" :content="selectedLibrary ? (selectedLibrary.display_name || selectedLibrary.name) : $t('library.rebuildLibrarySelectOne')">
                    <span>
                      <t-button variant="outline" theme="primary" size="small" :loading="libraryRebuilding && libraryRebuildScope === 'failed'"
                        :disabled="!selectedLibrary || libraryRebuilding || libraryRebuildConfirming" @click="confirmLibraryRebuild()">
                        {{ $t('library.rebuildLibrary') }}
                      </t-button>
                      <t-button variant="text" size="small" :loading="libraryRebuilding && libraryRebuildScope === 'all'"
                        :disabled="!selectedLibrary || libraryRebuilding || libraryRebuildConfirming" @click="confirmLibraryRebuild('all')">
                        {{ $t('library.rebuildLibraryAll') }}
                      </t-button>
                    </span>
                  </t-tooltip>
                  <t-button  variant="text" size="small" @click="commandPalette.openPalette(docSearchKeyword.trim())">
                    {{ $t('library.searchMessageContent') }}
                  </t-button>
                  <div class="doc-view-toggle" role="group" :aria-label="$t('library.viewModeToggle')">
                    <t-tooltip :content="$t('library.viewModeGrid')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'grid' }"
                        @click="viewMode = 'grid'" :aria-pressed="viewMode === 'grid'">
                        <t-icon name="view-module" size="16px" />
                      </button>
                    </t-tooltip>
                    <t-tooltip :content="$t('library.viewModeList')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'list' }"
                        @click="viewMode = 'list'" :aria-pressed="viewMode === 'list'">
                        <t-icon name="view-list" size="16px" />
                      </button>
                    </t-tooltip>
                  </div>
                  <div class="doc-filter-actions">
                  </div>
                </div>
              </div>
              <div class="doc-scroll-container"
                :class="{
                  'is-empty': !cardList.length && !docListLoading,
                  'is-marquee-active': docMarqueeVisible,
                }"
                ref="entryScroll" @scroll="handleScroll" @mousedown="onDocMarqueeMouseDown">
                <div v-if="docMarqueeVisible" class="doc-marquee-box"
                  :class="{ 'is-add': docMarqueeMode === 'add', 'is-subtract': docMarqueeMode === 'subtract' }"
                  :style="docMarqueeBoxStyle" aria-hidden="true" />
                <!-- 文档骨架屏 -->
                <div v-if="docListLoading && cardList.length === 0" class="doc-card-list doc-card-list-animated">
                  <div v-for="n in 8" :key="'doc-skel-' + n" class="entry-card entry-card-skeleton">
                    <div class="card-content">
                      <div class="card-content-nav">
                        <t-skeleton animation="gradient" :row-col="[{ width: '70%', height: '18px' }]" />
                      </div>
                      <t-skeleton animation="gradient"
                        :row-col="[{ width: '100%', height: '14px' }, { width: '60%', height: '14px' }]" />
                    </div>
                    <div class="card-bottom">
                      <t-skeleton animation="gradient"
                        :row-col="[[{ width: '80px', height: '14px' }, { width: '40px', height: '18px', type: 'rect' }]]" />
                    </div>
                  </div>
                </div>
                <template v-else-if="cardList.length && viewMode === 'grid' && selectedParseStatus !== 'rejected'">
                  <DocumentCardView
                    :items="cardList" @open-entity="openArticleEntity" @judgment-updated="syncEntryJudgment"

                    :selected-ids="selectedIds"
                    :batch-mode="batchMode"

                    :trace-available-by-id="traceAvailableById"

                    @open="(item: any) => openEntryItem(item)"

                    @toggle-checkbox="onCardGridCheckboxChange"
                    @menu-visible-change="(visible: boolean, item: any) => onCardMoreVisibleChange(visible, item)"
                    @action="(action: any, item: any) => handleCardAction(action, item)"
                    :delete-entry="(item: any) => confirmDeleteEntry(cardList.findIndex((card: any) => card.id === item.id), item)"
                    @deleted="handlePersonalDocumentDeleted"

                  />
                </template>
                <template v-else-if="cardList.length && (viewMode === 'list' || selectedParseStatus === 'rejected')">
                  <DocumentListView :items="cardList" @open-entity="openArticleEntity" @judgment-updated="syncEntryJudgment"
                    :selected-ids="selectedIds"

                    :trace-visible-ids="traceAvailableById"

                    @open="(item: any) => openEntryItem(item)" @toggle-row="toggleSelectRow"
                    @toggle-all="toggleSelectAll" @action="(action: any, item: any) => handleListAction(action, item)"
                    @probe-trace="(item: any) => probeTraceAvailable(item)"
                    :delete-entry="(item: any) => confirmDeleteEntry(cardList.findIndex((card: any) => card.id === item.id), item)"
                    @deleted="handlePersonalDocumentDeleted"

                     />
                </template>
                <template v-else-if="!docListLoading">
                  <div class="doc-empty-state">

                    <EmptyEntry />
                  </div>
                </template>
              </div>
              <div class="doc-batch-bar-anchor" v-show="batchMode || selectedIds.size > 0">
                <DocumentBatchBar :count="selectedIds.size" :delete-loading="batchDeleting"
                  :reparse-loading="batchReparsing" :visible="batchMode || selectedIds.size > 0"
                  :reparse-allowed="selectedDocumentsReprocessable"

                  @cancel="handleBatchCancel" @delete="confirmBatchDelete" @reparse="confirmBatchReparse"
                   />
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- Document content drawer -->
      <DocContent ref="docContentRef" :visible="isCardDetails" :details="details" :reference="details.id === referencedEntryId ? activeReference : undefined"
        :libraryId="libraryId"
        @openEntry="openSourceDoc" @openEntity="openArticleEntity" @openTopic="openReportTopic"
        @closeDoc="closeDoc" @getDoc="getDoc" @processingStateChange="syncDocumentProcessingState" @review-changed="handleReviewChanged">
      </DocContent>
    </div>

  <!-- 库编辑器（创建/编辑统一组件） -->

</template>
<style scoped lang="less">
.entry-layout {
  display: flex;
  flex-direction: column;
  margin: 0 16px 0 4px;
  gap: 20px;
  height: 100%;
  flex: 1;
  width: auto;
  min-width: 0;
  padding: 24px 32px 0px;
  box-sizing: border-box;
}

// Breadcrumb tab switch
.breadcrumb-tab {
  border: 0;
  background: transparent;
  padding: 0;
  font: inherit;
  cursor: pointer;
  color: var(--td-text-color-placeholder);
  font-weight: 400;
  transition: color 0.15s;
  display: inline-flex;
  align-items: center;
  gap: 4px;

  &:hover {
    color: var(--td-text-color-primary);
  }

  &.active {
    color: var(--td-brand-color);
    font-weight: 600;
  }

  &:focus-visible {
    outline: 2px solid var(--td-brand-color);
    outline-offset: 2px;
  }

  &.indexing {
    color: var(--td-brand-color);
  }
}

.breadcrumb-tab-indicator {
  display: inline-flex;
  align-items: center;
  color: var(--td-brand-color);
  font-size: 12px;
  line-height: 1;
}

.breadcrumb-tab-sep {
  margin: 0 6px;
  color: var(--td-text-color-disabled);
  font-weight: 400;
}

// 与列表页一致：浅灰底圆角区，左侧筛选为白底卡片
.entry-main {
  display: flex;
  flex: 1;
  min-height: 0;
  background: transparent;
  border: none;
}

.entity-workbench-area {
  flex: 1;
  min-height: 0;
  min-width: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.documents-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 0;
  border: none;
  overflow: hidden;
  background: transparent;
}

.doc-card-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  position: relative;
  container-type: inline-size;
  container-name: doc-card-area;
  /* 作为批量工具栏悬浮的定位上下文 */
}

// 目录树选中路径的面包屑：与顶部库面包屑同一套视觉语言，只是更轻量。

.doc-filter-bar__filters > .library-scope { flex: 0 0 220px; width: 220px; min-width: 220px; }

.doc-filter-bar {
  padding: 0 0 12px 0;
  flex-shrink: 0;
  display: grid;
  grid-template-columns: 1fr auto;
  grid-template-areas:
    'search trailing'
    'filters filters';
  gap: 8px 12px;
  align-items: center;

  .doc-search-input {
    grid-area: search;
    min-width: 0;
    width: 100%;
  }

  &__filters {
    grid-area: filters;
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    overflow-x: auto;
    flex-wrap: nowrap;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: thin;
    scrollbar-color: rgba(0, 0, 0, 0.15) transparent;

    &::-webkit-scrollbar {
      height: 4px;
    }

    &::-webkit-scrollbar-thumb {
      background-color: rgba(0, 0, 0, 0.15);
      border-radius: 2px;
    }
  }

  &__trailing {
    grid-area: trailing;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    position: relative;
    z-index: 1;
  }

  // The document area width can differ from the viewport width.
  // Switch to a single row only when this content area itself
  // is wide enough for TDesign's fixed-width filter controls.
  @container doc-card-area (min-width: 1240px) {
    display: flex;
    flex-direction: row;
    flex-wrap: nowrap;
    gap: 12px;

    &__filters {
      flex: 1 1 auto;
      overflow-x: auto;
    }
  }

  .doc-filter-field {
    width: 140px;
    flex-shrink: 0;

    &--wide {
      width: 280px;
    }

    &__control {
      width: 100%;
    }
  }

  @media (min-width: 1280px) {
    .doc-search-input {
      flex: 1 1 220px;
      min-width: 220px;
    }
  }

  .doc-type-select {
    width: 100%;
  }

  .doc-date-range {
    width: 100%;

    // TDesign focuses both the outer popup reference and inner inputs, which
    // visually stacks into a "double border" — drop the inner shadow.
    :deep(.t-input--focused),
    :deep(.t-is-focused) {
      box-shadow: none;
    }
  }

  .doc-view-toggle {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    padding: 2px;
    background: var(--td-bg-color-secondarycontainer);
    border-radius: 6px;
    gap: 0;

    .doc-view-toggle-btn {
      width: 28px;
      height: 24px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      border: 0;
      background: transparent;
      border-radius: 4px;
      color: var(--td-text-color-secondary, #888);
      cursor: pointer;
      transition: background-color 0.12s ease, color 0.12s ease;

      &:hover {
        color: var(--td-text-color-primary, #232323);
      }

      &.active {
        background: var(--td-bg-color-container, #fff);
        color: var(--td-brand-color, #0052d9);
        box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
      }
    }
  }

  .doc-filter-actions {
    flex-shrink: 0;

    :deep(.content-bar-icon-btn) {
      color: var(--td-text-color-secondary);
      background: transparent;
      border: none;

      &:hover {
        color: var(--td-brand-color);
        background: var(--td-bg-color-secondarycontainer);
      }
    }
  }

  :deep(.t-input) {
    font-size: 13px;
    background-color: var(--td-bg-color-secondarycontainer);
    border-color: transparent;
    border-radius: 6px;
    box-shadow: none !important;

    &:hover,
    &:focus,
    &.t-is-focused {
      border-color: var(--td-brand-color);
      background-color: var(--td-bg-color-container);
      box-shadow: none !important;
    }
  }

  :deep(.t-select) {
    .t-input {
      font-size: 13px;
      background-color: var(--td-bg-color-secondarycontainer);
      border-color: transparent;
      border-radius: 6px;
      box-shadow: none !important;

      &:hover,
      &.t-is-focused {
        border-color: var(--td-brand-color);
        background-color: var(--td-bg-color-container);
        box-shadow: none !important;
      }
    }
  }
}

.doc-scroll-container {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding-right: 4px;

  &.is-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    overflow-y: hidden;
  }

  &.is-marquee-active {
    cursor: crosshair;
  }
}

.doc-marquee-box {
  position: absolute;
  z-index: 4;
  pointer-events: none;
  border: 1px solid var(--td-brand-color);
  background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
  border-radius: 2px;

  &.is-add {
    border-color: var(--td-brand-color);
    background: color-mix(in srgb, var(--td-brand-color) 14%, transparent);
  }

  &.is-subtract {
    border-color: var(--td-error-color-6);
    background: color-mix(in srgb, var(--td-error-color-6) 12%, transparent);
  }
}

/* 批量条悬浮在滚动区底部，不挤占列表高度 */
.doc-batch-bar-anchor {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 12px;
  z-index: 6;
  display: flex;
  justify-content: center;
  padding: 0 16px;
  pointer-events: none;

  &>* {
    pointer-events: auto;
  }
}

// Header 样式（无底部分割线，留更多空间给下方内容区）
.document-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  flex-shrink: 0;

  .document-header-title {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .document-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .library-title-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    margin-left: 4px;
  }

  .document-breadcrumb {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .breadcrumb-link {
    border: none;
    background: transparent;
    padding: 4px 8px;
    margin: -4px -8px;
    font: inherit;
    color: var(--td-text-color-secondary);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border-radius: 6px;
    transition: all 0.12s ease;

    &:hover:not(:disabled) {
      color: var(--td-success-color);
      background: var(--td-bg-color-container);
    }

    &:disabled {
      cursor: not-allowed;
      color: var(--td-text-color-placeholder);
    }

    &.dropdown {
      padding-right: 6px;

      :deep(.t-icon) {
        font-size: 14px;
        transition: transform 0.12s ease;
      }

      &:hover:not(:disabled) {
        :deep(.t-icon) {
          transform: translateY(1px);
        }
      }
    }
  }

  .breadcrumb-separator {
    font-size: 14px;
    color: var(--td-text-color-placeholder);
  }

  .breadcrumb-current {
    color: var(--td-text-color-primary);
    font-weight: 600;
  }

  h2 {
    margin: 0;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 24px;
    font-weight: 600;
    line-height: 32px;
  }

  .document-subtitle {
    margin: 0;
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 14px;
    font-weight: 400;
    line-height: 20px;
  }

  .entry-header-hint__action {
    padding: 0;
    border: 0;
    background: transparent;
    font: inherit;
    text-align: left;
    cursor: pointer;

    &:focus-visible {
      outline: 2px solid var(--td-brand-color);
      outline-offset: 2px;
    }
  }

  .storage-engine-warning {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 2px 0 0;
    color: var(--td-warning-color);
    font-size: 12px;
    line-height: 1.4;
    cursor: pointer;
    transition: color 0.15s ease;

    &:hover {
      color: var(--td-warning-color-active);

      .warning-link {
        text-decoration: underline;
      }
    }

    .warning-icon {
      font-size: 12px;
      flex-shrink: 0;
    }

    .warning-link {
      color: var(--td-brand-color);
      margin-left: 2px;
      white-space: nowrap;
    }
  }
}

.library-settings-button {
  width: 30px;
  height: 30px;
  border: none;
  border-radius: 50%;
  background: var(--td-bg-color-secondarycontainer);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  padding: 0;

  &:hover:not(:disabled) {
    background: var(--td-success-color-light);
    color: var(--td-brand-color);
    box-shadow: none;
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }

  :deep(.t-icon) {
    font-size: 18px;
  }
}

.card-bottom-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  overflow: hidden;
}

@media (max-width: 1250px) and (min-width: 1045px) {
  .answers-input {
    transform: translateX(-329px);
  }

  :deep(.t-textarea__inner) {
    width: 654px !important;
  }
}

@media (max-width: 1045px) {
  .answers-input {
    transform: translateX(-250px);
  }

  :deep(.t-textarea__inner) {
    width: 500px !important;
  }
}

@media (max-width: 750px) {
  .answers-input {
    transform: translateX(-182px);
  }

  :deep(.t-textarea__inner) {
    width: 340px !important;
  }
}

@media (max-width: 600px) {
  .answers-input {
    transform: translateX(-164px);
  }

  :deep(.t-textarea__inner) {
    width: 300px !important;
  }
}

@keyframes contentFadeIn {
  from {
    opacity: 0;
    transform: translateY(6px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.doc-card-list {
  box-sizing: border-box;
  display: grid;
  // 文档卡片信息量较大（标题 + 摘要 + 类型），保持稍宽的最小列宽，避免一行塞太多导致内容拥挤。
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
  align-content: flex-start;
  width: 100%;

  &.doc-card-list-animated {
    animation: contentFadeIn 0.32s ease-out;
  }
}

.entry-card-skeleton {
  cursor: default;

  .card-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 14px 8px;
  }

  .card-content-nav {
    margin-bottom: 8px;
  }

  .card-bottom {
    flex-shrink: 0;
    margin-top: auto;
    width: 100%;
    padding: 0 14px;
    box-sizing: border-box;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    border-top: 1px solid var(--td-component-stroke);
  }
}

.doc-empty-state {
  flex: 1;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  min-height: 100%;
}

.card-menu {
  display: flex;
  flex-direction: column;
  min-width: 140px;
  gap: 1px;
}

.card-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  cursor: pointer;
  color: var(--td-text-color-primary);
  transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);
  border-radius: 6px;
  font-size: 14px;
  line-height: 20px;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:active {
    background: var(--td-bg-color-container-active);
    transform: scale(0.98);
  }

  .icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
    transition: all 0.15s cubic-bezier(0.2, 0, 0, 1);
  }

  &:hover .icon {
    color: var(--td-text-color-primary);
  }

  &.danger {
    color: var(--td-error-color-6);
    margin-top: 4px;
    position: relative;

    &::before {
      content: '';
      position: absolute;
      top: -3px;
      left: 8px;
      right: 8px;
      height: 1px;
      background: var(--td-component-stroke);
    }

    .icon {
      color: var(--td-error-color-6);
    }

    &:hover {
      background: var(--td-error-color-1);
      color: var(--td-error-color-6);

      .icon {
        color: var(--td-error-color-6);
      }
    }

    &:active {
      background: var(--td-error-color-2);
    }
  }
}

.card-draft {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  flex-shrink: 0;
}

.card-draft-tip {
  color: var(--td-warning-color);
  font-size: 11px;
}

.entry-card {
  min-width: 240px;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--td-component-border);
  height: 136px;
  border-radius: 8px;
  overflow: hidden;
  box-sizing: border-box;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
  background: var(--td-bg-color-container);
  position: relative;
  cursor: pointer;
  transition: border-color 0.2s ease, box-shadow 0.2s ease, background-color 0.2s ease;

  /* 仅在批量管理模式下渲染 checkbox，常态下不占位，避免标题在 hover 时右滑 */
  .card-nav-check {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 29px;
    margin-right: 8px;
    cursor: pointer;

    .card-select-checkbox {
      margin: 0;
      line-height: 0;

      :deep(.t-checkbox) {
        align-items: center;
      }

      :deep(.t-checkbox__label) {
        display: none !important;
        width: 0 !important;
        min-width: 0 !important;
        margin: 0 !important;
        padding: 0 !important;
      }

      :deep(.t-checkbox__input) {
        margin: 0;
      }

      :deep(.t-checkbox__input-wrapper) {
        margin: 0;
      }
    }
  }

  .card-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 14px 8px;
  }

  .card-analyze {
    flex-shrink: 0;
    height: 52px;
    display: flex;
    align-items: flex-start;
  }

  .card-analyze-loading {
    display: block;
    color: var(--td-brand-color);
    font-size: 14px;
    margin-top: 2px;
  }

  .card-analyze-txt {
    color: var(--td-brand-color);
    font-family: var(--app-font-family);
    font-size: 11px;
    margin-left: 8px;
  }

  // In-flight / failed: only status text + trace icon open the drawer.
  .card-analyze-trace {
    height: auto;
    min-height: 0;
    align-items: center;
    gap: 2px;
  }

  .card-analyze-trace-link {
    cursor: pointer;

    &:hover {
      text-decoration: underline;
    }
  }

  .card-analyze-trace-btn {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    margin: 0;
    padding: 2px;
    border: none;
    background: transparent;
    color: var(--td-brand-color);
    cursor: pointer;
    line-height: 1;
    border-radius: 4px;

    :deep(.t-icon) {
      font-size: 14px;
    }

    &:hover {
      background: var(--td-bg-color-component-hover);
    }
  }

  .card-analyze.failure .card-analyze-trace-btn {
    color: var(--td-error-color);
  }

  .failure {
    color: var(--td-error-color);
  }

  .card-content-nav {
    flex-shrink: 0;
    display: flex;
    align-items: flex-start;
    gap: 0;
    margin-bottom: 6px;
  }

  .card-content-title {
    flex: 1;
    min-width: 0;
    height: 24px;
    line-height: 24px;
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 14px;
    font-weight: 600;
    letter-spacing: 0.01em;
    margin-right: 8px;
  }

  .more-wrap {
    flex-shrink: 0;
    display: flex;
    width: 25px;
    height: 25px;
    justify-content: center;
    align-items: center;
    border-radius: 5px;
    cursor: pointer;
  }

  .more-wrap:hover {
    background: var(--td-component-stroke);
  }

  .more-icon {
    width: 14px;
    height: 14px;
  }

  .active-more {
    background: var(--td-component-stroke);
  }

  .card-content-txt {
    flex: 1;
    min-height: 0;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    line-height: 19px;
  }

  .card-bottom {
    flex-shrink: 0;
    margin-top: auto;
    padding: 0 14px;
    box-sizing: border-box;
    height: 32px;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--td-bg-color-container);
    border-top: 1px solid var(--td-component-stroke);
  }

  .card-time {
    flex-shrink: 0;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
  }

  .card-type {
    flex-shrink: 0;
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 11px;
    font-weight: 500;
    padding: 0;
    background: transparent;
    letter-spacing: 0.02em;
  }
}

.card-bottom-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  overflow: hidden;
}

.entry-card:hover {
  border-color: color-mix(in srgb, var(--td-component-stroke) 55%, var(--td-brand-color));
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.07);
}

/* 悬停知识卡片时跟随鼠标的详情气泡 */
.entry-card-hover-popover {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  min-width: 220px;
  max-width: 360px;
  padding: 12px 14px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  font-family: var(--app-font-family);
  transition: opacity 0.15s ease;
  will-change: transform;

  /* 防止气泡内容抖动 */
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
  transform: translateZ(0);
  -webkit-transform: translateZ(0);

  .card-popover-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin-bottom: 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-popover-status {
    font-size: 12px;
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 6px;

    &.parsing {
      color: var(--td-brand-color);
    }

    &.failure {
      color: var(--td-error-color);
    }

    &.draft {
      color: var(--td-warning-color);
    }
  }

  .card-popover-desc {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    line-height: 1.5;
    margin-bottom: 8px;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 5;
    line-clamp: 5;
    overflow: hidden;
  }

  .card-popover-error-msg {
    display: block;
    margin-top: 4px;
    font-size: 11px;
    color: var(--td-error-color);
    opacity: 0.95;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 280px;
  }

  .card-popover-source {
    font-size: 11px;
    color: var(--td-brand-color);
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .card-popover-extra {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
    margin-bottom: 6px;
  }

  .card-popover-created,
  .card-popover-size {
    flex-shrink: 0;
  }

  .card-popover-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }

  .card-popover-channel {
    padding: 1px 6px;
    background: var(--td-warning-color-light);
    color: var(--td-warning-color);
    border-radius: 4px;
  }

  .card-popover-type {
    padding: 1px 6px;
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-secondary);
    border-radius: 4px;
  }

  .card-popover-hint {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--td-component-stroke);
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }
}

.del-card {
  vertical-align: middle;
}
@media (max-width: 640px) {
  .entry-layout {
    margin: 0;
    padding: 16px 12px 0;
    gap: 12px;
    min-height: 0;
  }

  .document-header {
    min-width: 0;

    .document-header-title,
    .document-title-row {
      min-width: 0;
      max-width: 100%;
    }

    .document-breadcrumb {
      flex-wrap: wrap;
      min-width: 0;
      font-size: 16px;
      overflow-wrap: anywhere;
    }

    .breadcrumb-link {
      max-width: 100%;
      white-space: normal;
      text-align: left;
    }

    .entry-header-hint {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      min-width: 0;
      max-width: 100%;

      .entry-header-hint__action {
        grid-column: 2;
        justify-self: start;
        margin-left: 0;
        max-width: 100%;
        white-space: normal;
      }
    }

    .entry-header-hint__text {
      min-width: 0;
      overflow-wrap: anywhere;
      white-space: normal;
    }

  }
}

@media (max-width: 640px) {
  .doc-filter-bar { display: flex; flex-direction: column; gap: 12px; }
  .doc-filter-bar__filters > .library-scope { width: 100%; min-width: 0; max-width: none; }
  .doc-filter-bar__filters { display: grid; grid-template-columns: minmax(0, 1fr); width: 100%; }
  .doc-filter-field, .doc-filter-field__control, .doc-date-range { width: 100%; min-width: 0; }
}
</style>
