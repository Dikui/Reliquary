<template>

  <div class="system-settings settings-form">
    <div class="section-header">
      <div class="section-header__titlewrap">
        <details class="settings-priority-hint"><summary>{{ t('system.globalSettings.priorityHint.disclosure') }}</summary><ul><li>{{ t('system.globalSettings.priorityHint.tier1') }}</li><li>{{ t('system.globalSettings.priorityHint.tier2') }}</li><li>{{ t('system.globalSettings.priorityHint.tier3') }}</li></ul></details>
      </div>
      <p class="section-description">
        {{ t('system.globalSettings.description') }}
      </p>
    </div>

    <QueueConnectionNotice poll />

    <div v-if="loading && settings.length === 0" class="loading-state">
      <p role="status">{{ t('common.loading') }}</p>
    </div>

    <div v-else-if="settings.length === 0" class="empty-state">

      <span>{{ t('system.globalSettings.empty') }}</span>
    </div>

    <template v-else>
      <nav class="settings-section-tabs" :aria-label="t('system.globalSettings.title')"><Button v-for="section in (hasUnknownSettings ? ['runtime', 'security', 'other'] : ['runtime', 'security'])" :key="section" :variant="activeSettingsSection === section ? 'primary' : 'default'" :aria-pressed="activeSettingsSection === section" @click="activeSettingsSection = section as SettingsSection">{{ sectionTabLabel(section as SettingsSection) }}</Button></nav>

      <section class="settings-section-panel" :aria-label="activeSectionTitle">
        <div
          class="settings-section-intro"
          :class="{ 'settings-section-intro--runtime': activeSettingsSection === 'runtime' }"
        >
          <p>{{ activeSectionDescription }}</p>
          <Tag v-if="activeSettingsSection === 'runtime'" variant="plain">
            {{ t('system.globalSettings.sections.runtime.restartHint') }}
          </Tag>
        </div>

        <div
          class="settings-group"
          :class="{ 'settings-group--runtime': activeSettingsSection === 'runtime' }"
        >

      <div
            v-for="item in activeSectionSettings"
        :key="item.key"
        class="setting-row"
      >
        <div class="setting-info">
              <div class="setting-label">
            <span>{{ keyLabel(item.key) }}</span>
            <Tag
              v-if="item.requires_restart"
              variant="plain"
              class="setting-badge"
            >{{ t('system.globalSettings.badgeRequiresRestart') }}</Tag>
            <Tag
              v-if="item.is_secret"
              variant="plain"
              class="setting-badge"
            >{{ t('system.globalSettings.badgeSecret') }}</Tag>
            <Tag
                  v-if="isHighImpactKey(item.key)"
                  variant="plain"
                  class="setting-badge"
                >{{ t('system.globalSettings.badgeHighRisk') }}</Tag>
                <Tag
              v-if="hasOverride(item)"
              variant="plain"
              class="setting-badge"
              :title="t('system.globalSettings.badgeOverrideTooltip')"
            >{{ t('system.globalSettings.badgeOverride') }}</Tag>
              </div>
          <p v-if="settingDescription(item)" class="desc">{{ settingDescription(item) }}</p>
          <div v-if="modifiedMeta(item)" class="setting-meta">
            {{ t('system.globalSettings.modifiedAt', { value: modifiedMeta(item) }) }}
          </div>
        </div>

        <div class="setting-control">

          <div class="setting-control-row">

          <Select
            v-if="hasEnum(item)"
            :model-value="String(editValues[item.key] ?? '')" @update:model-value="editValues[item.key] = $event; onChange(item)"
                :aria-label="keyLabel(item.key)"
            :disabled="savingKey === item.key"
            class="setting-input"
          ><option v-for="option in enumOptions(item)" :key="option.value" :value="option.value">{{ option.label }}</option></Select>

          <Switch
            v-else-if="item.value_type === 'bool'"
            :model-value="Boolean(editValues[item.key])" @update:model-value="editValues[item.key] = $event; onChange(item)" :aria-label="keyLabel(item.key)" :disabled="savingKey === item.key"
          />
          <Input type="number"
            v-else-if="item.value_type === 'int'"
            :model-value="String(editValues[item.key] ?? '')" @update:model-value="editValues[item.key] = Number($event)"
            :placeholder="placeholderFor(item)"
                :aria-label="keyLabel(item.key)"
            :disabled="savingKey === item.key"
            :step="1"
            :min="minimumFor(item)"
            class="setting-input"
            @blur="onChange(item)"
          />
          <div v-else-if="item.value_type === 'string_list' && item.key === 'ssrf.whitelist'" class="ssrf-whitelist-input">
            <div class="settings-actions"><Tag v-for="entry in ssrfWhitelistModelValue()" :key="entry">{{ entry }} <Button variant="ghost" :aria-label="`${t('common.delete')} ${entry}`" :disabled="savingKey === item.key || Boolean(listConfirmBusyKey)" @click="onSsrfWhitelistModelUpdate(ssrfWhitelistModelValue().filter(value => value !== entry))">×</Button></Tag></div>
            <div class="settings-actions"><Button v-if="ssrfWhitelistModelValue().length" variant="ghost" :disabled="Boolean(listConfirmBusyKey)" @click="onSsrfWhitelistModelUpdate([])">{{ t('common.clear') }}</Button><Input v-model="newWhitelistEntry" :placeholder="emptyListPlaceholder" :aria-label="keyLabel(item.key)" :disabled="savingKey === item.key || Boolean(listConfirmBusyKey)" @keydown.enter.prevent="!$event.isComposing && addWhitelistEntry()" /><Button :disabled="!newWhitelistEntry.trim() || Boolean(listConfirmBusyKey)" @click="addWhitelistEntry">{{ t('common.add') }}</Button></div>
          </div>
          <Input
            v-else
            :model-value="String(editValues[item.key] ?? '')" @update:model-value="editValues[item.key] = $event"
            :placeholder="placeholderFor(item)"
                :aria-label="keyLabel(item.key)"
            :disabled="savingKey === item.key"
            class="setting-input"
            @blur="onChange(item)"
          />

              <div v-if="savingKey === item.key" class="setting-save-state" role="status">
                <p role="status">{{ t('common.loading') }}</p>
                <span>{{ t('system.globalSettings.saving') }}</span>
              </div>
              <div
                v-else-if="savedKey === item.key"
                class="setting-save-state setting-save-state--success"
                role="status"
              >

                <span>{{ t('system.globalSettings.saved') }}</span>
              </div>
          </div>

          <div
            v-if="hasOverride(item)"
            class="setting-control-actions"
          >

            <template v-if="hasOverride(item)">
              <Button @click="confirmAction(t('system.globalSettings.reset.confirmBody', { label: keyLabel(item.key) }), () => resetSetting(item))"
                variant="ghost"

                :disabled="savingKey === item.key"
                :title="t('system.globalSettings.reset.tooltip')"
                class="setting-reset-btn"
              >

                {{ t('system.globalSettings.reset.label') }}
              </Button>
            </template>
          </div>
        </div>
      </div>
        </div>
      </section>
      <div class="sr-only" role="status" aria-live="polite">{{ saveAnnouncement }}</div>
    </template>

  </div>
