<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, reactive, computed } from "vue";
import { Button, Input, Select, Chip, LibMark, Skeleton, EmptyState, ErrorState, confirm, toast } from '@/components/ui';
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue';
import { useLibraryScope } from '@/composables/useLibraryScope';
import { entryDetailLocation, parseReference } from '@/utils/bodyReference';
import type { SourceLocation } from '@/types/search';
import useLibrary from '@/composables/useLibrary';
import { useRoute, useRouter } from 'vue-router';

import { useResourcesStore } from '@/stores/resources';

import {
  batchQueryEntry,
  reparseEntry,
  cancelEntryParse,
  deleteDocument,
  getEntryDetails,
} from "@/api/library";
import DocumentListView from './components/DocumentListView.vue';
import EntryPreview from './components/EntryPreview.vue';
import EntryBatchDialog from '@/components/EntryBatchDialog.vue';
import type { EntryBatchSelection } from '@/api/entry-batches';
import { listEntities, type Entity } from '@/api/entities';
import { listTopics, type Topic } from '@/api/topics';
import DocumentBatchBar from './components/DocumentBatchBar.vue';
import EntryWorkbench from './components/EntryWorkbench.vue';
import { usePendingCountsStore } from '@/stores/pendingCounts';
import type { LibraryContentType } from '@/api/library';
import { SEARCH_SOURCES } from '@/types/search';
import {
  documentListFiltersFromQuery,
  documentListFiltersToQuery,
  documentListFilterQueriesEqual,
  documentListFiltersEqual,
  type DocumentListFilters,
} from './documentListFilters';
import type { EntryProcessOverrides } from '@/types/entryProcess';
import { useReparseConfirmStore } from '@/stores/reparseConfirm';
import {
  entryNeedsStatusPolling,
} from './entryStatusPolling';
import { useI18n } from 'vue-i18n';
import { useMarqueeSelect } from '@/composables/useMarqueeSelect';
const resources = useResourcesStore();
const router = useRouter();
const route = useRoute();
const { t } = useI18n();
const pendingCounts = usePendingCountsStore();
const libraryId = computed(() => resources.library?.id || '');
const libraryInfo = ref<any>(null);
const libraryScope = useLibraryScope();
watch(() => route.query.libraries, value => {
  const ids = typeof value === 'string' ? value.split(',').filter(Boolean) : [];
  if (ids.join(',') !== libraryScope.value.join(',')) libraryScope.value = ids;
}, { immediate: true });
const libraryCatalog = ref<import('@/api/library').LibraryCatalogItem[]>([]);
const currentLibraryLabel = computed(() => {
  const selected = libraryCatalog.value.find(item => item.id === libraryScope.value[0]);
  return libraryScope.value.length === 1 && selected
    ? selected.display_name || selected.name : t('libraryEditor.document.title');
});
const libraryLoading = ref(false);
const libraryError = ref('');
const docListLoading = ref(true);
const validTabs = ['documents', 'reviews', 'entities'] as const
type KbTab = typeof validTabs[number]
const initTab = validTabs.includes(route.query.tab as any) ? (route.query.tab as KbTab) : 'documents'
const activeLibraryTab = ref<KbTab>(initTab);

let { cardList, total, listError, loadEntries, delEntry } = useLibrary(libraryId.value)

let disposed = false;
let pollGeneration = 0;
let timeout: ReturnType<typeof setTimeout> | null = null;
let entryScroll = ref()
let page = 1;
let pageSize = 35;
const scrollLoading = ref(false);
let listGeneration = 0;
let loadingGeneration = 0;
const resetPage = () => { listGeneration++; page = 1; scrollLoading.value = false; };

