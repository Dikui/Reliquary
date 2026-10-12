<script setup lang="ts">
import ReparseConfirmDialog from '@/views/knowledge-base/components/ReparseConfirmDialog.vue'
import { useReparseConfirmStore } from '@/stores/reparseConfirm'

const reparseConfirmStore = useReparseConfirmStore()
</script>

<template>
  <ReparseConfirmDialog
    v-model:visible="reparseConfirmStore.visible"
    :kb-info="reparseConfirmStore.kbInfo"
    :reparse-preview="reparseConfirmStore.reparse"
    @confirm="reparseConfirmStore.resolveConfirm"
    @cancel="reparseConfirmStore.rejectConfirm"
  />
</template>
