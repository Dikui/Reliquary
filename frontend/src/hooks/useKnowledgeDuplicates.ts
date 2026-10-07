import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { resolveKnowledgeDuplicate, type KnowledgeDuplicateNotice } from '../utils/knowledgeDuplicate';

export function useKnowledgeDuplicates(currentKnowledgeBaseId: () => string | undefined) {
  const route = useRoute();
  const router = useRouter();
  const duplicateNotices = ref<KnowledgeDuplicateNotice[]>([]);

  function showDuplicate(value: unknown, inputLabel = ''): boolean {
    const duplicate = resolveKnowledgeDuplicate(value, currentKnowledgeBaseId(), inputLabel);
    if (!duplicate) return false;
    const key = duplicate.id || `${duplicate.code}:${duplicate.inputLabel}`;
    const index = duplicateNotices.value.findIndex(item => (item.id || `${item.code}:${item.inputLabel}`) === key);
    if (index >= 0) duplicateNotices.value.splice(index, 1, duplicate);
    else duplicateNotices.value.push(duplicate);
    return true;
  }

  function dismissDuplicate(duplicate: KnowledgeDuplicateNotice) {
    duplicateNotices.value = duplicateNotices.value.filter(item => item !== duplicate);
  }

  function clearDuplicateNotices() {
    duplicateNotices.value = [];
  }

  async function openDuplicate(duplicate: KnowledgeDuplicateNotice): Promise<boolean> {
    if (!duplicate.id || !duplicate.knowledgeBaseId) return false;
    if (route.params.kbId === duplicate.knowledgeBaseId) {
      window.dispatchEvent(new CustomEvent('reliquary:open-knowledge', {
        detail: { kbId: duplicate.knowledgeBaseId, knowledgeId: duplicate.id },
      }));
    } else {
      await router.push({
        path: `/platform/knowledge-bases/${encodeURIComponent(duplicate.knowledgeBaseId)}`,
        query: { knowledge_id: duplicate.id },
      });
    }
    return true;
  }

  return { duplicateNotices, showDuplicate, dismissDuplicate, clearDuplicateNotices, openDuplicate };
}
