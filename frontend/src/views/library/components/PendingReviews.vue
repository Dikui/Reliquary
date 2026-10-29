<script setup lang="ts">
import { formatDateTime } from '@/utils/dateTime'
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState, LibMark, Tag } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import type { ReviewEntry } from '@/api/entry-review'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { isValidURL } from '@/utils/security'
import { entryDetailHref } from '@/utils/bodyReference'
import PendingReviewActions from './PendingReviewActions.vue'

const props = defineProps<{ records: ReviewEntry[]; libraries?: LibraryCatalogItem[]; total: number | null; page: number; loading: boolean; error: string }>()
const emit = defineEmits<{ changed: []; refresh: []; page: [page: number]; 'busy-change': [value: boolean] }>()
const { t, n, locale } = useI18n()
const busy = ref(false)
function busyChanged(value: boolean) { busy.value = value; emit('busy-change', value) }
function originalURL(item: ReviewEntry) {
  const url = item.metadata?.intake?.source_info?.original_url || item.source || ''
  return /^https?:\/\//i.test(url) && isValidURL(url) ? url : ''
}
function library(id?: string) { return props.libraries?.find(item => item.id === id) }
function formatDate(value: string) { return formatDateTime(value, locale.value, { dateStyle: 'medium', timeStyle: 'short' }, value) }
function typeLabel(value: string) { return CONTENT_TYPES.includes(value as ContentType) ? t(`library.contentTypes.${value}`) : value }
onBeforeUnmount(() => emit('busy-change', false))
</script>

<template>
  <section class="pending-reviews" :aria-label="t('review.title')">
    <p class="review-description">{{ t('review.intakeHint') }}</p>
    <ErrorState v-if="error" :title="error"><Button :disabled="busy" @click="emit('refresh')">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <EmptyState v-else-if="!records.length && !error" :title="t('review.emptyIntake')" />
    <ul v-else class="reviews">
      <li v-for="item in records" :key="item.id" class="review-record" :data-review-id="item.id">
        <h3><a :href="entryDetailHref(item.id)">{{ item.title || item.file_name || t('library.untitledDocument') }}</a></h3>
        <p v-if="item.structured_summary?.overview || item.description">{{ item.structured_summary?.overview || item.description }}</p>
        <p v-if="item.related_entry_id" class="review-reason"><b>{{ t('pendingPage.reviewReason') }}</b>{{ t('review.conflictReason') }} <a :href="entryDetailHref(item.related_entry_id)">{{ t('review.relatedArticle') }}</a></p>
        <p v-else-if="item.metadata?.intake?.reason" class="review-reason"><b>{{ t('pendingPage.reviewReason') }}</b>{{ item.metadata.intake.reason }}</p>
        <div class="review-meta"><LibMark v-if="library(item.library_id)" :color="library(item.library_id)!.color" /><span v-if="item.library_name">{{ item.library_name }}</span><Tag v-if="item.content_type">{{ typeLabel(item.content_type) }}</Tag><Tag v-if="item.source_kind === 'mine'">{{ t('library.sourceKinds.mine') }}</Tag><Tag v-if="item.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag><a v-if="originalURL(item)" :href="originalURL(item)" target="_blank" rel="noopener noreferrer">{{ originalURL(item) }}</a><time v-if="item.created_at" :datetime="item.created_at">{{ formatDate(item.created_at) }}</time><span class="mono" :title="item.id">{{ item.id.slice(0, 8) }}</span></div>
        <PendingReviewActions :entry="item" :disabled="busy || loading" @changed="emit('changed')" @busy-change="busyChanged" />
      </li>
    </ul>
    <nav v-if="total && total > 20" class="review-pagination" :aria-label="t('pendingPage.pagination')"><Button :disabled="busy || loading || page === 1" @click="emit('page', page - 1)">{{ t('pendingPage.previous') }}</Button><span class="mono">{{ n(page) }} / {{ n(Math.ceil(total / 20)) }}</span><Button :disabled="busy || loading || page * 20 >= total" @click="emit('page', page + 1)">{{ t('pendingPage.next') }}</Button></nav>
  </section>
</template>

<style scoped>
.pending-reviews { display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; color: var(--text); }
.review-description { margin: 0; color: var(--muted); font-size: var(--text-sm); line-height: var(--leading); }
.reviews { list-style: none; margin: 0; padding: 0; border-top: 2px solid var(--rule); }
.review-record { display: grid; gap: var(--space-2); padding: var(--space-4) 0; border-bottom: 1px solid var(--border); overflow-wrap: anywhere; }
.review-record h3 { margin: 0; color: var(--text); font: 800 var(--text-md)/var(--leading) var(--font); }
.review-record p { margin: 0; font-size: var(--text-sm); }
.review-reason b { color: var(--danger); }
.review-meta { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; color: var(--muted); font-size: var(--text-xs); }
.review-meta .mono, .review-pagination .mono { font-family: var(--mono); }
.review-record a { color: var(--text); text-decoration: underline; text-underline-offset: 3px; }
.review-record a:hover { color: var(--accent); }
.review-pagination { display: flex; flex-wrap: wrap; gap: var(--space-3); align-items: center; }
.pending-reviews :deep(.rq-empty) { padding: var(--space-5) 0; }
</style>
