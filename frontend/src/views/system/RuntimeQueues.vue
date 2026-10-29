<template>
  <div class="runtime-queues settings-form">
    <header class="section-header rq-header">
      <div class="rq-title-block">
        <p class="section-description">{{ t('system.globalSettings.runtime.description') }}</p>
      </div>
      <div class="rq-actions">
        <label class="rq-auto-refresh">
          <span class="rq-live-dot" :class="{ 'rq-live-dot--active': autoRefresh }" />
          <span>{{ t('system.globalSettings.runtime.autoRefresh') }}</span>
          <Switch
            v-model="autoRefresh"
            :aria-label="t('system.globalSettings.runtime.autoRefresh')"
          />
        </label>
        <button
          type="button"
          class="rq-refresh"
          :disabled="loading"
          :title="t('system.globalSettings.runtime.refresh')"
          :aria-label="t('system.globalSettings.runtime.refresh')"
          @click="reload"
        >
          {{ t('system.globalSettings.runtime.refresh') }}
        </button>
      </div>
    </header>

    <QueueConnectionNotice :status="queueConnection" />

    <ChatGPTUsageStatus v-if="chatgptPolicy?.paused || chatgptAccounts.some(account => account.status === 'needs_login')" :policy="chatgptPolicy" :accounts="chatgptAccounts" :busy="chatgptResuming" :error="chatgptError" settings-link @resume="resumeChatGPTUsage" />

    <div v-if="chatgptError && !chatgptPolicy" class="rq-state rq-state--error" role="alert">
      <div class="rq-state-copy"><strong>{{ t('chatgpt.loadFailed') }}</strong><span>{{ chatgptError }}</span></div>
      <Button variant="default" @click="loadChatGPTStatus">{{ t('system.globalSettings.runtime.retry') }}</Button>
    </div>

    <div v-if="loading && !loadedOnce" class="rq-loading" aria-live="polite">
      <div class="rq-loading-metrics">
        <Skeleton :label="t('common.loading')" />
      </div>
      <Skeleton :label="t('common.loading')" />
    </div>

    <div v-else-if="error" class="rq-state rq-state--error" role="alert">
      <div class="rq-state-icon"></div>
      <div class="rq-state-copy">
        <strong>{{ t('system.globalSettings.runtime.errors.generic') }}</strong>
        <span>{{ error }}</span>
      </div>
      <Button variant="default" @click="reload">
        {{ t('system.globalSettings.runtime.retry') }}
      </Button>
    </div>

    <div v-else-if="!available && !modelLimiterAvailable" class="rq-state">
      <div class="rq-state-icon"></div>
      <div class="rq-state-copy">
        <strong>{{ t('system.globalSettings.runtime.unavailableTitle') }}</strong>
        <span>{{ t('system.globalSettings.runtime.unavailable') }}</span>
      </div>
    </div>

    <template v-else>
      <template v-if="available">
      <section class="rq-overview" :aria-label="t('system.globalSettings.runtime.summary.title')">
        <div class="rq-overview-title">
          <span class="rq-overview-mark"></span>
          <span>{{ t('system.globalSettings.runtime.summary.title') }}</span>
        </div>
        <div class="rq-overview-metrics">
          <div class="rq-metric rq-metric--active">
            <span class="rq-metric-label">{{ t('system.globalSettings.runtime.summary.active') }}</span>
            <strong class="rq-metric-value">{{ totalActive }}</strong>
          </div>
          <div class="rq-metric">
            <span class="rq-metric-label">{{ t('system.globalSettings.runtime.summary.pending') }}</span>
            <strong class="rq-metric-value">{{ totalPending }}</strong>
          </div>
          <div class="rq-metric" :class="{ 'rq-metric--warning': totalRetry > 0 }">
            <span class="rq-metric-label">{{ t('system.globalSettings.runtime.summary.retry') }}</span>
            <strong class="rq-metric-value">{{ totalRetry }}</strong>
          </div>
          <div class="rq-metric" :class="{ 'rq-metric--danger': totalArchived > 0 }">
            <span class="rq-metric-label">{{ t('system.globalSettings.runtime.summary.archived') }}</span>
            <strong class="rq-metric-value">{{ totalArchived }}</strong>
          </div>
        </div>
      </section>

      <section class="rq-pools">
        <div class="rq-pools-header">
          <div>
            <h3 class="rq-section-title">{{ t('system.globalSettings.runtime.poolsTitle') }}</h3>
            <p>{{ t('system.globalSettings.runtime.poolsDescription') }}</p>
          </div>
          <span class="rq-pools-note">{{ t('system.globalSettings.runtime.perInstance') }}</span>
        </div>
        <div class="rq-pool-grid">
          <div v-for="pool in pools" :key="pool.name" class="rq-pool-card">
            <div class="rq-pool-topline">
              <span class="rq-pool-name">{{ poolLabel(pool.name) }}</span>
              <strong class="rq-pool-value">
                {{ pool.instances > 0 ? `${pool.active}/${pool.cluster_capacity}` : pool.concurrency }}
              </strong>
            </div>
            <p class="rq-pool-desc">
              {{ poolDescription(pool.name) }}
              <span class="rq-pool-meta">
                {{ t('system.globalSettings.runtime.poolConfigured', { value: pool.concurrency }) }}
                <template v-if="pool.instances > 0">
                  · {{ t('system.globalSettings.runtime.poolInstances', { value: pool.instances }, pool.instances) }}
                  · {{ t('system.globalSettings.runtime.poolUtilization', { value: poolUtilization(pool) }) }}
                </template>
                · {{ t('system.globalSettings.runtime.queueCount', { value: pool.queue_count }, pool.queue_count) }}
              </span>
            </p>
          </div>
        </div>
      </section>

      <section class="rq-details">
        <div class="rq-details-header">
          <div>
            <h3 class="rq-section-title">{{ t('system.globalSettings.runtime.detailsTitle') }}</h3>
            <p>{{ t('system.globalSettings.runtime.detailsDescription') }}</p>
          </div>
          <span v-if="updatedAt" class="rq-updated-at">

            {{ t('system.globalSettings.runtime.updatedAt', { value: updatedAt }) }}
          </span>
        </div>

        <div v-if="totalArchived > 0" class="rq-failed-notice" role="status">
          <span class="rq-failed-notice__icon" aria-hidden="true">

          </span>
          <div class="rq-failed-notice__text">
            <p class="rq-failed-notice__title">
              {{ t('system.globalSettings.runtime.failedNotice.title', { count: totalArchived }) }}
            </p>
            <p class="rq-failed-notice__desc">
              {{ t('system.globalSettings.runtime.failedNotice.description') }}
            </p>
          </div>
        </div>

        <div v-if="queues.length === 0" class="rq-empty">

          <span>{{ t('system.globalSettings.runtime.empty') }}</span>
        </div>

        <div v-else class="data-table-shell rq-table-shell">
          <DataTable
            row-key="name"
            :rows="queues" v-model:focused="focusedQueue" :label="t('system.globalSettings.runtime.detailsTitle')"
            :columns="columns"
            hover
          >
            <template #cell-name="{ row }">
              <div class="rq-queue-cell">
                <span class="rq-queue-name">{{ queueLabel(row.name) }}</span>
                <span class="rq-queue-meta">{{ queueMeta(row) }}</span>
              </div>
            </template>
            <template #cell-active="{ row }">
              <Button
                v-if="row.active > 0"
                variant="ghost"

                class="rq-task-count rq-task-count--active"
                @click="openRuntimeTasks(row, 'active')"
              >
                {{ row.active }}
              </Button>
              <span v-else class="rq-number">0</span>
            </template>
            <template #cell-pending="{ row }">
              <div class="rq-backlog">
                <Button
                  v-if="row.pending > 0"
                  variant="ghost"

                  class="rq-task-count"
                  @click="openRuntimeTasks(row, 'pending')"
                >{{ row.pending }}</Button>
                <span v-else class="rq-number">0</span>
                <Button
                  v-if="row.scheduled > 0"
                  variant="ghost"

                  class="rq-scheduled-count"
                  @click="openRuntimeTasks(row, 'scheduled')"
                >+{{ row.scheduled }} {{ t('system.globalSettings.runtime.columns.scheduled') }}</Button>
              </div>
            </template>
            <template #cell-retry="{ row }">
              <Button
                v-if="row.retry > 0"
                variant="ghost"

                class="rq-task-count"
                @click="openRuntimeTasks(row, 'retry')"
              >{{ row.retry }}</Button>
              <span v-else class="rq-number">0</span>
            </template>
            <template #cell-archived="{ row }">
              <Button
                v-if="row.archived > 0"
                variant="ghost"

                class="rq-task-count rq-failed-count"
                :aria-label="t('system.globalSettings.runtime.tasks.openAria', { state: taskStateLabel('archived'), queue: queueLabel(row.name), count: row.archived })"
                @click="openRuntimeTasks(row, 'archived')"
              >
                {{ row.archived }}
              </Button>
              <span v-else class="rq-number">0</span>
            </template>
            <template #cell-completed="{ row }">
              <Button
                v-if="row.completed > 0"
                variant="ghost"

                class="rq-task-count rq-task-count--completed"
                @click="openRuntimeTasks(row, 'completed')"
              >{{ row.completed }}</Button>
              <span v-else class="rq-number">0</span>
            </template>
            <template #cell-cancelled="{ row }">
              <Button
                v-if="(row.cancelled || 0) > 0"
                variant="ghost"

                class="rq-task-count"
                @click="openRuntimeTasks(row, 'cancelled')"
              >{{ row.cancelled }}</Button>
              <span v-else class="rq-number">0</span>
            </template>
            <template #cell-latency_ms="{ row }">
              <span class="rq-latency">{{ formatLatency(row.latency_ms) }}</span>
            </template>
            <template #cell-status="{ row }">
              <span class="rq-status" :class="`rq-status--${queueState(row).tone}`">
                <i />{{ queueState(row).label }}
              </span>
            </template>
          </DataTable>
        </div>
      </section>
      </template>

      <section class="rq-details rq-models">
        <div class="rq-details-header">
          <div>
            <h3 class="rq-section-title">{{ t('system.globalSettings.runtime.models.title') }}</h3>
            <p>{{ t('system.globalSettings.runtime.models.description') }}</p>
          </div>
          <span class="rq-pools-note">{{ t('system.globalSettings.runtime.models.scope') }}</span>
        </div>
        <div v-if="!modelLimiterAvailable" class="rq-empty">

          <span>{{ t('system.globalSettings.runtime.models.disabled') }}</span>
        </div>
        <div v-else-if="models.length === 0" class="rq-empty">

          <span>{{ t('system.globalSettings.runtime.models.empty') }}</span>
        </div>
        <div v-else class="data-table-shell rq-table-shell">
          <DataTable row-key="model_id" :rows="models" v-model:focused="focusedModel" :label="t('system.globalSettings.runtime.models.title')" :columns="modelColumns" hover>
            <template #cell-model_id="{ row }">
              <div class="rq-queue-cell">
                <span class="rq-queue-name">{{ row.name || row.model_id }}</span>
                <span class="rq-queue-meta" :title="row.model_id">{{ row.name ? row.model_id.slice(0, 8) : t('system.globalSettings.runtime.models.backgroundOnly') }}</span>
              </div>
            </template>
            <template #cell-active="{ row }"><span class="rq-number" :class="{ 'rq-number--active': row.active > 0 }">{{ row.active }}</span></template>
            <template #cell-waiting="{ row }"><span class="rq-number" :class="{ 'rq-number--warning': row.waiting > 0 }">{{ row.waiting }}</span></template>
            <template #cell-usage="{ row }">
              <div class="rq-model-usage">
                <progress :value="modelUsage(row)" max="100" :aria-label="t('system.globalSettings.runtime.models.columns.usage')" />
                <span>{{ row.active }} / {{ row.limit }}</span>
              </div>
            </template>
            <template #cell-status="{ row }">
              <span class="rq-status" :class="`rq-status--${modelState(row).tone}`"><i />{{ modelState(row).label }}</span>
            </template>
          </DataTable>
        </div>
      </section>

      <p class="rq-footnote">{{ t('system.globalSettings.runtime.footnote') }}</p>
    </template>

    <Drawer v-model="taskDrawerVisible" :title="t('system.globalSettings.runtime.tasks.title', { queue: taskQueueLabel })" :description="t('system.globalSettings.runtime.tasks.description')" content-class="runtime-task-drawer">
      <section class="setting-drawer__section">
        <div
          class="rq-task-state-filter"
          role="tablist"
          :aria-label="t('system.globalSettings.runtime.tasks.stateFilter')"
        >
          <button
            v-for="state in taskStates"
            :key="state"
            type="button"
            role="tab"
            class="rq-task-state-option"
            :class="{ 'is-active': taskState === state }"
            :aria-selected="taskState === state"
            @click="selectTaskState(state)"
          >
            <span class="rq-task-state-option__label">{{ taskStateLabel(state) }}</span>
            <span
              class="rq-task-state-option__count"
              :class="{ 'has-value': taskStateCount(taskQueue, state) > 0 }"
            >
              {{ taskStateCount(taskQueue, state) }}
            </span>
          </button>
        </div>
        <p class="rq-failed-guide-desc">{{ taskStateGuide }}</p>
      </section>

      <section class="setting-drawer__section">
        <div class="rq-failed-section-head">
          <h4 class="setting-drawer__section-title">
            {{ t('system.globalSettings.runtime.tasks.listTitle', { state: taskStateLabel(taskState) }) }}
          </h4>
          <div class="rq-failed-section-actions">
            <template v-if="taskState === 'archived' && tasks.length > 0">
              <Button variant="danger" @click="confirmAction(formatCountCopy(t, 'system.globalSettings.runtime.tasks.purgeArchivedConfirm', { count: taskStateCount(taskQueue, 'archived') }), () => purgeArchivedTasks())"

                :loading="purging"
                :disabled="Boolean(taskActionID)"
              >

                {{ t('system.globalSettings.runtime.tasks.purgeArchived') }}
              </Button>
            </template>
            <Button
              variant="ghost"
              :loading="tasksLoading && !tasksLoadingMore"
              @click="reloadRuntimeTasks"
            >

              {{ t('system.globalSettings.runtime.refresh') }}
            </Button>
          </div>
        </div>

        <div v-if="tasksLoading && tasks.length === 0" class="rq-failed-loading">
          <p role="status">{{ t('common.loading') }}</p>
          <span>{{ t('system.globalSettings.runtime.loading') }}</span>
        </div>
        <div v-else-if="tasksError" class="rq-failed-error-state">
          <span>{{ tasksError }}</span>
          <Button variant="default" @click="reloadRuntimeTasks">
            {{ t('system.globalSettings.runtime.retry') }}
          </Button>
        </div>
        <p
          v-else-if="tasks.length === 0"
         role="status">{{ t('system.globalSettings.runtime.tasks.empty', { state: taskStateLabel(taskState) }) }}</p>
        <div v-else class="rq-failed-list-panel">
          <article
            v-for="task in tasks"
            :key="task.id"
            class="rq-failed-row"
          >
            <div class="rq-failed-row-content">
              <div class="rq-failed-row-summary">
                <span class="rq-failed-row-type">{{ runtimeTaskTypeLabel(task.type) }}</span>
                <span class="rq-failed-row-sep" aria-hidden="true">·</span>
                <span class="rq-task-state-pill" :class="`rq-task-state-pill--${task.state}`">
                  {{ taskStateLabel(task.state) }}
                </span>
                <span class="rq-failed-row-sep" aria-hidden="true">·</span>
                <span class="rq-failed-row-stat">
                  {{ t('system.globalSettings.runtime.tasks.attempts', { current: task.retried + 1, max: task.max_retry + 1 }) }}
                </span>
              </div>
              <a v-if="task.batch_id" :href="`/platform/entries?batch_id=${encodeURIComponent(task.batch_id)}`">{{ t('entryBatch.resultLink') }}</a>
              <dl v-if="runtimeTaskMeta(task).length > 0" class="rq-failed-row-refs">
                <div v-for="ref in runtimeTaskMeta(task)" :key="ref.key" class="rq-failed-ref">
                  <dt>{{ ref.label }}</dt>
                  <dd :title="ref.value">{{ ref.value.length > 36 ? ref.value.slice(0, 12) + '…' : ref.value }}</dd>
                </div>
              </dl>
              <p v-else class="rq-failed-row-unknown">
                {{ t('system.globalSettings.runtime.tasks.unknownTarget') }}
              </p>
              <p v-if="task.cancellation_unavailable_reason" class="rq-failed-guide-desc">
                {{ t('system.globalSettings.runtime.tasks.cancelUnavailable', { state: task.cancellation_unavailable_reason === 'completed' ? t('system.globalSettings.runtime.tasks.states.completed') : t('system.globalSettings.runtime.tasks.businessDeleting') }) }}
              </p>
              <p v-if="task.last_error" class="rq-failed-row-error">
                {{ task.last_error }}
              </p>
            </div>

            <div v-if="task.allowed_actions.length > 0" class="rq-failed-row-actions">
              <template v-if="task.allowed_actions.includes('cancel')">
                <Button variant="danger" @click="confirmAction(t('system.globalSettings.runtime.tasks.cancelConfirm'), () => runTaskAction(task, 'cancel'))"

                  class="rq-task-action"
                  :title="t('system.globalSettings.runtime.tasks.cancel')"
                  :aria-label="t('system.globalSettings.runtime.tasks.cancel')"
                  :loading="taskActionID === task.id && taskAction === 'cancel'"
                  :disabled="Boolean(taskActionID)"
                >{{ t('common.cancel') }}</Button>
              </template>
              <template v-if="task.allowed_actions.includes('run_now')">
                <Button @click="confirmAction(t('system.globalSettings.runtime.tasks.runNowConfirm'), () => runTaskAction(task, 'run_now'))"

                  variant="ghost"

                  class="rq-task-action"
                  :title="t('system.globalSettings.runtime.tasks.runNow')"
                  :aria-label="t('system.globalSettings.runtime.tasks.runNow')"
                  :loading="taskActionID === task.id && taskAction === 'run_now'"
                  :disabled="Boolean(taskActionID)"
                >{{ t('system.globalSettings.runtime.tasks.runNow') }}</Button>
              </template>
              <template v-if="task.allowed_actions.includes('delete')">
                <Button variant="danger" @click="confirmAction(t(task.state === 'cancelled' ? 'system.globalSettings.runtime.tasks.deleteCancelledConfirm' : 'system.globalSettings.runtime.tasks.deleteConfirm'), () => runTaskAction(task, 'delete'))"

                  class="rq-task-action"
                  :title="t('system.globalSettings.runtime.tasks.deleteRecord')"
                  :aria-label="t('system.globalSettings.runtime.tasks.deleteRecord')"
                  :loading="taskActionID === task.id && taskAction === 'delete'"
                  :disabled="Boolean(taskActionID)"
                >{{ t('common.delete') }}</Button>
              </template>
            </div>
          </article>

          <div ref="tasksSentinelRef" class="rq-failed-load-sentinel" aria-hidden="true" />

          <div class="rq-failed-list-footer">
            <span class="rq-failed-list-status">
              <template v-if="tasksLoadingMore">
                {{ t('system.globalSettings.runtime.tasks.loadingMore') }}
              </template>
              <template v-else-if="!tasksHasMore">
                {{ t('system.globalSettings.runtime.tasks.loadedAll', { count: tasks.length }) }}
              </template>
              <template v-else>
                {{ t('system.globalSettings.runtime.tasks.loadedSummary', { count: tasks.length }) }}
              </template>
            </span>
            <Button
              v-if="tasksHasMore"
              variant="default"
              block
              :loading="tasksLoadingMore"
              @click="loadMoreRuntimeTasks"
            >
              {{ t('system.globalSettings.runtime.tasks.loadMore') }}
            </Button>
          </div>
        </div>
      </section>
    </Drawer>
  </div>
