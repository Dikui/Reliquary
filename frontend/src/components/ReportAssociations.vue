<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { batchQueryEntry } from '@/api/library'

interface RelatedEntry {
  id: string
  title: string
  libraryName: string
  available: boolean
}

interface RelatedEntity {
  id: string
  name: string
  canonical_name?: string
}


const props = withDefaults(defineProps<{
  entryIds?: string[]
  entities?: RelatedEntity[]
}>(), {
  entryIds: () => [],
  entities: () => [],
})

const emit = defineEmits<{
  'open-entry': [id: string]
  'open-entity': [id: string]
}>()

const { t } = useI18n()
const relatedEntries = ref<RelatedEntry[]>([])
const loading = ref(false)
let loadGeneration = 0

const uniqueValues = (values: unknown[]) => Array.from(new Set(values
  .filter((value): value is string => typeof value === 'string')
  .map(value => value.trim())
  .filter(Boolean)))

const normalizedEntryIds = computed(() => uniqueValues(props.entryIds))
const normalizedEntities = computed(() => {
  const seen = new Set<string>()
  return props.entities.filter(entity => {
    if (!entity?.id || seen.has(entity.id)) return false
    seen.add(entity.id)
    return true
  })
})
const hasAssociations = computed(() => normalizedEntryIds.value.length > 0 || normalizedEntities.value.length > 0)

const unavailableEntry = (id: string): RelatedEntry => ({
  id,
  title: t('library.relatedEntryUnavailable'),
  libraryName: '',
  available: false,
})

watch(() => normalizedEntryIds.value.join('\u0000'), async () => {
  const generation = ++loadGeneration
  const ids = normalizedEntryIds.value
  relatedEntries.value = []
  if (!ids.length) {
    loading.value = false
    return
  }

  loading.value = true
  try {
    const query = new URLSearchParams()
    ids.forEach(id => query.append('ids', id))
    const response: any = await batchQueryEntry(query.toString())
    if (generation !== loadGeneration) return
    const returned = Array.isArray(response?.data) ? response.data : []
    const byID = new Map(returned
      .filter((entry: any) => entry && typeof entry.id === 'string')
      .map((entry: any) => [entry.id, entry]))
    relatedEntries.value = ids.map(id => {
      const entry: any = byID.get(id)
      if (!entry) return unavailableEntry(id)
      return {
        id,
        title: entry.title || entry.file_name || t('library.untitledDocument'),
        libraryName: entry.library_name || t('library.relatedLibraryUnknown'),
        available: true,
      }
    })
  } catch {
    if (generation === loadGeneration) relatedEntries.value = ids.map(unavailableEntry)
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}, { immediate: true })

const entityName = (entity: RelatedEntity) => entity.name || entity.canonical_name || entity.id
</script>

<template>
  <section v-if="hasAssociations" class="setting-drawer__section report-associations" data-report-associations>
    <h4 class="setting-drawer__section-title">{{ t('library.reportAssociations') }}</h4>

    <div v-if="normalizedEntryIds.length" class="association-group related-entry-group">
      <h5>{{ t('library.relatedEntries') }}</h5>
      <div v-if="loading" class="association-loading" role="status">
        <t-loading size="small" />
        <span>{{ t('common.loading') }}</span>
      </div>
      <ul v-else class="association-list">
        <li v-for="entry in relatedEntries" :key="entry.id" :class="{ 'association-unavailable': !entry.available }">
          <button v-if="entry.available" type="button" class="association-link" @click="emit('open-entry', entry.id)">{{ entry.title }}</button>
          <span v-else class="association-placeholder">{{ entry.title }}</span>
          <span v-if="entry.libraryName" class="association-library">{{ entry.libraryName }}</span>
        </li>
      </ul>
    </div>

    <div v-if="normalizedEntities.length" class="association-group related-entity-group">
      <h5>{{ t('library.articleEntities') }}</h5>
      <div class="association-tags">
        <button v-for="entity in normalizedEntities" :key="entity.id" type="button" class="association-tag" @click="emit('open-entity', entity.id)">{{ entityName(entity) }}</button>
      </div>
    </div>


  </section>
</template>

<style scoped lang="less">
.report-associations,
.association-group,
.association-list {
  min-width: 0;
}

.association-group {
  display: grid;
  gap: 8px;
}

.association-group h5 {
  margin: 0;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  font-weight: 500;
}

.association-list {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.association-list li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}

.association-link,
.association-tag {
  border: 0;
  color: var(--td-brand-color);
  background: transparent;
  cursor: pointer;
  font: inherit;
}

.association-link {
  min-width: 0;
  padding: 0;
  overflow-wrap: anywhere;
  text-align: left;
}

.association-link:hover,
.association-tag:hover {
  text-decoration: underline;
}

.association-link:focus-visible,
.association-tag:focus-visible {
  outline: 2px solid var(--td-brand-color);
  outline-offset: 2px;
}

.association-library {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: var(--td-radius-small);
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-secondarycontainer);
  font-size: 11px;
}

.association-unavailable,
.association-loading {
  color: var(--td-text-color-placeholder);
}

.association-loading {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.association-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.association-tag {
  padding: 2px 7px;
  border-radius: var(--td-radius-small);
  background: var(--td-bg-color-secondarycontainer);
}
</style>
