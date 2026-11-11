<script setup lang="ts">
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import EntryWorkbench from './components/EntryWorkbench.vue'
import type { SourceLocation } from '@/types/search'
import { entryDetailLocation } from '@/utils/bodyReference'
const route = useRoute(), router = useRouter()
function openSource(id: string, location?: SourceLocation) { void router.push(entryDetailLocation(id, location)) }
function selectEntity(id: string, library: string) {
  if (route.query.entity_id === id && route.query.libraries === library) return
  const query: LocationQueryRaw = { ...route.query, libraries: library }
  delete query.entity_id
  if (id) query.entity_id = id
  // Opening an entity from a page without one adds history, so Back returns to the list or shelf.
  void (id && !route.query.entity_id ? router.push : router.replace)({ path: '/platform/entities', query })
}
</script>
<template>
  <main class="entities-page"><EntryWorkbench :initial-entity-id="typeof route.query.entity_id === 'string' ? route.query.entity_id : ''" :library-id="typeof route.query.libraries === 'string' ? route.query.libraries.split(',')[0] : ''" @open-source="openSource" @select-entity="selectEntity" /></main>
</template>
<style scoped>
.entities-page { flex: 1; min-width: 0; min-height: 0; overflow: auto; background: var(--bg); }
</style>
