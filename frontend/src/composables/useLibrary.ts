import { ref, reactive } from "vue";
import { storeToRefs } from "pinia";
import { formatStringDate } from "../utils/index";
import { MessagePlugin } from "tdesign-vue-next";
import {
  listEntryFiles,
  getEntryDetails,
  delEntryDetails,
  getEntryDetailsCon,
} from "@/api/library";
import { entryStore } from "@/stores/entry";
import { useI18n } from 'vue-i18n';
import type { EntryRepostSource } from '@/api/library';
import type { ClassifiedEntry, StructuredSummary, EntryTopic } from '@/types/entryClassification';

export default function (defaultLibraryId?: string) {
  const usemenuStore = entryStore();
  const { t } = useI18n();
  const { cardList, total } = storeToRefs(usemenuStore);
  let moreIndex = ref(-1);
  const details = reactive({
    library_name: "",
    library_id: "",
    title: "",
    time: "",
    md: [] as any[],
    id: "",
    total: 0,
    type: "",
    source_kind: '' as NonNullable<ClassifiedEntry['source_kind']> | '',
    vault_path: '',
    source: "",
    channel: "",
    file_type: "",
    description: "",
    collect_reason: "",
    content_type: '',
    library_content_types: [] as NonNullable<ClassifiedEntry['library_content_types']>,
    image_descriptions: [] as NonNullable<ClassifiedEntry['image_descriptions']>,
    starred: false,
    source_unverified: false,
    has_update: false,
    latest_entry_id: '',
    acquisition: undefined as ClassifiedEntry['acquisition'],
    ingest_api_key_name: '',
    last_ingest_api_key_name: '',
    related_entry_id: '',
    structured_summary: null as StructuredSummary | null,
    entities: [] as NonNullable<ClassifiedEntry['entities']>,
    topics: [] as EntryTopic[],
    parse_status: "",
    error_message: "",
	metadata: {} as Record<string, any>,
	custom_metadata: {} as Record<string, unknown>,
    chunkLoading: false,
    chunkLoadError: "",
    repost_sources: [] as EntryRepostSource[],
  });
  let entryListGeneration = 0;
  let chunkRequestGeneration = 0;
  let detailRequestGeneration = 0;
  let activeEntryId = '';
  const loadEntries = (
    query: {
      page: number;
      page_size: number;
      libraries?: string[];
      keyword?: string;
      file_type?: string;
      content_type?: string;
      starred?: boolean;
      parse_status?: string;
      source?: string;
      source_kind?: ClassifiedEntry['source_kind'];
      start_time?: string;
      end_time?: string;
    } = { page: 1, page_size: 35 },
    libraryId?: string,
  ): Promise<void> => {
    const targetLibraryId = libraryId || defaultLibraryId;
    if (!targetLibraryId) return Promise.resolve();
    const requestGeneration = query.page === 1 ? ++entryListGeneration : entryListGeneration;

    return listEntryFiles(targetLibraryId, query)
      .then((result: any) => {
        if (requestGeneration !== entryListGeneration) return;

        const { data, total: totalResult } = result;
    const cardList_ = data.map((item: any) => {
      const isVaultDocument = item.source_kind === 'mine' || item.source_kind === 'report'
      const rawName = (isVaultDocument ? item.title || item.file_name : item.file_name || item.title)
        || item.source || t('library.untitledDocument')
      const dotIndex = rawName.lastIndexOf('.')
      const displayName = !isVaultDocument && dotIndex > 0 ? rawName.substring(0, dotIndex) : rawName
      const fileTypeSource = item.file_type || (item.type === 'manual' ? 'MANUAL' : '')
      return {
        ...item,
        original_file_name: item.file_name,
        display_name: displayName,
        file_name: displayName,
        updated_at: formatStringDate(new Date(item.updated_at)),
        isMore: false,
        file_type: fileTypeSource ? String(fileTypeSource).toLocaleUpperCase() : '',
      }
    });

        if (query.page === 1) {
          cardList.value = cardList_;
        } else {
          cardList.value.push(...cardList_);
        }
        total.value = totalResult;
      })
      .catch(() => {});
  };
  const delEntry = (index: number, item: any, onSuccess?: () => void) => {
    return delEntryDetails(item.id)
      .then(async (result: any) => {
        if (result.success) {
          MessagePlugin.info(t(item.source_kind === 'mine' ? 'personalDocument.deleted' : 'library.deleteSuccess'));
          if (onSuccess) {
            await onSuccess();
          } else {
            // 后端已将单条删除放入异步队列，立即拉列表仍可能包含待删项；
            // 短轮询直到列表与后端一致或超时。
            const maxPolls = 30;
            const delayMs = 400;
            for (let i = 0; i < maxPolls; i++) {
              await loadEntries();
              const stillPresent = (cardList.value || []).some((c: any) => c.id === item.id);
              if (!stillPresent) break;
              await new Promise<void>((r) => setTimeout(r, delayMs));
            }
          }
          return true;
        } else {
          MessagePlugin.error(t('library.deleteFailed'));
          return false;
        }
      })
      .catch(() => {
        MessagePlugin.error(t('library.deleteFailed'));
        return false;
      });
  };
  const openMore = (index: number) => {
    moreIndex.value = index;
  };
  const onVisibleChange = (visible: boolean) => {
    if (!visible) {
      moreIndex.value = -1;
    }
  };
  const clearCardDetails = () => {
    detailRequestGeneration++;
    chunkRequestGeneration++;
    activeEntryId = '';
    Object.assign(details, {
      title: "",
      time: "",
      md: [],
      id: "",
      type: "",
      source_kind: '',
      vault_path: '',
      source: "",
      channel: "",
      file_type: "",
      description: "",
      collect_reason: "",
      library_name: '',
      library_id: '',
      content_type: '',
      library_content_types: [],
      image_descriptions: [],
      starred: false,
      source_unverified: false,
      has_update: false,
      latest_entry_id: '',
      acquisition: undefined,
      ingest_api_key_name: '',
      last_ingest_api_key_name: '',
      related_entry_id: '',
      structured_summary: null,
      entities: [],
      topics: [],
      parse_status: "",
      error_message: "",
	  metadata: {},
	  custom_metadata: {},
      chunkLoadError: "",
      repost_sources: [],
    });
    details.chunkLoading = false;
  };
  const getCardDetails = (item: any) => {
    clearCardDetails();
    const requestGeneration = detailRequestGeneration;
    activeEntryId = item.id;

    getEntryDetails(item.id)
      .then((result: any) => {
        if (requestGeneration !== detailRequestGeneration || activeEntryId !== item.id) return;
        if (result.success && result.data) {
          const { data } = result;
          Object.assign(details, {
            title: ((data.source_kind === 'mine' || data.source_kind === 'report')
              ? data.title || data.file_name : data.file_name || data.title)
              || data.source || t('library.untitledDocument'),
            time: formatStringDate(new Date(data.updated_at)),
            id: data.id,
            type: data.type || 'file',
            source_kind: data.source_kind || '',
            vault_path: data.vault_path || '',
            source: data.source || '',
            channel: data.channel || '',
            file_type: data.file_type || '',
            description: data.description || '',
            collect_reason: data.collect_reason || '',
            library_name: data.library_name || '',
            library_id: data.library_id || '',
            content_type: data.content_type || '',
            library_content_types: Array.isArray(data.library_content_types) ? data.library_content_types : [],
            image_descriptions: Array.isArray(data.image_descriptions) ? data.image_descriptions : [],
            starred: Boolean(data.starred),
            source_unverified: Boolean(data.source_unverified),
            has_update: Boolean(data.has_update),
            latest_entry_id: data.latest_entry_id || '',
            acquisition: data.acquisition,
            ingest_api_key_name: data.ingest_api_key_name || '',
            last_ingest_api_key_name: data.last_ingest_api_key_name || '',
            related_entry_id: data.related_entry_id || '',
            structured_summary: data.structured_summary || null,
            entities: Array.isArray(data.entities) ? data.entities : [],
            topics: Array.isArray(data.topics) ? data.topics : [],
            parse_status: data.parse_status || '',
            error_message: data.error_message || '',
			metadata: data.metadata || {},
			custom_metadata: data.custom_metadata || {},
            repost_sources: Array.isArray(data.repost_sources) ? data.repost_sources : [],
          });
        }
      })
      .catch(() => {});
    getfDetails(item.id, 1);
  };

  const getfDetails = (id: string, page: number) => {
    const requestGeneration = ++chunkRequestGeneration;
    details.chunkLoading = true;
    details.chunkLoadError = "";
    getEntryDetailsCon(id, page)
      .then((result: any) => {
        if (requestGeneration !== chunkRequestGeneration || activeEntryId !== id) return;
        if (result.success && result.data) {
          const { data, total: totalResult } = result;
          details.md = data;
          details.total = totalResult;
        } else {
          details.chunkLoadError = result?.message || result?.error?.message || t('library.chunkLoadFailed');
        }
      })
      .catch((err: any) => {
        if (requestGeneration !== chunkRequestGeneration || activeEntryId !== id) return;
        details.chunkLoadError = err?.message || t('library.chunkLoadFailed');
        console.error("[ChunkLoad] failed", {
          entryId: id,
          page,
          error: err,
        });
      })
      .finally(() => {
        if (requestGeneration === chunkRequestGeneration) {
          details.chunkLoading = false;
        }
      });
  };
  return {
    cardList,
    moreIndex,
    loadEntries,
    details,
    delEntry,
    openMore,
    onVisibleChange,
    getCardDetails,
    clearCardDetails,
    total,
    getfDetails,
  };
}
