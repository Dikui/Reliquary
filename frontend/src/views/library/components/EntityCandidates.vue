<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { getEntityCandidate, listEntities, listEntityCandidates, reviewEntityCandidate, type CandidateDetail, type CandidateStatus, type Entity, type EntityCandidate, type EntityEnvelope } from '@/api/entities'

const emit = defineEmits<{ changed: [] }>()
const { t, locale } = useI18n()
const status = ref<CandidateStatus>('pending'), items = ref<EntityCandidate[]>([]), total = ref(0)
const loading = ref(false), error = ref(''), selected = ref<EntityCandidate | null>(null)
const detail = ref<CandidateDetail | null>(null), detailLoading = ref(false), detailError = ref('')
const targets = ref<Entity[]>([]), targetsLoading = ref(false), targetsError = ref(''), moreTargets = ref(false), targetId = ref('')
const busy = ref(false), actionError = ref('')
const statusOptions = computed(() => [{ value: 'pending', label: t('entityCandidates.pending') }, { value: 'ignored', label: t('entityCandidates.ignored') }])
const targetOptions = computed(() => targets.value.map(item => ({ value: item.id, label: item.canonical_name })))
let listRequest = 0, detailRequest = 0, targetRequest = 0, active = true
function data<T>(response: EntityEnvelope<T>): T {
  if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
  return response.data
}
function message(cause: unknown) { return cause instanceof Error ? cause.message : t('entityMaintenance.requestFailed') }
async function load(more = false) {
  if (more && loading.value) return
  const current = ++listRequest
  loading.value = true; error.value = ''
  try {
    const page = data(await listEntityCandidates(status.value, 50, more ? items.value.length : 0))
    if (current === listRequest) { items.value = more ? [...items.value, ...page.items] : page.items; total.value = page.total }
  } catch (cause) { if (current === listRequest) error.value = message(cause) }
  finally { if (current === listRequest) loading.value = false }
}
async function select(item: EntityCandidate, more = false) {
  if (busy.value || (more && detailLoading.value)) return
  const current = ++detailRequest
  if (!more) { selected.value = item; detail.value = null; targetId.value = ''; actionError.value = '' }
  detailLoading.value = true; detailError.value = ''
  try {
    const result = data(await getEntityCandidate(item.id, 50, more ? detail.value?.occurrences.length || 0 : 0))
    if (current === detailRequest) detail.value = { ...result, occurrences: more ? [...(detail.value?.occurrences || []), ...result.occurrences] : result.occurrences }
  } catch (cause) { if (current === detailRequest) detailError.value = message(cause) }
  finally { if (current === detailRequest) detailLoading.value = false }
}
async function loadTargets(more = false) {
  if (more && targetsLoading.value) return
  const current = ++targetRequest
  targetsLoading.value = true; targetsError.value = ''
  try {
    const page = data(await listEntities({ limit: 50, offset: more ? targets.value.length : 0 }))
    if (current === targetRequest) { targets.value = more ? [...targets.value, ...page] : page; moreTargets.value = page.length === 50 }
  } catch (cause) { if (current === targetRequest) targetsError.value = message(cause) }
  finally { if (current === targetRequest) targetsLoading.value = false }
}
async function review(action: 'approve' | 'alias' | 'ignore') {
  if (busy.value || !selected.value || (action === 'alias' && !targetId.value)) return
  busy.value = true; actionError.value = ''
  try {
    const reviewedId = selected.value.id
    data(await reviewEntityCandidate(reviewedId, action, action === 'alias' ? targetId.value : undefined))
    if (!active) return
    items.value = items.value.filter(item => item.id !== reviewedId); total.value = Math.max(0, total.value - 1)
    selected.value = null; detail.value = null; detailRequest++
    MessagePlugin.success(action === 'approve' ? t('entityCandidates.approveSaved') : action === 'alias' ? t('entityCandidates.aliasSaved') : t('entityCandidates.ignoreSaved')); emit('changed')
    await Promise.all([load(), loadTargets()])
  } catch (cause) { if (active) actionError.value = message(cause) }
  finally { if (active) busy.value = false }
}
watch(status, () => { selected.value = null; detail.value = null; detailRequest++; items.value = []; total.value = 0; void load() })
onMounted(() => { void load(); void loadTargets() })
onBeforeUnmount(() => { active = false; listRequest++; detailRequest++; targetRequest++ })
</script>

