<template>
  <div class="graph-query-results">
    <div v-if="data.graph_data" class="stats-card">
      <div class="stats-title">{{ t('chat.graphResultsHeader', { count: factCount }) }}</div>
      <div class="graph-summary">
        <span>{{ t('knowledgeWorkbench.entities.title') }}: {{ data.graph_data.total_nodes }}</span>
        <span>{{ t('knowledgeWorkbench.facts') }}: {{ factCount }}</span>
        <span>{{ t('knowledgeWorkbench.query.relation') }}: {{ data.graph_data.total_edges }}</span>
      </div>
      <div v-if="data.graph_data.nodes.length" class="entity-list">
        <span v-for="node in data.graph_data.nodes" :key="`${node.kb_id}:${node.id}`" class="entity-chip">
          {{ node.label }}<small>{{ node.type }}</small>
        </span>
      </div>
    </div>

    <section v-for="(base, index) in knowledgeBases" :key="base.key" class="knowledge-section">
      <header class="knowledge-section__header">
        <h4>{{ t('knowledgeBase.title') }} {{ index + 1 }}</h4>
        <RouterLink
          v-if="base.id"
          :to="{ name: 'knowledgeBaseDetail', params: { kbId: base.id }, query: { tab: 'context' } }"
        >
          {{ t('knowledgeWorkbench.tabLabel') }}
        </RouterLink>
      </header>

      <p v-if="base.result.template_only_message_count" class="fact-meta" role="status">{{ t('knowledgeWorkbench.query.templateOnlyMessages', { count: base.result.template_only_message_count }) }}</p>

      <div v-if="base.result.facts.length" class="fact-list">
        <article v-for="fact in base.result.facts" :key="fact.id" class="fact-card">
          <div class="fact-heading">
            <div class="fact-expression">
              <strong>{{ entityName(base.result.entities, fact.subject_entity_id) }}</strong>
              <span>{{ fact.predicate }}</span>
              <strong>{{ fact.kind === 'relation'
                ? entityName(base.result.entities, fact.object_entity_id)
                : formatValue(fact.value) + (fact.unit ? ` ${fact.unit}` : '') }}</strong>
            </div>
            <span class="fact-status">{{ factStatus(fact.status) }}</span>
          </div>

          <div class="fact-meta">
            <span>{{ t('knowledgeWorkbench.factPeriod') }}: {{ formatDate(fact.effective_from) }}<template v-if="fact.effective_to"> — {{ formatDate(fact.effective_to) }}</template></span>
            <span>{{ t('knowledgeWorkbench.evidence.source') }}: {{ sourceTitle(base.result.evidence, fact.evidence_id) }}</span>
          </div>
          <blockquote v-if="fact.quote">{{ fact.quote }}</blockquote>

          <div class="fact-actions">
            <button type="button" class="text-link" @click="toggleSnapshot(base.id || '', fact.evidence_id)">
              {{ t('knowledgeWorkbench.evidence.openSnapshot') }}
            </button>
            <a
              v-if="safeExternalUrl(evidenceFor(base.result.evidence, fact.evidence_id)?.original_url)"
              :href="safeExternalUrl(evidenceFor(base.result.evidence, fact.evidence_id)?.original_url)"
              target="_blank"
              rel="noopener noreferrer"
            >{{ t('knowledgeWorkbench.evidence.openSource') }}</a>
            <RouterLink
              v-if="base.id && evidenceFor(base.result.evidence, fact.evidence_id)?.knowledge_id"
              :to="{ name: 'knowledgeBaseDetail', params: { kbId: base.id }, query: { tab: 'context' } }"
            >{{ t('knowledgeWorkbench.evidence.openKnowledge') }}</RouterLink>
          </div>

          <div v-if="isExpanded(base.id || '', fact.evidence_id)" class="evidence-snapshot">
            <span v-if="isLoading(base.id || '', fact.evidence_id)">{{ t('common.loading') }}</span>
            <span v-else-if="hasError(base.id || '', fact.evidence_id)" class="snapshot-error">{{ t('knowledgeWorkbench.requestFailed') }}</span>
            <template v-else>
              <div class="snapshot-meta">
                <span>{{ t('knowledgeWorkbench.evidence.version') }}: {{ snapshot(base.id || '', fact.evidence_id)?.version || evidenceFor(base.result.evidence, fact.evidence_id)?.version || 1 }}</span>
                <span>{{ t('knowledgeWorkbench.evidence.extraction') }}: {{ snapshot(base.id || '', fact.evidence_id)?.extraction_status || evidenceFor(base.result.evidence, fact.evidence_id)?.extraction_status || 'pending' }}</span>
                <span v-if="snapshot(base.id || '', fact.evidence_id)?.extraction_mode || evidenceFor(base.result.evidence, fact.evidence_id)?.extraction_mode">{{ t('knowledgeWorkbench.evidence.extractionMode') }}: {{ t(`knowledgeWorkbench.evidence.extractionModes.${snapshot(base.id || '', fact.evidence_id)?.extraction_mode || evidenceFor(base.result.evidence, fact.evidence_id)?.extraction_mode}`) }}</span>
              </div>
              <pre>{{ snapshot(base.id || '', fact.evidence_id)?.body || evidenceFor(base.result.evidence, fact.evidence_id)?.body || t('knowledgeWorkbench.evidence.noBody') }}</pre>
            </template>
          </div>
        </article>
      </div>
    </section>

    <div v-if="factCount === 0" class="empty-state">
      {{ t('chat.graphNoResults') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { getKnowledgeEvidence, type KnowledgeContextResult, type KnowledgeEvidence } from '@/api/knowledge-context'
import type { GraphQueryResultsData } from '@/types/tool-results'

const props = defineProps<{ data: GraphQueryResultsData }>()
const { t } = useI18n()

const knowledgeBases = computed(() => Object.entries(props.data.contexts || {}).map(([key, result]) => ({
  key,
  id: result.entities?.[0]?.knowledge_base_id
    || result.facts?.[0]?.knowledge_base_id
    || result.events?.[0]?.knowledge_base_id
    || result.evidence?.[0]?.knowledge_base_id,
  result: {
    template_only_message_count: result.template_only_message_count || 0,
    entities: result.entities || [],
    facts: result.facts || [],
    events: result.events || [],
    evidence: result.evidence || [],
    attention: result.attention || [],
  } as KnowledgeContextResult,
})))

const factCount = computed(() => knowledgeBases.value.reduce((count, base) => count + base.result.facts.length, 0))
const expanded = reactive<Record<string, boolean>>({})
const loading = reactive<Record<string, boolean>>({})
const errors = reactive<Record<string, boolean>>({})
const snapshots = reactive<Record<string, KnowledgeEvidence>>({})

function snapshotKey(kbId: string, evidenceId: string) {
  return `${kbId}:${evidenceId}`
}

function isExpanded(kbId: string, evidenceId: string) {
  return expanded[snapshotKey(kbId, evidenceId)]
}

function isLoading(kbId: string, evidenceId: string) {
  return loading[snapshotKey(kbId, evidenceId)]
}

function hasError(kbId: string, evidenceId: string) {
  return errors[snapshotKey(kbId, evidenceId)]
}

function snapshot(kbId: string, evidenceId: string) {
  return snapshots[snapshotKey(kbId, evidenceId)]
}

async function toggleSnapshot(kbId: string, evidenceId: string) {
  if (!kbId || !evidenceId) return
  const key = snapshotKey(kbId, evidenceId)
  expanded[key] = !expanded[key]
  if (!expanded[key] || snapshots[key] || loading[key]) return

  loading[key] = true
  errors[key] = false
  try {
    const response = await getKnowledgeEvidence(kbId, evidenceId)
    if (!response.success || !response.data) throw new Error('Evidence snapshot is unavailable')
    snapshots[key] = response.data
  } catch {
    errors[key] = true
  } finally {
    loading[key] = false
  }
}

function entityName(entities: KnowledgeContextResult['entities'], id?: string) {
  return entities.find(entity => entity.id === id)?.canonical_name || id || t('knowledgeWorkbench.entities.unknownObject')
}

function evidenceFor(evidence: KnowledgeEvidence[], id: string) {
  return evidence.find(item => item.id === id)
}

function sourceTitle(evidence: KnowledgeEvidence[], id: string) {
  return evidenceFor(evidence, id)?.title || id.slice(0, 8)
}

function factStatus(status: string) {
  const key = `knowledgeWorkbench.factStatus.${status}`
  const translated = t(key)
  return translated === key ? status : translated
}

function formatValue(value: unknown) {
  if (value === undefined || value === null || value === '') return t('knowledgeWorkbench.emptyValue')
  if (typeof value === 'string') return value
  try { return JSON.stringify(value) } catch { return String(value) }
}

function formatDate(value?: string) {
  if (!value) return t('knowledgeWorkbench.unknownDate')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString()
}

function safeExternalUrl(value?: string) {
  if (!value) return ''
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) ? url.href : ''
  } catch {
    return ''
  }
}
</script>

