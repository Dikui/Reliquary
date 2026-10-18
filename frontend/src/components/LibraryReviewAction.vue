<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { LibraryCatalogItem, LibraryReviewScope } from '@/api/library'
import EntryBatchDialog from '@/components/EntryBatchDialog.vue'
const props = defineProps<{ library?: LibraryCatalogItem; scope: LibraryReviewScope; disabled?: boolean }>()
const emit = defineEmits<{ submitted: [] }>()
const route = useRoute(), router = useRouter()
function created(id: string) { visible.value = false; void router.replace({ path: '/platform/entries', query: { ...route.query, batch_id: id } }); }
const { t } = useI18n()
const visible = ref(false), target = ref<LibraryCatalogItem>()
const label = computed(() => t(props.scope === 'all' ? 'knowledgeReview.reviewAll' : 'knowledgeReview.reviewPending'))
const unavailable = computed(() => props.disabled || !props.library || props.library.template === 'generated')
function open() { if (!unavailable.value) { target.value = props.library; visible.value = true } }
</script>
<template>
  <span v-if="library?.template !== 'generated'" class="library-review-action"><t-tooltip :content="library ? (library.display_name || library.name) : t('library.rebuildLibrarySelectOne')"><span><t-button variant="text" size="small" :disabled="unavailable" @click="open">{{ label }}</t-button></span></t-tooltip>
    <EntryBatchDialog v-if="target" v-model:visible="visible" initial-action="review" :libraries="[target]" :scope-label="`${target.display_name || target.name} · ${t(scope === 'all' ? 'entryBatch.wholeLibraryReview' : 'entryBatch.wholeLibraryPending')}`" :selection="{ filters: { libraries: [target.id], include_rejected: true, ...(scope === 'pending' ? { parse_status: 'rejected' } : {}) } }" @created="created" @changed="emit('submitted')" />
  </span>
</template>
<style scoped>.library-review-action { display: inline-flex; flex-shrink: 0; }</style>
