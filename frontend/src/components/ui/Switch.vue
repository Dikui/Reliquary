<script setup lang="ts">
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
defineProps<{ modelValue?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const attrs = useFieldAttrs()
</script>

<template>
  <input v-bind="attrs" type="checkbox" role="switch" class="rq-switch" :checked="modelValue" :aria-checked="!!modelValue" @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)">
</template>
