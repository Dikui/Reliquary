<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPendingEntry, type ReviewEntry } from '@/api/entry-review'
import { isValidURL } from '@/utils/security'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import LibraryReviewAction from '@/components/LibraryReviewAction.vue'
import type { LibraryCatalogItem } from '@/api/library'
import { useLibraryScope } from '@/composables/useLibraryScope'
import ReviewActions from '@/components/ReviewActions.vue'

const emit = defineEmits<{ 'open-source': [id: string]; changed: []; refreshed: [] }>()
const { t } = useI18n()
const libraryScope = useLibraryScope()
const libraries = ref<LibraryCatalogItem[]>([])
const selectedLibrary = computed(() => libraryScope.value.length === 1 ? libraries.value.find(item => item.id === libraryScope.value[0]) : undefined)
const records = ref<ReviewEntry[]>([]), total = ref(0), page = ref(1)
const loading = ref(false), error = ref(''), busyIds = ref(new Set<string>())
const busy = computed(() => busyIds.value.size > 0)
const pageSize = 20
let generation = 0
function safeURL(url: string) { return /^https?:\/\//i.test(url) && isValidURL(url) ? url : '' }
function originalURL(item: ReviewEntry) { return safeURL(item.metadata?.intake?.source_info?.original_url || item.source || '') }
function articleLink(id: string) { return `/platform/entries?entry_id=${encodeURIComponent(id)}` }
async function load() {
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const response = await listPendingEntry({ libraries: libraryScope.value, kind: 'intake', page: page.value, page_size: pageSize })
    if (!response.success) throw new Error(response.message || t('knowledgeReview.loadFailed'))
    if (current !== generation) return
    records.value = (response.data || []) as ReviewEntry[]
    total.value = response.total || 0
    if (!response.data?.length && page.value > 1) { page.value--; await load() }
  } catch (cause: any) { if (current === generation) error.value = cause?.message || t('knowledgeReview.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function changePage(value: { current: number }) { page.value = value.current; void load() }
function setBusy(id: string, value: boolean) { if (value) busyIds.value.add(id); else busyIds.value.delete(id) }
async function reviewed() { emit('changed'); await load() }
async function refresh() { if (busy.value) return; emit('refreshed'); await load() }
watch(libraryScope, () => { page.value = 1; records.value = []; void load() }, { deep: true })
onMounted(load)
onUnmounted(() => { generation++ })
defineExpose({ refresh: load })
</script>

<template>
  <section class="pending-reviews" :aria-label="t('knowledgeReview.title')">
    <div class="review-heading"><h3>{{ t('knowledgeReview.title') }}</h3><t-button variant="outline" :loading="loading" :disabled="busy" @click="refresh">{{ t('knowledgeWorkbench.refresh') }}</t-button></div>
    <div class="review-scope-controls">
      <LibraryScopeSelect v-model="libraryScope" :disabled="busy" @loaded="libraries = $event" />
      <LibraryReviewAction :library="selectedLibrary" scope="pending" :disabled="busy" @submitted="refresh" />
    </div>
    <p class="review-description">{{ t('knowledgeReview.intakeHint') }}</p>
    <p v-if="selectedLibrary" class="review-description">{{ t('ikb18.currentReviewCriteria') }} {{ selectedLibrary.review_criteria }}</p>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" :disabled="busy" @click="refresh">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <div class="review-list">
        <t-empty v-if="!records.length && !loading && !error" :description="t('knowledgeReview.emptyIntake')" />
        <article v-for="item in records" :key="item.id" class="review-record" :data-review-id="item.id">
          <div class="review-record-heading"><h4>{{ item.title || item.file_name || t('library.untitledDocument') }}</h4><t-button variant="text" :disabled="busy" @click="emit('open-source', item.id)">{{ t('library.intakePreviewOriginal') }}</t-button></div>
          <t-tag v-if="item.library_name" size="small" variant="light-outline">{{ item.library_name }}</t-tag>
          <t-tag v-if="item.source_kind === 'mine'" class="acquisition-tag" size="small" variant="light-outline">{{ t('library.sourceKinds.mine') }}</t-tag>
          <t-tag v-if="item.acquisition === 'model_written'" class="acquisition-tag" size="small" variant="light-outline" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</t-tag>
          <p v-if="item.related_entry_id">{{ t('knowledgeReview.conflictReason') }} <a :href="articleLink(item.related_entry_id)" @click.prevent="emit('open-source', item.related_entry_id)">{{ t('knowledgeReview.relatedArticle') }}</a></p>
          <p v-else-if="item.metadata?.intake?.reason">{{ item.metadata.intake.reason }}</p>
          <a v-if="originalURL(item)" :href="originalURL(item)" target="_blank" rel="noopener noreferrer">{{ originalURL(item) }}</a>
          <ReviewActions :entry="item" @changed="reviewed" @busy-change="setBusy(item.id, $event)" />
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
.review-scope-controls { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; }
.review-scope-controls > .library-scope { flex: 1 1 220px; }
.review-heading { margin-bottom: 16px; }
.review-description, .review-record > p { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; }
.review-list { display: grid; gap: 16px; min-height: 160px; margin: 16px 0; }
.review-record { display: grid; gap: 12px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); overflow-wrap: anywhere; min-width: 0; }
.review-record p { margin: 0; }
.review-record a { color: var(--td-brand-color); }
.review-record a:hover { text-decoration: underline; }
.review-record a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.acquisition-tag { justify-self: start; }
@media (max-width: 560px) { .review-record { padding: 14px; } .review-record-heading { flex-direction: column; gap: 8px; } }
</style>