</template>

<script setup lang="ts">
import { Button, Switch, Drawer, DataTable, Skeleton, toast, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import QueueConnectionNotice from './QueueConnectionNotice.vue'
import type { QueueConnectionStatus } from '@/api/system'
import { formatCountCopy } from '@/i18n/countCopy'
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import ChatGPTUsageStatus from '@/components/ChatGPTUsageStatus.vue'
import { getChatGPTAccounts, getChatGPTPolicy, resumeChatGPT, type ChatGPTAccount, type ChatGPTPolicy } from '@/api/chatgpt'
import { mergeRuntimeTaskPage } from './runtimeTaskPagination'
import {
  getRuntimeTasks,
  getRuntimeQueues,
  getQueueConnectionStatus,
  mutateRuntimeTask,
  purgeArchivedRuntimeTasks,
  type ModelRuntimeStat,
  type QueueStat,
  type RuntimeTask,
  type RuntimeTaskAction,
  type RuntimeTaskState,
  type RuntimeWorkerPool,
} from '@/api/system'

const confirm = useConfirm()
const { t, te, locale } = useI18n()

const chatgptPolicy = ref<ChatGPTPolicy | null>(null), chatgptAccounts = ref<ChatGPTAccount[]>([])
const chatgptResuming = ref(false), chatgptError = ref('')
let chatgptRequestVersion = 0
async function loadChatGPTStatus() {
  const version = ++chatgptRequestVersion
  try {
    const [accounts, policy] = await Promise.all([getChatGPTAccounts(), getChatGPTPolicy()])
    if (version !== chatgptRequestVersion) return
    chatgptAccounts.value = accounts.accounts; chatgptPolicy.value = policy; chatgptError.value = ''
  } catch (err: any) { if (version === chatgptRequestVersion) chatgptError.value = err?.message || t('chatgpt.loadFailed') }
}
async function resumeChatGPTUsage() {
  if (chatgptResuming.value) return
  chatgptResuming.value = true; chatgptError.value = ''
  try { chatgptPolicy.value = await resumeChatGPT() }
  catch (err: any) { chatgptError.value = err?.message || t('chatgpt.resumeFailed') }
  finally { chatgptResuming.value = false }
}

const POLL_INTERVAL_MS = 5000

const queues = ref<QueueStat[]>([])
const pools = ref<RuntimeWorkerPool[]>([])
const models = ref<ModelRuntimeStat[]>([])
const modelLimiterAvailable = ref(false)
const queueConnection = ref<QueueConnectionStatus>()
const available = ref(true)
const loading = ref(false)
const loadedOnce = ref(false)
const error = ref('')
const autoRefresh = ref(true)
const updatedAt = ref('')
const taskDrawerVisible = ref(false)
const taskQueue = ref<QueueStat | null>(null)
const taskState = ref<RuntimeTaskState>('archived')
const tasks = ref<RuntimeTask[]>([])
const tasksLoading = ref(false)
const tasksLoadingMore = ref(false)
const tasksError = ref('')
const tasksCursor = ref('')
const tasksHasMore = ref(false)
const tasksSentinelRef = ref<HTMLElement | null>(null)
const taskActionID = ref('')
const taskAction = ref<RuntimeTaskAction | ''>('')
const purging = ref(false)

const TASK_PAGE_SIZE = 20
const taskStates: RuntimeTaskState[] = ['active', 'pending', 'scheduled', 'retry', 'archived', 'completed', 'cancelled']
const runtimeTaskTypeKeys: Record<string, string> = {
  'entry:batch': 'entryBatch',
  'document:process': 'documentProcess',
  'manual:process': 'manualProcess',
  'knowledge:list_reparse': 'batchReparse',
  'knowledge:list_delete': 'batchDelete',
  'index:delete': 'indexDelete',
  'report:index': 'reportIndex',
  'entity:name_embedding_rebuild': 'entityNameEmbeddingRebuild',
  'knowledge:embedding_rebuild': 'embeddingRebuild',
  'topic:backfill': 'topicBackfill',
}

let pollTimer: ReturnType<typeof setInterval> | null = null
let tasksScrollObserver: IntersectionObserver | null = null
let tasksRequestID = 0

function confirmAction(body: string, action: () => unknown) {
  confirm({ header: t('common.confirm'), body, confirmBtn: t('common.confirm'), cancelBtn: t('common.cancel'), theme: 'danger', onConfirm: async () => { await action() } })
}
const focusedQueue = ref(''), focusedModel = ref('')
const columns = computed(() => [
  { key: 'name', label: t('system.globalSettings.runtime.columns.queue'), minWidth: 188 },
  { key: 'active', label: t('system.globalSettings.runtime.columns.active'), width: 74, align: 'center' as const },
  { key: 'pending', label: t('system.globalSettings.runtime.columns.pending'), width: 84, align: 'center' as const },
  { key: 'retry', label: t('system.globalSettings.runtime.columns.retry'), width: 68, align: 'center' as const },
  { key: 'archived', label: t('system.globalSettings.runtime.columns.archived'), width: 96, align: 'center' as const },
  { key: 'completed', label: t('system.globalSettings.runtime.columns.completed'), width: 84, align: 'center' as const },
  { key: 'cancelled', label: t('system.globalSettings.runtime.tasks.states.cancelled'), width: 84, align: 'center' as const },
  { key: 'latency_ms', label: t('system.globalSettings.runtime.columns.latency'), width: 104, align: 'center' as const },
  { key: 'status', label: t('system.globalSettings.runtime.columns.status'), width: 96 },
])
const modelColumns = computed(() => [
  { key: 'model_id', label: t('system.globalSettings.runtime.models.columns.model'), minWidth: 240 },
  { key: 'active', label: t('system.globalSettings.runtime.models.columns.active'), width: 86, align: 'center' as const },
  { key: 'waiting', label: t('system.globalSettings.runtime.models.columns.waiting'), width: 86, align: 'center' as const },
  { key: 'usage', label: t('system.globalSettings.runtime.models.columns.usage'), width: 190 },
  { key: 'status', label: t('system.globalSettings.runtime.columns.status'), width: 96 },
])

function modelUsage(row: ModelRuntimeStat): number {
  return row.limit > 0 ? Math.min(100, Math.round(row.active / row.limit * 100)) : 0
}

function modelState(row: ModelRuntimeStat): { label: string; tone: string } {
  if (row.waiting > 0) return { label: t('system.globalSettings.runtime.models.status.queued'), tone: 'attention' }
  if (row.active >= row.limit) return { label: t('system.globalSettings.runtime.models.status.full'), tone: 'waiting' }
  if (row.active > 0) return { label: t('system.globalSettings.runtime.status.working'), tone: 'working' }
  return { label: t('system.globalSettings.runtime.status.idle'), tone: 'idle' }
}

const totalActive = computed(() => queues.value.reduce((s, q) => s + q.active, 0))
const totalPending = computed(() => queues.value.reduce((s, q) => s + q.pending, 0))
const totalRetry = computed(() => queues.value.reduce((s, q) => s + q.retry, 0))
const totalArchived = computed(() => queues.value.reduce((s, q) => s + q.archived, 0))
const taskQueueLabel = computed(() => taskQueue.value ? queueLabel(taskQueue.value.name) : '')
const taskStateGuide = computed(() => t(`system.globalSettings.runtime.tasks.guides.${taskState.value}`))

// Friendly per-queue label lives in i18n; falls back to the raw queue
// name so a queue added on the backend still renders before translations
// catch up.
function queueLabel(name: string): string {
  const path = `system.globalSettings.runtime.queueNames.${name}`
  return te(path) ? (t(path) as string) : name
}

function queueDescription(name: string): string {
  const path = `system.globalSettings.runtime.queueDescriptions.${name}`
  return te(path) ? (t(path) as string) : name
}

function queueMeta(row: QueueStat): string {
  const scope = queueDescription(row.name)
  if (poolQueueCount(row.pool) > 1) {
    return `${scope} · ${t('system.globalSettings.runtime.weightShort', { value: row.weight })}`
  }
  return scope
}

function runtimeTaskTypeLabel(type: string): string {
  const key = runtimeTaskTypeKeys[type]
  if (!key) return type
  const path = `system.globalSettings.runtime.tasks.taskTypes.${key}`
  return te(path) ? (t(path) as string) : type
}

interface RuntimeTaskMeta {
  key: string
  label: string
  value: string
}

function taskStateLabel(state: RuntimeTaskState): string {
  return t(`system.globalSettings.runtime.tasks.states.${state}`)
}

function taskStateCount(row: QueueStat | null, state: RuntimeTaskState): number {
  if (!row) return 0
  return row[state] ?? 0
}

function formatTaskTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

function runtimeTaskMeta(task: RuntimeTask): RuntimeTaskMeta[] {
  const refs: RuntimeTaskMeta[] = []
  if (task.library_id) {
    refs.push({
      key: 'library',
      label: t('system.globalSettings.runtime.tasks.libraryLabel'),
      value: task.library_id,
    })
  }
  if (task.entry_id) {
    refs.push({
      key: 'entry',
      label: t('system.globalSettings.runtime.tasks.entryLabel'),
      value: task.entry_id,
    })
  }
  if (task.task_id) {
    refs.push({
      key: 'task',
      label: t('system.globalSettings.runtime.tasks.taskIDLabel'),
      value: task.task_id,
    })
  }
  if (task.source_library_id) refs.push({ key: 'source-library', label: t('system.globalSettings.runtime.tasks.sourceLibraryLabel'), value: task.source_library_id })
  if (task.target_library_id) refs.push({ key: 'target-library', label: t('system.globalSettings.runtime.tasks.targetLibraryLabel'), value: task.target_library_id })
  if (task.entry_count) {
    refs.push({ key: 'entry-count', label: t('system.globalSettings.runtime.tasks.entryCountLabel'), value: String(task.entry_count) })
  }
  if (task.space_id) {
    refs.push({
      key: 'space',
      label: t('system.globalSettings.runtime.tasks.spaceLabel'),
      value: String(task.space_id),
    })
  }
  if (task.enqueued_at) refs.push({ key: 'enqueued', label: t('system.globalSettings.runtime.tasks.enqueuedAt'), value: formatTaskTime(task.enqueued_at) })
  if (task.started_at) refs.push({ key: 'started', label: t('system.globalSettings.runtime.tasks.startedAt'), value: formatTaskTime(task.started_at) })
  if (task.next_process_at) refs.push({ key: 'next', label: t('system.globalSettings.runtime.tasks.nextProcessAt'), value: formatTaskTime(task.next_process_at) })
  if (task.last_failed_at) refs.push({ key: 'failed', label: t('system.globalSettings.runtime.tasks.lastFailedAt'), value: formatTaskTime(task.last_failed_at) })
  if (task.completed_at) refs.push({ key: 'completed', label: t('system.globalSettings.runtime.tasks.completedAt'), value: formatTaskTime(task.completed_at) })
  if (task.deadline) refs.push({ key: 'deadline', label: t('system.globalSettings.runtime.tasks.deadline'), value: formatTaskTime(task.deadline) })
  if (task.worker) refs.push({ key: 'worker', label: t('system.globalSettings.runtime.tasks.worker'), value: poolLabel(task.worker) })
  if (task.is_orphaned) refs.push({ key: 'orphaned', label: t('system.globalSettings.runtime.tasks.health'), value: t('system.globalSettings.runtime.tasks.orphaned') })
  return refs
}

function poolLabel(pool: string): string {
  const path = `system.globalSettings.runtime.pools.${pool}`
  return te(path) ? (t(path) as string) : pool
}

function poolDescription(pool: string): string {
  const path = `system.globalSettings.runtime.poolDescriptions.${pool}`
  return te(path) ? (t(path) as string) : pool
}

function poolQueueCount(pool: string): number {
  return pools.value.find((item) => item.name === pool)?.queue_count ?? 0
}

function poolUtilization(pool: RuntimeWorkerPool): number {
  return Math.round(Math.max(0, Math.min(1, pool.utilization || 0)) * 100)
}

function formatLatency(ms: number): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms} ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)} s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  return `${m}m ${rem}s`
}

