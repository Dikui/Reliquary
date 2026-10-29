<script setup lang="ts">
defineProps<{ modelValue?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>
<template><button type="button" class="rq-chip" :disabled="disabled" :aria-pressed="!!modelValue" @click="emit('update:modelValue', !modelValue)"><slot /></button></template>
