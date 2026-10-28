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
  <Dialog v-if="active" :key="active.id" :model-value="true" :title="active.options.header" :description="active.options.body"
    :closable="!active.busy" :close-on-overlay-click="!active.busy && (active.options.closeOnOverlayClick ?? true)"
    :close-on-esc-keydown="!active.busy && (active.options.closeOnEscKeydown ?? true)"
    @update:model-value="!$event && active.close()" @open-auto-focus="focusCancel">
    <p v-if="active.failed" class="rq-field__error" role="alert">{{ t('ui.confirmFailed') }}</p>
    <template #footer>
      <Button data-cancel :disabled="active.busy" @click="active.cancel">{{ active.options.cancelBtn || t('common.cancel') }}</Button>
      <Button data-confirm :loading="active.busy" :variant="active.options.theme === 'danger' ? 'danger' : 'primary'" @click="active.accept">{{ active.options.confirmBtn || t('common.confirm') }}</Button>
    </template>
  </Dialog>
</template>

<style src="./ui.css"></style>