function queueState(row: QueueStat): { label: string; tone: string } {
  if (row.paused) {
    return { label: t('system.globalSettings.runtime.status.paused'), tone: 'paused' }
  }
  if (row.archived > 0) {
    return { label: t('system.globalSettings.runtime.status.actionRequired'), tone: 'danger' }
  }
  if (row.retry > 0) {
    return { label: t('system.globalSettings.runtime.status.retrying'), tone: 'attention' }
  }
  if (row.active > 0) {
    return { label: t('system.globalSettings.runtime.status.working'), tone: 'working' }
  }
  if (row.pending > 0 || row.scheduled > 0) {
    return { label: t('system.globalSettings.runtime.status.waiting'), tone: 'waiting' }
  }
  return { label: t('system.globalSettings.runtime.status.idle'), tone: 'idle' }
}

async function fetchRuntimeTasks(reset: boolean) {
  const queue = taskQueue.value?.name
  if (!queue) return
  if (!reset && (tasksLoadingMore.value || !tasksHasMore.value)) return

  const requestedState = taskState.value
  const requestID = ++tasksRequestID
  const cursor = reset ? '' : tasksCursor.value
  if (reset) {
    tasksCursor.value = ''
    tasksHasMore.value = false
    tasks.value = []
    tasksLoading.value = true
  } else {
    tasksLoadingMore.value = true
  }
  tasksError.value = ''
  try {
    const response = await getRuntimeTasks(queue, requestedState, cursor, TASK_PAGE_SIZE)
    if (requestID !== tasksRequestID || taskQueue.value?.name !== queue || taskState.value !== requestedState) return
    if (!response.available) {
      tasksError.value = t('system.globalSettings.runtime.tasks.unavailable')
      return
    }
    tasks.value = reset ? response.tasks : mergeRuntimeTaskPage(tasks.value, response.tasks)
    tasksCursor.value = response.next_cursor || ''
    tasksHasMore.value = response.has_more && Boolean(response.next_cursor)
  } catch (err: any) {
    if (requestID !== tasksRequestID) return
    if (!reset && err?.code === 'runtime_task_cursor_expired') {
      tasksLoadingMore.value = false
      await fetchRuntimeTasks(true)
      return
    }
    tasksError.value = err?.message || t('system.globalSettings.runtime.tasks.loadError')
  } finally {
    if (requestID !== tasksRequestID) return
    if (reset) {
      tasksLoading.value = false
    } else {
      tasksLoadingMore.value = false
    }
    await nextTick()
    attachTasksScrollObserver()
  }
}