// List and table preference is persisted per browser.
type DocViewMode = 'list' | 'table';
const VIEW_MODE_KEY = 'reliquary.library.docs.viewMode';
const initViewMode = (): DocViewMode => {
  try {
    return localStorage.getItem(VIEW_MODE_KEY) === 'table' ? 'table' : 'list';
  } catch { return 'list'; }
};
const viewMode = ref<DocViewMode>(initViewMode());
const focusedId = ref('');
const focusedEntry = computed(() => cardList.value.find((item: EntryCard) => item.id === focusedId.value));
const currentLibrary = computed(() => libraryScope.value.length === 1 ? libraryCatalog.value.find(item => item.id === libraryScope.value[0]) : undefined);
const libraryNumber = computed(() => currentLibrary.value ? String(libraryCatalog.value.indexOf(currentLibrary.value) + 1).padStart(2, '0') : '');
const searchInput = ref<InstanceType<typeof Input>>();
const batchInitialAction = ref<import('@/api/entry-batches').EntryBatchAction>('star');
const composingSearch = ref(false);
function submitSearch(event?: KeyboardEvent) {
  if (composingSearch.value || event?.isComposing) return;
  if (docSearchDebounce !== null) { window.clearTimeout(docSearchDebounce); docSearchDebounce = null; }
  resetPage(); void loadEntryFiles(libraryId.value);
}
function clearSearch() { docSearchKeyword.value = ''; searchInput.value?.focus(); }
const today = new Date();
const latestFilterDate = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`;
function updateDate(range: 'published' | 'updated', index: number, event: Event) {
  const target = range === 'published' ? publishedRange : updatedTimeRange;
  const values = [...target.value]; values[index] = (event.target as HTMLInputElement).value;
  target.value = values.some(Boolean) ? values : [];
}
function updateSelectedOptions(event: Event): string[] { return Array.from((event.target as HTMLSelectElement).selectedOptions, option => option.value); }
watch(cardList, items => { if (!items.some(item => item.id === focusedId.value)) focusedId.value = items[0]?.id || ''; });
watch(viewMode, (v) => {
  try { localStorage.setItem(VIEW_MODE_KEY, v); } catch { /* ignore */ }
});

// Checked IDs are independent from the focused entry.
// Vue 3.5 tracks Set#add/delete natively, so direct mutation is reactive.
const selectedIds = ref<Set<string>>(new Set());
let docSearchDebounce: number | null = null;
const initialDocumentFilters = documentListFiltersFromQuery(route.query);
const docSearchKeyword = ref(initialDocumentFilters.keyword);
const selectedFileType = ref(initialDocumentFilters.fileType);
const selectedContentType = ref(initialDocumentFilters.contentType);
const selectedStarred = ref(initialDocumentFilters.starred);
const selectedEntityIds = ref(initialDocumentFilters.entityIds);
const selectedTopicIds = ref(initialDocumentFilters.topicIds);
const publishedRange = ref<string[]>(initialDocumentFilters.publishedFrom || initialDocumentFilters.publishedTo ? [initialDocumentFilters.publishedFrom, initialDocumentFilters.publishedTo] : []);
const entityChoices = ref<Entity[]>([]), topicChoices = ref<Topic[]>([]), choicesError = ref(''), choicesLoading = ref(false), moreEntityChoices = ref(true);
let choicesGeneration = 0, choicesDisposed = false, choicesOffset = 0;
async function loadFilterChoices(more = false) {
  const current = ++choicesGeneration; choicesLoading.value = true; choicesError.value = '';
  const libraries = libraryScope.value.length ? libraryScope.value : libraryCatalog.value.map(lib => lib.id);
  if (!more) choicesOffset = 0;
  if (!libraries.length) { entityChoices.value = []; topicChoices.value = []; moreEntityChoices.value = false; choicesLoading.value = false; return; }
  try {
    const choices = await Promise.all(libraries.map(async library_id => {
      const [entities, topics] = await Promise.all([listEntities({ library_id, limit: 50, offset: choicesOffset }), listTopics(library_id)]);
      if (!entities.success || !topics.success) throw new Error(t('entryBatch.choicesFailed'));
      return { entities: entities.data, topics: topics.data };
    }));
    if (choicesDisposed || current !== choicesGeneration) return;
    entityChoices.value = more ? [...entityChoices.value, ...choices.flatMap(choice => choice.entities)] : choices.flatMap(choice => choice.entities);
    moreEntityChoices.value = choices.some(choice => choice.entities.length === 50);
    topicChoices.value = choices.flatMap(choice => choice.topics);
    choicesOffset += 50;
  } catch { if (!choicesDisposed && current === choicesGeneration) choicesError.value = t('entryBatch.choicesFailed'); }
  finally { if (!choicesDisposed && current === choicesGeneration) choicesLoading.value = false; }
}
watch([libraryScope, libraryCatalog], () => { entityChoices.value = []; topicChoices.value = []; void loadFilterChoices(); }, { deep: true });
onUnmounted(() => { choicesDisposed = true; choicesGeneration++; });
const entityFilterValues = computed({ get: () => selectedEntityIds.value.split(',').filter(Boolean), set: (ids: string[]) => { selectedEntityIds.value = ids.join(','); } });
const topicFilterValues = computed({ get: () => selectedTopicIds.value.split(',').filter(Boolean), set: (ids: string[]) => { selectedTopicIds.value = ids.join(','); } });
const entityFilterOptions = computed(() => [...new Map([...entityFilterValues.value.map(id => ({ value: id, label: id })), ...entityChoices.value.map(item => ({ value: item.id, label: `${item.canonical_name}${item.library_id ? ` · ${libraryCatalog.value.find(lib => lib.id === item.library_id)?.display_name || item.library_id}` : ''}` }))].map(item => [item.value, item])).values()]);
const topicFilterOptions = computed(() => [...new Map([...topicFilterValues.value.map(id => ({ value: id, label: id })), ...topicChoices.value.map(item => ({ value: item.id, label: `${item.name}${item.library_id ? ` · ${libraryCatalog.value.find(lib => lib.id === item.library_id)?.display_name || item.library_id}` : ''}` }))].map(item => [item.value, item])).values()]);
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
  return libraryInfo.value?.content_types?.filter((row: LibraryContentType) => row.enabled).map((row: LibraryContentType) => ({ value: row.name, label: row.name })) || [];
});
const selectedEntityId = ref('');
watch(() => route.query.entity_id, id => { if (typeof id === 'string') selectedEntityId.value = id; }, { immediate: true });
const openArticleEntity = (id: string) => { void router.push({ path: '/platform/entities', query: { entity_id: id } }); };
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
  ...SEARCH_SOURCES.map(value => ({ value, label: t(`search.sources.${value}`) })),
]);
// Submission channels are independent of file format and material category.
const sourceOptions = computed(() => [
  { label: t('library.allSources'), value: '' },
  { label: t('library.channelApi'), value: 'api' },
  { label: t('library.channelWeb'), value: 'web' },
  { label: t('library.channelRSS'), value: 'rss' },
  { label: t('library.channelBrowserExtension'), value: 'browser_extension' },
]);
// Keep date ranges in the existing URL-compatible YYYY-MM-DD format.
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
  entityIds: selectedEntityIds.value, topicIds: selectedTopicIds.value, publishedFrom: publishedRange.value?.[0] || '', publishedTo: publishedRange.value?.[1] || '',
}));
const hasAdvancedFilters = computed(() => !!(selectedStarred.value || selectedSourceKind.value || selectedSource.value || selectedFileType.value || selectedEntityIds.value || selectedTopicIds.value || publishedRange.value.length || updatedTimeRange.value.length));

const filterParams = computed(() => {
  const [start, end] = updatedTimeRange.value || [];
  return {
    libraries: libraryScope.value,
    entity_ids: entityFilterValues.value, topic_ids: topicFilterValues.value,
    published_from: publishedRange.value?.[0] || undefined, published_to: publishedRange.value?.[1] || undefined,
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

  const request = ++loadingGeneration;
  docListLoading.value = true;

  return loadEntries(
    {
      page: 1,
      page_size: pageSize,
      ...filterParams.value,
    },
    libraryIdValue,
  ).finally(() => {
    if (request === loadingGeneration && isCurrentLibrary(libraryIdValue)) {
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
  libraryError.value = '';
  try {
    const data = await resources.fetchLibrary(force);
    if (!isCurrentLibrary(targetLibraryId)) return;

    libraryInfo.value = data;

      loadEntryFiles(targetLibraryId);

  } catch (error) {
    if (!isCurrentLibrary(targetLibraryId)) return;

    libraryError.value = t('entries.loadFailed');
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
watch(() => [route.query.tab, route.query.section], ([tab]) => {
  if (tab === 'reviews' || (tab === 'entities' && route.query.section === 'merges')) {
    const { tab: _tab, section: _section, ...query } = route.query
    void router.replace({ path: '/platform/pending', query: { ...query, tab: tab === 'reviews' ? 'reviews' : 'merges' }, hash: route.hash })
    return
  }
  if (tab === 'entities') {
    const { tab: _tab, ...query } = route.query
    void router.replace({ path: '/platform/entities', query, hash: route.hash })
    return
  }
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
    cardList.value = [];
    total.value = 0;
    docListLoading.value = true;
    resetPage();
  }
  loadLibraryInfo(newLibraryId);
}, { immediate: true });

watch(libraryScope, ids => {
  const value = ids.join(',');
  if (value !== String(route.query.libraries || '')) {
    const query = { ...route.query };
    if (value) query.libraries = value;
    else delete query.libraries;
    selectedEntityIds.value = ''; selectedTopicIds.value = '';
    delete query.entity_ids; delete query.topic_ids;
    void router.replace({ query });
  }
  selectedIds.value.clear();
  resetPage(); void loadEntryFiles(libraryId.value);
}, { deep: true });

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
    selectedEntityIds.value = restored.entityIds; selectedTopicIds.value = restored.topicIds;
    publishedRange.value = restored.publishedFrom || restored.publishedTo ? [restored.publishedFrom, restored.publishedTo] : [];
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
  if (libraryScope.value.length) query.libraries = libraryScope.value.join(',');
  else delete query.libraries;
  if (!documentListFilterQueriesEqual(route.query, query)) void router.replace({ query });
  if (docSearchDebounce !== null) {
    window.clearTimeout(docSearchDebounce);
    docSearchDebounce = null;
  }
  resetPage();
  if (!libraryId.value) return;
  if (composingSearch.value) return;
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

// Preserve old material links when query parameters change on the list route.
const pendingEntryId = ref<string | null>(
  (route.query.entry_id as string) || null
);

const tryAutoOpenDocument = async () => {
  if (!pendingEntryId.value) return;
  const id = pendingEntryId.value;
  pendingEntryId.value = null;
  await router.replace(entryDetailLocation(id, parseReference(route.query.reference)));
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

// Compatibility for extensions still dispatching the old material-open event.
const handleOpenEntryEvent = async (e: Event) => {
  const detail = (e as CustomEvent<{ entryId: string; location?: SourceLocation }>).detail;
  if (detail?.entryId) await router.push(entryDetailLocation(detail.entryId, detail.location));
};

onMounted(() => {
  void resources.fetchLibrary().catch(() => { libraryError.value = t('entries.loadFailed'); }).finally(() => {
    if (!libraryId.value) docListLoading.value = false;
  });

  window.addEventListener('reliquary:open-entry', handleOpenEntryEvent as EventListener);
});

onUnmounted(() => {
  disposed = true;
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

  pollGeneration++;
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
  parse_status?: string;
  summary_status?: string;
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
  if (disposed) return;
  const request = ++pollGeneration;
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
      if (disposed || request !== pollGeneration) return;
      let hasChanges = false;
      if (result.success && result.data) {
        (result.data as EntryCard[]).forEach((item: EntryCard) => {
          const index = cardList.value.findIndex(card => card.id == item.id);
          if (index == -1) return;

          let parseStatus = item.parse_status;


          if (cardList.value[index].parse_status !== parseStatus ||
            cardList.value[index].description !== item.description ||
            cardList.value[index].summary_status !== item.summary_status ||
            cardList.value[index].error_message !== item.error_message) {

            // Always update the card data
            cardList.value[index].parse_status = parseStatus;
            Object.assign(cardList.value[index], item, { parse_status: parseStatus });
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
      if (disposed || request !== pollGeneration) return;
      // 错误处理
      const stillPending = cardList.value.filter(needsStatusPolling);
      if (stillPending.length > 0) {
        updateStatus(stillPending);
      }
    });
  }, 1500);
};

const openCardDetails = (item: EntryCard) => {
  void router.push(entryDetailLocation(item.id));
};

const openSourceDoc = (entryId: string, location?: SourceLocation) => {
  void router.push(entryDetailLocation(entryId, location));
};

const confirmDeleteEntry = async (index: number, item: EntryCard) => {
  if (item.source_kind === 'mine') {
    const result = await deleteDocument(item.id);
    if (!result.success) throw new Error(result.message || t('personalDocument.deleteFailed'));
    toast.success(t('personalDocument.deleted'));
    await handlePersonalDocumentDeleted(item.id);
    return true;
  }
  const deletedId = item?.id;
  return delEntry(index, item, async () => {
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

const reparseConfirmStore = useReparseConfirmStore();

const batchResultId = ref('');
const batchDialogVisible = ref(false);
const batchSelection = ref<EntryBatchSelection>();
const batchSelectedIds = ref<string[]>([]);
const batchLibraryIds = computed(() => [...new Set(cardList.value.filter((item: EntryCard) => batchSelectedIds.value.includes(item.id)).map((item: EntryCard) => item.library_id))].filter(Boolean) as string[]);
function batchCreated(id: string) { batchResultId.value = id; void router.replace({ query: { ...route.query, batch_id: id } }); }
watch(() => route.query.batch_id, id => { if (typeof id === 'string' && id && id !== batchResultId.value) { batchResultId.value = id; batchDialogVisible.value = true; } }, { immediate: true });
function openBatch(action: import('@/api/entry-batches').EntryBatchAction = 'star') {
  batchInitialAction.value = action;
  batchResultId.value = '';
  batchSelectedIds.value = [...selectedIds.value]; batchSelection.value = { filters: JSON.parse(JSON.stringify(filterParams.value)) };
  batchDialogVisible.value = true;
}
async function batchChanged() { clearSelection(); resetPage(); await Promise.all([loadEntryFiles(libraryId.value), pendingCounts.refresh()]); }

const handleViewTrace = (_index: number, item: EntryCard) => {
  void router.push({ ...entryDetailLocation(item.id), query: { tab: 'processing' } });
};

const confirmRebuildEntry = async (index: number, item: EntryCard) => {
  if (!item?.id) {
    toast.warning(t('libraryEditor.messages.missingId'));
    return;
  }
  if (entryNeedsStatusPolling(item)) {
    toast.info(t('library.rebuildInProgress'));
    return;
  }

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
    toast.success(t('library.rebuildSubmitted'));
    resetPage();
    loadEntryFiles(libraryId.value);
  } catch (error: any) {
    toast.error(error?.message || t('library.rebuildFailed'));
  }
};

async function loadMoreEntries() {
  if (docListLoading.value || scrollLoading.value || cardList.value.length >= total.value || !libraryId.value) return;
  const currentLibraryId = libraryId.value;
  const request = listGeneration;
  const nextPage = page + 1;
  scrollLoading.value = true;
  try { await loadEntries({ page: nextPage, page_size: pageSize, ...filterParams.value }, currentLibraryId); if (request === listGeneration && !listError.value && isCurrentLibrary(currentLibraryId)) page = nextPage; }
  finally { if (request === listGeneration) scrollLoading.value = false; }
}
const handleScroll = () => {
  const element = entryScroll.value;
  if (element && element.scrollTop + element.clientHeight >= element.scrollHeight - 10) void loadMoreEntries();
};
const clearSelection = () => { selectedIds.value.clear(); };
const {
  onContainerMouseDown: onDocMarqueeMouseDown,
  marqueeVisible: docMarqueeVisible,
  marqueeMode: docMarqueeMode,
  boxStyle: docMarqueeBoxStyle,
  shouldSuppressClick: shouldSuppressDocClick,
} = useMarqueeSelect({
  containerRef: entryScroll,
  itemSelector: '.rq-table__row[data-select-id]',
  selectedIds,
  getItemId: (el) => el.dataset.selectId || null,
  enabled: computed(() => cardList.value.length > 0 && viewMode.value === 'table'),

});

const openEntryItem = (item: EntryCard) => {
  if (shouldSuppressDocClick()) return;
  openCardDetails(item);
};

const handlePersonalDocumentDeleted = async (id: string) => {
  selectedIds.value.delete(id);
  resetPage();
  await Promise.all([loadEntryFiles(libraryId.value), pendingCounts.refresh()]);
};

const confirmCancelParseEntry = async (item: EntryCard) => {
  if (!item?.id) return;
  try {
    await cancelEntryParse(item.id);
    toast.success(t('library.cancelParseSubmitted'));
    loadEntryFiles(libraryId.value);
  } catch (error: any) {
    toast.error(error?.message || t('library.cancelParseFailed'));
    throw error;
  }
};

const handleListAction = (action: 'reparse' | 'cancel-parse' | 'delete' | 'view-trace', item: EntryCard) => {
  const index = cardList.value.findIndex((entry: EntryCard) => entry.id === item.id);
  if (action === 'reparse') {
    const confirmation = confirm({ header: t('library.reprocess'), body: t('library.rebuildConfirm', { fileName: item.file_name || item.title || item.id }) });
    void confirmation.result.then(accepted => { if (accepted && !disposed) void confirmRebuildEntry(index, item); });
    return confirmation;
  }
  if (action === 'cancel-parse') return confirm({ header: t('library.cancelParse'), body: t('library.cancelParseConfirmBody', { title: item.file_name || item.title || item.id }), theme: 'danger', onConfirm: () => confirmCancelParseEntry(item) });
  if (action === 'delete') return confirm({ header: t('library.deleteDocument'), body: t('library.confirmDeleteDocument', { fileName: item.file_name || item.title || item.id }), theme: 'danger', onConfirm: async () => { if (!await confirmDeleteEntry(index, item)) throw new Error(t('library.deleteFailed')); } });
  return handleViewTrace(index, item);
};

// Clear selection on filter/library change to avoid acting on hidden items.
watch(
  [docSearchKeyword, selectedFileType, selectedContentType, selectedStarred, selectedParseStatus, selectedSource, selectedSourceKind, updatedTimeRange, selectedEntityIds, selectedTopicIds, publishedRange, libraryId],
  () => {
    clearSelection();
  },
);

// Drop checked IDs that are no longer in the loaded result.
watch(cardList, items => {
  const visible = new Set(items.map((item: EntryCard) => item.id));
  for (const id of selectedIds.value) if (!visible.has(id)) selectedIds.value.delete(id);
});

// 处理库编辑成功后的回调
onMounted(() => window.addEventListener('reliquary:library-settings-saved', refreshLibrarySettings));
onUnmounted(() => window.removeEventListener('reliquary:library-settings-saved', refreshLibrarySettings));
const retryEntries = async () => {
  libraryError.value = '';
  try { await resources.fetchLibrary(true); await loadLibraryInfo(libraryId.value, true); }
  catch { libraryError.value = t('entries.loadFailed'); }
};
const refreshLibrarySettings = () => { if (libraryId.value) void loadLibraryInfo(libraryId.value, true); };
</script>

<template>
  <div class="entry-layout" :class="{ 'entries-page': activeLibraryTab === 'documents' }">
    <div v-if="activeLibraryTab === 'entities'" class="entity-workbench-area"><EntryWorkbench :library-id="libraryScope[0] || libraryId" :initial-entity-id="selectedEntityId" @open-source="openSourceDoc" /></div>
    <template v-if="activeLibraryTab === 'documents'">
      <header class="entries-head">
        <div class="entries-eyebrow"><LibMark v-if="currentLibrary" :color="currentLibrary.color" /><span>{{ currentLibrary ? t('entries.libraryNumber', { number: libraryNumber }) : t('menu.materials') }}</span><router-link to="/platform/libraries">{{ t('menu.library') }}</router-link></div>
        <div class="entries-heading"><h1>{{ currentLibraryLabel }}</h1><p v-if="!docListLoading && !libraryError && !listError">{{ t('entries.count', { count: total }) }}</p></div>
      </header>
      <div class="entries-toolbar">
        <div class="entries-chips" :aria-label="t('library.contentTypeFilter')"><Chip :model-value="!selectedContentType" @click="selectedContentType = ''">{{ t('entries.all') }}</Chip><Chip v-for="option in contentTypeOptions" :key="option.value" :model-value="selectedContentType === option.value" @click="selectedContentType = option.value">{{ option.label }}</Chip><Chip v-if="selectedContentType && !contentTypeOptions.some((option: any) => option.value === selectedContentType)" :model-value="true" @click="selectedContentType = ''">{{ selectedContentType }}</Chip></div>
        <Select v-model="selectedParseStatus" :aria-label="t('library.parseStatusFilter')"><option v-for="option in parseStatusOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        <Select model-value="newest" :aria-label="t('entries.sort')"><option value="newest">{{ t('entries.newest') }}</option></Select>
        <div class="entries-views" role="group" :aria-label="t('library.viewModeToggle')"><Button :aria-pressed="viewMode === 'list'" @click="viewMode = 'list'">{{ t('entries.list') }}</Button><Button :aria-pressed="viewMode === 'table'" @click="viewMode = 'table'">{{ t('entries.table') }}</Button></div>
      </div>
      <div class="entries-search-row"><div class="entries-search"><Input ref="searchInput" v-model="docSearchKeyword" class="doc-search-input" :aria-label="t('library.docSearchPlaceholder')" :placeholder="t('library.docSearchPlaceholder')" @compositionstart="composingSearch = true" @compositionend="composingSearch = false" @keydown.enter="submitSearch" /><Button v-if="docSearchKeyword" :aria-label="t('entries.clearSearch')" @click="clearSearch">×</Button></div><Button @click="openBatch()">{{ t('entryBatch.title') }}</Button></div>
      <details class="entries-filters" :open="hasAdvancedFilters"><summary>{{ t('ikb18.moreFilters') }}</summary><div class="entries-filters__grid">
        <LibraryScopeSelect v-model="libraryScope" @loaded="libraryCatalog = $event" />
        <Select v-model="selectedStarred" :aria-label="t('articleJudgments.starLabel')"><option v-for="option in starredOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        <Select v-model="selectedSourceKind" :aria-label="t('library.materialCategory')"><option v-for="option in sourceKindOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        <Select v-model="selectedSource" :aria-label="t('library.channelLabel')"><option v-for="option in sourceOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        <Select v-model="selectedFileType" :aria-label="t('library.fileTypeFilter')"><option v-for="option in fileTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        <label>{{ t('entryBatch.entities') }}<select class="rq-control" multiple :value="entityFilterValues" :aria-label="t('entryBatch.entities')" @change="entityFilterValues = updateSelectedOptions($event)"><option v-for="option in entityFilterOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select><Button v-if="moreEntityChoices" :loading="choicesLoading" @click="loadFilterChoices(true)">{{ t('workbench.loadMore') }}</Button></label>
        <label>{{ t('entryBatch.topics') }}<select class="rq-control" multiple :value="topicFilterValues" :aria-label="t('entryBatch.topics')" @change="topicFilterValues = updateSelectedOptions($event)"><option v-for="option in topicFilterOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
        <fieldset><legend>{{ t('entryBatch.publishedDates') }}</legend><label>{{ t('entryBatch.publishedFrom') }}<input type="date" class="rq-control" :aria-label="t('entryBatch.publishedFrom')" :value="publishedRange[0] || ''" @change="updateDate('published', 0, $event)" /></label><label>{{ t('entryBatch.publishedTo') }}<input type="date" class="rq-control" :aria-label="t('entryBatch.publishedTo')" :value="publishedRange[1] || ''" @change="updateDate('published', 1, $event)" /></label><small v-if="publishedRange.length">{{ t('entryBatch.publishedHint') }}</small></fieldset>
        <fieldset><legend>{{ t('entryBatch.updatedDates') }}</legend><label>{{ t('entryBatch.updatedFrom') }}<input type="date" class="rq-control" :aria-label="t('entryBatch.updatedFrom')" :max="updatedTimeRange[1] || latestFilterDate" :value="updatedTimeRange[0] || ''" @change="updateDate('updated', 0, $event)" /></label><label>{{ t('entryBatch.updatedTo') }}<input type="date" class="rq-control" :aria-label="t('entryBatch.updatedTo')" :min="updatedTimeRange[0] || undefined" :max="latestFilterDate" :value="updatedTimeRange[1] || ''" @change="updateDate('updated', 1, $event)" /></label></fieldset>
        <ErrorState v-if="choicesError" :title="choicesError"><Button @click="loadFilterChoices()">{{ t('common.retry') }}</Button></ErrorState>
      </div></details>
      <div ref="entryScroll" class="doc-scroll-container" :aria-busy="docListLoading" @scroll="handleScroll" @mousedown="onDocMarqueeMouseDown">
        <div v-if="docMarqueeVisible" class="doc-marquee-box" :style="docMarqueeBoxStyle" aria-hidden="true" />
        <div v-if="docListLoading && !cardList.length" class="entries-loading"><Skeleton v-for="n in 6" :key="n" :label="t('common.loading')" /></div>
        <ErrorState v-else-if="libraryError || listError" :title="t('entries.loadFailed')" :description="libraryError || listError"><Button @click="retryEntries">{{ t('common.retry') }}</Button></ErrorState>
        <div v-else-if="cardList.length" :class="{ 'entries-split': viewMode === 'list' }">
          <DocumentListView :items="cardList" :mode="viewMode" :focused="focusedId" :selected-ids="selectedIds" @update:focused="focusedId = $event" @update:selected="selectedIds = new Set($event)" @open="openEntryItem" />
          <EntryPreview v-if="viewMode === 'list' && focusedEntry" :entry="focusedEntry" :libraries="libraryCatalog" @open="openEntryItem(focusedEntry)" @open-entity="openArticleEntity" @action="handleListAction" />
        </div>
        <EmptyState v-else :title="t('library.emptyArticles')" :description="t('entries.emptyHint')" />
        <div v-if="cardList.length < total && !listError" class="entries-more"><Button :loading="docListLoading || scrollLoading" @click="loadMoreEntries">{{ t('workbench.loadMore') }}</Button></div>
      </div>
      <DocumentBatchBar v-if="selectedIds.size" :count="selectedIds.size" @cancel="clearSelection" @manage="openBatch()" @action="openBatch" />
    </template>
      <!-- Batch management -->
      <EntryBatchDialog v-model:visible="batchDialogVisible" :batch-id="batchResultId" :initial-action="batchInitialAction" @created="batchCreated" :ids="batchSelectedIds" :selection="batchSelection" :libraries="libraryCatalog" :selected-library-ids="batchLibraryIds" :entity-options="entityFilterOptions" :topic-options="topicFilterOptions" @changed="batchChanged" />

    </div>

  <!-- 库编辑器（创建/编辑统一组件） -->

</template>
<style scoped>
.entry-layout { display: flex; flex-direction: column; min-height: 0; min-width: 0; height: 100%; flex: 1; padding: 24px 32px; }
.entries-page { overflow-y: auto; padding: 0; color: var(--text); background: var(--bg); font-family: var(--font); }
.entries-page > :not(.doc-scroll-container) { flex-shrink: 0; }
.entity-workbench-area { min-height: 0; overflow: auto; flex: 1; }
.entries-head { padding: 24px 24px 16px; }
.entries-eyebrow, .entries-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
.entries-eyebrow { justify-content: flex-start; font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); }
.entries-eyebrow a { margin-left: auto; color: inherit; text-decoration: underline; }
.entries-heading h1 { margin: 8px 0 0; font-size: var(--type-page-size); font-weight: var(--type-page-weight); line-height: 1.02; letter-spacing: -0.04em; overflow-wrap: anywhere; }
.entries-heading p { font-family: var(--mono); }
.entries-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 24px; border-top: 2px solid var(--rule); border-bottom: 1px solid var(--rule); }
.entries-toolbar > .rq-select { width: auto; max-width: 100%; }
.entries-chips, .entries-views, .entries-search-row, .entries-search { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.entries-views { margin-left: auto; }
.entries-views [aria-pressed="true"] { background: var(--text); color: var(--bg); }
.entries-search-row { padding: 12px 24px; }
.entries-search { flex: 1; flex-wrap: nowrap; min-width: 160px; }
.entries-search .rq-input, .entries-search input { min-width: 0; flex: 1; }
.entries-filters { padding: 0 24px 12px; }
.entries-filters summary { cursor: pointer; font-size: var(--text-sm); }
.entries-filters__grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px; padding: 12px 0; }
.entries-filters label { display: grid; gap: 4px; font-size: var(--text-sm); }
.entries-filters fieldset { min-width: 0; border: 1px solid var(--border); }
.doc-scroll-container { overflow: auto; min-height: 240px; flex: 1 0 240px; position: relative; scrollbar-gutter: stable; }
.entries-split { display: grid; grid-template-columns: minmax(288px, 384px) minmax(0, 1fr); min-height: 100%; align-items: start; }
.entries-loading { display: grid; gap: 16px; padding: 24px; }
.entries-loading :deep(.rq-skeleton) { min-height: 80px; }
.entries-more { padding: 16px 24px; }
.doc-marquee-box { position: absolute; pointer-events: none; z-index: 2; border: 1px solid var(--text); background: var(--surface-soft); opacity: 0.4; }
@media (max-width: 900px) { .entries-split { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 640px) { .entries-head, .entries-toolbar, .entries-search-row { padding-left: 16px; padding-right: 16px; } .entries-filters { padding-inline: 16px; } }
</style>
