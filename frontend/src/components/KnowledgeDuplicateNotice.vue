<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { knowledgeDuplicateMessageKey, type KnowledgeDuplicateNotice } from '../utils/knowledgeDuplicate';

const props = defineProps<{ duplicate: KnowledgeDuplicateNotice }>();
const emit = defineEmits<{ open: [duplicate: KnowledgeDuplicateNotice]; dismiss: [duplicate: KnowledgeDuplicateNotice] }>();
const { t } = useI18n();
const message = computed(() => t(knowledgeDuplicateMessageKey(props.duplicate)));
</script>

<template>
  <t-alert class="knowledge-duplicate-notice" theme="info" role="status" aria-live="polite">
    <span>{{ message }}</span>
    <span class="knowledge-duplicate-notice__title">{{ duplicate.title || duplicate.inputLabel }}</span>
    <template #operation>
      <div class="knowledge-duplicate-notice__actions">
        <t-button v-if="duplicate.id && duplicate.knowledgeBaseId" variant="text" size="small"
          @click="emit('open', duplicate)">{{ t('knowledgeBase.viewExistingMessage') }}</t-button>
        <t-button theme="default" variant="text" size="small"
          @click="emit('dismiss', duplicate)">{{ t('knowledgeBase.dismissDuplicateNotice') }}</t-button>
      </div>
    </template>
  </t-alert>
</template>

<style scoped>
.knowledge-duplicate-notice {
  min-width: 0;
}
.knowledge-duplicate-notice__title {
  display: block;
  overflow-wrap: anywhere;
}
.knowledge-duplicate-notice__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
@media (max-width: 640px) {
  .knowledge-duplicate-notice :deep(.t-alert__message) {
    flex-direction: column;
    gap: 4px;
  }
  .knowledge-duplicate-notice :deep(.t-alert__operation) {
    margin-left: 0;
  }
}
</style>
