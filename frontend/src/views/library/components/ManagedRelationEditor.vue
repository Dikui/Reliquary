<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, Combobox, Select, toast } from '@/components/ui'
import { listEntities, type Entity } from '@/api/entities'
import { createManagedRelation, updateManagedRelation, type EntityRelation } from '@/api/relations'
import ManagedAttributes from './ManagedAttributes.vue'
import type { LibraryCatalogItem } from '@/api/library'
const props = defineProps<{ library: LibraryCatalogItem; entity: Entity; relation?: EntityRelation }>()
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const open = ref(false), busy = ref(false), loading = ref(false), error = ref(''), peers = ref<Entity[]>([]), more = ref(false)
const attributes = ref<Record<string, unknown>>({})
const fields = computed(() => props.library.relation_types.find(r => r.name === type.value)?.fields || [])
const peer = ref(''), type = ref(''), outgoing = ref(true)
const options = computed(() => peers.value.filter(e => e.id !== props.entity.id).map(e => ({ value: e.id, label: e.canonical_name })))
let request = 0
async function load(append = false) {
  const current = ++request; loading.value = true
  try { const result = await listEntities({ library_id: props.library.id, limit: 100, offset: append ? peers.value.length : 0 }); if (!result.success) throw new Error(result.error); if (current === request) { peers.value = append ? [...peers.value, ...result.data] : result.data; more.value = result.data.length === 100 } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
function begin() { attributes.value = { ...props.relation?.attributes }; peer.value = props.relation?.peer.id || ''; type.value = props.relation?.relation_type || props.library.relation_types.find(r => r.enabled)?.name || ''; outgoing.value = props.relation?.direction !== 'incoming'; error.value = ''; open.value = true; void load() }
async function save() {
  if (busy.value || !peer.value || !type.value) return
  busy.value = true; error.value = ''
  const data = { subject_id: outgoing.value ? props.entity.id : peer.value, object_id: outgoing.value ? peer.value : props.entity.id, relation_type: type.value, attributes: attributes.value }
  try { const result = props.relation ? await updateManagedRelation(props.relation.id, { ...data, version: props.relation.version! }) : await createManagedRelation(props.library.id, data); if (!result.success) throw new Error(result.message); open.value = false; emit('saved'); toast.success(t('common.save')) }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
onBeforeUnmount(() => { request++ })
</script>
<template>
  <Button @click="begin">{{ t(relation ? 'managed.editRelation' : 'managed.addRelation') }}</Button>
  <Dialog v-model="open" :title="t(relation ? 'managed.editRelation' : 'managed.addRelation')" :closable="!busy" :close-on-esc-keydown="false" :close-on-overlay-click="false">
    <form class="relation-form" novalidate @submit.prevent="save"><fieldset :disabled="busy">
      <p>{{ entity.canonical_name }}</p><label>{{ t('managed.direction') }}<Select :model-value="outgoing ? 'outgoing' : 'incoming'" @update:model-value="outgoing = $event === 'outgoing'"><option value="outgoing">{{ t('managed.outgoing') }}</option><option value="incoming">{{ t('managed.incoming') }}</option></Select></label>
      <label>{{ t('ikb18.relations') }}<Select v-model="type"><option v-for="r in library.relation_types.filter(r => r.enabled)" :key="r.name" :value="r.name">{{ r.name }}</option></Select></label>
      <label>{{ t('managed.peer') }}<Combobox v-model="peer" :options="options" :disabled="loading" /></label>
      <ManagedAttributes v-model="attributes" :fields="fields" :library-id="library.id" />
      <p v-if="loading" role="status">{{ t('common.loading') }}</p><Button v-if="more" :loading="loading" @click="load(true)">{{ t('workbench.loadMore') }}</Button>
      <p v-if="error" role="alert">{{ error }} <Button @click="load()">{{ t('common.retry') }}</Button></p>
      <Button type="submit" variant="primary" :loading="busy" :disabled="!peer || !type">{{ t('common.save') }}</Button>
    </fieldset></form>
  </Dialog>
</template>
<style scoped>.relation-form fieldset { display: grid; gap: var(--space-3); border: 0; padding: 0; } label { display: grid; gap: var(--space-1); } [role='alert'] { color: var(--danger); }</style>
