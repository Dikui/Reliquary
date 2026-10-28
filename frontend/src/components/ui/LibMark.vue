<script setup lang="ts">
defineProps<{ color: 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8; label?: string }>()
</script>
<template><span class="rq-lib-mark" :style="{ backgroundColor: `var(--lib-${color})` }" :role="label ? 'img' : undefined" :aria-label="label" :aria-hidden="label ? undefined : true" /></template>
