<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPendingEntry, type ReviewKind, type ReviewEntry } from '@/api/entry-review'
import { listIncompleteEntrys } from '@/api/ingest'
import type { IncompleteEntry } from '@/types/ingest'
import { isValidURL } from '@/utils/security'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import LibraryReviewAction from '@/components/LibraryReviewAction.vue'
import type { LibraryCatalogItem } from '@/api/library'
import { useLibraryScope } from '@/composables/useLibraryScope'
import ReviewActions from '@/components/ReviewActions.vue'
import MissingMaterials from '@/components/MissingMaterials.vue'
import IncompleteActions from '@/components/IncompleteActions.vue'

const emit = defineEmits<{ 'open-source': [id: string]; changed: []; refreshed: [] }>()
const { t } = useI18n()
const libraryScope = useLibraryScope()
const libraries = ref<LibraryCatalogItem[]>([])
const selectedLibrary = computed(() => libraryScope.value.length === 1 ? libraries.value.find(item => item.id === libraryScope.value[0]) : undefined)
type PendingKind = ReviewKind | 'incomplete'
const kind = ref<PendingKind>('intake'), records = ref<ReviewEntry[]>([]), incompleteRecords = ref<IncompleteEntry[]>([]), total = ref(0), page = ref(1)
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
    const selected = kind.value
    const response = selected === 'incomplete'
      ? await listIncompleteEntrys({ page: page.value, libraries: libraryScope.value, page_size: pageSize, include_attempted: true })
      : await listPendingEntry({ libraries: libraryScope.value, kind: selected, page: page.value, page_size: pageSize })
    if (!response.success) throw new Error(response.message || t('knowledgeReview.loadFailed'))
    if (current !== generation) return
    if (selected === 'incomplete') incompleteRecords.value = (response.data || []) as IncompleteEntry[]
    else records.value = (response.data || []) as ReviewEntry[]
    total.value = response.total || 0
    if (!response.data?.length && page.value > 1) { page.value--; await load() }
  } catch (cause: any) { if (current === generation) error.value = cause?.message || t('knowledgeReview.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function changeKind(value: string | number) {
  if (busy.value || !['intake', 'incomplete'].includes(String(value))) return
  generation++; kind.value = value as PendingKind; page.value = 1; records.value = []; incompleteRecords.value = []; total.value = 0; void load()
}
function changePage(value: { current: number }) { page.value = value.current; void load() }
function setBusy(id: string, value: boolean) { if (value) busyIds.value.add(id); else busyIds.value.delete(id) }
async function reviewed() { emit('changed'); await load() }
async function refresh() { if (busy.value) return; emit('refreshed'); await load() }
watch(libraryScope, () => { page.value = 1; records.value = []; incompleteRecords.value = []; void load() }, { deep: true })
onMounted(load)
onUnmounted(() => { generation++ })
defineExpose({ refresh: load })
</script>

<template>
  <section class="pending-reviews" :aria-label="t('knowledgeReview.title')">
    <div class="review-heading"><h3>{{ t('knowledgeReview.title') }}</h3><t-button variant="outline" :loading="loading" :disabled="busy" @click="refresh">{{ t('knowledgeWorkbench.refresh') }}</t-button></div>
    <div class="review-scope-controls">
      <LibraryScopeSelect v-model="libraryScope" :disabled="busy" @loaded="libraries = $event" />
      <LibraryReviewAction v-if="kind === 'intake'" :library="selectedLibrary" scope="pending" :disabled="busy" @submitted="refresh" />
    </div>
    <t-tabs :value="kind" @change="changeKind">
      <t-tab-panel value="intake" :label="t('knowledgeReview.intake')" :disabled="busy" />
      <t-tab-panel value="incomplete" :label="t('knowledgeReview.incomplete')" :disabled="busy" />
    </t-tabs>
    <p class="review-description">{{ t(kind === 'incomplete' ? 'ikb18.incompleteHint' : 'knowledgeReview.intakeHint') }}</p>
    <p v-if="kind === 'intake' && selectedLibrary" class="review-description">{{ t('ikb18.currentReviewCriteria') }} {{ selectedLibrary.review_criteria }}</p>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" :disabled="busy" @click="refresh">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <div class="review-list">
        <t-empty v-if="!(kind === 'incomplete' ? incompleteRecords.length : records.length) && !loading && !error" :description="t(kind === 'intake' ? 'knowledgeReview.emptyIntake' : 'knowledgeReview.emptyIncomplete')" />
        <article v-for="item in records" :key="item.id" class="review-record" :data-review-id="item.id">
          <div class="review-record-heading"><h4>{{ item.title || item.file_name || t('library.untitledDocument') }}</h4><t-button variant="text" :disabled="busy" @click="emit('open-source', item.id)">{{ t('library.intakePreviewOriginal') }}</t-button></div>
          <t-tag v-if="item.library_name" size="small" variant="light-outline">{{ item.library_name }}</t-tag>
          <t-tag v-if="item.source_kind === 'mine'" class="acquisition-tag" size="small" variant="light-outline">{{ t('library.sourceKinds.mine') }}</t-tag>
          <t-tag v-if="kind === 'intake' && item.acquisition === 'model_written'" class="acquisition-tag" size="small" variant="light-outline" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</t-tag>
          <p v-if="kind === 'intake' && item.related_entry_id">{{ t('knowledgeReview.conflictReason') }} <a :href="articleLink(item.related_entry_id)" @click.prevent="emit('open-source', item.related_entry_id)">{{ t('knowledgeReview.relatedArticle') }}</a></p>
          <p v-else-if="kind === 'intake' && item.metadata?.intake?.reason">{{ item.metadata.intake.reason }}</p>
          <a v-if="originalURL(item)" :href="originalURL(item)" target="_blank" rel="noopener noreferrer">{{ originalURL(item) }}</a>
          <ReviewActions :entry="item" @changed="reviewed" @busy-change="setBusy(item.id, $event)" />
        </article>
        <article v-for="item in incompleteRecords" :key="item.entry_id" class="review-record" :data-incomplete-id="item.entry_id">
          <t-tag v-if="item.library_name" size="small" variant="light-outline">{{ item.library_name }}</t-tag>
          <t-tag v-if="item.library_template === 'own'" class="acquisition-tag" size="small" variant="light-outline">{{ t('library.sourceKinds.mine') }}</t-tag>
          <div class="review-record-heading"><h4><a :href="articleLink(item.entry_id)" @click.prevent="emit('open-source', item.entry_id)">{{ item.title || t('library.untitledDocument') }}</a></h4><t-tag v-if="item.external_completion_attempted" size="small" theme="warning" variant="light-outline">{{ t('ingestStatus.attemptedTitle') }}</t-tag></div>
          <a v-if="safeURL(item.url)" :href="safeURL(item.url)" target="_blank" rel="noopener noreferrer">{{ item.url }}</a>
          <p>{{ t('ingestStatus.acquisitionLabel') }}: {{ item.acquisition ? t(`ingestStatus.acquisition.${item.acquisition}`) : t('ingestStatus.unknownAcquisition') }}</p>
          <p v-if="item.external_completion_attempted">{{ t('ingestStatus.attemptedHint') }}</p>
          <MissingMaterials :items="item.missing || []" />
          <IncompleteActions :article-id="item.entry_id" :title="item.title" allow-discard @changed="reviewed" @busy-change="setBusy(item.entry_id, $event)" />
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
