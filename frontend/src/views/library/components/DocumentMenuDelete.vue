<script setup lang="ts">
import { computed, nextTick, ref, shallowRef } from 'vue';
import { useI18n } from 'vue-i18n';
import { MessagePlugin } from 'tdesign-vue-next';
import { deleteDocument } from '@/api/library';
import DocumentDeleteConfirm from '@/components/DocumentDeleteConfirm.vue';

interface DeleteItem {
  id: string;
  source_kind?: string;
  file_name?: string;
  title?: string;
}

const emit = defineEmits<{ deleted: [id: string] }>();
const { t } = useI18n();
const target = shallowRef<DeleteItem | null>(null);
const confirmDialog = ref<InstanceType<typeof DocumentDeleteConfirm> | null>(null);
const busy = ref(false);
let deleteArticle: (() => Promise<boolean>) | undefined;
const isPersonal = computed(() => target.value?.source_kind === 'mine');
const title = computed(() => target.value?.file_name || target.value?.title || target.value?.id || '');
const label = computed(() => t(isPersonal.value ? 'personalDocument.delete' : 'library.deleteDocument'));
const confirmation = computed(() => isPersonal.value
  ? t('personalDocument.deleteConfirm', { title: title.value })
  : t('library.confirmDeleteDocument', { fileName: title.value }));
const failureMessage = computed(() => t(isPersonal.value ? 'personalDocument.deleteFailed' : 'library.deleteFailed'));

async function open(item: DeleteItem, removeArticle?: () => Promise<boolean>) {
  if (busy.value) return;
  target.value = item;
  deleteArticle = removeArticle;
  await nextTick();
  confirmDialog.value?.open();
}

async function remove() {
  const item = target.value;
  if (!item) return;
  if (item.source_kind === 'mine') {
    const result = await deleteDocument(item.id);
    if (!result.success) throw new Error(result.message || failureMessage.value);
  } else if (!deleteArticle || !await deleteArticle()) {
    throw new Error(failureMessage.value);
  }
}

function deleted(id: string) {
  if (isPersonal.value) {
    MessagePlugin.success(t('personalDocument.deleted'));
    emit('deleted', id);
  }
}

defineExpose({ open });
</script>

<template>
  <DocumentDeleteConfirm ref="confirmDialog" :document-id="target?.id || ''" :label="label"
    :confirmation="confirmation" :failure-message="failureMessage" :hide-trigger="true"
    :delete-action="remove" @deleted="deleted" @busy-change="busy = $event" />
</template>