function detachTasksScrollObserver() {
  tasksScrollObserver?.disconnect()
  tasksScrollObserver = null
}

function attachTasksScrollObserver() {
  detachTasksScrollObserver()
  const sentinel = tasksSentinelRef.value
  if (!sentinel || !taskDrawerVisible.value || !tasksHasMore.value) return
  const root = sentinel.closest('.rq-dialog__body') as HTMLElement | null
  if (!root) return
  tasksScrollObserver = new IntersectionObserver(
    (entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        void loadMoreRuntimeTasks()
      }
    },
    { root, rootMargin: '96px 0px', threshold: 0 },
  )
  tasksScrollObserver.observe(sentinel)
}

function openRuntimeTasks(row: QueueStat, state: RuntimeTaskState) {
  taskQueue.value = row
  taskState.value = state
  taskDrawerVisible.value = true
  void fetchRuntimeTasks(true)
}

function selectTaskState(state: RuntimeTaskState) {
  if (taskState.value === state) return
  taskState.value = state
  void fetchRuntimeTasks(true)
}

function reloadRuntimeTasks() {
  return fetchRuntimeTasks(true)
}

function loadMoreRuntimeTasks() {
  if (tasksLoading.value || tasksLoadingMore.value || !tasksHasMore.value) return
  return fetchRuntimeTasks(false)
}

