<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, reactive, computed, nextTick } from "vue";
import { MessagePlugin } from "tdesign-vue-next";
import DocContent from "@/components/doc-content.vue";
import KnowledgeDuplicateNotice from '@/components/KnowledgeDuplicateNotice.vue';
import useKnowledgeBase from '@/hooks/useKnowledgeBase';
import { useRoute, useRouter } from 'vue-router';
import EmptyKnowledge from '@/components/empty-knowledge.vue';
import KBInfoPopover from '@/components/KBInfoPopover.vue';
import KBSwitcherDropdown from '@/components/KBSwitcherDropdown.vue';
import { useUIStore } from '@/stores/ui';
import { useCommandPaletteStore } from '@/stores/commandPalette';
import { useResourcesStore } from '@/stores/resources';
import { useEditorResourcesStore } from '@/stores/editorResources';
import KnowledgeBaseEditorModal from './KnowledgeBaseEditorModal.vue';
const uiStore = useUIStore();
const commandPalette = useCommandPaletteStore();
const resources = useResourcesStore();
const editorResources = useEditorResourcesStore();
const router = useRouter();
import {
  batchQueryKnowledge,
  uploadKnowledgeFile,
  createKnowledgeFromURL,
  reparseKnowledge,
  cancelKnowledgeParse,
  batchDeleteKnowledge,
  batchReparseKnowledge,
  getKnowledgeSpans,
  getKnowledgeDetails,
  listKnowledgeFolders,
  moveKnowledgeToFolder,
  renameKnowledgeFolder,
  downKnowledgeDetails,
  reviewKnowledge,
  type KnowledgeFolderTree,
} from "@/api/knowledge-base/index";
import { knowledgeSpansPayloadHasTrace } from '@/utils/knowledgeTrace';
import DocumentListView from './components/DocumentListView.vue';
import DocumentCardView from './components/DocumentCardView.vue';
import DocumentBatchBar from './components/DocumentBatchBar.vue';
import KbUploadSourceDropdown from './components/KbUploadSourceDropdown.vue';
import KbFolderTree from './components/KbFolderTree.vue';
import KnowledgeWorkbench from './components/KnowledgeWorkbench.vue';
import type { KnowledgeProcessOverrides } from '@/types/knowledgeProcess';
import { useUploadConfirmStore, type UploadConfirmResult, type IntakeSourceInfo } from '@/stores/uploadConfirm';
import {
  isKnowledgeParseInFlight,
  knowledgeNeedsStatusPolling,
} from './knowledgeStatusPolling';
import { listMoveTargets, moveKnowledge, getKnowledgeMoveProgress } from '@/api/knowledge-base';
import { resolveKnowledgeDownloadFileName } from './knowledgeDownloadFileName';
import {
  buildUploadFileName,
  canMoveFolderTo,
  childFolders,
  folderBreadcrumbs as buildFolderBreadcrumbs,
  folderPathExists as folderExistsInTree,
  isFilteringDocuments,
  isFolderUpload,
  ROOT_FOLDER_PATH,
} from './folderTree';
import { useI18n } from 'vue-i18n';
import { useMarqueeSelect } from '@/hooks/useMarqueeSelect';
import type { ParserEngineInfo } from '@/api/system';
import { getUploadFileKey, filterUnsupportedAudioFiles } from './utils/uploadSources';
const route = useRoute();
const { t } = useI18n();
const kbId = computed(() => (route.params as any).kbId as string || '');
const kbInfo = ref<any>(null);
const uploadSourceRef = ref<InstanceType<typeof KbUploadSourceDropdown> | null>(null);
const uploading = ref(false);
const kbLoading = ref(false);
const docListLoading = ref(true);
const validTabs = ['documents', 'entities'] as const
type KbTab = typeof validTabs[number]
const initTab = validTabs.includes(route.query.tab as any) ? (route.query.tab as KbTab) : 'documents'
const activeKbTab = ref<KbTab>(initTab);

const parserEngines = computed<ParserEngineInfo[]>(() => editorResources.parserEngines);

const supportedFileTypes = computed<Set<string>>(() => {
  const engines = parserEngines.value
  if (!engines.length) return new Set<string>()

  const rules: { file_types: string[]; engine: string }[] =
    kbInfo.value?.chunking_config?.parser_engine_rules || []

  const ruleMap = new Map<string, string>()
  for (const r of rules) {
    for (const ft of r.file_types) ruleMap.set(ft, r.engine)
  }

  const available = new Set<string>()
  const availableEngineNames = new Set(
    engines.filter(e => e.Available !== false).map(e => e.Name)
  )

  for (const engine of engines) {
    for (const ft of engine.FileTypes || []) {
      if (available.has(ft)) continue

      const explicitEngine = ruleMap.get(ft)
      if (explicitEngine) {
        if (availableEngineNames.has(explicitEngine)) available.add(ft)
      } else {
        if (engine.Available !== false) available.add(ft)
      }
    }
  }
  return available
})

const acceptFileTypes = computed(() =>
  [...supportedFileTypes.value].map(t => '.' + t).join(',')
)

const unsupportedFileTypes = computed<string[]>(() => {
  const engines = parserEngines.value
  if (!engines.length) return []

  const allTypes = new Set<string>()
  for (const engine of engines) {
    for (const ft of engine.FileTypes || []) allTypes.add(ft)
  }

  const supported = supportedFileTypes.value
  return [...allTypes].filter(ft => !supported.has(ft)).sort()
})

const goToParserSettings = () => {
  if (kbId.value) {
    uiStore.openKBSettings(kbId.value, 'parser')
  }
}

const knowledgeList = ref<Array<{ id: string; name: string; type?: string }>>([]);
let { cardList, total, moreIndex, details, getKnowled, delKnowledge, openMore, onVisibleChange: _onVisibleChange, getCardDetails, getfDetails,
  duplicateNotices, showDuplicate, dismissDuplicate, clearDuplicateNotices, openDuplicate } = useKnowledgeBase(kbId.value)

const onVisibleChange = (visible: boolean) => {
  _onVisibleChange(visible);
  if (!visible) {
    moveMenuMode.value = 'normal';
  }
};

/** Per-knowledge cache: whether /spans has a real trace (see knowledgeSpansPayloadHasTrace). */
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
  return isKnowledgeParseInFlight(status);
}

function isTraceMenuVisible(item: KnowledgeCard): boolean {
  if (!item?.id) return false;
  if (isParseInFlight(item.parse_status)) {
    return true;
  }
  return traceAvailableById[item.id] === true;
}

async function probeTraceAvailable(item: KnowledgeCard) {
  const id = item.id;
  if (!id || traceProbeInflight.has(id)) return;
  if (isParseInFlight(item.parse_status)) {
    traceAvailableById[id] = true;
    return;
  }
  if (Object.prototype.hasOwnProperty.call(traceAvailableById, id)) return;
  traceProbeInflight.add(id);
  try {
    const res: any = await getKnowledgeSpans(id);
    traceAvailableById[id] = !!(res?.success && knowledgeSpansPayloadHasTrace(res.data));
  } catch {
    traceAvailableById[id] = false;
  } finally {
    traceProbeInflight.delete(id);
  }
}

const onCardMoreVisibleChange = (visible: boolean, item: KnowledgeCard) => {
  onVisibleChange(visible);
  if (visible) {
    probeTraceAvailable(item);
  }
};
let isCardDetails = ref(false);
let timeout: ReturnType<typeof setTimeout> | null = null;
let knowledgeScroll = ref()
let page = 1;
let pageSize = 35;
let scrollLoading = false;
const resetPage = () => { page = 1; scrollLoading = false; };

// Move state — inline in card menu
const moveMenuMode = ref<'normal' | 'targets' | 'confirm'>('normal');
const moveKnowledgeId = ref('');
const moveTargetKbs = ref<any[]>([]);
const moveTargetsLoading = ref(false);
const moveSelectedTargetId = ref('');
const moveSelectedTargetName = ref('');
const moveMode = ref<'reuse_vectors' | 'reparse'>('reuse_vectors');
const moveSubmitting = ref(false);
let movePollTimer: ReturnType<typeof setInterval> | null = null;

// View mode (grid / list) — persisted per browser
type DocViewMode = 'grid' | 'list';
const VIEW_MODE_KEY = 'reliquary.kb.docs.viewMode';
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
    card.summary_status = undefined;
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
    await loadKnowledgeFiles(kbId.value);
    syncReparseAckFromServer(ids);
    applyOptimisticBatchReparse(Array.from(pendingReparseAck.value));
    await new Promise<void>((r) => setTimeout(r, delayMs));
  }
  pendingReparseAck.value.clear();
};

