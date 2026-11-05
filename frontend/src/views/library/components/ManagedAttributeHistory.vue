<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog } from '@/components/ui'
import { getEntityAttributes, getAttributeHistory, getEntityReorganizations, type Entity, type AttributeSnapshot, type AttributeVersion, type EntityReorganization } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import ManagedAttributes from './ManagedAttributes.vue'
import { formatDateTime, localDateTimeISO } from '@/utils/dateTime'
const props = defineProps<{ entity: Entity; library: LibraryCatalogItem }>()
const { t, locale } = useI18n()
const queriedId = ref(props.entity.id)
function navigate(id: string) { queriedId.value = id; at.value = ''; snapshot.value = undefined; versions.value = []; lineage.value = []; void load() }
const open = ref(false), at = ref(''), loading = ref(false), error = ref(''), page = ref(1), total = ref(0), snapshot = ref<AttributeSnapshot>(), versions = ref<AttributeVersion[]>([]), lineage = ref<EntityReorganization[]>([])
let request = 0
async function load(nextPage = 1) {
  const current = ++request; open.value = true; loading.value = true; error.value = ''
  try {
    const date = at.value ? localDateTimeISO(at.value) : undefined
    const [values, history, origins] = await Promise.all([getEntityAttributes(queriedId.value, date), getAttributeHistory(queriedId.value, nextPage), getEntityReorganizations(queriedId.value)])
    if (!values.success || !history.success || !origins.success) throw new Error(t('common.operationFailed'))
    if (current !== request) return
    snapshot.value = values.data; versions.value = history.data.items; total.value = history.data.total; page.value = nextPage; lineage.value = origins.data
  } catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
watch(() => props.entity.id, id => { request++; queriedId.value = id; open.value = false; snapshot.value = undefined; versions.value = []; lineage.value = [] })
onBeforeUnmount(() => { request++ })
</script>
<template>
  <Button @click="load()">{{ t('managed.attributeHistory') }}</Button>
  <Dialog v-model="open" :title="t('managed.attributeHistory')">
    <p>{{ t('managed.historyEntity') }}: {{ queriedId }} <Button v-if="queriedId !== entity.id" @click="navigate(entity.id)">{{ t('managed.returnCurrent') }}</Button></p>
    <form class="history-form" novalidate @submit.prevent="load()"><label>{{ t('managed.asOf') }}<input v-model="at" class="rq-control" type="datetime-local"></label><Button type="submit" :loading="loading">{{ t('managed.queryValues') }}</Button></form>
    <p v-if="error" role="alert">{{ error }}</p><p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <section v-if="snapshot"><h3>{{ t(at ? 'managed.historicalValues' : 'managed.currentValues') }}</h3><p v-if="!snapshot.known">{{ t('managed.unknownValues') }}</p><ManagedAttributes v-else :model-value="snapshot.attributes" :fields="library.entity_schema?.fields || []" :library-id="library.id" readonly /></section>
    <h3>{{ t('managed.attributeHistory') }}</h3><p v-if="!versions.length && !loading">{{ t('managed.noHistory') }}</p>
    <details v-for="version in versions" :key="version.id"><summary>{{ t('managed.effectiveAt') }}: {{ formatDateTime(version.effective_at, locale, { dateStyle: 'medium', timeStyle: 'short' }) }} · {{ t('managed.recordedAt') }}: {{ formatDateTime(version.recorded_at, locale, { dateStyle: 'medium', timeStyle: 'short' }) }} · {{ version.revision }}</summary><ManagedAttributes :model-value="version.attributes" :fields="library.entity_schema?.fields || []" :library-id="library.id" readonly /></details>
    <div class="history-form"><Button :disabled="loading || page === 1" @click="load(page - 1)">{{ t('follows.previousPage') }}</Button><span>{{ page }}</span><Button :disabled="loading || page * 20 >= total" @click="load(page + 1)">{{ t('follows.nextPage') }}</Button></div>
    <h3>{{ t('managed.lineage') }}</h3><p v-if="!lineage.length">{{ t('managed.noHistory') }}</p><p v-for="item in lineage" :key="item.id">{{ t(item.kind === 'merge' ? 'managed.merge' : 'managed.split') }} · <Button variant="ghost" @click="navigate(item.source_id)">{{ item.source_id }}</Button> → <Button variant="ghost" @click="navigate(item.target_id)">{{ item.target_id }}</Button> · {{ formatDateTime(item.created_at, locale) }}</p>
  </Dialog>
</template>
<style scoped>.history-form { display:flex; flex-wrap:wrap; gap:var(--space-2); align-items:end; margin-block:var(--space-3); } label { display:grid; gap:var(--space-1); } details { padding-block:var(--space-2); } [role=alert] { color:var(--danger); } p { overflow-wrap:anywhere; }</style>
