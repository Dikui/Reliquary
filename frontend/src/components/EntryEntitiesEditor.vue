<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { listEntities, setEntryEntities, type Entity } from '@/api/entities'
import EntryAssociationsEditor from './EntryAssociationsEditor.vue'

const props = defineProps<{ entryId: string; entities: Pick<Entity, 'id' | 'canonical_name'>[]; manual?: boolean }>()
const emit = defineEmits<{ saved: [value: { entities: Entity[]; entities_manual: boolean }] }>()
const { t } = useI18n()
async function loadOptions(offset: number) {
  const response = await listEntities({ limit: 100, offset })
  if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
  return { options: response.data.map(item => ({ value: item.id, label: item.canonical_name })), hasMore: response.data.length === 100 }
}
async function saveChoice(id: string, ids: string[]) {
  const response = await setEntryEntities(id, ids)
  if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
  if (id === props.entryId) emit('saved', { entities: response.data.entities, entities_manual: response.data.entities_manual })
}
</script>

<template>
  <EntryAssociationsEditor :entry-id="entryId" :initial="entities.map(item => ({ value: item.id, label: item.canonical_name }))" :manual="manual" :title="t('entityMaintenance.editEntry')" :hint="t('entityMaintenance.entryHint')" :label="t('library.articleEntities')" :load-options="loadOptions" :save-choice="saveChoice" />
</template>
