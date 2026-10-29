<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
<template><section class="rq-empty rq-error" role="alert"><h2>{{ title }}</h2><p v-if="description">{{ description }}</p><slot /></section></template>
