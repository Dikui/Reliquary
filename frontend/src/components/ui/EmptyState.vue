<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
<template><section class="rq-empty" role="status"><h2>{{ title }}</h2><p v-if="description">{{ description }}</p><slot /></section></template>
