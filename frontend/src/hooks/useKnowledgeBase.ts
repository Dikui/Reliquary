import { ref, reactive } from "vue";
import { storeToRefs } from "pinia";
import { formatStringDate, kbFileTypeVerification } from "../utils/index";
import { MessagePlugin } from "tdesign-vue-next";
import {
  uploadKnowledgeFile,
  listKnowledgeFiles,
  getKnowledgeDetails,
  delKnowledgeDetails,
  getKnowledgeDetailsCon,
} from "@/api/knowledge-base/index";
import { knowledgeStore } from "@/stores/knowledge";
import { useI18n } from 'vue-i18n';
import type { KnowledgeRepostSource } from '@/api/knowledge-base/index';
import type { ClassifiedKnowledge, StructuredSummary, KnowledgeTopic } from '@/types/knowledgeClassification';
import { useResourcesStore } from '@/stores/resources';
import { useKnowledgeDuplicates } from './useKnowledgeDuplicates';

export default function (knowledgeBaseId?: string) {
  const usemenuStore = knowledgeStore();
  const { t } = useI18n();
  const { cardList, total } = storeToRefs(usemenuStore);
  const duplicates = useKnowledgeDuplicates(() => knowledgeBaseId || useResourcesStore().knowledgeBase?.id);
  let moreIndex = ref(-1);
  const details = reactive({
    title: "",
    time: "",
    md: [] as any[],
    id: "",
    total: 0,
    type: "",
    source_kind: '' as NonNullable<ClassifiedKnowledge['source_kind']> | '',
    vault_path: '',
    source: "",
    channel: "",
    file_type: "",
    description: "",
    content_type: '',
    processing_mode: 'full' as 'full' | 'summary_only',
    structured_summary: null as StructuredSummary | null,
    entities: [] as NonNullable<ClassifiedKnowledge['entities']>,
    topics: [] as KnowledgeTopic[],
    parse_status: "",
    error_message: "",
	metadata: {} as Record<string, any>,
	custom_metadata: {} as Record<string, unknown>,
    chunkLoading: false,
    chunkLoadError: "",
    repost_sources: [] as KnowledgeRepostSource[],
  });
  let knowledgeListGeneration = 0;
  let chunkRequestGeneration = 0;
  let activeKnowledgeId = '';
  const getKnowled = (
    query: {
      page: number;
      page_size: number;
      keyword?: string;
      file_type?: string;
      content_type?: string;
      parse_status?: string;
      source?: string;
      start_time?: string;
      end_time?: string;
    } = { page: 1, page_size: 35 },
    kbId?: string,
  ): Promise<void> => {
    const targetKbId = kbId || knowledgeBaseId;
    if (!targetKbId) return Promise.resolve();
    const requestGeneration = query.page === 1 ? ++knowledgeListGeneration : knowledgeListGeneration;

    return listKnowledgeFiles(targetKbId, query)
      .then((result: any) => {
        if (requestGeneration !== knowledgeListGeneration) return;

        const { data, total: totalResult } = result;
    const cardList_ = data.map((item: any) => {
      const isVaultDocument = item.source_kind === 'mine' || item.source_kind === 'report'
      const rawName = (isVaultDocument ? item.title || item.file_name : item.file_name || item.title)
        || item.source || t('knowledgeBase.untitledDocument')
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
  const delKnowledge = (index: number, item: any, onSuccess?: () => void) => {
    cardList.value[index].isMore = false;
    moreIndex.value = -1;
    return delKnowledgeDetails(item.id)
      .then(async (result: any) => {
        if (result.success) {
          MessagePlugin.info(t('knowledgeBase.deleteSuccess'));
          if (onSuccess) {
            onSuccess();
          } else {
            // 后端已将单条删除放入异步队列，立即拉列表仍可能包含待删项；
            // 短轮询直到列表与后端一致或超时。
            const maxPolls = 30;
            const delayMs = 400;
            for (let i = 0; i < maxPolls; i++) {
              await getKnowled();
              const stillPresent = (cardList.value || []).some((c: any) => c.id === item.id);
              if (!stillPresent) break;
              await new Promise<void>((r) => setTimeout(r, delayMs));
            }
          }
          return true;
        } else {
          MessagePlugin.error(t('knowledgeBase.deleteFailed'));
          return false;
        }
      })
      .catch(() => {
        MessagePlugin.error(t('knowledgeBase.deleteFailed'));
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
  const requestMethod = (file: any, uploadInput: any) => {
    if (!(file instanceof File) || !uploadInput) {
      MessagePlugin.error(t('error.invalidFileType'));
      return;
    }

    if (kbFileTypeVerification(file)) {
      return;
    }

    const currentKbId = knowledgeBaseId || useResourcesStore().knowledgeBase?.id;
    if (!currentKbId) {
      MessagePlugin.error(t('error.missingKbId'));
      return;
    }

    return uploadKnowledgeFile(currentKbId, { file })
      .then((result: any) => {
        if (duplicates.showDuplicate(result, file.name)) return;
        if (result.success) {
          MessagePlugin.info(t('knowledgeBase.uploadSuccess'));
          getKnowled({ page: 1, page_size: 35 }, currentKbId);
        } else {
          const errorMessage = result.error?.message || result.message || t('knowledgeBase.uploadFailed');
          MessagePlugin.error(errorMessage);
        }
      })
      .catch((err: any) => {
        if (duplicates.showDuplicate(err, file.name)) return;
        const errorMessage = err.error?.message || err.message || t('knowledgeBase.uploadFailed');
        MessagePlugin.error(errorMessage);
      })
      .finally(() => {
        uploadInput.value.value = "";
      });
  };
  const getCardDetails = (item: any) => {
    activeKnowledgeId = item.id;
    chunkRequestGeneration++;
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
      content_type: '',
      processing_mode: 'full',
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
    getKnowledgeDetails(item.id)
      .then((result: any) => {
        if (activeKnowledgeId !== item.id) return;
        if (result.success && result.data) {
          const { data } = result;
          Object.assign(details, {
            title: ((data.source_kind === 'mine' || data.source_kind === 'report')
              ? data.title || data.file_name : data.file_name || data.title)
              || data.source || t('knowledgeBase.untitledDocument'),
            time: formatStringDate(new Date(data.updated_at)),
            id: data.id,
            type: data.type || 'file',
            source_kind: data.source_kind || '',
            vault_path: data.vault_path || '',
            source: data.source || '',
            channel: data.channel || '',
            file_type: data.file_type || '',
            description: data.description || '',
            content_type: data.content_type || '',
            processing_mode: data.processing_mode || 'full',
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
    getKnowledgeDetailsCon(id, page)
      .then((result: any) => {
        if (requestGeneration !== chunkRequestGeneration || activeKnowledgeId !== id) return;
        if (result.success && result.data) {
          const { data, total: totalResult } = result;
          details.md = data;
          details.total = totalResult;
        } else {
          details.chunkLoadError = result?.message || result?.error?.message || t('knowledgeBase.chunkLoadFailed');
        }
      })
      .catch((err: any) => {
        if (requestGeneration !== chunkRequestGeneration || activeKnowledgeId !== id) return;
        details.chunkLoadError = err?.message || t('knowledgeBase.chunkLoadFailed');
        console.error("[ChunkLoad] failed", {
          knowledgeId: id,
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
    ...duplicates,
    cardList,
    moreIndex,
    getKnowled,
    details,
    delKnowledge,
    openMore,
    onVisibleChange,
    requestMethod,
    getCardDetails,
    total,
    getfDetails,
  };
}
