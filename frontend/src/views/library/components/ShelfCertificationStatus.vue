<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { StatusSquare, Tag } from '@/components/ui'
import type { ShelfCertificationStatus } from '@/api/shelf'
import { certificationStatusKey } from '@/utils/shelfCertification'

const props = defineProps<{ status?: ShelfCertificationStatus; progress?: string }>()
const { t, availableLocales } = useI18n()
const labelKey = computed(() => certificationStatusKey(props.status))
const label = computed(() => t(labelKey.value))
const warning = computed(() => props.status === 'delayed' || props.status === 'date_conflict')
const detail = computed(() => {
  const progress = props.progress?.trim().toLowerCase()
  if (!progress || progress === (props.status ?? 'unknown')) return
  if (availableLocales.some(locale => progress === t(labelKey.value, {}, { locale }).toLowerCase())) return
  return props.progress
})
</script>

<template>
  <Tag :variant="status === 'obtained' ? 'success' : 'plain'">
    <StatusSquare v-if="warning" status="warning" :label="label" />
    <template v-else>{{ label }}</template>
  </Tag>
  <small v-if="detail">{{ detail }}</small>
</template>

<style scoped>
small { display: block; margin-top: var(--space-1); color: var(--muted); font-size: var(--text-xs); white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