async function runTaskAction(task: RuntimeTask, action: RuntimeTaskAction) {
  const queue = taskQueue.value?.name
  if (!queue) return
  taskActionID.value = task.id
  taskAction.value = action
  try {
    await mutateRuntimeTask(queue, task.id, action)
    toast.success(t(`system.globalSettings.runtime.tasks.actionSuccess.${action}`))
    await Promise.all([reloadRuntimeTasks(), load(false)])
    taskQueue.value = queues.value.find((item) => item.name === queue) ?? taskQueue.value
  } catch (err: any) {
    toast.error(err?.message || t(`system.globalSettings.runtime.tasks.actionError.${action}`))
  } finally {
    taskActionID.value = ''
    taskAction.value = ''
  }
}

async function purgeArchivedTasks() {
  const queue = taskQueue.value?.name
  if (!queue || purging.value) return
  purging.value = true
  try {
    const { deleted } = await purgeArchivedRuntimeTasks(queue)
    toast.success(t('system.globalSettings.runtime.tasks.purgeArchivedSuccess', { count: deleted }))
    await Promise.all([reloadRuntimeTasks(), load(false)])
    taskQueue.value = queues.value.find((item) => item.name === queue) ?? taskQueue.value
  } catch (err: any) {
    toast.error(err?.message || t('system.globalSettings.runtime.tasks.purgeArchivedError'))
  } finally {
    purging.value = false
  }
}

