<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { listDomains, setEntryDomains, type Domain } from '@/api/domains'
import EntryAssociationsEditor from './EntryAssociationsEditor.vue'

const props = defineProps<{ entryId: string; domains: Domain[]; manual?: boolean }>()
const emit = defineEmits<{ saved: [value: { domains: Domain[]; domains_manual: boolean }] }>()
const { t } = useI18n()
async function loadOptions() {
  const response = await listDomains()
  if (!response.success) throw new Error(response.error || t('domains.requestFailed'))
  return { options: response.data.map(item => ({ value: item.id, label: item.name })), hasMore: false }
}
async function saveChoice(id: string, ids: string[]) {
  const response = await setEntryDomains(id, ids)
  if (!response.success) throw new Error(response.error || t('domains.requestFailed'))
  if (id === props.entryId) emit('saved', { domains: response.data.domains, domains_manual: response.data.domains_manual })
}
</script>

<template>
  <EntryAssociationsEditor :entry-id="entryId" :initial="domains.map(item => ({ value: item.id, label: item.name }))" :manual="manual" :title="t('domains.editEntry')" :hint="t('domains.entryHint')" :label="t('domains.title')" :load-options="loadOptions" :save-choice="saveChoice" />
</template>
