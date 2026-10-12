<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { entryDuplicateMessageKey, type EntryDuplicateNotice } from '../utils/entryDuplicate';

const props = defineProps<{ duplicate: EntryDuplicateNotice }>();
const emit = defineEmits<{ open: [duplicate: EntryDuplicateNotice]; dismiss: [duplicate: EntryDuplicateNotice] }>();
const { t } = useI18n();
const message = computed(() => t(entryDuplicateMessageKey(props.duplicate)));
</script>

<template>
  <t-alert class="entry-duplicate-notice" theme="info" role="status" aria-live="polite">
    <span>{{ message }}</span>
    <span class="entry-duplicate-notice__title">{{ duplicate.title || duplicate.inputLabel }}</span>
    <template #operation>
      <div class="entry-duplicate-notice__actions">
        <t-button v-if="duplicate.id && duplicate.knowledgeBaseId" variant="text" size="small"
          @click="emit('open', duplicate)">{{ t('knowledgeBase.viewExistingMessage') }}</t-button>
        <t-button theme="default" variant="text" size="small"
          @click="emit('dismiss', duplicate)">{{ t('knowledgeBase.dismissDuplicateNotice') }}</t-button>
      </div>
    </template>
  </t-alert>
</template>

<style scoped>
.entry-duplicate-notice {
  min-width: 0;
}
.entry-duplicate-notice__title {
  display: block;
  overflow-wrap: anywhere;
}
.entry-duplicate-notice__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
@media (max-width: 640px) {
  .entry-duplicate-notice :deep(.t-alert__message) {
    flex-direction: column;
    gap: 4px;
  }
  .entry-duplicate-notice :deep(.t-alert__operation) {
    margin-left: 0;
  }
}
</style>
