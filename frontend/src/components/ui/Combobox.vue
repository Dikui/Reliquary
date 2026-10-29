<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ref } from 'vue'
import { ComboboxRoot, ComboboxAnchor, ComboboxInput, ComboboxTrigger, ComboboxCancel, ComboboxPortal, ComboboxContent, ComboboxItem, ComboboxEmpty } from 'reka-ui'
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ modelValue?: string; options: { value: string; label: string; disabled?: boolean }[]; disabled?: boolean; placeholder?: string; name?: string; clearable?: boolean }>(), { clearable: true })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const attrs = useFieldAttrs()
const { t } = useI18n()
const search = ref('')
const displayValue = (value: unknown) => props.options.find(option => option.value === value)?.label ?? ''
</script>
<template>
  <ComboboxRoot class="rq-combobox" :model-value="modelValue" :disabled="disabled" :name="name" open-on-focus open-on-click :reset-model-value-on-clear="clearable" @update:model-value="emit('update:modelValue', String($event ?? ''))">
    <ComboboxAnchor class="rq-combobox__anchor">
      <ComboboxInput v-model="search" v-bind="attrs" class="rq-control" :disabled="disabled" :placeholder="placeholder" :display-value="displayValue" />
      <ComboboxCancel v-if="clearable && (search || modelValue)" class="rq-icon-button" :disabled="disabled" :tabindex="0" :aria-label="t('ui.clearSelection')" @click="search = ''">×</ComboboxCancel>
      <ComboboxTrigger class="rq-icon-button" :disabled="disabled" :aria-label="t('ui.showOptions')">▾</ComboboxTrigger>
    </ComboboxAnchor>
    <ComboboxPortal><ComboboxContent class="rq-menu rq-combobox__content" position="popper" :side-offset="4">
      <ComboboxEmpty class="rq-menu__empty">{{ t('ui.noOptions') }}</ComboboxEmpty>
      <ComboboxItem v-for="option in options" :key="option.value" class="rq-menu__item" :value="option.value" :text-value="option.label" :disabled="option.disabled">{{ option.label }}</ComboboxItem>
    </ComboboxContent></ComboboxPortal>
  </ComboboxRoot>
</template>
