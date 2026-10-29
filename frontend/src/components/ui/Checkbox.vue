<script setup lang="ts">
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
defineProps<{ modelValue?: boolean; indeterminate?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const attrs = useFieldAttrs()
</script>

<template>
  <input v-bind="attrs" type="checkbox" class="rq-checkbox" :checked="modelValue" :indeterminate.prop="indeterminate" @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)">
</template>
