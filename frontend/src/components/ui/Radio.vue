<script setup lang="ts">
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
defineProps<{ modelValue?: string | number; value: string | number }>()
const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>()
const attrs = useFieldAttrs()
</script>

<template>
  <input v-bind="attrs" type="radio" class="rq-radio" :value="value" :checked="modelValue === value" @change="emit('update:modelValue', value)">
</template>