async function load(showSpinner: boolean) {
  if (showSpinner) loading.value = true
  try {
    const connection = await getQueueConnectionStatus().catch(() => undefined)
    queueConnection.value = connection
    loadChatGPTStatus()
    const resp = await getRuntimeQueues()
    queueConnection.value = resp.queue_connection ?? connection
    available.value = resp.available
    pools.value = resp.pools || []
    queues.value = resp.queues || []
    if (taskQueue.value) {
      taskQueue.value = queues.value.find((item) => item.name === taskQueue.value?.name) ?? taskQueue.value
    }
    models.value = resp.models || []
    modelLimiterAvailable.value = Boolean(resp.model_limiter_available)
    updatedAt.value = new Date((resp.timestamp || Date.now() / 1000) * 1000)
      .toLocaleTimeString(locale.value, { hour12: false })
    error.value = ''
    loadedOnce.value = true
  } catch (err: any) {
    error.value = err?.message || t('system.globalSettings.runtime.errors.generic')
  } finally {
    if (showSpinner) loading.value = false
  }
}

function reload() {
  load(true)
}

function startPolling() {
  stopPolling()
  if (!autoRefresh.value) return
  pollTimer = setInterval(() => {
    // Silent background refresh — no spinner so the table doesn't flash.
    if (!loading.value) load(false)
  }, POLL_INTERVAL_MS)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

watch(autoRefresh, (on) => {
  if (on) startPolling()
  else stopPolling()
})

watch(taskDrawerVisible, async (open) => {
  if (!open) {
    detachTasksScrollObserver()
    return
  }
  await nextTick()
  attachTasksScrollObserver()
}, { flush: 'post' })

watch(tasksHasMore, async () => {
  if (!taskDrawerVisible.value) return
  await nextTick()
  attachTasksScrollObserver()
})

onMounted(() => {
  load(true)
  startPolling()
})

onUnmounted(() => {
  chatgptRequestVersion++
  stopPolling()
  detachTasksScrollObserver()
})
</script>

<style scoped>
 .rq-header, .rq-actions, .rq-auto-refresh, .rq-details-header, .rq-pools-header, .rq-pool-topline, .rq-failed-section-head, .rq-failed-section-actions, .rq-failed-row-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); }
