<template>
  <div v-if="sources?.length" class="repost-sources">
    <strong>{{ t('knowledgeWorkbench.sourceTime.reposts') }}</strong>
    <ul>
      <li v-for="source in sources" :key="source.id">
        <span>{{ source.publisher || source.author || t('knowledgeWorkbench.sourceTime.unknownPublisher') }}</span>
        <span>{{ source.published_at_missing || !source.published_at ? t('knowledgeWorkbench.sourceTime.missing') : source.published_at }}</span>
        <span v-if="source.match_method" class="repost-sources__method">{{ t('knowledgeWorkbench.sourceTime.matchMethod') }}: {{ matchMethodLabel(source.match_method) }}</span>
        <a v-if="safeExternalUrl(source.original_url)" :href="safeExternalUrl(source.original_url)" target="_blank" rel="noopener noreferrer">{{ source.original_url }}</a>
        <span v-else-if="source.original_url">{{ source.original_url }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { EntryRepostSource } from '@/api/knowledge-base'
import { safeExternalUrl } from '@/utils/externalUrl'

defineProps<{ sources?: EntryRepostSource[] }>()
const { t } = useI18n()
function matchMethodLabel(method: string) {
  const known = ['url', 'content_hash', 'simhash', 'file_hash'].includes(method)
  return t(`knowledgeWorkbench.sourceTime.matchMethods.${known ? method : 'other'}`)
}
</script>

<style scoped>
.repost-sources { margin-top: 12px; font-size: 12px; min-width: 0; max-width: 100%; }
ul { padding-left: 18px; margin: 8px 0; min-width: 0; max-width: 100%; box-sizing: border-box; }
li { margin-bottom: 8px; overflow-wrap: anywhere; }
li > span { margin-right: 8px; }
a { color: var(--td-brand-color); white-space: normal; overflow-wrap: anywhere; word-break: break-word; }
</style>
