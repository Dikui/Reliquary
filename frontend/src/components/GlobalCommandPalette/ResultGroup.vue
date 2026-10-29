<template>
  <div v-if="$slots.default" class="rq-cmdk-group" role="group" :aria-label="label">
    <div class="rq-cmdk-group__header">
      <span>{{ label }}</span><span v-if="count != null">({{ count }})</span>
      <button v-if="action" type="button" @click="$emit('action')">{{ action }}</button>
    </div>
    <slot />
  </div>
</template>
<script setup lang="ts">
defineProps<{ label: string; count?: number; action?: string }>()
defineEmits<{ action: [] }>()
</script>
<style scoped>
.rq-cmdk-group__header { display: flex; align-items: center; gap: var(--space-2); padding: var(--space-3) var(--space-4) var(--space-1); color: var(--accent); font-size: var(--text-xs); font-weight: 800; letter-spacing: 0.04em; }
.rq-cmdk-group__header button { margin-left: auto; padding: 0; border: 0; background: transparent; color: var(--muted); font: inherit; cursor: pointer; }
.rq-cmdk-group__header button:hover { color: var(--accent); }
</style>
