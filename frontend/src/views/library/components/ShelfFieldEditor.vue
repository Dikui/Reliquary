<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Dialog, Field, Input, Select, Textarea } from '@/components/ui'
import type { EntityField, LibraryCatalogItem } from '@/api/library'
import { configureShelf, shelfWriteError } from '@/api/shelf-files'
const props = defineProps<{ library: LibraryCatalogItem; field?: EntityField; lineId: string }>()
const emit = defineEmits<{ close: []; saved: [] }>(), { t } = useI18n()
const field = ref<EntityField>(JSON.parse(JSON.stringify(props.field || { key: `param_${crypto.randomUUID().replaceAll('-', '')}`, name: '', type: 'text', kinds: ['model'], product_line_ids: [props.lineId] }))), options = ref(props.field?.options?.join('\n') || ''), busy = ref(false), error = ref('')
const types = computed(() => [{ key: 'text', label: t('shelfSettings.text') }, { key: 'number', label: t('shelfSettings.number') }, { key: 'boolean', label: t('shelfSettings.boolean') }, { key: 'date', label: t('shelfSettings.date') }, { key: 'select', label: t('shelfSettings.select') }, { key: 'multiselect', label: t('shelfSettings.multiselect') }, { key: 'list', label: t('shelfSettings.list') }, { key: 'entity_ref', label: t('shelfSettings.entity_ref') }, { key: 'url', label: t('shelfSettings.urlField') }, { key: 'attachment', label: t('shelfSettings.attachment') }])
async function save() {
  if (busy.value) return
  if (!field.value.name.trim() || !props.library.updated_at || (['select', 'multiselect'].includes(field.value.type) && !options.value.trim())) { error.value = t('shelfSettings.invalid'); return }
  busy.value = true; error.value = ''
  try {
    const schema = JSON.parse(JSON.stringify(props.library.entity_schema || { kinds: [], fields: [] }))
    const value = { ...field.value, options: ['select', 'multiselect'].includes(field.value.type) ? options.value.split('\n').filter(value => value.trim()) : [] }
    const index = schema.fields.findIndex((f: EntityField) => f.key === value.key)
    if (index < 0) schema.fields.push(value); else schema.fields[index] = value
    const response = await configureShelf(props.library.id, props.library.updated_at, { entity_schema: schema })
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    emit('saved')
  } catch (cause) { error.value = shelfWriteError(cause, t('shelfSettings.saveFailed')) }
  finally { busy.value = false }
}
</script>
<template>
  <Dialog :model-value="true" :title="t('shelfSettings.fields')" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @update:model-value="!$event && !busy && emit('close')">
    <form class="rq-shelf-field-editor" @submit.prevent="save"><p v-if="error" role="alert" class="rq-shelf-error">{{ error }}</p>
      <Field :label="t('shelfSettings.name')" id="parameter-name"><Input id="parameter-name" v-model="field.name" :aria-label="t('shelfSettings.name')" :disabled="busy" required /></Field>
      <Field :label="t('shelfSettings.type')" id="parameter-type"><Select id="parameter-type" v-model="field.type" :disabled="busy"><option v-for="type in types" :key="type.key" :value="type.key">{{ type.label }}</option></Select></Field>
      <Field :label="t('shelfSettings.unit')" id="parameter-unit"><Input id="parameter-unit" v-model="field.unit" :disabled="busy" /></Field>
      <Field v-if="['select', 'multiselect'].includes(field.type)" :label="t('shelfSettings.options')" id="parameter-options"><Textarea id="parameter-options" v-model="options" :disabled="busy" /></Field>
      <label><Checkbox v-model="field.archived" :disabled="busy" /> {{ t('shelfSettings.archived') }}</label>
      <footer><Button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</Button><Button type="submit" variant="primary" :loading="busy">{{ t('shelfSettings.save') }}</Button></footer>
    </form>
  </Dialog>
</template>
<style scoped>
.rq-shelf-field-editor { display: flex; flex-direction: column; gap: var(--space-4); } footer { display: flex; justify-content: flex-end; gap: var(--space-2); } .rq-shelf-error { color: var(--danger); }
</style>
