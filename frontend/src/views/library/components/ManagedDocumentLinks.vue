<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Combobox } from '@/components/ui'
import { listEntities, listEntryEntityLinks, setEntryEntityLink, type Entity, type EntryEntityLink } from '@/api/entities'
const props = defineProps<{ entryId: string; libraryId: string }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const links = ref<EntryEntityLink[]>([]), entities = ref<Entity[]>([]), selected = ref(''), error = ref(''), loading = ref(false), busy = ref(false), more = ref(false)
const options = computed(() => entities.value.map(entity => ({ value: entity.id, label: entity.canonical_name })))
let request = 0
async function load(append = false) {
  const current = ++request; loading.value = true; error.value = ''
  try {
    const [linked, available] = await Promise.all([listEntryEntityLinks(props.entryId), listEntities({ library_id: props.libraryId, limit: 100, offset: append ? entities.value.length : 0 })])
    if (!linked.success || !available.success) throw new Error(t('common.operationFailed'))
    if (current === request) { links.value = linked.data; entities.value = append ? [...entities.value, ...available.data] : available.data; more.value = available.data.length === 100 }
  } catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
async function change(id: string, excluded: boolean) {
  if (!id || busy.value) return
  busy.value = true; error.value = ''
  try { const result = await setEntryEntityLink(props.entryId, id, excluded); if (!result.success) throw new Error(result.error); selected.value = ''; await load(); emit('changed') }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
watch(() => props.entryId, () => { links.value = []; entities.value = []; void load() }, { immediate: true })
onBeforeUnmount(() => { request++ })
</script>
<template>
  <section class="managed-links" :aria-busy="loading">
    <h3>{{ t('managed.links') }}</h3><p>{{ t('managed.linksHint') }}</p>
    <label>{{ t('managed.chooseEntity') }}<Combobox v-model="selected" :options="options" :disabled="loading || busy" /></label>
    <Button :disabled="!selected || busy || loading" @click="change(selected, false)">{{ t('managed.link') }}</Button><Button v-if="more" :loading="loading" @click="load(true)">{{ t('workbench.loadMore') }}</Button>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert">{{ error }} <Button :disabled="busy" @click="load()">{{ t('common.retry') }}</Button></p>
    <ul><li v-for="link in links" :key="link.entity.id"><span>{{ link.entity.canonical_name }} · {{ t(link.excluded ? 'managed.excluded' : link.origin === 'manual' ? 'managed.manual' : 'managed.matched') }}</span><Button :disabled="busy || loading" @click="change(link.entity.id, !link.excluded)">{{ t(link.excluded ? 'managed.restoreLink' : 'managed.exclude') }}</Button></li></ul>
  </section>
</template>
<style scoped>.managed-links { display: grid; gap: var(--space-2); } label { display: grid; gap: var(--space-1); } ul { padding: 0; list-style: none; } li { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: var(--space-2); border-bottom: 1px solid var(--border); padding-block: var(--space-2); } [role='alert'] { color: var(--danger); }</style>
