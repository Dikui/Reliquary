<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  modelValue: string[]
  entities: { value: string; label: string }[]
  domains: { value: string; label: string }[]
  loading?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>()
const { t } = useI18n()
const options = computed(() => {
  const group = (kind: string, items: { value: string; label: string }[]) => {
    const choices = items.map(item => ({ value: `${kind}:${item.value}`, label: item.label }))
    for (const value of props.modelValue) {
      if (value.startsWith(`${kind}:`) && !choices.some(item => item.value === value)) {
        choices.push({ value, label: value.slice(kind.length + 1) })
      }
    }
    return choices
  }
  return [
    { group: t('library.articleEntities'), children: group('entity', props.entities) },
    { group: t('domains.title'), children: group('domain', props.domains) },
  ]
})
</script>

<template>
  <t-select :model-value="modelValue" multiple filterable clearable :options="options" :loading="loading"
    :placeholder="t('domains.mixedFilter')" :aria-label="t('domains.mixedFilter')"
    @update:model-value="emit('update:modelValue', $event)" />
</template>
