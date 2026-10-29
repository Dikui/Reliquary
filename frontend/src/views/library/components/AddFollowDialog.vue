<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listEntities } from '@/api/entities'
import { listTopics } from '@/api/topics'
import type { LibraryCatalogItem } from '@/api/library'
import type { FollowTargetType } from '@/types/follows'
import { useFollowStore } from '@/stores/follows'
import { Button, Dialog, EmptyState, Field, Input, Select } from '@/components/ui'
import { toast } from '@/components/ui/feedback'

const props = defineProps<{ libraries: LibraryCatalogItem[]; libraryId: string }>()
const emit = defineEmits<{ added: [] }>()
const { t } = useI18n()
const store = useFollowStore()
const open = ref(false), type = ref<FollowTargetType>('entity'), library = ref(''), query = ref(''), selected = ref('')
const candidates = ref<{ id: string; name: string }[]>([])
const loading = ref(false), saving = ref(false), error = ref('')
const options = computed(() => candidates.value.filter(item => !store.isFollowing(type.value, item.id)))
let sequence = 0
async function search() {
  const current = ++sequence
  selected.value = ''; candidates.value = []; error.value = ''
  if (!open.value || !library.value) return
  loading.value = true
  try {
    if (type.value === 'entity') {
      const response = await listEntities({ library_id: library.value, entity: query.value.trim(), limit: 50 })
      if (!response.success) throw new Error(response.error)
      if (current === sequence) candidates.value = (response.data || []).map(item => ({ id: item.id, name: item.canonical_name }))
    } else {
      const response = await listTopics(library.value)
      if (!response.success) throw new Error(response.message)
      if (current === sequence) candidates.value = (response.data || []).filter(item => item.name.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())).map(item => ({ id: item.id, name: item.name }))
    }
  } catch { if (current === sequence) error.value = t('follows.candidatesFailed') }
  finally { if (current === sequence) loading.value = false }
}
async function save() {
  const item = options.value.find(item => item.id === selected.value)
  if (!item || saving.value || loading.value) return
  saving.value = true; error.value = ''
  try {
    await store.setFollowing(type.value, item.id, item.name, true)
    toast.success(t('follows.followed'))
    open.value = false
    emit('added')
  } catch (cause) {
    const failure = cause as { status?: number; code?: number; error?: { code?: number } } | undefined
    if ([404, 1003].includes(Number(failure?.status || failure?.error?.code || failure?.code))) {
      error.value = t('follows.targetMissing')
      candidates.value = candidates.value.filter(candidate => candidate.id !== item.id)
      selected.value = ''
    } else error.value = t('follows.saveFailed')
  } finally { saving.value = false }
}
watch([open, library, type], ([isOpen, nextLibrary, nextType], [wasOpen, oldLibrary, oldType]) => {
  if (!isOpen) { sequence++; loading.value = false; return }
  if (!wasOpen) {
    const initial = props.libraries.some(item => item.id === props.libraryId) ? props.libraryId : props.libraries[0]?.id || ''
    if (initial !== nextLibrary) { library.value = initial; return }
  }
  if (!wasOpen || nextLibrary !== oldLibrary || nextType !== oldType) { query.value = ''; void search() }
})
onUnmounted(() => { sequence++ })
</script>

<template>
  <Dialog v-model="open" :title="t('follows.add')" :closable="!saving" :close-on-esc-keydown="!saving" :close-on-overlay-click="!saving">
    <template #trigger><Button :disabled="!libraries.length">{{ t('follows.add') }}</Button></template>
    <Field :label="t('follows.targetType')"><Select v-model="type" :disabled="saving" :aria-label="t('follows.targetType')"><option value="entity">{{ t('follows.targetTypes.entity') }}</option><option value="topic">{{ t('follows.targetTypes.topic') }}</option></Select></Field>
    <Field :label="t('follows.library')" :hint="t('follows.libraryScopeHint')"><Select v-model="library" :disabled="saving" :aria-label="t('follows.library')"><option v-for="item in libraries" :key="item.id" :value="item.id">{{ item.display_name || item.name }}</option></Select></Field>
    <form class="follow-search" @submit.prevent="search">
      <Field :label="t('follows.searchName')"><Input v-model="query" :disabled="saving" :aria-label="t('follows.searchName')" /></Field>
      <Button type="submit" :disabled="saving || loading || !library" :loading="loading">{{ t('follows.search') }}</Button>
    </form>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <Field v-else-if="options.length" :label="t('follows.name')" :hint="type === 'entity' && candidates.length === 50 ? t('follows.searchLimit') : undefined"><Select v-model="selected" :disabled="saving" :aria-label="t('follows.name')"><option value="" disabled>{{ t('follows.chooseTarget') }}</option><option v-for="item in options" :key="item.id" :value="item.id" :title="item.id">{{ item.name }} ({{ item.id.slice(0, 8) }})</option></Select></Field>
    <EmptyState v-else-if="!error" class="follow-add-empty" :title="t('follows.noCandidates')" />
    <p v-if="error" role="alert" class="follow-add-error">{{ error }}</p>
    <template #footer><Button :disabled="saving" @click="open = false">{{ t('common.cancel') }}</Button><Button variant="primary" :disabled="!selected || loading || saving" :loading="saving" @click="save">{{ t('follows.follow') }}</Button></template>
  </Dialog>
</template>

<style scoped>
.follow-search { display: flex; align-items: end; gap: var(--space-2); }
.follow-search .rq-field { flex: 1; }
.follow-add-error { margin: 0; color: var(--danger); font-size: var(--text-sm); }
.follow-add-empty { padding: var(--space-3) 0; }
.follow-search :deep(.rq-control) { min-width: 0; }
@media (max-width: 480px) { .follow-search { flex-wrap: wrap; } }
</style>
