<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState } from '@/components/ui'
import { entityHistory, type Entity } from '@/api/entities'
import { OPERATION_TYPES, type OperationLog, type OperationLogsData } from '@/api/operation-logs'
import { getShelfNode, type ShelfNode, type ShelfCondition, type ShelfCertification, type ShelfRequirement, type ShelfDocumentVersion } from '@/api/shelf'
import type { LibraryCatalogItem } from '@/api/library'
import { shelfConditionText } from '@/utils/productShelf'
import { formatDateTime } from '@/utils/dateTime'
import { safeExternalUrl } from '@/utils/externalUrl'

const props = defineProps<{ library: LibraryCatalogItem; nodeId: string }>()
const { t, te, locale } = useI18n()
const node = ref<ShelfNode>(), ancestors = ref<Entity[]>([]), loading = ref(false), error = ref('')
const choices = ref<Record<string, string>>({}), selected = ref(false)
const history = ref<OperationLogsData>(), historyLoading = ref(false), historyError = ref(''), historyPage = ref(1)
let generation = 0, historyGeneration = 0
const detail = computed(() => node.value?.shelf)
const optional = computed(() => detail.value?.composition.filter(group => group.optional) || [])
const sections = ['composition', 'sale', 'cert', 'docs', 'params', 'files', 'history'] as const
const statusKeys = ['missing', 'draft_only', 'delivered', 'not_due', 'has_file', 'no_file'] as const
const counts = computed(() => statusKeys.map(status => ({ status, count: new Set((detail.value?.checklist || []).filter(row => row.status === status).map(row => row.requirement.id)).size })).filter(item => item.count))
const groups = computed(() => (['self', 'component', 'ancestor'] as const).map(origin => ({ origin, rows: (detail.value?.certifications || []).filter(row => row.origin === origin) })))
const fields = computed(() => (props.library.entity_schema?.fields || []).filter(field => field.product_line_ids?.includes(ancestors.value.find(item => item.kind === 'product_line')?.id || '') && !field.archived))
const href = (id: string) => `/platform/entities?${new URLSearchParams({ libraries: props.library.id, entity_id: id })}`
const fileHref = (id: string) => `/platform/entries?${new URLSearchParams({ libraries: props.library.id, tab: 'files', file: id })}`
const statusHref = computed(() => `/platform/entries?${new URLSearchParams({ libraries: props.library.id })}`)
const kind = (entity: Entity) => props.library.entity_schema?.kinds.find(item => item.key === entity.kind)?.name || t(`productShelf.kinds.${entity.kind}`)
const owner = (id?: string) => [node.value?.entity, ...ancestors.value, ...(detail.value?.composition.flatMap(group => group.parts) || [])].find(item => item?.id === id)
const ownerText = (id?: string) => { const entity = owner(id); return entity ? `${kind(entity)} ${entity.canonical_name}` : id || t('shelfStatus.noRecord') }
const stage = (key?: string) => props.library.shelf_config?.stages?.find(item => item.key === key)?.name || key || t('shelfStatus.noRecord')
const conditionText = (conditions?: ShelfCondition[] | null) => shelfConditionText(conditions, props.library.shelf_config?.module_types, detail.value?.composition) || t('shelfModel.allConditions')
const requirement = (entity: Entity) => (entity.attributes?.requirement || {}) as ShelfRequirement
const certConditions = (cert: ShelfCertification) => node.value?.facts.find(fact => fact.field === 'certification' && fact.entity_id === cert.entity.id)?.conditions || cert.certification.conditions
const documentURL = (document: ShelfDocumentVersion) => document.attachment_id ? `/api/v1/entities/${encodeURIComponent(document.file_id)}/document-versions/${encodeURIComponent(document.id)}/download` : safeExternalUrl(document.cloud_url)
const valueText = (value: unknown): string => {
  if (value === null || value === undefined || value === '') return t('shelfStatus.noRecord')
  if (Array.isArray(value)) return value.map(valueText).join(' · ')
  if (typeof value === 'object' && 'min' in value && 'max' in value) return `${value.min} ~ ${value.max}`
  if (typeof value === 'boolean') return t(value ? 'common.yes' : 'common.no')
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
const parseStatus = (status: string) => te(`shelfModel.parse.${status}`) ? t(`shelfModel.parse.${status}`) : status
const operationName = (item: OperationLog) => OPERATION_TYPES.includes(item.operation_type) ? t(`operationLogs.types.${item.operation_type}`) : item.operation_type
function sourceText(item: OperationLog) {
  if (item.source?.type === 'agent') return `agent · ${item.source.agent_name || item.actor_name || item.actor}`
  if (item.source?.type === 'import') return `${t('shelfModel.import')} ${item.source.file_name || item.source.import_batch_id || ''}`
  return item.source?.type === 'web' ? t('shelfModel.web') : item.actor_name || item.actor
}
function unwrap<T>(response: { success: boolean; data: T; message?: string; error?: string }): T {
  if (!response.success) throw new Error(response.message || response.error || t('workbench.requestFailed'))
  return response.data
}
async function load() {
  const current = ++generation, libraryID = props.library.id, id = props.nodeId, firstLoad = !node.value
  loading.value = true; error.value = ''
  const combination = optional.value.length && optional.value.every(group => choices.value[group.module_type]) ? { ...choices.value } : undefined
  try {
    const result = unwrap(await getShelfNode(libraryID, id, undefined, combination))
    if (current !== generation) return
    const path: Entity[] = [], seen = new Set([id])
    let parent = (result.facts.find(fact => fact.field === 'belongs_to')?.value as { object_id?: string } | undefined)?.object_id
    while (parent && !seen.has(parent) && path.length < 3) {
      seen.add(parent)
      const response = unwrap(await getShelfNode(libraryID, parent, result.as_of))
      if (current !== generation) return
      path.unshift(response.entity)
      parent = (response.facts.find(fact => fact.field === 'belongs_to')?.value as { object_id?: string } | undefined)?.object_id
    }
    if (current === generation) {
      node.value = result; ancestors.value = path; selected.value = !!combination
      // Matrix links target a requirement after its asynchronous detail has rendered.
      if (firstLoad) void nextTick(() => {
        if (current !== generation || typeof document === 'undefined') return
        const target = window.location.hash.slice(1)
        if (target.startsWith('requirement-')) document.getElementById(decodeURIComponent(target))?.scrollIntoView()
      })
    }
  } catch (cause) { if (current === generation) error.value = (cause as { message?: string } | undefined)?.message || t('workbench.requestFailed') }
  finally { if (current === generation) loading.value = false }
}
function choose(module: string, id: string) { choices.value = { ...choices.value, [module]: id }; void load() }
async function loadHistory(page = 1) {
  const current = ++historyGeneration
  historyLoading.value = true; historyError.value = ''; history.value = undefined; historyPage.value = page
  try { const result = unwrap(await entityHistory(props.nodeId, page)); if (current === historyGeneration) history.value = result }
  catch (cause) { if (current === historyGeneration) historyError.value = (cause as { message?: string } | undefined)?.message || t('workbench.requestFailed') }
  finally { if (current === historyGeneration) historyLoading.value = false }
}
watch(() => [props.library.id, props.nodeId], () => { generation++; node.value = undefined; ancestors.value = []; choices.value = {}; selected.value = false; void load(); void loadHistory() }, { immediate: true })
onBeforeUnmount(() => { generation++; historyGeneration++ })
</script>

<template>
  <article class="shelf-model" :aria-label="t('shelfModel.title')">
    <a :href="statusHref">{{ t('shelfStatus.title') }}</a>
    <ErrorState v-if="error" :title="error">
      <Button @click="load">{{ t('common.retry') }}</Button>
    </ErrorState>
    <header v-if="node">
      <nav class="crumb" :aria-label="t('shelfModel.hierarchy')">
        <template v-for="item in ancestors" :key="item.id">
          <span class="kind">{{ kind(item) }}</span>
          <a :href="href(item.id)">{{ item.canonical_name }}</a>
          <span aria-hidden="true">›</span>
        </template>
        <span class="kind" aria-current="page">{{ t('productShelf.kinds.model') }}</span>
      </nav>
      <h1>{{ node.entity.canonical_name }}</h1>
      <p v-if="node.entity.description">{{ node.entity.description }}</p>
    </header>
    <dl v-if="node?.recorded" class="facts" :aria-busy="loading">
      <template v-if="!loading && !error">
        <div>
          <dt>{{ t('shelfModel.currentStage') }}</dt>
          <dd>{{ stage(detail?.current_stage) }}
            <small v-if="detail?.stage_owner_id && detail.stage_owner_id !== node.entity.id">{{ t('shelfModel.inheritedStage') }}</small>
          </dd>
        </div>
        <div>
          <dt>{{ t('shelfModel.sections.sale') }}</dt>
          <dd>
            <div v-for="(status, index) in detail?.sale_statuses" :key="index">
              <span>{{ library.shelf_config?.regions?.find(region => region.key === status.region)?.name || status.region }} {{ status.status }}</span>
              <small v-if="!selected" class="condition">{{ conditionText(status.conditions) }}</small>
            </div>
            <span v-if="!detail?.sale_statuses.length">{{ t('shelfStatus.noRecord') }}</span>
          </dd>
        </div>
        <div>
          <dt>{{ t('shelfModel.sections.cert') }}</dt>
          <dd>{{ t('shelfModel.certCount', { count: detail?.certifications?.length || 0 }) }}</dd>
        </div>
        <div>
          <dt>{{ t('shelfModel.sections.docs') }}</dt>
          <dd class="summary-counts">
            <span v-for="item in counts" :key="item.status">{{ t(`shelfModel.status.${item.status}`) }} {{ item.count }}</span>
            <span v-if="!counts.length">{{ t('productShelf.noRecords') }}</span>
          </dd>
        </div>
      </template>
    </dl>
    <div v-if="optional.length" class="combo">
      <fieldset v-for="group in optional" :key="group.module_type">
        <legend>{{ library.shelf_config?.module_types?.find(module => module.key === group.module_type)?.name || group.module_type }}</legend>
        <label v-for="part in [{ id: '', canonical_name: t('shelfStatus.all') }, ...group.parts]" :key="part.id">
          <input type="radio" :name="`option-${group.module_type}`" :value="part.id" :checked="(choices[group.module_type] || '') === part.id" @change="choose(group.module_type, part.id)">
          <span>{{ part.canonical_name }}</span>
        </label>
      </fieldset>
      <p v-if="!selected && Object.values(choices).some(Boolean)">{{ t('shelfModel.completeCombination') }}</p>
    </div>
    <p v-if="loading" class="loading" role="status">{{ t('productShelf.loading') }}</p>
    <template v-if="node && !loading && !error">
      <EmptyState v-if="!node.recorded" :title="t('shelfModel.noRecord')" />
      <template v-else>
        <nav class="toc" :aria-label="t('shelfModel.onPage')">
          <a v-for="section in sections" :key="section" :href="`#${section}`">{{ t(`shelfModel.sections.${section}`) }}</a>
        </nav>
        <section id="composition">
          <h2>{{ t('shelfModel.sections.composition') }}</h2>
          <div v-if="detail?.composition.length" class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{{ t('shelfModel.module') }}</th>
                  <th>{{ t('shelfModel.part') }}</th>
                  <th>{{ t('managed.code') }}</th>
                  <th>{{ t('shelfModel.pack') }}</th>
                  <th>{{ t('shelfModel.cluster') }}</th>
                  <th>{{ t('shelfModel.container') }}</th>
                </tr>
              </thead>
              <tbody v-for="group in detail.composition" :key="group.module_type">
                <tr v-for="(part, index) in group.parts" :key="part.id" :class="{ chosen: choices[group.module_type] === part.id }">
                  <th v-if="!index" :rowspan="group.parts.length" scope="rowgroup">{{ library.shelf_config?.module_types?.find(module => module.key === group.module_type)?.name || group.module_type }}
                    <small v-if="group.optional">{{ t('shelfModel.optional', { count: group.parts.length }) }}</small>
                  </th>
                  <td>
                    <a :href="href(part.id)">{{ part.canonical_name }}</a>
                  </td>
                  <td>{{ part.code || t('shelfStatus.noRecord') }}</td>
                  <td>{{ valueText(part.attributes?.pack_model) }}</td>
                  <td>{{ valueText(part.attributes?.cluster_model) }}</td>
                  <td>{{ valueText(part.attributes?.container_model) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section id="sale">
          <h2>{{ t('shelfModel.sections.sale') }}</h2>
          <div class="regions">
            <div v-for="region in library.shelf_config?.regions" :key="region.key">
              <h3>{{ region.name }}</h3>
              <div v-for="(status, index) in detail?.sale_statuses.filter(row => row.region === region.key)" :key="index" class="sale-record">
                <span class="st" :class="`st-${status.status}`">{{ status.status }}</span>
                <b v-if="status.planned_eom">{{ t('shelfStatus.planned') }}</b>
                <span v-if="status.notes">{{ status.notes }}</span>
                <small v-if="status.raw">{{ status.raw }}</small>
                <small v-if="!selected" class="condition">{{ conditionText(status.conditions) }}</small>
              </div>
              <small v-if="!detail?.sale_statuses.some(row => row.region === region.key)">{{ t('shelfStatus.noRecord') }}</small>
            </div>
          </div>
        </section>
        <section id="cert">
          <h2>{{ t('shelfModel.sections.cert') }}</h2>
          <div v-if="detail?.certifications?.length" class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{{ t('shelfModel.standard') }}</th>
                  <th>{{ t('shelfModel.certObject') }}</th>
                  <th>{{ t('shelfModel.applies') }}</th>
                  <th>{{ t('shelfModel.progress') }}</th>
                  <th>{{ t('shelfModel.agency') }}</th>
                  <th>{{ t('shelfModel.dates') }}</th>
                  <th>{{ t('shelfModel.validity') }}</th>
                  <th>{{ t('shelfModel.number') }}</th>
                </tr>
              </thead>
              <template v-for="group in groups" :key="group.origin">
                <tbody v-if="group.rows.length">
                  <tr class="group">
                    <th colspan="8" scope="rowgroup">{{ t(`shelfModel.origins.${group.origin}`) }}</th>
                  </tr>
                  <tr v-for="cert in group.rows" :key="cert.entity.id">
                    <td>
                      <a :href="href(cert.entity.id)">{{ cert.standard_name }}{{ cert.certification.year ? ` ${cert.certification.year}` : '' }}</a>
                      <small v-if="cert.entity.description">{{ cert.entity.description }}</small>
                    </td>
                    <td>{{ cert.certification.certification_object }}</td>
                    <td>
                      <span v-if="group.origin !== 'self'">{{ ownerText(cert.certification.owner_id) }}</span>
                      <small>{{ cert.certification.region }}</small>
                      <small v-if="!selected" class="condition">{{ conditionText(certConditions(cert)) }}</small>
                    </td>
                    <td>{{ cert.certification.progress || t('shelfStatus.noRecord') }}</td>
                    <td>{{ cert.certification.agency || t('shelfStatus.noRecord') }}</td>
                    <td>
                      <small v-for="key in (['start_date', 'obtained_date', 'issued_date'] as const)" :key="key">
                        <template v-if="cert.certification[key]">{{ t(`shelfModel.${key}`) }} {{ cert.certification[key] }}</template>
                      </small>
                    </td>
                    <td>{{ cert.certification.validity || t('shelfStatus.noRecord') }}</td>
                    <td>
                      <a v-if="safeExternalUrl(cert.certification.cloud_url)" :href="safeExternalUrl(cert.certification.cloud_url)" target="_blank" rel="noopener noreferrer">{{ cert.certification.number || t('shelfModel.cloudLink') }}</a>
                      <span v-else>{{ cert.certification.number || t('shelfStatus.noRecord') }}</span>
                    </td>
                  </tr>
                </tbody>
              </template>
            </table>
          </div>
          <p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section id="docs">
          <h2>{{ t('shelfModel.sections.docs') }}</h2>
          <div class="tally">
            <span v-for="item in counts" :key="item.status">
              <b>{{ item.count }}</b> {{ t(`shelfModel.status.${item.status}`) }}
            </span>
          </div>
          <div v-if="detail?.checklist?.length" class="table-wrap doclist">
            <table>
              <thead>
                <tr>
                  <th>{{ t('shelfModel.state') }}</th>
                  <th>{{ t('shelfModel.material') }}</th>
                  <th>{{ t('shelfModel.stageLabel') }}</th>
                  <th>{{ t('shelfModel.due') }}</th>
                  <th>{{ t('shelfModel.file') }}</th>
                  <th>{{ t('shelfModel.from') }}</th>
                  <th>{{ t('shelfModel.responsible') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, index) in detail.checklist" :id="detail.checklist.findIndex(item => item.requirement.id === row.requirement.id) === index ? `requirement-${row.requirement.id}` : undefined" :key="`${row.requirement.id}-${index}`">
                  <td>
                    <span class="doc" :class="row.status">{{ t(`shelfModel.status.${row.status}`) }}</span>
                    <small v-if="row.ambiguous" role="status">{{ t('shelfModel.ambiguous') }}</small>
                  </td>
                  <td>{{ row.requirement.canonical_name }}
                    <small>{{ requirement(row.requirement).name_en }}</small>
                    <small>{{ requirement(row.requirement).file_category }}</small>
                    <small>{{ requirement(row.requirement).source_list }} {{ requirement(row.requirement).source_version }}</small>
                  </td>
                  <td>{{ requirement(row.requirement).stage_label }}</td>
                  <td>
                    <small v-if="requirement(row.requirement).draft_due_stage">{{ t('shelfModel.draft') }} {{ stage(requirement(row.requirement).draft_due_stage) }}</small>
                    <small v-if="requirement(row.requirement).final_due_stage">{{ t('shelfModel.final') }} {{ stage(requirement(row.requirement).final_due_stage) }}</small>
                    <small>{{ requirement(row.requirement).due_text }}</small>
                  </td>
                  <td>
                    <template v-if="row.document">
                      <a :href="fileHref(row.document.file_id)">{{ row.document_name || t('shelfModel.file') }}</a>
                      <a v-if="documentURL(row.document)" :href="documentURL(row.document)" target="_blank" rel="noopener noreferrer">{{ t(row.document.attachment_id ? 'shelfModel.download' : 'shelfModel.cloudLink') }}</a>
                      <small>{{ t(`shelfModel.${row.document.stage}`) }} {{ row.document.version_label }}</small>
                    </template>
                    <span v-else>{{ t('shelfStatus.noRecord') }}</span>
                  </td>
                  <td>
                    <span v-if="row.owner_id">{{ ownerText(row.owner_id) }}</span>
                    <small v-if="!selected" class="condition">{{ conditionText(row.conditions) }}</small>
                  </td>
                  <td>{{ requirement(row.requirement).owner }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section id="params">
          <h2>{{ t('shelfModel.sections.params') }}</h2>
          <dl v-if="fields.length" class="params">
            <div v-for="field in fields" :key="field.key">
              <dt>{{ field.name }}</dt>
              <dd>
                <div v-for="(value, index) in detail?.parameters?.[field.key]" :key="index">{{ valueText(value.value) }}{{ field.unit ? ` ${field.unit}` : '' }}
                  <small v-if="!selected" class="condition">{{ conditionText(value.conditions) }}</small>
                </div>
                <span v-if="!detail?.parameters?.[field.key]?.length">{{ t('shelfStatus.noRecord') }}</span>
              </dd>
            </div>
          </dl>
          <p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
        <section id="files">
          <h2>{{ t('shelfModel.sections.files') }}</h2>
          <div v-if="detail?.other_documents?.length" class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{{ t('shelfModel.file') }}</th>
                  <th>{{ t('shelfModel.category') }}</th>
                  <th>{{ t('shelfModel.from') }}</th>
                  <th>{{ t('shelfModel.version') }}</th>
                  <th>{{ t('shelfModel.text') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="file in detail.other_documents" :key="file.document.id">
                  <td>
                    <a :href="fileHref(file.entity.id)">{{ file.entity.canonical_name }}</a>
                    <a v-if="documentURL(file.document)" :href="documentURL(file.document)" target="_blank" rel="noopener noreferrer">{{ t(file.document.attachment_id ? 'shelfModel.download' : 'shelfModel.cloudLink') }}</a>
                  </td>
                  <td>{{ file.document.file_category }}</td>
                  <td>{{ t(`shelfModel.origins.${file.origin}`) }} · {{ kind(file.owner) }} {{ file.owner.canonical_name }}
                    <small v-if="!selected" class="condition">{{ conditionText(file.conditions) }}</small>
                  </td>
                  <td>{{ file.document.version_label }}</td>
                  <td>
                    <a v-if="file.document.entry_id" :href="`/platform/entries/${encodeURIComponent(file.document.entry_id)}`">{{ t('shelfModel.openText') }}</a>
                    <span>{{ parseStatus(file.document.parse_status) }}</span>
                    <small v-if="file.document.parse_error">{{ file.document.parse_error }}</small>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else>{{ t('productShelf.noRecords') }}</p>
        </section>
      </template>
    </template>
    <section id="history">
      <h2>{{ t('shelfModel.sections.history') }}</h2>
      <p v-if="historyLoading" role="status">{{ t('common.loading') }}</p>
      <ErrorState v-if="historyError" :title="historyError">
        <Button @click="loadHistory(historyPage)">{{ t('common.retry') }}</Button>
      </ErrorState>
      <ol v-if="history?.items.length" class="history">
        <li v-for="item in history.items" :key="item.id">
          <time :datetime="item.created_at">{{ formatDateTime(item.created_at, locale, { dateStyle: 'short', timeStyle: 'short' }) }}</time>
          <strong>{{ sourceText(item) }}</strong>
          <div>{{ operationName(item) }}
            <small v-if="item.changeset_id">{{ t('shelfModel.changeset') }} {{ item.changeset_id }}</small>
            <details>
              <summary>{{ t('managed.changes') }}</summary>
              <pre>{{ JSON.stringify({ before: item.before, after: item.after }, null, 2) }}</pre>
            </details>
          </div>
        </li>
      </ol>
      <p v-if="history && !history.items.length">{{ t('managed.noHistory') }}</p>
      <div class="history-paging">
        <Button :disabled="historyLoading || historyPage <= 1" @click="loadHistory(historyPage - 1)">{{ t('follows.previousPage') }}</Button>
        <span>{{ historyPage }}</span>
        <Button :disabled="historyLoading || !history || historyPage * history.page_size >= history.total" @click="loadHistory(historyPage + 1)">{{ t('follows.nextPage') }}</Button>
      </div>
    </section>
  </article>
</template>

<style scoped>
.shelf-model { display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; padding: var(--space-5) var(--space-6) var(--space-7); color: var(--text); }
h1, h2, h3, p, dl { margin: 0; }
h1 { margin-top: var(--space-4); font-family: var(--mono); font-size: var(--text-2xl); font-weight: 800; letter-spacing: -0.03em; overflow-wrap: anywhere; }
h2 { font-size: var(--text-xl); font-weight: 800; }
h3 { font-size: var(--text-xs); }
a { color: inherit; text-decoration: underline; cursor: pointer; }
a:hover { color: var(--danger); } a:focus-visible, summary:focus-visible { outline: 2px solid var(--text); outline-offset: 3px; }
a:active { opacity: 0.7; }
.crumb, .toc, .tally { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2) var(--space-3); font-size: var(--text-sm); }
.kind { border: 1px solid var(--text); padding-inline: 4px; font-size: var(--text-xs); font-weight: 700; }
.facts { display: grid; grid-template-columns: 1fr 1fr 1fr 2fr; min-height: 88px; border-top: 2px solid var(--rule); }
.facts > div { padding-block: var(--space-3); }
.summary-counts span + span::before { content: " · "; }
dt { font-size: var(--text-xs); font-weight: 700; }
dd { margin: var(--space-1) 0 0; }
.combo { position: sticky; top: 0; z-index: 5; padding-block: var(--space-3); border-block: 1px solid var(--rule); border-top-width: 2px; background: var(--bg); }
fieldset { display: flex; flex-wrap: wrap; gap: var(--space-2); margin: 0; padding: 0; border: 0; }
fieldset + fieldset { margin-top: var(--space-3); }
legend { float: left; margin-right: var(--space-3); padding-top: 8px; font-weight: 700; }
label { position: relative; cursor: pointer; }
label span { display: inline-block; min-height: 40px; padding: 8px; border: 1px solid var(--text); font-size: var(--text-sm); }
input[type=radio] { position: absolute; opacity: 0; }
input:checked + span { background: var(--text); color: var(--bg); font-weight: 700; }
input:focus-visible + span { outline: 2px solid var(--text); outline-offset: 3px; }
label:hover span { text-decoration: underline; }
label:active span { background: var(--surface-soft); color: var(--text); }
section { display: flex; flex-direction: column; gap: var(--space-3); scroll-margin-top: 160px; min-width: 0; }
.loading { min-height: 160px; }
.table-wrap { overflow: auto; max-height: 560px; border-top: 2px solid var(--rule); }
table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: 8px 12px; border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; min-width: 100px; overflow-wrap: anywhere; }
thead th { position: sticky; top: 0; z-index: 1; background: var(--bg); font-size: var(--text-xs); border-bottom-color: var(--rule); }
.group th { border-bottom-color: var(--rule); font-size: var(--text-xs); padding-top: 16px; }
.chosen td { background: var(--surface-soft); }
small { display: block; font-size: var(--text-xs); color: var(--muted); white-space: pre-wrap; overflow-wrap: anywhere; }
.condition { margin-top: 4px; }
.regions { display: flex; border-top: 2px solid var(--rule); overflow-x: auto; }
.regions > div { flex: 1; min-width: 110px; padding: 8px 12px 8px 0; }
.sale-record { display: grid; gap: 4px; padding-block: 8px; font-size: var(--text-xs); }
.st, .doc { display: inline-block; width: max-content; padding: 1px 8px; font-size: var(--text-xs); font-weight: 800; }
.st-GA, .delivered { background: var(--success); color: var(--bg); }
.st-DEV { background: var(--blue); color: var(--on-blue); }
.st-Planning, .draft_only { background: var(--surface-soft); }
.st-EOM { background: var(--muted); color: var(--bg); }
.st-待定, .not_due { outline: 1px dashed var(--muted); outline-offset: -1px; }
.missing { background: var(--danger-soft); color: var(--danger); }
.params { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-top: 2px solid var(--rule); }
.params > div { padding: 8px 12px 8px 0; border-bottom: 1px solid var(--border); }
.params dt { color: var(--muted); }
.params dd { font-weight: 700; }
.doclist tr[id] { scroll-margin-top: 160px; }
.tally b { font-family: var(--mono); font-size: var(--text-lg); }
.history { list-style: none; padding: 0; margin: 0; border-top: 2px solid var(--rule); }
.history li { display: grid; grid-template-columns: 160px 160px minmax(0, 1fr); gap: 12px; padding-block: 8px; border-bottom: 1px solid var(--border); font-size: var(--text-sm); overflow-wrap: anywhere; }
time { font-family: var(--mono); font-size: var(--text-xs); }
summary { cursor: pointer; }
pre { max-width: 100%; max-height: 320px; overflow: auto; white-space: pre-wrap; }
.history-paging { display: flex; align-items: center; gap: 12px; }
</style>
