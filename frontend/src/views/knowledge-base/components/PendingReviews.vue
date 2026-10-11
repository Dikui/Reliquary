<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPendingEntry, type ReviewKind, type ReviewEntry } from '@/api/entry-review'
import { isValidURL } from '@/utils/security'
import ReviewActions from '@/components/ReviewActions.vue'

const emit = defineEmits<{ 'open-source': [id: string]; changed: [] }>()
const { t } = useI18n()
const kind = ref<ReviewKind>('intake'), records = ref<ReviewEntry[]>([]), total = ref(0), page = ref(1)
const loading = ref(false), error = ref(''), busy = ref(false)
const pageSize = 20
let generation = 0
function originalURL(item: ReviewEntry) { const url = item.metadata?.intake?.source_info?.original_url || ''; return /^https?:\/\//i.test(url) && isValidURL(url) ? url : '' }
async function load() {
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const response = await listPendingEntry({ kind: kind.value, page: page.value, page_size: pageSize })
    if (!response.success) throw new Error(response.message || t('knowledgeReview.loadFailed'))
    if (current !== generation) return
    records.value = response.data || []; total.value = response.total || 0
    if (!records.value.length && page.value > 1) { page.value--; await load() }
  } catch (cause: any) { if (current === generation) error.value = cause?.message || t('knowledgeReview.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function changeKind(value: string | number) { generation++; kind.value = value as ReviewKind; page.value = 1; records.value = []; total.value = 0; void load() }
function changePage(value: { current: number }) { page.value = value.current; void load() }
async function reviewed() { emit('changed'); await load() }
onMounted(load)
onUnmounted(() => { generation++ })
defineExpose({ refresh: load })
</script>

<template>
  <section class="pending-reviews" :aria-label="t('knowledgeReview.title')">
    <div class="review-heading"><h3>{{ t('knowledgeReview.title') }}</h3><t-button variant="outline" :loading="loading" :disabled="busy" @click="load">{{ t('knowledgeWorkbench.refresh') }}</t-button></div>
    <t-tabs :value="kind" @change="changeKind">
      <t-tab-panel value="intake" :label="t('knowledgeReview.intake')" :disabled="busy" />
      <t-tab-panel value="processing" :label="t('knowledgeReview.processing')" :disabled="busy" />
    </t-tabs>
    <p class="review-description">{{ t(kind === 'intake' ? 'knowledgeReview.intakeHint' : 'knowledgeReview.processingHint') }}</p>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" @click="load">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <div class="review-list">
        <t-empty v-if="!records.length && !loading && !error" :description="t(kind === 'intake' ? 'knowledgeReview.emptyIntake' : 'knowledgeReview.emptyProcessing')" />
        <article v-for="item in records" :key="item.id" class="review-record" :data-review-id="item.id">
          <div class="review-record-heading"><h4>{{ item.title || item.file_name || t('knowledgeBase.untitledDocument') }}</h4><t-button variant="text" :disabled="busy" @click="emit('open-source', item.id)">{{ t('knowledgeBase.intakePreviewOriginal') }}</t-button></div>
          <p v-if="kind === 'intake' && item.metadata?.intake?.reason">{{ item.metadata.intake.reason }}</p>
          <a v-if="originalURL(item)" :href="originalURL(item)" target="_blank" rel="noopener noreferrer">{{ originalURL(item) }}</a>
          <p v-if="item.metadata?.intake?.source_info?.original_url_source === 'from_file'" class="review-link-source">{{ t('knowledgeReview.extractedLink') }}</p>
          <ReviewActions :entry="item" @changed="reviewed" @busy-change="busy = $event" />
        </article>
      </div>
    </t-loading>
    <t-pagination v-if="total" :current="page" :page-size="pageSize" :total="total" :show-page-size="false" :disabled="busy || loading" @change="changePage" />
  </section>
</template>

<style scoped lang="less">
.pending-reviews { flex: 1; min-height: 0; overflow-y: auto; scrollbar-gutter: stable; padding: 0 0 24px; min-width: 0; color: var(--td-text-color-primary); }
.review-heading, .review-record-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.review-heading h3, .review-record h4 { margin: 0; }
.review-heading { margin-bottom: 16px; }
.review-description, .review-record > p { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; }
.review-list { display: grid; gap: 16px; min-height: 160px; margin: 16px 0; }
.review-record { display: grid; gap: 12px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); overflow-wrap: anywhere; min-width: 0; }
.review-record p { margin: 0; }
.review-record a { color: var(--td-brand-color); }
.review-record a:hover { text-decoration: underline; }
@media (max-width: 560px) { .review-record { padding: 14px; } .review-record-heading { flex-direction: column; gap: 8px; } }
</style>