const confirmBatchReparse = async () => {
  if (batchReparsing.value || batchDeleting.value || selectedIds.value.size === 0) return;
  const allIds = Array.from(selectedIds.value);
  const ids = allIds.filter((id) => {
    const item = cardList.value.find((c) => c.id === id);
    return !item || !isParseInFlight(item.parse_status);
  });
  const skipped = allIds.length - ids.length;
  if (ids.length === 0) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
    return;
  }
  if (skipped > 0) {
    MessagePlugin.warning(t('knowledgeBase.batchReparseSkippedInFlight', { count: skipped }));
  }
  batchReparsing.value = true;
  try {
    const res: any = await batchReparseKnowledge(kbId.value, ids);
    if (res?.success) {
      MessagePlugin.success(t('knowledgeBase.batchReparseSuccess', { count: ids.length }));
      applyOptimisticBatchReparse(ids);
      clearSelection();
      batchMode.value = false;
      void awaitBatchReparseReflection(ids);
    } else {
      MessagePlugin.error(res?.message || t('knowledgeBase.batchReparseFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeBase.batchReparseFailed'));
  } finally {
    batchReparsing.value = false;
  }
};
let docSearchDebounce: number | null = null;
const docSearchKeyword = ref('');
const selectedFileType = ref('');
const fileTypeOptions = computed(() => [
  { label: t('knowledgeBase.allFileTypes'), value: '' },
  { label: 'PDF', value: 'pdf' },
  { label: 'DOCX', value: 'docx' },
  { label: 'DOC', value: 'doc' },
  { label: 'PPTX', value: 'pptx' },
  { label: 'PPT', value: 'ppt' },
  { label: 'EPUB', value: 'epub' },
  { label: 'TXT', value: 'txt' },
  { label: 'MD', value: 'md' },
  { label: 'URL', value: 'url' },
  { label: t('knowledgeBase.typeManual'), value: 'manual' },
  { label: 'MP3', value: 'mp3' },
  { label: 'WAV', value: 'wav' },
  { label: 'M4A', value: 'm4a' },
  { label: 'FLAC', value: 'flac' },
  { label: 'OGG', value: 'ogg' },
]);
const selectedParseStatus = ref('');
const parseStatusOptions = computed(() => [
  { label: t('knowledgeBase.allParseStatuses'), value: '' },
  { label: t('knowledgeBase.parseStatusPending'), value: 'pending' },
  { label: t('knowledgeBase.parseStatusProcessing'), value: 'processing' },
  { label: t('knowledgeBase.parseStatusCompleted'), value: 'completed' },
  { label: t('knowledgeBase.parseStatusFailed'), value: 'failed' },
  { label: t('knowledgeBase.parseStatusCancelled'), value: 'cancelled' },
  { label: t('knowledgeBase.parseStatusFinalizing'), value: 'finalizing' },
  { label: t('knowledgeBase.parseStatusDraft'), value: 'draft' },
  { label: t('knowledgeBase.intakeWaiting'), value: 'rejected' },
]);
const selectedSource = ref('');
// Source filter combines ingestion channels and the "manual"/"url" virtual
// sources that the backend routes onto the `type` column.
const sourceOptions = computed(() => [
  { label: t('knowledgeBase.allSources'), value: '' },
  { label: t('knowledgeBase.sourceUpload'), value: 'web' },
  { label: t('knowledgeBase.sourceUrl'), value: 'url' },
  { label: t('knowledgeBase.sourceManual'), value: 'manual' },
  { label: t('datasource.connector.rss'), value: 'rss' },
  { label: t('knowledgeBase.sourceApi'), value: 'api' },
  { label: t('knowledgeBase.sourceBrowserExtension'), value: 'browser_extension' },
]);
// Date range as [start, end] in "YYYY-MM-DD" form (t-date-range-picker default).
const updatedTimeRange = ref<string[]>([]);
// Disable any date after today so users cannot filter into the future.
const disableFutureDate = { after: new Date(new Date().setHours(23, 59, 59, 999)) };

// ── Folder tree (documents uploaded as a folder keep their relative path) ──
const FOLDER_TREE_COLLAPSED_KEY = 'reliquary.kbFolderTreeCollapsed';
const readStoredFlag = (key: string, fallback = false) => {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? fallback : raw === 'true';
  } catch {
    return fallback;
  }
};
const writeStoredFlag = (key: string, value: boolean) => {
  try {
    localStorage.setItem(key, String(value));
  } catch {
    // Private-mode storage failures must not break navigation.
  }
};
const folderTree = ref<KnowledgeFolderTree | null>(null);
const folderTreeLoading = ref(false);
// The folder being browsed; ROOT_FOLDER_PATH ('') is the knowledge base top
// level, a real node of the tree rather than a separate mode.
const selectedFolderPath = ref<string>(ROOT_FOLDER_PATH);
const folderTreeCollapsed = ref(readStoredFlag(FOLDER_TREE_COLLAPSED_KEY));
const hasFolders = computed(() => (folderTree.value?.folders?.length ?? 0) > 0);
// The folder column only earns its space once the knowledge base actually has
// folders, so knowledge bases filled with single-file uploads look unchanged.
const showFolderTree = computed(() => hasFolders.value);
// Browsing lists one folder's own contents; filtering searches its whole
// subtree. There is no mode switch: the list follows what the user is doing.
const isFiltering = computed(() =>
  isFilteringDocuments({
    keyword: docSearchKeyword.value,
    fileType: selectedFileType.value,
    parseStatus: selectedParseStatus.value,
    source: selectedSource.value,
    timeRange: updatedTimeRange.value,
  }),
);
// Sub-folder entries shown at the top of the list while browsing. Search results
// are flat, so they are dropped as soon as a filter is active. When the sidebar
// tree is open it already lists the same folders, so skip the duplicate rows.
const currentChildFolders = computed(() => {
  if (isFiltering.value) return [];
  if (showFolderTree.value && !folderTreeCollapsed.value) return [];
  return childFolders(folderTree.value, selectedFolderPath.value);
});
// A row's folder is worth showing only when the list can span folders.
const showDocumentFolderPath = computed(() => hasFolders.value && isFiltering.value);
const folderBreadcrumbs = computed(() => buildFolderBreadcrumbs(selectedFolderPath.value));

const filterParams = computed(() => {
  const [start, end] = updatedTimeRange.value || [];
  return {
    keyword: docSearchKeyword.value ? docSearchKeyword.value.trim() : undefined,
    file_type: selectedFileType.value || undefined,
    parse_status: selectedParseStatus.value || undefined,
    source: selectedSource.value || undefined,
    start_time: start ? `${start} 00:00:00` : undefined,
    end_time: end ? `${end} 23:59:59` : undefined,
    folder_path: selectedFolderPath.value,
    // Searching descends into sub-folders; browsing shows one level, with the
    // sub-folders themselves rendered as entries in the list.
    folder_recursive: isFiltering.value,
  };
});
const getPageSize = () => {
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
  const itemHeight = 148;
  let itemsInView = Math.floor(viewportHeight / itemHeight) * 5;
  pageSize = Math.max(35, itemsInView);
}
getPageSize()

const loadKnowledgeFiles = (kbIdValue: string): Promise<void> => {
  if (!kbIdValue) return Promise.resolve();

    docListLoading.value = true;

  return getKnowled(
    {
      page: 1,
      page_size: pageSize,
      ...filterParams.value,
    },
    kbIdValue,
  ).finally(() => {
    if (isCurrentKb(kbIdValue)) {
      docListLoading.value = false;
    }
  });
};

const isCurrentKb = (targetKbId: string) => targetKbId === kbId.value;

const loadFolderTree = async (kbIdValue: string) => {
  if (!kbIdValue) {
    folderTree.value = null;
    return;
  }
  folderTreeLoading.value = true;
  try {
    const res: any = await listKnowledgeFolders(kbIdValue);
    if (!isCurrentKb(kbIdValue)) return;
    folderTree.value = (res?.data as KnowledgeFolderTree) || null;
    // A folder can disappear (its last document was deleted or moved); fall
    // back to the root instead of leaving an empty, unreachable view.
    if (!folderExistsInTree(folderTree.value?.folders || [], selectedFolderPath.value)) {
      selectedFolderPath.value = ROOT_FOLDER_PATH;
    }
  } catch (error) {
    if (!isCurrentKb(kbIdValue)) return;
    console.error('Failed to load knowledge folders', error);
    folderTree.value = null;
  } finally {
    if (isCurrentKb(kbIdValue)) {
      folderTreeLoading.value = false;
    }
  }
};

const handleFolderSelect = (path: string) => {
  if (selectedFolderPath.value === path) return;
  selectedFolderPath.value = path;
};

// ── Re-filing documents and renaming folders ──
// folder_path is display-only, so both operations are a plain column update:
// nothing is re-parsed, re-chunked or re-embedded.

// Flat folder list shared by every "move to folder" picker.
const folderOptions = computed(() => {
  const result: Array<{ path: string; name: string; depth: number }> = [];
  const walk = (nodes: KnowledgeFolderTree['folders'], depth: number) => {
    nodes.forEach((node) => {
      result.push({ path: node.path, name: node.name, depth });
      walk(node.children || [], depth + 1);
    });
  };
  walk(folderTree.value?.folders || [], 0);
  return result;
});