<template>
  <section class="entity-candidates" :aria-label="t('entityCandidates.title')">
    <header><div><h3>{{ t('entityCandidates.title') }}</h3><p>{{ t('entityCandidates.description') }}</p></div><div class="candidate-actions"><t-select v-model="status" :options="statusOptions" :disabled="busy" :aria-label="t('entityCandidates.status')" /><t-button variant="outline" :loading="loading" :disabled="busy" @click="load()">{{ t('common.refresh') }}</t-button></div></header>
    <p v-if="error" role="alert">{{ error }} <t-button variant="text" :loading="loading" @click="load()">{{ t('common.retry') }}</t-button></p>
    <t-loading :loading="loading" show-overlay>
      <t-empty v-if="!items.length && !loading && !error" :description="t('entityCandidates.empty')" />
      <ol class="candidate-list"><li v-for="item in items" :key="item.id"><t-button variant="text" :disabled="busy" :aria-pressed="selected?.id === item.id" @click="select(item)">{{ item.name }}</t-button><span>{{ t(item.entity_type === 'person' ? 'entityMaintenance.person' : 'entityMaintenance.organization') }} · {{ t('entityCandidates.count', { count: item.occurrence_count.toLocaleString(locale) }) }}</span></li></ol>
      <t-button v-if="items.length < total" variant="text" :loading="loading" :disabled="busy" @click="load(true)">{{ t('common.loadMore') }}</t-button>
    </t-loading>
    <section v-if="selected" class="candidate-detail" :aria-label="selected.name" :aria-busy="busy">
      <h4>{{ selected.name }}</h4>
      <p v-if="detailError" role="alert">{{ detailError }} <t-button variant="text" :loading="detailLoading" @click="select(selected)">{{ t('common.retry') }}</t-button></p>
      <t-loading :loading="detailLoading">
        <ul>
          <li v-for="occurrence in detail?.occurrences" :key="occurrence.entry ? `entry:${occurrence.entry.id}` : `fact:${occurrence.fact?.id}:${occurrence.endpoint}`">
            <template v-if="occurrence.entry">
              <a :href="`/platform/entries?entry_id=${encodeURIComponent(occurrence.entry.id)}`">{{ occurrence.entry.title }}</a>
              <p>{{ occurrence.quote }}</p>
            </template>
            <template v-else-if="occurrence.fact">
              <a :href="`/platform/stories/${encodeURIComponent(occurrence.fact.story_id)}#fact-${encodeURIComponent(occurrence.fact.id)}`">{{ occurrence.fact.title }}</a><span>{{ t(occurrence.endpoint === 'subject' ? 'entityCandidates.subject' : 'entityCandidates.object') }}</span><p>{{ occurrence.fact.subject }} · {{ occurrence.fact.action }} · {{ occurrence.fact.object }}</p>
            </template>
          </li>
        </ul>
        <t-button v-if="detail && detail.occurrences.length < detail.total" variant="text" :loading="detailLoading" :disabled="busy" @click="select(selected, true)">{{ t('common.loadMore') }}</t-button>
      </t-loading>
      <div class="candidate-actions"><t-button :loading="busy" :disabled="busy" @click="review('approve')">{{ t('entityCandidates.approve') }}</t-button><t-button v-if="status === 'pending'" variant="outline" :disabled="busy" @click="review('ignore')">{{ t('entityCandidates.ignore') }}</t-button></div>
      <p>{{ t('entityCandidates.ignoreHint') }}</p>
      <label class="candidate-target"><span>{{ t('entityCandidates.target') }}</span><t-select v-model="targetId" filterable :options="targetOptions" :loading="targetsLoading" :disabled="busy" :aria-label="t('entityCandidates.target')" /></label>
      <p v-if="targetsError" role="alert">{{ targetsError }} <t-button variant="text" :loading="targetsLoading" @click="loadTargets()">{{ t('common.retry') }}</t-button></p>
      <div class="candidate-actions"><t-button v-if="moreTargets" variant="text" :loading="targetsLoading" :disabled="busy" @click="loadTargets(true)">{{ t('common.loadMore') }}</t-button><t-button variant="outline" :loading="busy" :disabled="!targetId || busy || targetsLoading" @click="review('alias')">{{ t('entityCandidates.alias') }}</t-button></div>
      <p v-if="actionError" role="alert">{{ actionError }}</p>
    </section>
  </section>
</template>

<style scoped>
.entity-candidates { margin-top: 24px; color: var(--td-text-color-primary); }
header, .candidate-actions { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
header h3, h4 { margin: 0 0 8px; font-size: 16px; }
p, li > span { color: var(--td-text-color-secondary); line-height: 1.65; overflow-wrap: anywhere; }
.candidate-actions { justify-content: flex-start; }
.candidate-actions :deep(.t-select__wrap) { width: 180px; }
ol, ul { list-style: none; margin: 16px 0; padding: 0; }
.candidate-list { min-height: 80px; }
li { padding: 12px 0; border-bottom: 1px solid var(--td-component-stroke); min-width: 0; overflow-wrap: anywhere; }
li > span { margin-left: 12px; font-size: 13px; }
li :deep(.t-button), li :deep(.t-button__text) { height: auto; max-width: 100%; white-space: normal; overflow-wrap: anywhere; }
.candidate-detail { margin-top: 16px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.candidate-target { display: grid; gap: 8px; max-width: 420px; margin: 16px 0; }
a { color: var(--td-brand-color); }
a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
p[role="alert"] { color: var(--td-error-color); }
@media (max-width: 720px) { header { flex-direction: column; } .candidate-detail { padding: 12px; } }
</style>
