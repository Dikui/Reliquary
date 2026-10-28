<script setup lang="ts">
withDefaults(defineProps<{ status?: 'neutral' | 'success' | 'warning' | 'danger'; label: string }>(), { status: 'neutral' })
</script>
<template><span class="rq-status"><span class="rq-status__square" :class="`rq-status__square--${status}`" aria-hidden="true" />{{ label }}</span></template>
