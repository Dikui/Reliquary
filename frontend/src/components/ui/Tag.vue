<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'accent' | 'plain' | 'success' | 'danger' }>(), { variant: 'plain' })
</script>
<template><span class="rq-tag" :class="`rq-tag--${variant}`"><slot /></span></template>
