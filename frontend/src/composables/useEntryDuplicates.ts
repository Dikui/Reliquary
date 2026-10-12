import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { resolveEntryDuplicate, type EntryDuplicateNotice } from '../utils/entryDuplicate';

export function useEntryDuplicates(currentKnowledgeBaseId: () => string | undefined) {
  const route = useRoute();
  const router = useRouter();
  const duplicateNotices = ref<EntryDuplicateNotice[]>([]);

  function showDuplicate(value: unknown, inputLabel = ''): boolean {
    const duplicate = resolveEntryDuplicate(value, currentKnowledgeBaseId(), inputLabel);
    if (!duplicate) return false;
    const key = duplicate.id || `${duplicate.code}:${duplicate.inputLabel}`;
    const index = duplicateNotices.value.findIndex(item => (item.id || `${item.code}:${item.inputLabel}`) === key);
    if (index >= 0) duplicateNotices.value.splice(index, 1, duplicate);
    else duplicateNotices.value.push(duplicate);
    return true;
  }

  function dismissDuplicate(duplicate: EntryDuplicateNotice) {
    duplicateNotices.value = duplicateNotices.value.filter(item => item !== duplicate);
  }

  function clearDuplicateNotices() {
    duplicateNotices.value = [];
  }

  async function openDuplicate(duplicate: EntryDuplicateNotice): Promise<boolean> {
    if (!duplicate.id || !duplicate.knowledgeBaseId) return false;
    if (route.name === 'entries') {
      window.dispatchEvent(new CustomEvent('reliquary:open-entry', {
        detail: { kbId: duplicate.knowledgeBaseId, entryId: duplicate.id },
      }));
    } else {
      await router.push({
        path: '/platform/entries',
        query: { entry_id: duplicate.id },
      });
    }
    return true;
  }

  return { duplicateNotices, showDuplicate, dismissDuplicate, clearDuplicateNotices, openDuplicate };
}