const moveKnowledgeIntoFolder = async (ids: string[], folderPath: string) => {
  if (!kbId.value || ids.length === 0) return;
  try {
    await moveKnowledgeToFolder(kbId.value, ids, folderPath);
    MessagePlugin.success(t('knowledgeBase.moveToFolder.success', { count: ids.length }));
    clearSelection();
    batchMode.value = false;
    resetPage();
    await loadKnowledgeFiles(kbId.value);
    await loadFolderTree(kbId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.moveToFolder.failed'));
  }
};

const handleFolderRename = async ({ from, to }: { from: string; to: string }) => {
  if (!kbId.value || !to || from === to) return;
  if (!canMoveFolderTo(from, to)) {
    MessagePlugin.warning(t('knowledgeBase.folderTree.renameInvalid'));
    return;
  }
  try {
    const res: any = await renameKnowledgeFolder(kbId.value, from, to);
    const movedCount = res?.data?.moved_count ?? 0;
    if (movedCount === 0) {
      MessagePlugin.warning(t('knowledgeBase.folderTree.renameFailed'));
      await loadFolderTree(kbId.value);
      return;
    }
    MessagePlugin.success(t('knowledgeBase.folderTree.renameSuccess'));
    // Follow the folder to its new path so the user stays where they were.
    if (selectedFolderPath.value === from) {
      selectedFolderPath.value = to;
    } else if (selectedFolderPath.value.startsWith(`${from}/`)) {
      selectedFolderPath.value = to + selectedFolderPath.value.slice(from.length);
    }
    resetPage();
    await loadKnowledgeFiles(kbId.value);
    await loadFolderTree(kbId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.folderTree.renameFailed'));
  }
};

const handleFolderTreeCollapsedChange = (value: boolean) => {
  folderTreeCollapsed.value = value;
  writeStoredFlag(FOLDER_TREE_COLLAPSED_KEY, value);
};

const loadKnowledgeBaseInfo = async (targetKbId: string, force = false) => {
  if (!targetKbId) {
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  kbLoading.value = true;
  try {
    const data = await resources.fetchKnowledgeBaseById(targetKbId, force);
    if (!isCurrentKb(targetKbId)) return;

    kbInfo.value = data;

      loadKnowledgeFiles(targetKbId);
      void loadFolderTree(targetKbId);

  } catch (error) {
    if (!isCurrentKb(targetKbId)) return;

    console.error('Failed to load knowledge base info:', error);
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
  } finally {
    if (isCurrentKb(targetKbId)) {
      kbLoading.value = false;
    }
  }
};

const loadKnowledgeList = async () => {
  try {
    await resources.ensureKnowledgeBases();
    const myKbs = resources.rawKnowledgeBases.map((item: any) => ({
      id: String(item.id),
      name: item.name,
      type: item.type || 'document',
    }));

    knowledgeList.value = myKbs;
  } catch (error) {
    console.error('Failed to load knowledge list:', error);
  }
};

// 监听路由参数变化，重新获取知识库内容
// Sync activeKbTab to URL query so it survives page refresh
watch(activeKbTab, (tab) => {
  const query = { ...route.query }
  if (tab === 'documents') {
    delete query.tab
  } else {
    query.tab = tab
  }
  router.replace({ query })
})

watch(() => kbId.value, (newKbId, oldKbId) => {
  if (newKbId !== oldKbId) clearDuplicateNotices();
  if (!newKbId) {
    kbInfo.value = null;
    cardList.value = [];
    total.value = 0;
    return;
  }
  if (newKbId === oldKbId && kbInfo.value) return;

  if (newKbId !== oldKbId) {
    clearTraceAvailabilityCache();
    cardList.value = [];
    total.value = 0;
    docListLoading.value = true;
    resetPage();
    folderTree.value = null;
    selectedFolderPath.value = ROOT_FOLDER_PATH;
  }
  loadKnowledgeBaseInfo(newKbId);
}, { immediate: true });

// 监听文档搜索关键词变化
watch(docSearchKeyword, (newVal, oldVal) => {
  if (newVal === oldVal) return;
  if (docSearchDebounce) {
    window.clearTimeout(docSearchDebounce);
  }
  docSearchDebounce = window.setTimeout(() => {
    if (kbId.value) {
      resetPage();
      loadKnowledgeFiles(kbId.value);
    }
  }, 300);
});

// 监听文件类型筛选变化
watch(selectedFileType, (newVal, oldVal) => {
  if (newVal === oldVal) return;
  if (kbId.value) {
    resetPage();
    loadKnowledgeFiles(kbId.value);
  }
});

// 监听解析状态/来源/更新时间范围筛选变化（与文件类型行为一致）
watch([selectedParseStatus, selectedSource, updatedTimeRange], () => {
  if (kbId.value) {
    resetPage();
    loadKnowledgeFiles(kbId.value);
  }
}, { deep: true });

// 切换目录只改变列表范围，行为与其他筛选一致。浏览态与筛选态之间的切换由各筛选项
// 自身的 watcher 触发刷新，这里不重复请求。
watch(selectedFolderPath, () => {
  if (!kbId.value) return;
  clearSelection();
  resetPage();
  loadKnowledgeFiles(kbId.value);
});

// 监听文件上传事件
const handleFileUploaded = (event: CustomEvent) => {
  const uploadedKbId = event.detail.kbId;
  console.log('接收到文件上传事件，上传的知识库ID:', uploadedKbId, '当前知识库ID:', kbId.value);
  if (uploadedKbId && uploadedKbId === kbId.value) {
    console.log('匹配当前知识库，开始刷新文件列表');
    // 如果上传的文件属于当前知识库，使用 loadKnowledgeFiles 刷新文件列表
    resetPage(); // Reset page counter when reloading files after upload
    loadKnowledgeFiles(uploadedKbId);
    void loadFolderTree(uploadedKbId);
    // 启动几次探测，尽快让面包屑的"索引中"亮起。
  }
};

// 监听从菜单触发的URL导入事件
const handleOpenURLImportDialog = (event: CustomEvent) => {
  const eventKbId = event.detail.kbId;
  console.log('接收到URL导入对话框打开事件，知识库ID:', eventKbId, '当前知识库ID:', kbId.value);
  if (eventKbId && eventKbId === kbId.value) {
    if (ensureDocumentKbReady()) {
      uploadSourceRef.value?.openUrlDialog();
    }
  }
};

// Global file drops are captured by the platform shell. Route them back into
// the same confirmation flow as the page upload button so per-batch
// processing settings are never skipped.
const handleKnowledgeFileDrop = (event: CustomEvent) => {
  const eventKbId = event.detail?.kbId;
  const files = Array.isArray(event.detail?.files) ? event.detail.files : [];
  if (eventKbId !== kbId.value || files.length === 0) return;
  handleUploadSourceFiles(files);
};

// Auto-open document detail when navigated with ?knowledge_id=xxx.
// Note: this runs both when the KB page mounts with a query param AND when a
// subsequent in-page navigation (e.g. from the global command palette) only
// changes the query without re-mounting the component — in that case kbId is
// the same and cardList may already be populated, so relying solely on the
// cardList watcher misses the trigger.
const pendingKnowledgeId = ref<string | null>(
  (route.query.knowledge_id as string) || null
);

let autoOpenRequest = 0;

const tryAutoOpenDocument = async () => {
  if (!pendingKnowledgeId.value) return;
  const targetId = pendingKnowledgeId.value;
  pendingKnowledgeId.value = null;
  const request = ++autoOpenRequest;
  const card = cardList.value.find((c: KnowledgeCard) => c.id === targetId);

  // The current card list only contains the folder being browsed. A document
  // opened from retrieval results may live in any nested folder, so resolve its
  // folder from the detail endpoint before opening the drawer. Otherwise the
  // drawer opens correctly while the page misleadingly remains at KB root.
  let target = card || ({ id: targetId } as KnowledgeCard);
  try {
    const response: any = await getKnowledgeDetails(targetId);
    if (request !== autoOpenRequest) return;
    const detail = response?.data || response;
    if (detail && typeof detail === 'object') {
      target = { ...target, ...detail, id: targetId } as KnowledgeCard;
      selectedFolderPath.value = detail.folder_path || ROOT_FOLDER_PATH;
    }
  } catch (error) {
    // Keep the previous ID-only fallback: getCardDetails will surface the
    // normal detail loading error, while links to root-level files still work.
    console.error('Failed to resolve referenced document folder', error);
  }

  if (request !== autoOpenRequest) return;
  await nextTick();
  openCardDetails(target);
};

// React to later ?knowledge_id= changes on the same KB route (no remount).
watch(
  () => route.query.knowledge_id,
  (newId) => {
    if (typeof newId !== 'string' || !newId) return;
    pendingKnowledgeId.value = newId;
    tryAutoOpenDocument();
  },
);

// Dispatched by the global command palette when the user picks a chunk that
// lives in the KB they are already viewing — vue-router dedupes identical
// navigations, so we rely on this event instead of a URL change.
const handleOpenKnowledgeEvent = (e: Event) => {
  const detail = (e as CustomEvent<{ kbId: string; knowledgeId: string }>).detail;
  if (!detail || !detail.knowledgeId) return;
  if (detail.kbId && detail.kbId !== kbId.value) return;
  pendingKnowledgeId.value = detail.knowledgeId;
  tryAutoOpenDocument();
};

onMounted(() => {
  loadKnowledgeList();
  editorResources.ensureParserEngines();

  window.addEventListener('knowledgeFileUploaded', handleFileUploaded as EventListener);
  window.addEventListener('openURLImportDialog', handleOpenURLImportDialog as EventListener);
  window.addEventListener('reliquary:knowledge-file-drop', handleKnowledgeFileDrop as EventListener);
  window.addEventListener('reliquary:open-knowledge', handleOpenKnowledgeEvent as EventListener);
});

onUnmounted(() => {
  window.removeEventListener('knowledgeFileUploaded', handleFileUploaded as EventListener);
  window.removeEventListener('openURLImportDialog', handleOpenURLImportDialog as EventListener);
  window.removeEventListener('reliquary:knowledge-file-drop', handleKnowledgeFileDrop as EventListener);
  window.removeEventListener('reliquary:open-knowledge', handleOpenKnowledgeEvent as EventListener);
  stopMovePoll();
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
});
watch(() => cardList.value, (newValue) => {

  docListLoading.value = false;

  // Auto-open document if navigated with ?knowledge_id=xxx.
  if (pendingKnowledgeId.value) {
    tryAutoOpenDocument();
  }

  let analyzeList = [];
  // Filter items that need polling: parsing in progress OR summary generation in progress
  analyzeList = newValue.filter(needsStatusPolling);
  if (timeout !== null) {
    clearTimeout(timeout);
    timeout = null;
  }
  if (analyzeList.length) {
    updateStatus(analyzeList)
  }

}, { deep: true })
type KnowledgeCard = {
  id: string;
  knowledge_base_id?: string;
  parse_status: string;
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
// needsStatusPolling decides whether a card row is still "in flight"
// enough that the doc list should keep refreshing it. Keep in sync with
// the backend lifecycle: pending / processing are the primary parse
// phase, finalizing is the post-process fan-out (summary / question /
// context extract still running), and a `completed` row whose summary
// hasn't landed yet keeps polling so the description fills in.
const needsStatusPolling = (item: KnowledgeCard) => {
  return knowledgeNeedsStatusPolling(item);
};

const updateStatus = (analyzeList: KnowledgeCard[]) => {
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
    batchQueryKnowledge(query).then((result: any) => {
      let hasChanges = false;
      if (result.success && result.data) {
        (result.data as KnowledgeCard[]).forEach((item: KnowledgeCard) => {
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
            cardList.value[index].summary_status !== item.summary_status ||
            cardList.value[index].description !== item.description) {

            // Always update the card data
            cardList.value[index].parse_status = parseStatus;
            cardList.value[index].summary_status = item.summary_status;
            cardList.value[index].description = item.description;
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
const openCardDetails = (item: KnowledgeCard) => {
  isCardDetails.value = true;
  getCardDetails(item);
};

// Open source document preview
const openSourceDoc = (knowledgeId: string) => {
  isCardDetails.value = true;
  getCardDetails({ id: knowledgeId });
};

const closeCardMoreMenu = (index: number) => {
  if (cardList.value?.[index]) {
    cardList.value[index].isMore = false;
  }
  moreIndex.value = -1;
};

const confirmDeleteKnowledge = (index: number, item: KnowledgeCard) => {
  closeCardMoreMenu(index);
  const deletedId = item?.id;
  delKnowledge(index, item, async () => {
    resetPage();
    const maxPolls = 30;
    const delayMs = 400;
    for (let i = 0; i < maxPolls; i++) {
      await loadKnowledgeFiles(kbId.value);
      const stillPresent = (cardList.value || []).some((c: KnowledgeCard) => c.id === deletedId);
      if (!stillPresent) break;
      await new Promise<void>((r) => setTimeout(r, delayMs));
    }
    void loadFolderTree(kbId.value);
  });
};

const onReparseMenuClick = (index: number, item: KnowledgeCard) => {
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
  }
};

const handleMoveKnowledge = async (item: KnowledgeCard) => {
  moveKnowledgeId.value = item.id;
  moveMenuMode.value = 'targets';
  moveTargetsLoading.value = true;
  moveTargetKbs.value = [];
  try {
    const res: any = await listMoveTargets(kbId.value);
    moveTargetKbs.value = res.data || [];
  } catch {
    moveTargetKbs.value = [];
  } finally {
    moveTargetsLoading.value = false;
  }
};

const handleMoveSelectTarget = (kb: any) => {
  moveSelectedTargetId.value = kb.id;
  moveSelectedTargetName.value = kb.name;
  moveMode.value = 'reuse_vectors';
  moveMenuMode.value = 'confirm';
};

const handleMoveBack = () => {
  if (moveMenuMode.value === 'confirm') {
    moveMenuMode.value = 'targets';
  } else {
    moveMenuMode.value = 'normal';
  }
};

const handleMoveConfirm = async () => {
  if (!moveSelectedTargetId.value || moveSubmitting.value) return;
  moveSubmitting.value = true;
  try {
    const res: any = await moveKnowledge({
      knowledge_ids: [moveKnowledgeId.value],
      source_kb_id: kbId.value,
      target_kb_id: moveSelectedTargetId.value,
      mode: moveMode.value,
    });
    const taskId = res.data?.task_id;
    MessagePlugin.info(t('knowledgeBase.moveStarted'));
    // Close the card menu
    moveMenuMode.value = 'normal';
    cardList.value.forEach(c => { c.isMore = false; });

    if (taskId) {
      startMovePoll(taskId);
    } else {
      moveSubmitting.value = false;
      resetPage(); // Reset page counter when reloading files after move
      loadKnowledgeFiles(kbId.value);
      void loadFolderTree(kbId.value);
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeBase.moveFailed'));
    moveSubmitting.value = false;
  }
};

const startMovePoll = (taskId: string) => {
  if (movePollTimer) clearInterval(movePollTimer);
  movePollTimer = setInterval(async () => {
    try {
      const res: any = await getKnowledgeMoveProgress(taskId);
      const data = res.data;
      if (!data) return;
      if (data.status === 'completed') {
        stopMovePoll();
        moveSubmitting.value = false;
        const failed = data.failed || 0;
        if (failed > 0) {
          MessagePlugin.warning(t('knowledgeBase.moveCompletedWithErrors', { success: (data.processed || 0) - failed, failed }));
        } else {
          MessagePlugin.success(t('knowledgeBase.moveCompleted'));
        }
        resetPage(); // Reset page counter when reloading files after move completion
        loadKnowledgeFiles(kbId.value);
        void loadFolderTree(kbId.value);
      } else if (data.status === 'failed') {
        stopMovePoll();
        moveSubmitting.value = false;
        MessagePlugin.error(t('knowledgeBase.moveFailed'));
      }
    } catch {
      // ignore poll errors
    }
  }, 2000);
};

const stopMovePoll = () => {
  if (movePollTimer) {
    clearInterval(movePollTimer);
    movePollTimer = null;
  }
};

const manualEditorSuccess = ({ kbId: savedKbId }: { kbId: string; knowledgeId: string; status: 'draft' | 'publish' }) => {
  if (savedKbId === kbId.value) {
    resetPage(); // Reset page counter when reloading files after manual edit
    loadKnowledgeFiles(savedKbId);
    void loadFolderTree(savedKbId);
  }
};

const documentTitle = computed(() => {
  if (kbInfo.value?.name) {
    return `${kbInfo.value.name} · ${t('knowledgeEditor.document.title')}`;
  }
  return t('knowledgeEditor.document.title');
});

const ensureDocumentKbReady = () => {

  if (!kbId.value) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return false;
  }
  if (!kbInfo.value || !kbInfo.value.summary_model_id) {
    MessagePlugin.warning(t('knowledgeBase.notInitialized'));
    return false;
  }
  // Embedding model only required when RAG indexing is enabled
  const strategy = (kbInfo.value as any).indexing_strategy
  const needsEmbedding = !strategy || strategy.vector_enabled || strategy.keyword_enabled
  if (needsEmbedding && !kbInfo.value.embedding_model_id) {
    MessagePlugin.warning(t('knowledgeBase.notInitialized'));
    return false;
  }
  return true;
};

const IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'];

const uploadConfirmStore = useUploadConfirmStore();

const getFolderUploadFileName = (file: File, targetFolder: string) =>
  buildUploadFileName(file, targetFolder);

const showUploadResultMessages = (
  successCount: number,
  failCount: number,
  totalCount: number,
  mode: 'document' | 'folder',
) => {
  if (mode === 'folder') {
    if (failCount === 0) {
      MessagePlugin.success(t('knowledgeBase.uploadAllSuccess', { count: successCount }));
    } else if (successCount > 0) {
      MessagePlugin.warning(t('knowledgeBase.uploadPartialSuccess', { success: successCount, fail: failCount }));
    } else {
      MessagePlugin.error(t('knowledgeBase.uploadAllFailed'));
    }
    return;
  }

  if (totalCount === 1) {
    if (successCount === 1) {
      MessagePlugin.success(t('knowledgeBase.uploadSuccess'));
    }
    return;
  }

  if (failCount === 0) {
    MessagePlugin.success(t('knowledgeBase.allUploadSuccess', { count: successCount }));
  } else if (successCount > 0) {
    MessagePlugin.warning(t('knowledgeBase.partialUploadSuccess', { success: successCount, fail: failCount }));
  } else {
    MessagePlugin.error(t('knowledgeBase.allUploadFailed', { count: failCount }));
  }
};

const executeUploadBatch = async (
  files: File[],
  options: {
    processConfig?: KnowledgeProcessOverrides;
    /** Destination folder confirmed in the upload dialog; '' is the root. */
    targetFolder?: string;
    sourceInfoByItem?: Record<string, IntakeSourceInfo>;
  } = {},
) => {
  const targetKbId = kbId.value;
  if (!targetKbId || files.length === 0) {
    return { successCount: 0, failCount: files.length, duplicateCount: 0 };
  }

  let successCount = 0;
  let failCount = 0;
  let duplicateCount = 0;
  const totalCount = files.length;
  const hasFolderPaths = files.some(isFolderUpload);

  for (const file of files) {
    try {
      const uploadData: {
        file: File
        fileName?: string
        process_config?: KnowledgeProcessOverrides
        source_info?: IntakeSourceInfo
      } = { file };

      const fileName = getFolderUploadFileName(file, options.targetFolder || ROOT_FOLDER_PATH);
      if (fileName) uploadData.fileName = fileName;
      if (options.processConfig) {
        uploadData.process_config = options.processConfig;
      }
      const sourceInfo = options.sourceInfoByItem?.[`file:${getUploadFileKey(file)}`]
      if (sourceInfo && Object.values(sourceInfo).some(Boolean)) uploadData.source_info = sourceInfo

      const responseData: any = await uploadKnowledgeFile(targetKbId, uploadData);
      if (showDuplicate(responseData, file.name)) {
        duplicateCount++;
        continue;
      }
      const isSuccess = responseData?.success || responseData?.code === 200 || responseData?.status === 'success' || (responseData?.success !== false && !responseData?.error && responseData);
      if (isSuccess) {
        successCount++;
      } else {
        failCount++;
        if (totalCount === 1) {
          let errorMessage = t('knowledgeBase.uploadFailed');
          if (responseData?.error?.message) {
            errorMessage = responseData.error.message;
          } else if (responseData?.message) {
            errorMessage = responseData.message;
          }
          MessagePlugin.error(errorMessage);
        }
      }
    } catch (error: any) {
      if (showDuplicate(error, file.name)) {
        duplicateCount++;
        continue;
      }
      failCount++;
      if (totalCount === 1) {
        let errorMessage = error?.error?.message || error?.message || t('knowledgeBase.uploadFailed');
        MessagePlugin.error(errorMessage);
      }
    }
  }

  if (successCount > 0) {
    window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', {
      detail: { kbId: targetKbId },
    }));
  }

  if (successCount > 0 || failCount > 0) {
    showUploadResultMessages(successCount, failCount, totalCount, hasFolderPaths ? 'folder' : 'document');
  }
  return { successCount, failCount, duplicateCount };
};

const executeUrlImport = async (
  url: string,
  processConfig?: KnowledgeProcessOverrides,
  sourceInfo?: IntakeSourceInfo,
) => {
  const targetKbId = kbId.value;
  if (!targetKbId) {
    MessagePlugin.error(t('error.missingKbId'));
    return;
  }

  try {
    const responseData: any = await createKnowledgeFromURL(targetKbId, {
      url,
      process_config: processConfig,
      source_info: sourceInfo,
    });
    if (showDuplicate(responseData, url)) return;
    window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', {
      detail: { kbId: targetKbId },
    }));
    const isSuccess = responseData?.success || responseData?.code === 200 || responseData?.status === 'success' || (responseData?.success !== false && !responseData?.error && responseData);
    if (isSuccess) {
      MessagePlugin.success(t('knowledgeBase.urlImportSuccess'));
    } else {
      let errorMessage = t('knowledgeBase.urlImportFailed');
      if (responseData?.error?.message) {
        errorMessage = responseData.error.message;
      } else if (responseData?.message) {
        errorMessage = responseData.message;
      }
      MessagePlugin.error(errorMessage);
    }
  } catch (error: any) {
    if (showDuplicate(error, url)) return;
    let errorMessage = error?.error?.message || error?.message || t('knowledgeBase.urlImportFailed');
    MessagePlugin.error(errorMessage);
  }
};

const handleUploadConfirmResult = async (result: UploadConfirmResult) => {
  if (result.mode === 'manual') {
    return;
  }

  const files = result.files || [];
  const urls = result.urls || [];
  const processConfig = result.processConfig;

  if (files.length > 0) {
    const hasFolderPaths = files.some(isFolderUpload);
    if (hasFolderPaths) {
      MessagePlugin.info(t('knowledgeBase.uploadingFolder', { total: files.length }, files.length));
    }
    await executeUploadBatch(files, {
      processConfig,
      targetFolder: result.targetFolder || ROOT_FOLDER_PATH,
      sourceInfoByItem: result.sourceInfoByItem,
    });
  }

  for (const url of urls) {
    await executeUrlImport(url, processConfig, result.sourceInfoByItem?.[`url:${url}`]);
  }
};

const openUploadConfirmDialog = async (files: File[], urls: string[] = []) => {
  if (!kbInfo.value) return;
  if (files.length === 0 && urls.length === 0) return;
  try {
    const result = await uploadConfirmStore.open({
      mode: 'file',
      kbInfo: kbInfo.value,
      files,
      urls,
      acceptFileTypes: acceptFileTypes.value,
      supportedFileTypes: [...supportedFileTypes.value],
      // Pre-fill the destination with the folder being browsed; the dialog shows
      // it and lets the user pick another folder (or the root) before confirming.
      targetFolder: selectedFolderPath.value,
      folderOptions: folderOptions.value,
    });
    await handleUploadConfirmResult(result);
  } catch {
    // cancelled
  }
};

const handleUploadSourceFiles = (files: File[]) => {
  const result = filterUnsupportedAudioFiles(files);
  if (result.audioFilteredCount > 0) {
    MessagePlugin.warning(t('error.unsupportedAudioUpload'));
  }
  if (result.validFiles.length === 0 || !ensureDocumentKbReady()) return;
  openUploadConfirmDialog(result.validFiles);
};

const handleUploadSourceUrl = (url: string) => {
  if (!ensureDocumentKbReady()) return;
  openUploadConfirmDialog([], [url]);
};

const handleManualCreate = () => {
  if (!ensureDocumentKbReady()) return;
  uiStore.openManualEditor({
    mode: 'create',
    kbId: kbId.value,
    status: 'draft',
    onSuccess: manualEditorSuccess,
  });
};

const handleOpenKBSettings = () => {
  if (!kbId.value) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return;
  }
  uiStore.openKBSettings(kbId.value);
};

const handleNavigateToKbList = () => {
  router.push('/platform/knowledge-bases');
};

const handleNavigateToCurrentKB = () => {
  if (!kbId.value) return;
  router.push(`/platform/knowledge-bases/${kbId.value}`);
};

const handleKnowledgeDropdownSelect = (data: { value: string }) => {
  if (!data?.value) return;
  if (data.value === kbId.value) return;
  router.push(`/platform/knowledge-bases/${data.value}`);
};

const handleManualEdit = (index: number, item: KnowledgeCard) => {

  if (cardList.value[index]) {
    cardList.value[index].isMore = false;
  }
  uiStore.openManualEditor({
    mode: 'edit',
    kbId: item.knowledge_base_id || kbId.value,
    knowledgeId: item.id,
    onSuccess: manualEditorSuccess,
  });
};

// Opens ONLY the trace drawer for this card — does NOT pop the
// document detail drawer behind it. The trace drawer attaches to
// body so it renders independent of its host's visibility; we just
// need `details` populated so the timeline component knows which
// knowledge_id to fetch. getCardDetails resets details synchronously
// then fills asynchronously, so we re-stamp the id/parse_status
// right after the call to avoid the brief empty-id window that
// would otherwise prevent the drawer from mounting.
const docContentRef = ref<any>(null);
const handleViewTrace = (index: number, item: KnowledgeCard) => {
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

const confirmRebuildKnowledge = async (index: number, item: KnowledgeCard) => {

  if (!item?.id) {
    MessagePlugin.warning(t('knowledgeEditor.messages.missingId'));
    return;
  }
  if (isParseInFlight(item.parse_status)) {
    MessagePlugin.info(t('knowledgeBase.rebuildInProgress'));
    return;
  }
  closeCardMoreMenu(index);

  // No KB context to seed the dialog defaults — fall back to a direct reparse
  // that reuses the overrides stored at upload time.
  if (!kbInfo.value) {
    await submitReparse(item.id);
    return;
  }

  // Prefill the confirm dialog with the overrides this doc was last parsed with.
  let processOverrides: KnowledgeProcessOverrides | null = item.metadata?.process_overrides ?? null;
  let fileName = item.file_name || item.title || '';
  let fileType = item.file_type || '';
  try {
    const detail: any = await getKnowledgeDetails(item.id);
    if (detail?.success && detail.data) {
      processOverrides = detail.data.metadata?.process_overrides ?? processOverrides;
      fileName = detail.data.file_name || detail.data.title || fileName;
      fileType = detail.data.file_type || fileType;
    }
  } catch {
    // fall back to the list item's fields
  }

  try {
    const result = await uploadConfirmStore.open({
      mode: 'reparse',
      kbInfo: kbInfo.value,
      reparse: { knowledgeId: item.id, fileName, fileType, processOverrides },
    });
    if (result.mode === 'reparse' && result.reparse) {
      await submitReparse(result.reparse.knowledgeId, result.processConfig);
    }
  } catch {
    // cancelled
  }
};

const submitReparse = async (id: string, processConfig?: KnowledgeProcessOverrides) => {
  try {
    await reparseKnowledge(id, processConfig ? { process_config: processConfig } : undefined);
    delete traceAvailableById[id];
    traceAvailableById[id] = true;
    MessagePlugin.success(t('knowledgeBase.rebuildSubmitted'));
    resetPage();
    loadKnowledgeFiles(kbId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.rebuildFailed'));
  }
};

const handleScroll = () => {

  if (docListLoading.value) return;
  if (scrollLoading) return;
  const currentKbId = kbId.value;
  if (!currentKbId) return;
  const element = knowledgeScroll.value;
  if (element) {
    let pageNum = Math.ceil(total.value / pageSize)
    const { scrollTop, scrollHeight, clientHeight } = element;
    if (scrollTop + clientHeight >= scrollHeight - 10) {
      if (cardList.value.length < total.value && page < pageNum) {
        page++;
        scrollLoading = true;
        getKnowled({ page, page_size: pageSize, ...filterParams.value }, currentKbId).finally(() => {
          if (isCurrentKb(currentKbId)) {
            scrollLoading = false;
          }
        });
      }
    }
  }
};
const getDoc = (page: number) => {
  getfDetails(details.id, page)
};

const syncDocumentSummaryState = (state: { id?: string; summary_status?: string; description?: string }) => {
  if (!state?.id) return;
  const card = cardList.value.find((item: KnowledgeCard) => item.id === state.id);
  if (!card) return;
  if (typeof state.summary_status === 'string' && state.summary_status) {
    card.summary_status = state.summary_status;
  }
  if (typeof state.description === 'string') {
    card.description = state.description;
  }
};

const toggleSelectRow = (id: string, checked: boolean, shiftKey?: boolean) => {
  const items = cardList.value || [];
  const idx = items.findIndex((i: KnowledgeCard) => i.id === id);
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
  containerRef: knowledgeScroll,
  itemSelector: '.knowledge-card[data-select-id], .doc-list-row[data-select-id]',
  selectedIds,
  getItemId: (el) => el.dataset.selectId || null,
  enabled: computed(() => cardList.value.length > 0),
  onSelectionStart: () => {
    batchMode.value = true;
  },
});

const isManualDraftKnowledge = (item: KnowledgeCard) =>
  item.type === 'manual' && item.parse_status === 'draft';

const openKnowledgeItem = (item: KnowledgeCard) => {
  if (shouldSuppressDocClick()) return;
  if (isManualDraftKnowledge(item)) {
    const index = cardList.value.findIndex((c) => c.id === item.id);
    if (index >= 0) {
      handleManualEdit(index, item);
      return;
    }
  }
  openCardDetails(item);
};

const handleIntakeReview = async (payload: { action: 'retry' | 'discard'; sourceInfo?: IntakeSourceInfo }) => {
  if (!details.id) return
  try {
    await reviewKnowledge(details.id, { action: payload.action, source_info: payload.sourceInfo })
    MessagePlugin.success(t('knowledgeBase.intakeReviewSubmitted'))
    details.parse_status = payload.action === 'retry' ? 'pending' : ''
    resetPage()
    await loadKnowledgeFiles(kbId.value)
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.intakeReviewFailed'))
  }
}

const confirmBatchDelete = async () => {
  if (batchDeleting.value || batchReparsing.value || selectedIds.value.size === 0) return;
  const ids = Array.from(selectedIds.value);
  const deletedIdSet = new Set(ids);
  batchDeleting.value = true;
  try {
    const res: any = await batchDeleteKnowledge(kbId.value, ids);
    if (res?.success) {
      MessagePlugin.success(t('knowledgeBase.batchDeleteSuccess', { count: ids.length }));
      clearSelection();
      batchMode.value = false;
      resetPage();
      // 后端将批量删除放入异步队列，立刻拉列表仍可能包含待删项；短轮询直到列表与后端一致或超时
      const maxPolls = 30;
      const delayMs = 400;
      for (let i = 0; i < maxPolls; i++) {
        await loadKnowledgeFiles(kbId.value);
        const stillPresent = (cardList.value || []).some((c: KnowledgeCard) => deletedIdSet.has(c.id));
        if (!stillPresent) break;
        await new Promise<void>((r) => setTimeout(r, delayMs));
      }
      void loadFolderTree(kbId.value);
    } else {
      MessagePlugin.error(res?.message || t('knowledgeBase.batchDeleteFailed'));
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('knowledgeBase.batchDeleteFailed'));
  } finally {
    batchDeleting.value = false;
  }
};

const confirmCancelParseKnowledge = async (item: KnowledgeCard) => {
  if (!item?.id) return;
  try {
    await cancelKnowledgeParse(item.id);
    MessagePlugin.success(t('knowledgeBase.cancelParseSubmitted'));
    loadKnowledgeFiles(kbId.value);
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.cancelParseFailed'));
  }
};

const downloadKnowledge = async (item: KnowledgeCard) => {
  if (!item?.id) return;
  try {
    const file = await downKnowledgeDetails(item.id);
    const objectUrl = URL.createObjectURL(file);
    const link = document.createElement('a');
    const fileName = resolveKnowledgeDownloadFileName(item);
    link.style.display = 'none';
    link.href = objectUrl;
    link.download = fileName;
    document.body.appendChild(link);
    link.click();
    nextTick(() => {
      link.remove();
      URL.revokeObjectURL(objectUrl);
    });
  } catch {
    MessagePlugin.error(t('file.downloadFailed'));
  }
};

// Bridge card-view actions back to existing per-card handlers.
const handleCardAction = (
  action: 'download' | 'edit' | 'reparse' | 'cancel-parse' | 'move' | 'move-folder' | 'delete' | 'view-trace' | 'batch-manage',
  item: KnowledgeCard,
) => {
  const idx = (cardList.value || []).findIndex((i: KnowledgeCard) => i.id === item.id);
  if (action === 'download') return downloadKnowledge(item);
  if (action === 'edit') return handleManualEdit(idx, item);
  if (action === 'reparse') {
    if (isParseInFlight(item.parse_status)) return onReparseMenuClick(idx, item);
    return confirmRebuildKnowledge(idx, item);
  }
  if (action === 'cancel-parse') return confirmCancelParseKnowledge(item);
  if (action === 'move') return handleMoveKnowledge(item);
  if (action === 'delete') return confirmDeleteKnowledge(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Bridge list-view actions back to existing per-card handlers.
const handleListAction = (
  action: 'download' | 'edit' | 'reparse' | 'cancel-parse' | 'move' | 'move-folder' | 'delete' | 'view-trace' | 'batch-manage',
  item: KnowledgeCard,
) => {
  const idx = (cardList.value || []).findIndex((i: KnowledgeCard) => i.id === item.id);
  if (action === 'download') return downloadKnowledge(item);
  if (action === 'edit') return handleManualEdit(idx, item);
  if (action === 'reparse') return confirmRebuildKnowledge(idx, item);
  if (action === 'cancel-parse') return confirmCancelParseKnowledge(item);
  if (action === 'move') return handleMoveKnowledge(item);
  if (action === 'delete') return confirmDeleteKnowledge(idx, item);
  if (action === 'view-trace') return handleViewTrace(idx, item);
  if (action === 'batch-manage') return handleEnterBatchFromCard(item);
};

// Clear selection on filter/kb change to avoid acting on hidden items.
watch(
  [docSearchKeyword, selectedFileType, selectedParseStatus, selectedSource, updatedTimeRange, kbId],
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
  const visible = new Set(items.map((i: KnowledgeCard) => i.id));
  for (const id of selectedIds.value) {
    if (!visible.has(id)) selectedIds.value.delete(id);
  }
}, { deep: false });

// 处理知识库编辑成功后的回调
const handleKBEditorSuccess = (kbIdValue: string) => {
  resources.invalidateKnowledgeBaseDetail(kbIdValue);
  resources.invalidate('knowledgeBases');
  loadKnowledgeList();
  if (kbIdValue === kbId.value) {
    loadKnowledgeBaseInfo(kbIdValue, true);
  }
};
</script>

<template>
  <div class="knowledge-layout">
      <div v-if="duplicateNotices.length" class="knowledge-duplicate-notices">
        <KnowledgeDuplicateNotice v-for="duplicate in duplicateNotices" :key="duplicate.id || duplicate.inputLabel"
          :duplicate="duplicate" @open="openDuplicate" @dismiss="dismissDuplicate" />
      </div>
      <div class="document-header">
        <div class="document-header-title">
          <div class="document-title-row">
            <h2 class="document-breadcrumb">
              <button type="button" class="breadcrumb-link" @click="handleNavigateToKbList">
                {{ $t('menu.knowledgeBase') }}
              </button>
              <t-icon name="chevron-right" class="breadcrumb-separator" />
              <KBSwitcherDropdown v-if="knowledgeList.length" :kb-list="knowledgeList" :current-kb-id="kbId"
                @select="(id) => handleKnowledgeDropdownSelect({ value: id })">
                <button type="button" class="breadcrumb-link dropdown" :disabled="!kbId">
                  <template v-if="!kbInfo">
                    <t-skeleton animation="gradient" :row-col="[{ width: '120px', height: '20px' }]" />
                  </template>
                  <template v-else>
                    <span>{{ kbInfo.name }}</span>
                    <t-icon name="chevron-down" />
                  </template>
                </button>
              </KBSwitcherDropdown>
              <button v-else type="button" class="breadcrumb-link" :disabled="!kbId" @click="handleNavigateToCurrentKB">
                <template v-if="!kbInfo">
                  <t-skeleton animation="gradient" :row-col="[{ width: '120px', height: '20px' }]" />
                </template>
                <template v-else>
                  {{ kbInfo.name }}
                </template>
              </button>
              <t-icon name="chevron-right" class="breadcrumb-separator" />

                <button type="button" :class="['breadcrumb-tab', { active: activeKbTab === 'documents' }]" :aria-pressed="activeKbTab === 'documents'"
                  @click="activeKbTab = 'documents'">{{ $t('knowledgeEditor.document.title') }}</button>
                <span class="breadcrumb-tab-sep">/</span><button type="button" :class="['breadcrumb-tab', { active: activeKbTab === 'entities' }]" :aria-pressed="activeKbTab === 'entities'"
                    @click="activeKbTab = 'entities'">{{ $t('knowledgeWorkbench.tabLabel') }}</button>
            </h2>
            <!-- 标题行右侧的动作锚点：聚拢"信息"和"设置"两个圆形按钮。 -->
            <div class="kb-title-actions">


              <KBInfoPopover v-if="kbInfo" :kb-info="kbInfo"
                :supported-file-types="[...supportedFileTypes]" />
              <t-tooltip :content="$t('knowledgeBase.settings')" placement="top">
                <button type="button" class="kb-settings-button" :disabled="!kbId" @click="handleOpenKBSettings">
                  <t-icon name="setting" size="16px" />
                </button>
              </t-tooltip>
            </div>
          </div>
          <p v-if="activeKbTab !== 'entities'" class="document-subtitle">{{ $t('knowledgeEditor.document.subtitle') }}</p>
          <p v-if="unsupportedFileTypes.length" class="parser-hint knowledge-header-hint" @click="goToParserSettings">
            <t-icon name="info-circle" class="parser-hint-icon" />
            <span class="knowledge-header-hint__text">{{$t('knowledgeBase.unsupportedTypesHint', {
              types: unsupportedFileTypes.map(t => '.' + t).join('、')
            })
              }}</span>
            <button type="button" class="parser-hint-link knowledge-header-hint__action" @click.stop="goToParserSettings">
              {{ $t('knowledgeBase.goToParserSettings') }} →
            </button>
          </p>
        </div>
      </div>

      <div v-if="activeKbTab === 'entities'" class="entity-workbench-area">
        <KnowledgeWorkbench v-if="kbId" :kb-id="kbId"
          @open-source="openSourceDoc" />
      </div>

      <template v-if="activeKbTab === 'documents'">
        <div class="knowledge-main">
          <KbFolderTree v-if="showFolderTree && !folderTreeCollapsed" :tree="folderTree" :selected-path="selectedFolderPath"
            :loading="folderTreeLoading"
            @select="handleFolderSelect" @update:collapsed="handleFolderTreeCollapsedChange"
            @rename="handleFolderRename" />
          <div class="documents-content">
            <div class="doc-card-area">
              <nav v-if="showFolderTree" class="doc-folder-path"
                :aria-label="$t('knowledgeBase.folderTree.title')">
                <t-tooltip v-if="folderTreeCollapsed" :content="$t('knowledgeBase.folderTree.expand')" placement="top">
                  <button type="button" class="doc-folder-path__tree-toggle"
                    :aria-label="$t('knowledgeBase.folderTree.expand')"
                    @click="handleFolderTreeCollapsedChange(false)">
                    <t-icon name="folder" size="14px" />
                  </button>
                </t-tooltip>
                <span v-if="!folderBreadcrumbs.length" class="doc-folder-path__crumb is-current">
                  {{ $t('knowledgeBase.folderTree.rootRow') }}
                </span>
                <button v-else type="button" class="doc-folder-path__crumb"
                  @click="handleFolderSelect('')">
                  {{ $t('knowledgeBase.folderTree.rootRow') }}
                </button>
                <template v-for="(crumb, index) in folderBreadcrumbs" :key="crumb.path">
                  <t-icon name="chevron-right" class="doc-folder-path__sep" />
                  <span v-if="index === folderBreadcrumbs.length - 1" class="doc-folder-path__crumb is-current">
                    {{ crumb.name }}
                  </span>
                  <button v-else type="button" class="doc-folder-path__crumb" @click="handleFolderSelect(crumb.path)">
                    {{ crumb.name }}
                  </button>
                </template>
                <!-- Filtering silently widens the scope to sub-folders, so say so. -->
                <span v-if="isFiltering" class="doc-folder-path__scope">
                  {{ $t('knowledgeBase.folderTree.searchingSubtree') }}
                </span>
              </nav>
              <div class="doc-filter-bar">
                <t-input v-model.trim="docSearchKeyword" :placeholder="$t('knowledgeBase.docSearchPlaceholder')"
                  clearable class="doc-search-input" @clear="loadKnowledgeFiles(kbId)"
                  @enter="loadKnowledgeFiles(kbId)">
                  <template #prefix-icon>
                    <t-icon name="search" size="16px" />
                  </template>
                </t-input>
                <div class="doc-filter-bar__filters">

                <div class="doc-filter-field">
                  <t-select v-model="selectedFileType" :options="fileTypeOptions"
                    :placeholder="$t('knowledgeBase.fileTypeFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="file" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedParseStatus" :options="parseStatusOptions"
                    :placeholder="$t('knowledgeBase.parseStatusFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="check-circle" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field">
                  <t-select v-model="selectedSource" :options="sourceOptions"
                    :placeholder="$t('knowledgeBase.sourceFilter')" class="doc-type-select doc-filter-field__control"
                    clearable>
                    <template #prefixIcon>
                      <t-icon name="link" size="16px" />
                    </template>
                  </t-select>
                </div>
                <div class="doc-filter-field doc-filter-field--wide">
                  <t-date-range-picker v-model="updatedTimeRange"
                    :placeholder="[$t('knowledgeBase.updatedTimeFrom'), $t('knowledgeBase.updatedTimeTo')]"
                    :disable-date="disableFutureDate" class="doc-date-range doc-filter-field__control" clearable
                    allow-input>
                    <template #prefixIcon>
                      <t-icon name="time" size="16px" />
                    </template>
                  </t-date-range-picker>
                </div>
                </div>
                <div class="doc-filter-bar__trailing">
                  <t-button  variant="text" size="small" @click="commandPalette.openPalette(docSearchKeyword.trim())">
                    {{ $t('knowledgeBase.searchMessageContent') }}
                  </t-button>
                  <div class="doc-view-toggle" role="group" :aria-label="$t('knowledgeBase.viewModeToggle')">
                    <t-tooltip :content="$t('knowledgeBase.viewModeGrid')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'grid' }"
                        @click="viewMode = 'grid'" :aria-pressed="viewMode === 'grid'">
                        <t-icon name="view-module" size="16px" />
                      </button>
                    </t-tooltip>
                    <t-tooltip :content="$t('knowledgeBase.viewModeList')" placement="top">
                      <button type="button" class="doc-view-toggle-btn" :class="{ active: viewMode === 'list' }"
                        @click="viewMode = 'list'" :aria-pressed="viewMode === 'list'">
                        <t-icon name="view-list" size="16px" />
                      </button>
                    </t-tooltip>
                  </div>
                  <div class="doc-filter-actions">
                    <KbUploadSourceDropdown ref="uploadSourceRef" :accept-file-types="acceptFileTypes"
                      :supported-file-types="[...supportedFileTypes]" include-manual trigger-icon="file-add"
                      trigger-class="content-bar-icon-btn"
                      :tooltip="t('knowledgeBase.addDocument')" placement="bottom-right" @files="handleUploadSourceFiles"
                      @url="handleUploadSourceUrl" @manual="handleManualCreate" />
                  </div>
                </div>
              </div>
              <div class="doc-scroll-container"
                :class="{
                  'is-empty': !cardList.length && !currentChildFolders.length && !docListLoading,
                  'is-marquee-active': docMarqueeVisible,
                }"
                ref="knowledgeScroll" @scroll="handleScroll" @mousedown="onDocMarqueeMouseDown">
                <div v-if="docMarqueeVisible" class="doc-marquee-box"
                  :class="{ 'is-add': docMarqueeMode === 'add', 'is-subtract': docMarqueeMode === 'subtract' }"
                  :style="docMarqueeBoxStyle" aria-hidden="true" />
                <!-- 文档骨架屏 -->
                <div v-if="docListLoading && cardList.length === 0 && !currentChildFolders.length" class="doc-card-list doc-card-list-animated">
                  <div v-for="n in 8" :key="'doc-skel-' + n" class="knowledge-card knowledge-card-skeleton">
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
                <template v-else-if="(cardList.length || currentChildFolders.length) && viewMode === 'grid' && selectedParseStatus !== 'rejected'">
                  <DocumentCardView
                    :items="cardList"
                    :folders="currentChildFolders" :folder-options="folderOptions"
                    :selected-ids="selectedIds"
                    :batch-mode="batchMode"


                    :trace-available-by-id="traceAvailableById"
                    :move-menu-mode="moveMenuMode"
                    :move-target-kbs="moveTargetKbs"
                    :move-targets-loading="moveTargetsLoading"
                    :move-selected-target-name="moveSelectedTargetName"
                    :move-mode="moveMode"
                    :move-submitting="moveSubmitting"
                    :show-folder-path="showDocumentFolderPath"
                    @open="(item: any) => openKnowledgeItem(item)"
                    @open-folder="handleFolderSelect"
                    @move-to-folder="(item: any, path: string) => moveKnowledgeIntoFolder([item.id], path)"
                    @toggle-checkbox="onCardGridCheckboxChange"
                    @menu-visible-change="(visible: boolean, item: any) => onCardMoreVisibleChange(visible, item)"
                    @action="(action: any, item: any) => handleCardAction(action, item)"
                    @move-select-target="(kb: any) => handleMoveSelectTarget(kb)"
                    @move-back="handleMoveBack"
                    @move-confirm="handleMoveConfirm"
                    @update:move-mode="(mode: any) => moveMode = mode"
                  />
                </template>
                <template v-else-if="(cardList.length || currentChildFolders.length) && (viewMode === 'list' || selectedParseStatus === 'rejected')">
                  <DocumentListView :items="cardList" :folders="currentChildFolders" :folder-options="folderOptions"
                    :selected-ids="selectedIds"

                    :trace-visible-ids="traceAvailableById"
                    :move-menu-mode="moveMenuMode"
                    :move-target-kbs="moveTargetKbs"
                    :move-targets-loading="moveTargetsLoading"
                    :move-selected-target-name="moveSelectedTargetName"
                    :move-mode="moveMode"
                    :move-submitting="moveSubmitting"
                    :show-folder-path="showDocumentFolderPath"
                    @open-folder="handleFolderSelect"
                    @move-to-folder="(item: any, path: string) => moveKnowledgeIntoFolder([item.id], path)"
                    @open="(item: any) => openKnowledgeItem(item)" @toggle-row="toggleSelectRow"
                    @toggle-all="toggleSelectAll" @action="(action: any, item: any) => handleListAction(action, item)"
                    @probe-trace="(item: any) => probeTraceAvailable(item)"
                    @move-select-target="(kb: any) => handleMoveSelectTarget(kb)"
                    @move-back="handleMoveBack"
                    @move-confirm="handleMoveConfirm"
                    @update:move-mode="(mode: any) => moveMode = mode"
                    @reset-move-state="moveMenuMode = 'normal'" />
                </template>
                <template v-else-if="!docListLoading">
                  <div class="doc-empty-state">
                    <p v-if="selectedFolderPath || isFiltering" class="doc-empty-folder">
                      {{ isFiltering
                        ? $t('knowledgeBase.folderTree.emptySearch')
                        : $t('knowledgeBase.folderTree.emptyFolder') }}
                    </p>
                    <EmptyKnowledge v-else :supported-file-types="[...supportedFileTypes]" />
                  </div>
                </template>
              </div>
              <div class="doc-batch-bar-anchor" v-show="batchMode || selectedIds.size > 0">
                <DocumentBatchBar :count="selectedIds.size" :delete-loading="batchDeleting"
                  :reparse-loading="batchReparsing" :visible="batchMode || selectedIds.size > 0"
                  :show-move-to-folder="true" :folder-options="folderOptions"
                  @cancel="handleBatchCancel" @delete="confirmBatchDelete" @reparse="confirmBatchReparse"
                  @move-to-folder="(path: string) => moveKnowledgeIntoFolder(Array.from(selectedIds), path)" />
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- Document content drawer -->
      <DocContent ref="docContentRef" :visible="isCardDetails" :details="details"
        :kbId="kbId"
        @closeDoc="closeDoc" @getDoc="getDoc" @summaryStateChange="syncDocumentSummaryState" @intake-review="handleIntakeReview">
      </DocContent>
    </div>

  <!-- 知识库编辑器（创建/编辑统一组件） -->
  <KnowledgeBaseEditorModal :visible="uiStore.showKBEditorModal" :mode="uiStore.kbEditorMode"
    :kb-id="uiStore.currentKBId || undefined" :initial-type="uiStore.kbEditorType"
    @update:visible="(val) => val ? null : uiStore.closeKBEditor()" @success="handleKBEditorSuccess" />





</template>
<style scoped lang="less">
.knowledge-layout {
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

.knowledge-duplicate-notices {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex-shrink: 0;
  max-height: 30vh;
  overflow-y: auto;
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
.knowledge-main {
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

// 目录树选中路径的面包屑：与顶部知识库面包屑同一套视觉语言，只是更轻量。
.doc-folder-path {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  padding: 0 0 8px;
  flex-shrink: 0;

  &__tree-toggle {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    margin-right: 4px;
    padding: 0;
    border: 1px solid var(--td-component-border);
    border-radius: 6px;
    background: var(--td-bg-color-container);
    color: var(--td-text-color-secondary);
    cursor: pointer;
    transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;

    &:hover {
      border-color: var(--td-brand-color);
      color: var(--td-brand-color);
      background: var(--td-bg-color-container-hover);
    }
  }

  &__crumb {
    max-width: 220px;
    padding: 2px 4px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    line-height: 18px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
    transition: color 0.15s ease, background 0.15s ease;

    &:hover {
      color: var(--td-brand-color);
      background: var(--td-bg-color-container-hover);
    }

    &.is-current {
      color: var(--td-text-color-primary);
      font-weight: 500;
      cursor: default;

      &:hover {
        background: transparent;
        color: var(--td-text-color-primary);
      }
    }
  }

  &__sep {
    flex-shrink: 0;
    font-size: 12px;
    color: var(--td-text-color-placeholder);
  }
}

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

  // The folder tree changes the available document width without changing the
  // viewport width. Switch to a single row only when this content area itself
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

  .kb-title-actions {
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

  .parser-hint {
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

      .parser-hint-link {
        text-decoration: underline;
      }
    }

    .parser-hint-icon {
      font-size: 12px;
      flex-shrink: 0;
    }

    .parser-hint-link {
      color: var(--td-brand-color);
      margin-left: 2px;
      white-space: nowrap;
    }
  }

  .knowledge-header-hint__action {
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

.document-upload-input {
  display: none;
}

.kb-settings-button {
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

.knowledge-card-skeleton {
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

.move-menu {
  min-width: 220px;
  max-width: 280px;
  max-height: 360px;
  overflow-y: auto;

  .move-menu-header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    font-size: 13px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    border-bottom: 1px solid var(--td-component-stroke);
    cursor: pointer;

    &:hover {
      background: var(--td-bg-color-container-hover);
    }
  }

  .move-menu-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px 0;
  }

  .move-menu-empty {
    padding: 12px 16px;
    font-size: 12px;
    color: var(--td-text-color-placeholder);
    text-align: center;
    line-height: 1.5;
  }

  .move-target-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .move-target-count {
    font-size: 12px;
    color: var(--td-text-color-placeholder);
  }

  .move-confirm-body {
    padding: 8px;

    .move-target-info {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 6px 8px;
      background: var(--td-bg-color-container-hover);
      border-radius: 6px;
      font-size: 13px;
      color: var(--td-text-color-secondary);
      margin-bottom: 8px;
    }

    .move-mode-item {
      display: flex;
      align-items: flex-start;
      gap: 6px;
      padding: 6px 8px;
      border-radius: 6px;
      cursor: pointer;
      margin-bottom: 4px;

      &:hover {
        background: var(--td-bg-color-container-hover);
      }

      &.active {
        background: var(--td-brand-color-light);
      }

      .move-mode-text {
        display: flex;
        flex-direction: column;
        gap: 2px;

        .move-mode-label {
          font-size: 13px;
          font-weight: 500;
          color: var(--td-text-color-primary);
        }

        .move-mode-desc {
          font-size: 11px;
          color: var(--td-text-color-placeholder);
          line-height: 1.4;
        }
      }
    }

    .move-confirm-actions {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
      margin-top: 8px;
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

.knowledge-card {
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

.knowledge-card:hover {
  border-color: color-mix(in srgb, var(--td-component-stroke) 55%, var(--td-brand-color));
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.07);
}

/* 悬停知识卡片时跟随鼠标的详情气泡 */
.knowledge-card-hover-popover {
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

.url-import-form {
  padding: 8px 0;

  .url-input-label {
    color: var(--td-text-color-primary);
    font-size: 14px;
    font-weight: 500;
    margin-bottom: 8px;
  }

  .url-input-tip {
    color: var(--td-text-color-secondary);
    font-size: 12px;
    margin-top: 8px;
    line-height: 1.5;
  }
}

.knowledge-card-upload {
  color: var(--td-text-color-primary);
  font-family: var(--app-font-family);
  font-size: 14px;
  font-weight: 400;
  cursor: pointer;

  .btn-upload {
    margin: 33px auto 0;
    width: 112px;
    height: 32px;
    border: 1px solid var(--td-component-border);
    display: flex;
    justify-content: center;
    align-items: center;
    margin-bottom: 24px;
  }

  .svg-icon-download {
    margin-right: 8px;
  }
}

.upload-described {
  color: var(--td-text-color-disabled);
  font-family: var(--app-font-family);
  font-size: 12px;
  font-weight: 400;
  text-align: center;
  display: block;
  width: 188px;
  margin: 0 auto;
}

.del-card {
  vertical-align: middle;
}
@media (max-width: 640px) {
  .knowledge-layout {
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

    .knowledge-header-hint {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: start;
      min-width: 0;
      max-width: 100%;

      .knowledge-header-hint__action {
        grid-column: 2;
        justify-self: start;
        margin-left: 0;
        max-width: 100%;
        white-space: normal;
      }
    }

    .knowledge-header-hint__text {
      min-width: 0;
      overflow-wrap: anywhere;
      white-space: normal;
    }

  }
}

</style>