</template>

<script setup lang="ts">
import { Button, Input, Select, Switch, Tag, toast, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import QueueConnectionNotice from './QueueConnectionNotice.vue'
import { ref, reactive, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  listSystemSettings,
  getQueueConnectionStatus,
  updateSystemSetting,
  resetSystemSetting,
  type SystemSettingItem,
} from '@/api/system'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { isSettingValueDirty, resolveCurrentSetting } from './systemSettingsEdit'

const deploymentCapabilities = useDeploymentCapabilitiesStore()

const confirm = useConfirm()
const { t, tm, te, locale } = useI18n()

// Friendly labels per key live in i18n (system.globalSettings.keyLabels.*).
// Adding a new entry there must accompany every new key registered in
// service/system_setting.go on the backend; locales without an entry
// fall back to the raw key so a misconfigured deploy still renders.
function keyLabel(k: string): string {
  const path = `system.globalSettings.keyLabels.${k}`
  return te(path) ? (t(path) as string) : k
}

// Descriptions are registered in Chinese on the backend for operator docs;
// user-facing copy lives in i18n (system.globalSettings.keyDescriptions.*).
function settingDescription(item: { key: string; description?: string }): string {
  const path = `system.globalSettings.keyDescriptions.${item.key}`
  return te(path) ? t(path) as string : item.description ?? ''
}

const HIGH_IMPACT_KEYS = new Set<string>([
  'ssrf.whitelist',
])

function isHighImpactKey(key: string): boolean {
  return HIGH_IMPACT_KEYS.has(key)
}

function confirmAction(body: string, action: () => unknown) {
  confirm({ header: t('common.confirm'), body, confirmBtn: t('common.confirm'), cancelBtn: t('common.cancel'), onConfirm: async () => { await action() } })
}
const newWhitelistEntry = ref('')
function addWhitelistEntry() {
  const entry = newWhitelistEntry.value.trim()
  if (!entry || listConfirmBusyKey.value) return
  onSsrfWhitelistModelUpdate([...ssrfWhitelistModelValue(), entry])
  newWhitelistEntry.value = ''
}

const emptyListPlaceholder = computed(() => t('system.globalSettings.tagInputPlaceholder'))

const settings = ref<SystemSettingItem[]>([])
const loading = ref(false)
const savingKey = ref<string | null>(null)
const savedKey = ref<string | null>(null)
const saveAnnouncement = ref('')
let savedKeyTimer: ReturnType<typeof setTimeout> | null = null

type SettingsSection = 'runtime' | 'security' | 'other'

// Product-oriented order, rather than the registry's alphabetical key order.
// Unknown/out-of-band rows remain visible in a conditional "Other" tab so the
// backend's diagnostic contract is preserved when a deployment contains an
// unexpected key.
const SETTINGS_SECTION_KEYS: Record<Exclude<SettingsSection, 'other'>, readonly string[]> = {
  runtime: [
    'queue.core_concurrency',
    'queue.enrichment_concurrency',
    'queue.maintenance_concurrency',
    'queue.shared_concurrency',
    'model.max_concurrency',
  ],
  security: ['ssrf.whitelist'],
}

const activeSettingsSection = ref<SettingsSection>('runtime')
const knownSettingKeys = new Set(Object.values(SETTINGS_SECTION_KEYS).flat())
const settingsByKey = computed<Map<string, SystemSettingItem>>(() => new Map(settings.value.map((item) => [item.key, item])))
const unknownSettings = computed(() => settings.value.filter((item) => !knownSettingKeys.has(item.key)))
const hasUnknownSettings = computed(() => unknownSettings.value.length > 0)

watch(hasUnknownSettings, (hasUnknown) => {
  if (!hasUnknown && activeSettingsSection.value === 'other') {
    activeSettingsSection.value = 'runtime'
  }
})

const activeSectionSettings = computed(() => {
  if (activeSettingsSection.value === 'other') return unknownSettings.value
  return SETTINGS_SECTION_KEYS[activeSettingsSection.value]
    .map((key) => settingsByKey.value.get(key))
    .filter((item): item is SystemSettingItem => Boolean(item))
})

const activeSectionTitle = computed(() =>
  t(`system.globalSettings.sections.${activeSettingsSection.value}.title`),
)
const activeSectionDescription = computed(() =>
  t(`system.globalSettings.sections.${activeSettingsSection.value}.description`),
)

function sectionTabLabel(section: SettingsSection): string {
  const count = section === 'other'
    ? unknownSettings.value.length
    : SETTINGS_SECTION_KEYS[section].filter((key) => settingsByKey.value.has(key)).length
  return t(`system.globalSettings.sections.${section}.tab`, { count })
}

function markSettingSaved(item: SystemSettingItem) {
  savedKey.value = item.key
  saveAnnouncement.value = t('system.globalSettings.saveAnnouncement', {
    label: keyLabel(item.key),
  })
  if (savedKeyTimer) clearTimeout(savedKeyTimer)
  savedKeyTimer = setTimeout(() => {
    if (savedKey.value === item.key) savedKey.value = null
    savedKeyTimer = null
  }, 2000)
}

// Guards ssrf.whitelist while an async confirm roundtrip is in flight.
const listConfirmBusyKey = ref<string | null>(null)

// Reactive map of in-progress edits, keyed by setting key. We don't
// mutate the canonical `settings` array directly so a failed save
// leaves the original value visible until the user retries or refreshes.
// Initialised lazily in loadSettings; setting.value is the JSON-decoded
// form (number / boolean / string / string[]).
const editValues = reactive<Record<string, unknown>>({})

function hasEnum(item: SystemSettingItem): boolean {
  return Array.isArray(item.enum) && item.enum.length > 0
}

function enumOptions(item: SystemSettingItem): { label: string; value: string }[] {
  const opts = item.enum ?? []
  return opts.map((v) => ({ label: v, value: v }))
}

// hasOverride reports whether the row carries a real DB override (vs a
// virtual row backed by ENV/default). Distinguishing these is what
// `last_modified_by` was made for: empty string means the value came
// from registry/ENV. Drives the "已覆盖" badge.
function hasOverride(item: SystemSettingItem): boolean {
  return Boolean(item.last_modified_by && item.last_modified_by.trim() !== '')
}

// modifiedMeta returns a humane "上次修改" line for rows that have been
// persisted (last_modified_by non-empty AND updated_at not the Go zero
// value). Returns '' for virtual rows so the meta line collapses
// entirely instead of rendering "1/1/1 08:05:43" garbage.
function modifiedMeta(item: SystemSettingItem): string {
  if (!hasOverride(item)) return ''
  const ts = item.updated_at
  if (!ts || ts.startsWith('0001-')) return ''
  const formatted = formatDate(ts)
  // Prefer the resolved username/email the server enriches via
  // last_modified_by_name. Fall back to the UUID's first 8 chars when
  // the user can't be resolved (deleted account, transient lookup
  // failure) — the full ID is still in the audit log.
  const actor = item.last_modified_by_name && item.last_modified_by_name.trim() !== ''
    ? item.last_modified_by_name
    : (item.last_modified_by || '').slice(0, 8)
  return `${formatted} · ${actor}`
}

const SSRF_WHITELIST_KEY = 'ssrf.whitelist'

function ssrfWhitelistModelValue(): string[] {
  const v = editValues[SSRF_WHITELIST_KEY]
  return Array.isArray(v) ? (v as string[]) : []
}

function globalSettingsText(path: string, params?: Record<string, string>): string {
  if (!te(path)) return path
  const msg = params ? t(path, params) : t(path)
  return typeof msg === 'string' ? msg : path
}

// Controlled SSRF tag-input: we commit editValues so a declined delta
// can be rolled back without the component re-applying a removal.
function onSsrfWhitelistModelUpdate(next: string[]) {
  if (listConfirmBusyKey.value === SSRF_WHITELIST_KEY) return
  editValues[SSRF_WHITELIST_KEY] = next
  void onSsrfWhitelistChange()
}

async function onSsrfWhitelistChange() {
  const item = settings.value.find((s) => s.key === SSRF_WHITELIST_KEY)
  if (!item || !isDirty(item)) return
  if (listConfirmBusyKey.value === SSRF_WHITELIST_KEY) return
  await handleSSRFListChange(item)
}

async function snapSsrfWhitelistToSaved(item: SystemSettingItem) {
  const saved = Array.isArray(item.value) ? (item.value as string[]) : []
  editValues[SSRF_WHITELIST_KEY] = [...saved]
  await nextTick()
}

function isDirty(item: SystemSettingItem): boolean {
  return isSettingValueDirty(editValues[item.key], item.value)
}

function formatDate(isoString: string): string {
  try {
    const d = new Date(isoString)
    return d.toLocaleString('zh-CN', { hour12: false })
  } catch {
    return isoString
  }
}

// placeholderFor renders the current effective value (DB / ENV / default)
// as a placeholder hint inside the edit control. For string_list it's
// joined with comma; for booleans we show nothing (the switch already
// reflects the value).
function placeholderFor(item: SystemSettingItem): string {
  const v = item.value
  if (v === null || v === undefined) return ''
  if (Array.isArray(v)) return v.join(', ')
  return String(v)
}

function minimumFor(item: SystemSettingItem): number {
  if (item.key.startsWith('queue.') && item.key.endsWith('_concurrency')) return 1
  return 0
}

async function loadSettings() {
  loading.value = true
  try {
    if ((await getQueueConnectionStatus())?.state === 'reconnecting') {
      settingsRetryTimer = setTimeout(loadSettings, 5000)
      return
    }
    const list = await listSystemSettings()
    settings.value = list
    // Reset edit values to the canonical state on every load — no
    // partial drafts survive a refresh, which avoids the "I came back
    // and my unsaved edits look saved" trap.
    for (const item of list) {
      // Defensive copy for arrays so the t-tag-input doesn't mutate
      // the canonical settings entry through the v-model binding.
      editValues[item.key] = Array.isArray(item.value)
        ? [...(item.value as unknown[])]
        : item.value
    }
  } catch (err: any) {
    const msg = err?.message || t('system.globalSettings.messages.loadFailed')
    toast.error(msg)
  } finally {
    loading.value = false
  }
}

// onChange persists non-SSRF settings. SSRF whitelist and system admins
// have dedicated handlers with inline popconfirm.
async function onChange(item: SystemSettingItem) {
  const currentItem = resolveCurrentSetting(settingsByKey.value, item.key)
  if (!currentItem) return
  if (!isDirty(currentItem)) return

  // SSRF whitelist gets the per-entry confirm flow — same shape as the
  // admin tag-input above. Adding or removing each host/CIDR is its
  // own privileged change (a single bad CIDR can punch a hole through
  // the egress firewall), so we ask once per delta instead of once
  // per "save". This matches the operator's mental model: every tag
  // they touch is acknowledged on its own.
  await persistSetting(currentItem)
}

function confirmSsrfListEntryChange(
  action: 'add' | 'remove',
  entry: string,
): Promise<boolean> {
  const base = `system.globalSettings.listConfirm.${SSRF_WHITELIST_KEY}.${action}`
  return confirm({
    header: t('common.confirm'),
    body: globalSettingsText(`${base}.body`, { entry }),
    theme: action === 'add' ? 'danger' : 'default',
    confirmBtn: globalSettingsText(`${base}.confirmBtn`),
    cancelBtn: t('common.cancel'),
  }).result
}

// handleSSRFListChange reconciles the current edit against the saved
// list one entry at a time. The strategy is "confirmed deltas only":
// we start from the saved value, then walk the user's added/removed
// sets and apply each entry the operator individually approves. If
// every prompt is declined we end up identical to the saved value
// and short-circuit before hitting the API. Otherwise we save the
// merged result in a single PUT so the audit log and pubsub get one
// coherent post-image (instead of N noisy events).
async function handleSSRFListChange(item: SystemSettingItem) {
  listConfirmBusyKey.value = item.key
  try {
    const oldArr = Array.isArray(item.value) ? (item.value as string[]) : []
    const nextArr = Array.isArray(editValues[item.key])
      ? (editValues[item.key] as string[])
      : []

    const oldSet = new Set(oldArr)
    const nextSet = new Set(nextArr)

    const added: string[] = []
    for (const v of nextSet) if (!oldSet.has(v)) added.push(v)
    const removed: string[] = []
    for (const v of oldSet) if (!nextSet.has(v)) removed.push(v)

    if (added.length === 0 && removed.length === 0) return

    // Build the candidate value from approved deltas only. We keep
    // insertion order roughly aligned with the operator's intent:
    // start from the saved list (so unchanged entries keep their
    // position), drop approved removals, append approved additions.
    const finalSet = new Set(oldArr)
    for (const entry of added) {
      const ok = await confirmSsrfListEntryChange('add', entry)
      if (ok) {
        finalSet.add(entry)
      } else {
        await snapSsrfWhitelistToSaved(item)
        return
      }
    }
    for (const entry of removed) {
      const ok = await confirmSsrfListEntryChange('remove', entry)
      if (ok) {
        finalSet.delete(entry)
      } else {
        await snapSsrfWhitelistToSaved(item)
        return
      }
    }

    const finalArr = Array.from(finalSet)
    // Compare against saved value, not against `editValues`. If every
    // delta was declined, the saved list still wins; we just need to
    // snap the input back to it.
    const sameAsSaved =
      finalArr.length === oldArr.length &&
      finalArr.every((v, i) => v === oldArr[i])
    if (sameAsSaved) {
      await snapSsrfWhitelistToSaved(item)
      return
    }

    editValues[item.key] = finalArr
    await persistSetting(item)
  } finally {
    await nextTick()
    listConfirmBusyKey.value = null
  }
}

// resetSetting drops the DB override and reloads the row so the UI
// reflects the resolved fallback (ENV value if set, otherwise the
// in-code default). We refetch the whole list rather than the single
// row because the list endpoint is what populates the canonical
// settings array and re-running it keeps the modified-by enrichment
// consistent for every row in the table.
async function resetSetting(item: SystemSettingItem) {
  savedKey.value = null
  savingKey.value = item.key
  try {
    await resetSystemSetting(item.key)
    await loadSettings()
    markSettingSaved(item)
    toast.success(t('system.globalSettings.reset.success'))
  } catch (err: any) {
    const msg = err?.message || t('system.globalSettings.reset.failed')
    saveAnnouncement.value = msg
    toast.error(msg)
  } finally {
    savingKey.value = null
  }
}

async function persistSetting(item: SystemSettingItem) {
  const newValue = editValues[item.key]
  savedKey.value = null
  savingKey.value = item.key
  try {
    const updated = await updateSystemSetting(item.key, newValue)
    // Replace the row in-place so the table stays at scroll position
    // and other rows' edit state isn't disturbed.
    const idx = settings.value.findIndex((s) => s.key === item.key)
    if (idx >= 0) {
      settings.value[idx] = updated
    }
    editValues[item.key] = Array.isArray(updated.value)
      ? [...(updated.value as unknown[])]
      : updated.value
    markSettingSaved(updated)
    toast.success(t('system.globalSettings.messages.saveSuccess'))
  } catch (err: any) {
    const msg = err?.message || t('system.globalSettings.messages.saveFailed')
    saveAnnouncement.value = msg
    toast.error(msg)
    // Roll the input back to the canonical value on failure. Without
    // this an invalid edit (e.g. SSRF whitelist with a malformed CIDR
    // that the backend 400'd) would stay rendered as if accepted, and
    // the user couldn't tell whether the rejection actually stuck.
    const failed = settings.value.find((s) => s.key === item.key)
    if (failed) {
      editValues[item.key] = Array.isArray(failed.value)
        ? [...(failed.value as unknown[])]
        : failed.value
    }
  } finally {
    savingKey.value = null
  }
}

let settingsRetryTimer: ReturnType<typeof setTimeout> | undefined
onMounted(() => {
  loadSettings()
})

onUnmounted(() => {
  if (settingsRetryTimer) clearTimeout(settingsRetryTimer)
  if (savedKeyTimer) clearTimeout(savedKeyTimer)
})
</script>

<style scoped>
 .settings-section-tabs, .setting-control-row, .setting-control-actions, .setting-label { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
.settings-section-tabs { margin-bottom: var(--space-4); }
.setting-control-row { flex: 1; min-width: 0; width: 100%; }
.setting-label { font-weight: 800; }
.setting-meta, .settings-section-intro { color: var(--muted); font-size: var(--text-sm); }
.ssrf-whitelist-input { display: grid; gap: var(--space-3); width: 100%; min-width: 0; }
.ssrf-whitelist-input .rq-control { flex: 1; min-width: 160px; }
.setting-save-state { display: flex; gap: var(--space-2); font-size: var(--text-sm); }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }

</style>

<style scoped>
.ssrf-whitelist-input .rq-tag { display: inline-flex; align-items: center; gap: var(--space-2); }
.ssrf-whitelist-input .rq-tag .rq-button { min-height: 24px; padding: 0 6px; }
</style>
