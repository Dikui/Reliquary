<script setup lang="ts">
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
defineProps<{ modelValue?: string | number }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const attrs = useFieldAttrs()
</script>

<template>
  <select v-bind="attrs" class="rq-control rq-select" :value="modelValue" @change="emit('update:modelValue', ($event.target as HTMLSelectElement).value)"><slot /></select>
</template>
