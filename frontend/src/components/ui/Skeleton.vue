<script setup lang="ts">
defineProps<{ label: string }>()
</script>
<template><div class="rq-skeleton" role="status" :aria-label="label" aria-busy="true"><span class="rq-sr-only">{{ label }}</span></div></template>
