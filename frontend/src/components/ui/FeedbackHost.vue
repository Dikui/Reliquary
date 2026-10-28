<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Dialog from './Dialog.vue'
import Button from './Button.vue'
import { useFeedback } from './feedback'

const { t } = useI18n()
const { toasts, confirmations } = useFeedback()
const active = computed(() => confirmations[0])
function focusCancel(event: Event) {
  const button = (event.target as HTMLElement)?.querySelector<HTMLElement>('[data-cancel]')
  if (button) { event.preventDefault(); button.focus() }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="toasts.length" class="rq-toasts">
      <div v-for="item in toasts" :key="item.id" data-toast class="rq-toast" :class="`rq-toast--${item.theme}`" :role="item.theme === 'error' ? 'alert' : 'status'" aria-atomic="true">
        <span>{{ item.content }}</span><button type="button" class="rq-icon-button" :aria-label="t('common.close')" @click="item.close">×</button>
      </div>
    </div>
  </Teleport>
  <Dialog v-for="dialog in active ? [active] : []" :key="dialog.id" :model-value="true" :title="dialog.options.header" :description="dialog.options.body"
    :closable="!dialog.busy" :close-on-overlay-click="!dialog.busy && (dialog.options.closeOnOverlayClick ?? true)"
    :close-on-esc-keydown="!dialog.busy && (dialog.options.closeOnEscKeydown ?? true)"
    @update:model-value="!$event && dialog.close()" @open-auto-focus="focusCancel" @close-auto-focus="dialog.restoreFocus">
    <p v-if="dialog.failed" class="rq-field__error" role="alert">{{ t('ui.confirmFailed') }}</p>
    <template #footer>
      <Button data-cancel :disabled="dialog.busy" @click="dialog.cancel">{{ dialog.options.cancelBtn || t('common.cancel') }}</Button>
      <Button data-confirm :loading="dialog.busy" :variant="dialog.options.theme === 'danger' ? 'danger' : 'primary'" @click="dialog.accept">{{ dialog.options.confirmBtn || t('common.confirm') }}</Button>
    </template>
  </Dialog>
</template>

<style src="./ui.css"></style>
