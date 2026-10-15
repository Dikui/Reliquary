<script setup lang="ts">
import ReparseConfirmDialog from '@/views/library/components/ReparseConfirmDialog.vue'
import { useReparseConfirmStore } from '@/stores/reparseConfirm'

const reparseConfirmStore = useReparseConfirmStore()
</script>

<template>
  <ReparseConfirmDialog
    v-model:visible="reparseConfirmStore.visible"
    :library-info="reparseConfirmStore.libraryInfo"
    :reparse-preview="reparseConfirmStore.reparse"
    @confirm="reparseConfirmStore.resolveConfirm"
    @cancel="reparseConfirmStore.rejectConfirm"
  />
</template>
