<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { DialogRoot, DialogTrigger, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogDescription, DialogClose } from 'reka-ui'

withDefaults(defineProps<{
  modelValue?: boolean
  title: string
  description?: string
  placement?: 'center' | 'right' | 'left'
  contentClass?: string
  closeOnOverlayClick?: boolean
  closeOnEscKeydown?: boolean
  closable?: boolean
}>(), { placement: 'center', closeOnOverlayClick: true, closeOnEscKeydown: true, closable: true })
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  openAutoFocus: [event: Event]
  closeAutoFocus: [event: Event]
}>()
const { t } = useI18n()
</script>

<template>
  <DialogRoot :open="modelValue" @update:open="emit('update:modelValue', $event)">
    <DialogTrigger v-if="$slots.trigger" as-child><slot name="trigger" /></DialogTrigger>
    <DialogPortal>
      <DialogOverlay class="rq-overlay" />
      <DialogContent class="rq-dialog" :class="[contentClass, { 'rq-dialog--drawer': placement !== 'center', 'rq-dialog--drawer-left': placement === 'left' }]"
        v-bind="description ? {} : { 'aria-describedby': undefined }"
        @open-auto-focus="emit('openAutoFocus', $event)" @close-auto-focus="emit('closeAutoFocus', $event)"
        @pointer-down-outside="!closeOnOverlayClick && $event.preventDefault()"
        @escape-key-down="!closeOnEscKeydown && $event.preventDefault()">
        <header class="rq-dialog__header">
          <DialogTitle class="rq-dialog__title">{{ title }}</DialogTitle>
          <DialogClose v-if="closable" class="rq-icon-button" :aria-label="t('common.close')">×</DialogClose>
        </header>
        <div class="rq-dialog__body">
          <DialogDescription v-if="description" class="rq-dialog__description">{{ description }}</DialogDescription>
          <slot />
        </div>
        <footer v-if="$slots.footer" class="rq-dialog__footer"><slot name="footer" /></footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
