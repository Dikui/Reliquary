<script setup lang="ts">
import Dialog from './Dialog.vue'
withDefaults(defineProps<{ modelValue?: boolean; title: string; description?: string; closeOnOverlayClick?: boolean; closeOnEscKeydown?: boolean; closable?: boolean }>(), { closeOnOverlayClick: true, closeOnEscKeydown: true, closable: true })
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; openAutoFocus: [event: Event]; closeAutoFocus: [event: Event] }>()
</script>
<template>
  <Dialog v-bind="$props" placement="right" @update:model-value="emit('update:modelValue', $event)" @open-auto-focus="emit('openAutoFocus', $event)" @close-auto-focus="emit('closeAutoFocus', $event)">
    <template v-if="$slots.trigger" #trigger><slot name="trigger" /></template>
    <slot />
    <template v-if="$slots.footer" #footer><slot name="footer" /></template>
  </Dialog>
</template>