.rq-auto-refresh, .rq-actions { justify-content: flex-start; }
.rq-refresh, .rq-task-state-option { padding: var(--space-2) var(--space-3); border: 1px solid var(--border); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.rq-task-state-option.is-active { background: var(--text); color: var(--bg); }
.rq-refresh:focus-visible, .rq-task-state-option:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.rq-state { display: flex; flex-wrap: wrap; gap: var(--space-3); padding: var(--space-4) 0; border-top: 2px solid var(--rule); }
.rq-state-copy, .rq-queue-cell { display: grid; gap: var(--space-1); }
.rq-state--error, .rq-failed-row-error { color: var(--danger); }
.rq-overview, .rq-pools, .rq-details { padding: var(--space-4) 0; border-top: 2px solid var(--rule); }
.rq-overview-metrics, .rq-pool-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: var(--space-4); }
.rq-metric { display: grid; gap: var(--space-2); }
.rq-metric-value { font-family: var(--mono); font-size: var(--text-2xl); }
.rq-metric-label, .rq-queue-meta, .rq-pool-desc, .rq-footnote, .rq-details-header p, .rq-pools-header p, .rq-failed-guide-desc { color: var(--muted); font-size: var(--text-sm); }
.rq-queue-name, .rq-queue-meta { overflow-wrap: anywhere; }
.rq-number, .rq-latency, .rq-pool-value { font-family: var(--mono); }
.rq-status { white-space: nowrap; }
.rq-failed-notice { border-top: 2px solid var(--danger); padding: var(--space-3); background: var(--danger-soft); }
.rq-table-shell { min-width: 0; }
.rq-table-shell :deep(.rq-table) { min-width: 820px; }
.rq-models :deep(.rq-table) { min-width: 640px; }
.rq-table-shell :deep(th), .rq-table-shell :deep(td) { min-width: 76px; }
.rq-model-usage progress { width: 100%; accent-color: var(--text); }
.rq-task-state-filter { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.rq-failed-row { border-top: 2px solid var(--rule); padding: var(--space-4) 0; }
.rq-task-action { min-height: 28px; padding: 4px 8px; font-size: var(--text-sm); }
.rq-failed-row-summary { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.rq-failed-row-refs { display: grid; grid-template-columns: fit-content(40%) minmax(0, 1fr); gap: 4px 8px; }
.rq-failed-ref { display: contents; }
.rq-failed-ref dt { white-space: normal; overflow-wrap: anywhere; }
.rq-failed-ref dd { overflow: hidden; text-overflow: ellipsis; }
.rq-failed-row-refs dd { margin: var(--space-1) 0; font-family: var(--mono); overflow-wrap: anywhere; }
.rq-failed-list-footer { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-3); }

</style>