<style lang="less" scoped>
@import './tool-results.less';

.graph-query-results { display: flex; flex-direction: column; gap: 12px; }
.graph-summary { display: flex; flex-wrap: wrap; gap: 8px 18px; color: var(--td-text-color-secondary); font-size: 12px; }
.entity-list { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.entity-chip { display: inline-flex; gap: 6px; align-items: center; border: 1px solid var(--td-component-stroke); border-radius: 12px; padding: 3px 9px; font-size: 12px; }
.entity-chip small { color: var(--td-text-color-placeholder); }
.knowledge-section { display: flex; flex-direction: column; gap: 8px; }
.knowledge-section__header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.knowledge-section__header h4 { margin: 0; font-size: 13px; }
.knowledge-section__header a, .fact-actions a { color: var(--td-brand-color); text-decoration: none; font-size: 12px; }
.fact-list { display: flex; flex-direction: column; gap: 8px; }
.fact-card { background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: 6px; padding: 10px 12px; }
.fact-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.fact-expression { display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px; font-size: 13px; }
.fact-expression span, .fact-status { color: var(--td-text-color-secondary); }
.fact-status { flex-shrink: 0; font-size: 11px; }
.fact-meta, .snapshot-meta { display: flex; flex-wrap: wrap; gap: 4px 16px; margin-top: 7px; color: var(--td-text-color-placeholder); font-size: 11px; }
blockquote { margin: 9px 0; padding: 7px 10px; border-left: 3px solid var(--td-brand-color); background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-primary); font-size: 12px; line-height: 1.6; white-space: pre-wrap; }
.fact-actions { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.text-link { padding: 0; border: 0; background: transparent; color: var(--td-brand-color); cursor: pointer; font-size: 12px; }
.evidence-snapshot { margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--td-component-stroke); }
.evidence-snapshot pre { max-height: 360px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; font-size: 12px; line-height: 1.6; }
.snapshot-error { color: var(--td-error-color); font-size: 12px; }
</style>
