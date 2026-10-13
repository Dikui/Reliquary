<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { MissingMaterial } from '@/types/ingest'
import { isValidURL } from '@/utils/security'

defineProps<{ items: MissingMaterial[] }>()
const { t, locale } = useI18n()
function originalURL(value?: string) { return value && /^https?:\/\//i.test(value) && isValidURL(value) ? value : '' }
function submittedTime(value?: string) {
  if (!value) return t('ingestStatus.unknownTime')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale.value)
}
</script>

<template>
  <section v-if="items.length" class="missing-materials" :aria-label="t('ingestStatus.missingMaterials')">
    <h5>{{ t('ingestStatus.missingMaterials') }}</h5>
    <ul>
      <li v-for="(item, index) in items" :key="`${item.type}-${item.source_url || ''}-${index}`">
        <strong>{{ t(`ingestStatus.missingTypes.${item.type}`) }}</strong>
        <dl>
          <div v-if="item.source_url"><dt>{{ t('ingestStatus.missingSource') }}</dt><dd><a v-if="originalURL(item.source_url)" :href="originalURL(item.source_url)" target="_blank" rel="noopener noreferrer">{{ item.source_url }}</a><span v-else>{{ item.source_url }}</span></dd></div>
          <div><dt>{{ t('ingestStatus.missingReason') }}</dt><dd>{{ item.reason }}</dd></div>
          <div><dt>{{ t('ingestStatus.submittedBy') }}</dt><dd>{{ item.api_key_name || t('ingestStatus.unknownTool') }}</dd></div>
          <div><dt>{{ t('ingestStatus.submittedAt') }}</dt><dd><time v-if="item.submitted_at" :datetime="item.submitted_at">{{ submittedTime(item.submitted_at) }}</time><span v-else>{{ t('ingestStatus.unknownTime') }}</span></dd></div>
        </dl>
      </li>
    </ul>
  </section>
</template>

<style scoped lang="less">
.missing-materials { min-width: 0; font-size: 13px; line-height: 1.7; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
h5 { margin: 0 0 8px; font-size: inherit; color: var(--td-text-color-primary); }
ul { display: grid; gap: 12px; list-style: none; margin: 0; padding: 0; }
li { padding: 12px; border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); }
strong { color: var(--td-text-color-primary); }
dl { margin: 6px 0 0; }
dl > div { display: grid; grid-template-columns: 100px minmax(0, 1fr); gap: 8px; }
dt, dd { margin: 0; }
a { color: var(--td-brand-color); }
a:hover { text-decoration: underline; }
a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
</style>
