<template>
  <SettingDrawer
    class="sandbox-config-drawer"
    :visible="visible"
    :title="record ? $t('settings.sandbox.editTitle') : $t('settings.sandbox.createTitle')"
    :description="stepDescription"
    icon="code"
    width="680px"
    :min-width="560"
    :max-width="920"
    storage-key="setting-drawer:width:sandbox-config-v2"
    :confirm-loading="saving || checking || templatesLoading"
    :confirm-disabled="primaryDisabled"
    :confirm-text="primaryText"
    @confirm="handlePrimaryAction"
    @cancel="close"
    @update:visible="(v: boolean) => emit('update:visible', v)"
  >
    <template #footer-left>
      <t-button v-if="wizardStep > 0" variant="outline" @click="previousStep">
        {{ $t('settings.sandbox.back') }}
      </t-button>
      <t-popconfirm
        v-if="canDeepCheck"
        :content="$t('settings.sandbox.deepCheckConfirm')"
        @confirm="runCheck(true)"
      >
        <t-button variant="outline" :loading="checking">
          {{ lastCheckWasDeep ? $t('settings.sandbox.recheck') : $t('settings.sandbox.deepCheck') }}
        </t-button>
      </t-popconfirm>
    </template>

    <template #header-extra>
      <nav class="sandbox-steps" :aria-label="$t('settings.sandbox.setupProgress')">
        <component
          :is="canJumpTo(index) ? 'button' : 'div'"
          v-for="(item, index) in wizardSteps"
          :key="item.key"
          :type="canJumpTo(index) ? 'button' : undefined"
          :class="['sandbox-step', {
            'is-active': wizardStep === index,
            'is-done': wizardStep > index,
            'is-clickable': canJumpTo(index),
          }]"
          :aria-current="wizardStep === index ? 'step' : undefined"
          @click="goToStep(index)"
        >
          <span class="sandbox-step__marker">
            <t-icon v-if="wizardStep > index" name="check" />
            <template v-else>{{ index + 1 }}</template>
          </span>
          <span class="sandbox-step__title">{{ item.title }}</span>
          <span v-if="index < wizardSteps.length - 1" class="sandbox-step__line" aria-hidden="true" />
        </component>
      </nav>
    </template>

    <!--
      Identity-change refusals must sit at the top: the form is long and the
      admin otherwise saves, sees nothing, and assumes the click did nothing.
    -->
    <div v-if="conflict" ref="conflictAlertRef" class="blocked blocked-top">
      <t-alert v-if="conflict.code === 'sandboxes_still_live'" theme="warning"
        :message="$t('settings.sandbox.sandboxesStillLive', { count: conflict.inventory?.sandbox_count ?? 0 })">
        <template #description>
          <p v-if="affectedSessionCount">{{ $t('settings.sandbox.affectedSessions', { count: affectedSessionCount }) }}</p>
          <p v-if="conflict.inventory?.agent_names?.length">
            {{ $t('settings.sandbox.affectedAgents', { names: conflict.inventory.agent_names.join('、') }) }}
          </p>
          <p>{{ $t('settings.sandbox.blockedHint') }}</p>
        </template>
      </t-alert>
      <t-alert v-else-if="conflict.code === 'skill_snapshot_blocks_template'" theme="warning"
        :message="$t('settings.sandbox.templateLockedBySkills')" />
      <t-alert v-else theme="warning" :message="$t('settings.sandbox.unverifiableBlocked')">
        <template #description>
          <p>{{ $t('settings.sandbox.unverifiableSaveHint') }}</p>
        </template>
      </t-alert>
    </div>

    <t-form label-align="top" class="sandbox-editor-form">
      <section v-if="currentStepKey === 'connection'" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.sectionBasic') }}</h4>
        <t-form-item :label="$t('settings.sandbox.backendType')">
          <t-select :value="backend" :placeholder="$t('settings.sandbox.backendTypePlaceholder')"
            class="backend-select" :popup-props="{ overlayClassName: 'sandbox-backend-popup' }"
            :disabled="retargetFrozen"
            @change="(v: any) => selectBackend(String(v))">
            <t-option v-for="opt in backendOptions" :key="opt" :value="opt" :label="backendLabel(opt)">
              <span class="backend-choice">
                <SandboxBackendBadge :type="opt" size="sm" />
                <span class="backend-choice__text">
                  <span class="backend-choice__name">{{ backendLabel(opt) }}</span>
                  <span class="backend-choice__desc">
                    {{ $t(`settings.sandbox.backendDescriptions.${opt}`) }}
                  </span>
                </span>
              </span>
            </t-option>
          </t-select>
          <p v-if="backend" class="section-help section-help--field">
            {{ $t(`settings.sandbox.backendDescriptions.${backend}`) }}
          </p>
        </t-form-item>
        <t-alert v-if="!dockerBackendEnabled" theme="warning" class="compact-alert"
          :message="$t('settings.sandbox.dockerDisabledAlert')">
          <template #description>
            <p>{{ $t('settings.sandbox.dockerDisabledHint') }}</p>
          </template>
        </t-alert>
        <t-form-item :label="$t('settings.sandbox.configName')" :status="nameError ? 'error' : undefined"
          :tips="nameError || undefined">
          <t-input v-model="name" :placeholder="$t('settings.sandbox.configNamePlaceholder')" />
        </t-form-item>
        <t-form-item :label="$t('settings.sandbox.configDescription')">
          <t-input v-model="description" :placeholder="$t('settings.sandbox.configDescriptionPlaceholder')" />
        </t-form-item>
      </section>







      <section v-if="currentStepKey === 'connection'" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.sectionRuntimeEnvironment') }}</h4>
        <div class="reliquary-template-card is-active">
          <SandboxBackendBadge type="docker" />
          <div class="reliquary-template-card__content">
            <div class="reliquary-template-card__title-row">
              <span class="reliquary-template-card__title">{{ $t('settings.sandbox.reliquaryDockerImage') }}</span>
              <t-tag theme="primary" variant="light" size="small">{{ $t('settings.sandbox.recommendedTag') }}</t-tag>
            </div>
            <p>{{ $t('settings.sandbox.reliquaryDockerImageHint') }}</p>
          </div>
        </div>
        <t-form-item :label="requiredLabel('dockerImage')" :status="fieldStatus('image')"
          :tips="fieldTip('image')">
          <t-input v-model="docker.image" :placeholder="defaultDockerImage"
            :disabled="retargetFrozen" @input="onFieldInput('image')" />
          <p v-if="retargetFrozen" class="section-help section-help--field">
            {{ hasSkillSnapshot
              ? $t('settings.sandbox.templateLockedBySkills')
              : $t('settings.sandbox.templateLockedByInFlight') }}
          </p>
        </t-form-item>
        <t-form-item :label="$t('settings.sandbox.dockerHost')" :help="$t('settings.sandbox.dockerHostHelp')">
          <t-input v-model="docker.host" placeholder="unix:///var/run/docker.sock"
            :disabled="retargetFrozen" @input="onFieldInput('host')" />
        </t-form-item>
        <t-form-item :label="$t('settings.sandbox.dockerTlsCertPath')"
          :help="$t('settings.sandbox.dockerTlsCertPathHelp')">
          <t-input v-model="docker.tls_cert_path" placeholder="/etc/reliquary/docker-certs"
            :disabled="retargetFrozen" @input="onFieldInput('tls_cert_path')" />
        </t-form-item>
        <t-alert theme="warning" class="compact-alert" :message="$t('settings.sandbox.dockerHostRisk')" />
        <div class="private-endpoint-row">
          <div>
            <p class="private-endpoint-row__title">{{ $t('settings.sandbox.allowPrivateEndpoints') }}</p>
            <p class="section-help">{{ $t('settings.sandbox.allowPrivateEndpointsHint') }}</p>
          </div>
          <t-switch
            v-model="allowPrivateEndpoints"
            :disabled="retargetFrozen"
            @change="invalidateConnection"
          />
        </div>
      </section>

      <section v-if="currentStepKey === 'runtime'" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.sectionRuntime') }}</h4>
        <div class="runtime-fields">
          <!--
            Each number keeps its own label and its own sentence — the reason
            these were one-per-row originally was that three unlabelled numbers
            side by side shared a single footnote. Moving the sentence into the
            field's own tips keeps that fixed while halving the height.
          -->
          <div class="form-grid form-grid--two">

            <!--
              Docker has no provider-side timeout at all: an abandoned container
              keeps its memory and CPU share on the daemon host until Reliquary
              reclaims it, so the idle TTL and the resource caps are the only
              things bounding what one workspace can hold.
            -->
            <t-form-item :label="$t('settings.sandbox.dockerIdleTtl')"
                :tips="$t('settings.sandbox.dockerIdleTtlHelp')">
                <t-input-number v-model="docker.idle_ttl_seconds" :min="0" theme="column"
                  placeholder="1800" />
              </t-form-item><t-form-item :label="$t('settings.sandbox.dockerCpuLimit')"
                :tips="$t('settings.sandbox.dockerCpuLimitHelp')">
                <t-input-number v-model="docker.cpu_limit" :min="0" :step="0.5" theme="column"
                  placeholder="2" />
              </t-form-item><t-form-item :label="$t('settings.sandbox.dockerMemoryLimit')"
                :tips="$t('settings.sandbox.dockerMemoryLimitHelp')">
                <t-input-number v-model="docker.memory_limit_mb" :min="0" theme="column"
                  placeholder="2048" />
              </t-form-item><t-form-item :label="$t('settings.sandbox.dockerPidsLimit')"
                :tips="$t('settings.sandbox.dockerPidsLimitHelp')">
                <t-input-number v-model="docker.pids_limit" :min="0" theme="column"
                  placeholder="512" />
              </t-form-item>
            <t-form-item :label="$t('settings.sandbox.defaultTimeout')"
              :tips="$t('settings.sandbox.defaultTimeoutHelp')">
              <t-input-number v-model="defaultTimeoutSec" :min="0" theme="column" placeholder="60" />
            </t-form-item>
          </div>
        </div>
      </section>

      <section v-if="currentStepKey === 'runtime'" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.sectionNetwork') }}</h4>
        <p class="section-help section-help--under-title">
          {{ $t('settings.sandbox.networkHint') }}
        </p>



        <t-form-item :label="$t('settings.sandbox.dockerNetworkMode')"
            :tips="$t('settings.sandbox.dockerNetworkModeHelp')">
            <t-select v-model="docker.network_mode"
              :placeholder="$t('settings.sandbox.dockerNetworkBridge')" clearable>
              <t-option value="bridge" :label="$t('settings.sandbox.dockerNetworkBridge')" />
              <t-option value="none" :label="$t('settings.sandbox.dockerNetworkNone')" />
            </t-select>
          </t-form-item>






      </section>

      <section v-if="currentStepKey === 'runtime'" class="setting-drawer__section">
        <div class="section-title-row">
          <div>
            <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.sectionEnvironment') }}</h4>
            <p class="section-help section-help--under-title">{{ $t('settings.sandbox.envVarsHint') }}</p>
          </div>
          <t-button variant="text" size="small" @click="envRows.push({ key: '', value: '' })">
            <template #icon><t-icon name="add" /></template>
            {{ $t('settings.sandbox.addRow') }}
          </t-button>
        </div>
        <div v-if="envRows.length" class="env-rows">
          <div v-for="(row, index) in envRows" :key="index" class="env-row">
            <t-input v-model="row.key" :placeholder="$t('settings.sandbox.envKey')" class="env-key" />
            <t-input v-model="row.value" type="password"
              :placeholder="row.stored ? $t('settings.sandbox.secretKeepHint') : $t('settings.sandbox.envValue')"
              class="env-value" />
            <t-button variant="text" shape="square" size="small" :aria-label="$t('common.delete')"
              @click="envRows.splice(index, 1)">
              <t-icon name="close" />
            </t-button>
          </div>
        </div>
        <div v-else class="env-empty">{{ $t('settings.sandbox.noEnvVars') }}</div>
      </section>

      <section v-if="currentStepKey === 'runtime'" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('settings.sandbox.skillRollout') }}</h4>
        <p class="section-help section-help--under-title">{{ $t('settings.sandbox.skillRolloutHint') }}</p>
        <t-radio-group v-model="skillRollout" class="skill-rollout-group">
          <t-radio value="next_turn">{{ $t('settings.sandbox.skillRolloutNextTurn') }}</t-radio>
          <t-radio value="new_session">{{ $t('settings.sandbox.skillRolloutNewSession') }}</t-radio>
        </t-radio-group>
      </section>
    </t-form>

    <div
      v-if="checkResult && showCheckResult"
      ref="checkResultRef"
      class="check-result"
    >
      <p :class="['check-result__title', checkResult.ok ? 'is-success' : 'is-error']">
        <t-icon :name="checkResult.ok ? 'check-circle-filled' : 'close-circle-filled'" />
        {{ checkResult.ok ? $t('settings.sandbox.checkPassed') : $t('settings.sandbox.checkFailed') }}
      </p>
      <p class="check-result__subtitle">{{ checkScopeHint }}</p>
      <ul class="check-list">
        <li v-for="item in reportedChecks" :key="item.name" class="check-item">
          <t-icon :name="item.ok === true ? 'check-circle-filled'
            : item.ok === false ? 'close-circle-filled' : 'minus-circle'"
            :class="item.ok === true ? 'ok' : item.ok === false ? 'err' : 'skip'" />
          <span class="check-name">{{ checkLabel(item.name) }}</span>
          <span v-if="item.latency_ms" class="check-latency">{{ item.latency_ms }} ms</span>
          <span v-if="checkDetail(item)" class="check-message">{{ checkDetail(item) }}</span>
        </li>
      </ul>
      <p v-if="pendingCheckNames.length" class="check-result__hint">
        {{ $t('settings.sandbox.checkPendingHint', { names: pendingCheckNames.join('、') }) }}
      </p>
    </div>

  </SettingDrawer>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import SandboxBackendBadge from '@/components/settings/SandboxBackendBadge.vue'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import {
  checkSandboxConfig,
  createSandboxConfig,
  parseSandboxConflict,
  updateSandboxConfigById,
  querySandboxTemplates,
  listConfigSkills,
  type SandboxCheckItem,
  type SandboxCheckResult,
  type SandboxConfig,
  type SandboxConfigRecord,
  type SandboxConflict,
  type SandboxDockerConfig,
  type SandboxTemplate,
  isNamedSandboxBackend,
  NAMED_SANDBOX_BACKEND_TYPES,
} from '@/api/system'

type SandboxStepKey = 'connection' | 'runtime'

const props = defineProps<{
  visible: boolean
  record: SandboxConfigRecord | null
  presetType?: string
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const deploymentCapabilities = useDeploymentCapabilitiesStore()
const dockerBackendEnabled = computed(() =>
  deploymentCapabilities.isSupported('settings.sandbox.docker'),
)

// The backend echoes stored secrets as this placeholder. It never leaves the
// form as visible text: inputs stay empty and say "configured", and the
// placeholder is only re-attached on submit so the stored value survives.
const secretPlaceholder = '***'
const isMaskedSecret = (value?: string) => value === secretPlaceholder

// Mirrors DefaultDockerImage on the server, including why it tracks main
// instead of latest: the latest tag still carries an image whose /workspace
// the sandbox account cannot write.
const defaultDockerImage = 'reliquary-sandbox:main'

const backendOptions = computed(() => {
  const types = [...NAMED_SANDBOX_BACKEND_TYPES]
  if (dockerBackendEnabled.value || true) return types
  return types.filter((type) => type !== 'docker')
})

const saving = ref(false)
const checking = ref(false)
const checkResult = ref<SandboxCheckResult | null>(null)
// Distinguishes "run the deep probes" from "run them again" on the panel button.
const lastCheckWasDeep = ref(false)
const conflict = ref<SandboxConflict | null>(null)
const conflictAlertRef = ref<HTMLElement | null>(null)
const checkResultRef = ref<HTMLElement | null>(null)
const nameError = ref('')

const name = ref('')
const description = ref('')
const backend = ref('')
// undefined rather than 0 so the input renders empty and shows its placeholder,
// matching the HTTP timeout / TTL fields. A literal 0 would read as a real value.
const defaultTimeoutSec = ref<number | undefined>(undefined)
const allowPrivateEndpoints = ref(false)
const docker = reactive<SandboxDockerConfig>({})
const envRows = ref<{ key: string; value: string; stored?: boolean }[]>([])
const skillRollout = ref<'next_turn' | 'new_session'>('next_turn')
// Both defaults are the zero value on the server too: egress allowed, inbound
// requiring the per-sandbox credential. Inbound is not editable in this form.
const denyEgressByDefault = ref(false)

const inFlightFromSkills = ref(false)
const templates = ref<SandboxTemplate[]>([])
const templatesLoading = ref(false)
const templatesLoaded = ref(false)
const templatesError = ref('')
const wizardStep = ref(0)
let templatePollTimer: ReturnType<typeof setTimeout> | undefined
const wizardSteps = computed<Array<{ key: SandboxStepKey; title: string }>>(() => {
  const steps: Array<{ key: SandboxStepKey; title: string }> = [
    { key: 'connection', title: t('settings.sandbox.stepConnection') },
  ]

  steps.push({ key: 'runtime', title: t('settings.sandbox.stepRuntime') })
  return steps
})
const currentStepKey = computed<SandboxStepKey>(() => wizardSteps.value[wizardStep.value]?.key || 'connection')
const stepDescription = computed(() => t(`settings.sandbox.stepDescriptions.${currentStepKey.value}`))
const primaryText = computed(() => {
  if (currentStepKey.value === 'connection') return t('settings.sandbox.connectAndContinue')
  return t('common.save')
})
// Deep check needs the fields that actually get probed. Docker collects
// there, so the action waits until the template step.
const canDeepCheck = computed(() => {
  if (!dockerBackendEnabled.value) return false
  return currentStepKey.value === 'connection'
})
const showCheckResult = computed(() => canDeepCheck.value)
const primaryDisabled = computed(() => !dockerBackendEnabled.value)


// savedRecord is the config this drawer is editing, including one it just
// created: a second press of save must update that config rather than create
// another one if the admin walks back through the wizard before closing.
const savedRecord = ref<SandboxConfigRecord | null>(null)
const effectiveRecord = computed(() => savedRecord.value || props.record)
const hasSkillSnapshot = computed(() => (
  Boolean(effectiveRecord.value?.config?.skill_image?.snapshot_id?.trim())
))
const hasInFlightSkill = computed(() => inFlightFromSkills.value)
const retargetFrozen = computed(() => hasSkillSnapshot.value || hasInFlightSkill.value)

// Jumping is what separates editing from creating. A config that does not exist
// yet has to be built in order — its connection has to check out before there
// are templates to choose from. Once it exists, every step is just a page of
// its settings, so all of them open directly; steps already visited stay
// clickable during creation so the rail works as a way back.
function canJumpTo(index: number): boolean {
  if (index === wizardStep.value) return false
  return Boolean(effectiveRecord.value) || index < wizardStep.value
}

function goToStep(index: number) {
  if (!canJumpTo(index)) return
  wizardStep.value = index
  if (currentStepKey.value !== 'connection') {
    stopTemplatePolling()
    return
  }
  // Landing on the template step without having passed through the connection
  // step still has to ask the cluster what it offers; a step already loaded
  // just resumes its poll, as walking back through it always has.
  if (templatesLoaded.value) scheduleTemplatePolling()
  else void loadTemplates()
}
const hasPendingTemplates = computed(() => templates.value.some(isTemplatePending))

const backendLabel = (value: string) => t(`settings.sandbox.backends.${value}`)
const checkLabel = (probe: string) => t(`settings.sandbox.checks.${probe}`, probe)

// Probes deferred to the opt-in deep check are expected, not a problem, so they
// are pulled out of the result list and explained once.
const PENDING_SKIP_REASON = 'needs_deep_check'

const reportedChecks = computed(() => (checkResult.value?.checks || []).filter(
  (item) => item.ok !== null || item.reason !== PENDING_SKIP_REASON,
))

const pendingCheckNames = computed(() => (checkResult.value?.checks || [])
  .filter((item) => item.ok === null && item.reason === PENDING_SKIP_REASON)
  .map((item) => checkLabel(item.name)))

const EGRESS_RESTRICTED_REASON = 'egress_restricted_by_policy'
const egressRestrictedByPolicy = computed(() => (checkResult.value?.checks || []).some(
  (item) => item.reason === EGRESS_RESTRICTED_REASON,
))

// Says which layer the verdict covers, so "检测通过" is not read as "everything
// works" after a connection-only probe, or after egress was skipped because
// the config denies outbound access by policy.
const checkScopeHint = computed(() => {
  if (pendingCheckNames.value.length) return t('settings.sandbox.checkScopeConnection')
  if (egressRestrictedByPolicy.value) return t('settings.sandbox.checkScopePolicyRestricted')
  return t('settings.sandbox.checkScopeFull')
})

function checkDetail(item: SandboxCheckItem): string {
  if (item.message) return item.message
  if (!item.reason) return ''
  return t(`settings.sandbox.skipReasons.${item.reason}`, item.reason)
}

// Mirrors sandbox.MissingRequiredFields on the server. Duplicated on purpose:
// the server stays the authority, this only spares the admin a round-trip and
// points at the offending input instead of showing one combined message.
const REQUIRED_FIELDS: Record<string, string[]> = {
  docker: ['image'],
}

const fieldErrors = ref<Record<string, string>>({})

const fieldStatus = (field: string) => (fieldErrors.value[field] ? 'error' : undefined)
const fieldTip = (field: string) => fieldErrors.value[field] || undefined

const requiredLabel = (labelKey: string) => `${t(`settings.sandbox.${labelKey}`)} *`

// Clearing on input rather than re-validating keeps the error from flickering
// back while the admin is still halfway through typing a URL.
function onFieldInput(field: string) {
  delete fieldErrors.value[field]
  invalidateCheck()
}

function onConnectionInput(field: string) {
  delete fieldErrors.value[field]
  invalidateConnection()
}
function submittedBackendValues(): Record<string, unknown> { return { ...docker } }


function validateRequiredFields(includeTemplate = true): boolean {
  const required = (REQUIRED_FIELDS[backend.value] || []).filter((field) => includeTemplate || field !== 'template_id')
  const values = submittedBackendValues()
  const errors: Record<string, string> = {}
  for (const field of required) {
    const value = values[field]
    if (typeof value !== 'string' || value.trim() === '') {
      errors[field] = t('settings.sandbox.fieldRequired')
    }
  }
  fieldErrors.value = errors
  return Object.keys(errors).length === 0
}

const affectedSessionCount = computed(() => conflict.value?.inventory?.session_ids?.length || 0)
function defaultBackendType(): string { return 'docker' }


function reset() {
  stopTemplatePolling()
  const cfg: SandboxConfig = props.record?.config || {}
  name.value = props.record?.name || ''
  description.value = props.record?.description || ''
  backend.value = isNamedSandboxBackend(cfg.sandbox_type || '')
    ? cfg.sandbox_type!
    : defaultBackendType()
  defaultTimeoutSec.value = cfg.default_timeout_sec || undefined
  allowPrivateEndpoints.value = cfg.allow_private_endpoints === true
  // Replace rather than merge: a reused reactive object would otherwise carry
  // the previously edited config's fields into the next one opened.
  Object.keys(docker).forEach((key) => delete (docker as Record<string, unknown>)[key])
  Object.assign(docker, cfg.docker || {})
  if (!docker.image) {
    docker.image = defaultDockerImage
  }
  envRows.value = Object.entries(cfg.env_vars || {}).map(([key, value]) => (
    isMaskedSecret(value) ? { key, value: '', stored: true } : { key, value }
  ))
  skillRollout.value = cfg.skill_rollout === 'new_session' ? 'new_session' : 'next_turn'
  const net = cfg.network || {}
  denyEgressByDefault.value = net.deny_egress_by_default === true
  checkResult.value = null
  conflict.value = null
  nameError.value = ''
  fieldErrors.value = {}
  templates.value = []
  templatesLoaded.value = false
  templatesError.value = ''
  savedRecord.value = null
  wizardStep.value = 0
}

function selectBackend(value: string) {
  if (backend.value === value) return
  backend.value = value
  if (value === 'docker' && !docker.image) {
    docker.image = defaultDockerImage
  }
  onBackendChange()
}

async function refreshInFlightSkill() {
  const id = props.record?.id
  if (!id) {
    inFlightFromSkills.value = false
    return
  }
  try {
    const res = await listConfigSkills(id)
    inFlightFromSkills.value = (res?.data || []).some(
      (skill) => skill.status === 'installing' || skill.status === 'removing',
    )
  } catch {
    inFlightFromSkills.value = false
  }
}

watch(() => props.visible, (open) => {
  if (open) {
    void deploymentCapabilities.ensureLoaded()
    reset()
    void refreshInFlightSkill()
  } else {
    stopTemplatePolling()
    inFlightFromSkills.value = false
  }
})
function connectionReady(): boolean { return true }

function isTemplatePending(item: SandboxTemplate): boolean {
  const status = item.status?.trim().toLowerCase()
  return ['building', 'waiting', 'pending', 'queued', 'processing', 'running'].includes(status || '')
}

function stopTemplatePolling() {
  if (templatePollTimer) clearTimeout(templatePollTimer)
  templatePollTimer = undefined
}

function scheduleTemplatePolling() {
  stopTemplatePolling()
  if (!props.visible || currentStepKey.value !== 'connection' || !hasPendingTemplates.value) return
  templatePollTimer = setTimeout(() => {
    void loadTemplates(false, true)
  }, 3000)
}

async function loadTemplates(ensureStandard = false, silent = false, replaceStandard = false): Promise<boolean> {

  if (!connectionReady()) return false
  if (!silent) templatesLoading.value = true
  templatesError.value = ''
  try {
    const res = await querySandboxTemplates({
      config: collectPayload(),
      config_id: effectiveRecord.value?.id,
      ensure_standard: ensureStandard,
      replace_standard: replaceStandard,
    })
    templates.value = res.data?.templates || []
    templatesLoaded.value = true
    scheduleTemplatePolling()
    return true
  } catch (e: any) {
    templatesError.value = e?.message || t('settings.sandbox.templateLoadFailed')
    return false
  } finally {
    if (!silent) templatesLoading.value = false
  }
}

function collectPayload(): SandboxConfig {
  const envVars: Record<string, string> = {}
  for (const row of envRows.value) {
    const key = row.key.trim()
    if (!key) continue
    envVars[key] = row.stored && row.value === '' ? secretPlaceholder : row.value
  }
  const payload: SandboxConfig = {
    sandbox_type: backend.value,
    default_timeout_sec: defaultTimeoutSec.value || undefined,
    allow_private_endpoints: allowPrivateEndpoints.value || undefined,
    env_vars: envVars,
    skill_rollout: skillRollout.value,
    network: denyEgressByDefault.value ? { deny_egress_by_default: true } : undefined,
  }
  // Send only the selected backend's block so an unused one cannot fail
  // validation (e.g. a stale private URL left in the other tab).
  payload.docker = { ...docker }
  return payload
}

function close() {
  stopTemplatePolling()
  emit('update:visible', false)
}

function validateName(): boolean {
  if (!name.value.trim()) {
    nameError.value = t('settings.sandbox.configNameRequired')
    return false
  }
  nameError.value = ''
  return true
}

async function handlePrimaryAction() {
  if (!dockerBackendEnabled.value) return
  if (currentStepKey.value === 'connection') {
    if (!validateName() || !validateRequiredFields(false)) return
    if (!(await runCheck(false))) return

    // Docker's template is the image typed on this step. Kick a background
    // pull so the first session does not block on a cold registry fetch.

      void loadTemplates(true)

    invalidateCheck()
    wizardStep.value += 1
    return
  }
  await save()
}

function previousStep() {
  if (wizardStep.value <= 0) return
  wizardStep.value -= 1
  stopTemplatePolling()
}

async function save() {
  const trimmed = name.value.trim()
  if (!validateName()) return
  if (!validateRequiredFields()) return

  saving.value = true
  conflict.value = null
  try {
    const payload = { name: trimmed, description: description.value, config: collectPayload() }
    const existing = effectiveRecord.value
    const res = existing
      ? await updateSandboxConfigById(existing.id, payload)
      : await createSandboxConfig(payload)
    MessagePlugin.success(t('common.saveSuccess'))
    // The list behind the drawer refreshes either way, so closing here is only
    // about whether the wizard has anything left to offer.
    emit('saved')
    const saved = res?.data
    if (saved) savedRecord.value = saved
    close()
  } catch (e: any) {
    const refusal = parseSandboxConflict(e)
    if (refusal) {
      // Keep the drawer open with the form intact: the admin has to act
      // elsewhere first, and retyping everything afterwards would be cruel.
      conflict.value = refusal
      await nextTick()
      conflictAlertRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return
    }
    MessagePlugin.error(e?.message || t('settings.sandbox.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function runCheck(deep: boolean): Promise<boolean> {
  // The probe builds a real client, so an incomplete config would come back as
  // a generic client_build failure instead of naming the empty field.
  if (!validateRequiredFields(deep)) return false
  checking.value = true
  checkResult.value = null
  try {
    // config_id lets the backend resolve masked secrets against the stored row,
    // so an edited form can be probed without retyping the API key.
    const res = await checkSandboxConfig({
      config: collectPayload(),
      config_id: effectiveRecord.value?.id,
      deep,
    })
    checkResult.value = res?.data || null
    lastCheckWasDeep.value = deep
    if (checkResult.value) {
      await nextTick()
      checkResultRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
    return checkResult.value?.ok === true
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('settings.sandbox.checkFailed'))
    return false
  } finally {
    checking.value = false
  }
}

// A result that no longer matches the form is worse than none.
function invalidateCheck() {
  checkResult.value = null
}

function invalidateConnection() {
  stopTemplatePolling()
  invalidateCheck()
  templates.value = []
  templatesLoaded.value = false
  templatesError.value = ''
}

// The two backends require different fields, so carrying errors across a switch
// would flag inputs the admin can no longer even see.
function onBackendChange() {
  fieldErrors.value = {}
  invalidateConnection()
}

onUnmounted(stopTemplatePolling)
</script>

<style lang="less" scoped>
/*
  A step rail rather than segmented buttons: the connector line carries the
  "these run in order" meaning that boxed segments only implied, and the flat
  background keeps the drawer's first visual weight on the form itself.
*/
.sandbox-steps {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
}

.sandbox-step {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--td-text-color-placeholder);
  transition: color 0.15s ease;

  /* Only the steps that draw a connector need to absorb the leftover width. */
  &:not(:last-child) {
    flex: 1;
  }

  &.is-active {
    color: var(--td-brand-color);
  }

  &.is-done {
    color: var(--td-text-color-secondary);
  }

  /*
    Reachable steps render as <button>, so the browser's own control styling has
    to be undone to keep the rail looking identical either way. Only the cursor
    and hover state give the affordance away.
  */
  &.is-clickable {
    padding: 0;
    font: inherit;
    text-align: left;
    background: none;
    border: 0;
    cursor: pointer;

    &:hover:not(.is-active) {
      color: var(--td-brand-color);
    }

    &:focus-visible {
      outline: 2px solid var(--td-brand-color);
      outline-offset: 2px;
      border-radius: 4px;
    }
  }
}

.sandbox-step__marker {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border: 1px solid currentColor;
  border-radius: 50%;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;

  .is-active & {
    background: var(--td-brand-color);
    border-color: var(--td-brand-color);
    color: #fff;
  }

  .is-done & {
    background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
    border-color: color-mix(in srgb, var(--td-brand-color) 35%, transparent);
    color: var(--td-brand-color);
  }
}

.sandbox-step__title {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sandbox-step__line {
  flex: 1;
  min-width: 16px;
  height: 1px;
  margin: 0 4px;
  background: var(--td-component-stroke);

  .is-done & {
    background: color-mix(in srgb, var(--td-brand-color) 35%, transparent);
  }
}

.sandbox-editor-form {
  min-width: 0;
  max-width: 100%;
  overflow-x: hidden;

  :deep(.setting-drawer__section) {
    min-width: 0;
  }

  :deep(.t-form__item) {
    margin-bottom: 0;
  }

  /*
    Form items here stack a control with its own hint text and doc link, so the
    controls area has to be a block instead of TDesign's default single row.
  */
  :deep(.t-form__controls-content) {
    display: block;
  }

  :deep(.t-form__label) {
    padding-bottom: 6px;
    font-size: 13px;
    font-weight: 500;
    line-height: 1.4;
  }

  :deep(.t-input),
  :deep(.t-input-number),
  :deep(.t-select) {
    width: 100%;
    font-size: 13px;
  }
}

.identity-hint {
  margin: 0;
}

.compact-alert {
  padding: 9px 11px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 7px;
  background: var(--td-bg-color-secondarycontainer);

  :deep(.t-alert__icon) {
    font-size: 15px;
  }

  :deep(.t-alert__message) {
    color: var(--td-text-color-secondary);
    font-size: 12px;
    line-height: 1.5;
  }
}

.private-endpoint-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
}

.private-endpoint-row__title {
  margin: 0 0 3px;
  color: var(--td-text-color-primary);
  font-size: 13px;
  font-weight: 600;
}

.form-grid {
  display: grid;
  gap: 12px;

  &--two {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.backend-choice {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 2px 0;
}

.backend-choice__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.backend-choice__name {
  color: var(--td-text-color-primary);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
}

.backend-choice__desc {
  color: var(--td-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
  white-space: normal;
}

.section-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;

  .setting-drawer__section-title {
    margin-bottom: 0;
  }
}

.reliquary-template-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);

  &.is-active {
    border-color: color-mix(in srgb, var(--td-brand-color) 45%, var(--td-component-stroke));
    background: color-mix(in srgb, var(--td-brand-color) 5%, var(--td-bg-color-container));
  }
}

.reliquary-template-card__content {
  flex: 1;
  min-width: 0;

  p {
    margin: 4px 0 0;
    color: var(--td-text-color-secondary);
    font-size: 12px;
    line-height: 1.5;
  }
}

.reliquary-template-card__title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.reliquary-template-card__title {
  font-size: 13px;
  font-weight: 600;
}

.template-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  width: 100%;
  min-height: 88px;
  border: 1px dashed var(--td-component-stroke);
  border-radius: 8px;
  color: var(--td-text-color-secondary);
  font-size: 12px;
}

.template-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  min-width: 0;
  max-width: 100%;
  overflow-x: hidden;
}

.template-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  box-sizing: border-box;
  width: 100%;
  max-width: 100%;
  min-width: 0;
  padding: 10px 12px;
  overflow: hidden;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;

  &:hover:not(.is-disabled):not(.template-row--offer) {
    border-color: var(--td-brand-color-3, var(--td-brand-color));
  }

  &.is-active {
    border-color: var(--td-brand-color);
  }

  &:focus-visible {
    outline: 2px solid var(--td-brand-color);
    outline-offset: 2px;
  }

  &.is-disabled {
    cursor: default;
  }
}

.template-row--offer {
  cursor: default;
  border-style: dashed;
}

.template-row__marker {
  flex-shrink: 0;
  width: 14px;
  height: 14px;
  margin-top: 3px;
  box-sizing: border-box;
  border: 1.5px solid var(--td-border-level-2-color, var(--td-component-stroke));
  border-radius: 50%;
  background: var(--td-bg-color-container);

  .is-active & {
    border-color: var(--td-brand-color);
    box-shadow: inset 0 0 0 3.5px var(--td-brand-color);
  }

  .is-disabled & {
    opacity: 0.45;
  }
}

.template-row__main {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
}

.template-row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 100%;

  :deep(.t-tag) {
    flex-shrink: 0;
  }
}

.template-row__title {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  font-size: 13px;
  font-weight: 600;
  line-height: 22px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.template-row__spacer {
  flex: 1 1 8px;
  min-width: 8px;
}

.template-row__rebuild {
  flex-shrink: 0;

  :deep(.t-button) {
    color: var(--td-text-color-secondary);
    padding-left: 4px;
    padding-right: 4px;

    &:hover {
      color: var(--td-text-color-primary);
    }
  }
}

.template-row__fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 4px 16px;
  margin: 0;
  min-width: 0;
}

.template-row__field {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: baseline;
  gap: 8px;
  min-width: 0;

  dt {
    color: var(--td-text-color-placeholder);
    font-size: 11px;
    line-height: 18px;
    white-space: nowrap;
  }

  dd {
    margin: 0;
    min-width: 0;
    overflow: hidden;
    color: var(--td-text-color-secondary);
    font-size: 12px;
    line-height: 18px;
    text-overflow: ellipsis;
    white-space: nowrap;

    &.is-mono {
      font-family: var(--td-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
      font-size: 11px;
    }
  }
}

.template-row__hint {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  line-height: 1.5;

  &--error {
    color: var(--td-error-color);
  }
}

.field-hints {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  margin-top: 6px;

  .inline-guide-link {
    margin-top: 0;
  }
}

.inline-guide-link {
  display: inline-flex;
  align-items: center;
  align-self: flex-start;
  gap: 5px;
  margin-top: -4px;
  color: var(--td-brand-color);
  font-size: 12px;
  text-decoration: none;

  &:hover {
    color: var(--td-brand-color-hover);
  }
}

.runtime-fields :deep(.t-form__item) {
  margin-bottom: 0;
}

.runtime-fields :deep(.t-input-number) {
  width: 100%;
}

.net-list {
  margin-top: 16px;
}

.net-list__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.net-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 32px;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.net-row--double {
  grid-template-columns: minmax(140px, 0.7fr) minmax(160px, 1.3fr) 32px;
}

.net-row--triple {
  grid-template-columns: minmax(120px, 0.6fr) minmax(140px, 1fr) minmax(120px, 0.8fr) 32px;
}

.net-rule {
  border: 1px solid var(--td-component-border);
  border-radius: 6px;
  padding: 12px;
  margin-bottom: 12px;
}

.net-rule--collapsible {
  padding: 0;
  margin-bottom: 2px;
  border: 0;
  border-radius: 4px;
}

.net-rule--collapsible.is-open {
  margin-bottom: 6px;
  border: 1px solid var(--td-component-border);
}

.net-rule__bar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  min-height: 26px;
  padding: 0 2px;
}

.net-rule__actions {
  display: flex;
  align-items: center;
}

.net-rule__toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  height: 26px;
  padding: 0 4px;
  border: 0;
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: 4px;
  text-align: left;
}

.net-rule__toggle:hover {
  background: var(--td-bg-color-container-hover);
  color: var(--td-text-color-primary);
}

.net-rule__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  line-height: 18px;
  color: var(--td-text-color-primary);
}

.net-rule__name.is-empty {
  color: var(--td-text-color-placeholder);
}

.net-rule__move,
.net-rule__remove {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  border-radius: 4px;
}

.net-rule__move:hover:not(:disabled) {
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-container-hover);
}

.net-rule__move:disabled {
  opacity: 0.35;
  cursor: default;
}

.net-rule__remove:hover {
  color: var(--td-error-color);
  background: var(--td-bg-color-container-hover);
}

.net-rule__body {
  padding: 8px 10px 10px;
  border-top: 1px solid var(--td-component-border);
}

.net-inject {
  margin-top: 8px;
}

.env-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.env-row {
  display: grid;
  grid-template-columns: minmax(140px, 0.7fr) minmax(200px, 1.3fr) 32px;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.env-empty {
  padding: 18px;
  border: 1px dashed var(--td-component-stroke);
  border-radius: 8px;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  text-align: center;
}

.skill-rollout-group {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.section-help {
  margin: 0;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  line-height: 1.5;

  &--under-title {
    margin-top: 5px;
    max-width: 540px;
  }

  /* Sits under an input inside the same form item. */
  &--field {
    margin-top: 6px;
  }
}

.check-result {
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--td-component-stroke);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.check-result__title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  color: var(--td-text-color-primary);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.45;

  &.is-success {
    color: var(--td-success-color);
  }

  &.is-error {
    color: var(--td-error-color);
  }
}

.check-result__subtitle,
.check-result__hint {
  margin: 0;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  line-height: 1.55;
}

.check-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.check-item {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 2px 0;
  font-size: 12px;
}

.check-item .ok {
  color: var(--td-success-color);
}

.check-item .err {
  color: var(--td-error-color);
}

.check-item .skip {
  color: var(--td-text-color-placeholder);
}

.check-name {
  min-width: 140px;
}

.check-latency,
.check-message {
  color: var(--td-text-color-secondary);
}

.footer-check-ok {
  color: var(--td-success-color);
}

.footer-check-error {
  color: var(--td-error-color);
}

.blocked {
  margin-top: 16px;
}

.blocked-top {
  margin-top: 0;
  margin-bottom: 16px;
}

.blocked p {
  margin: 4px 0 0;
}

@media (max-width: 720px) {
  .form-grid--two,
  .reliquary-template-card {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .sandbox-step__title {
    font-size: 12px;
  }

  .template-row__fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

<!--
  The select popup is attached to body, outside the scoped style boundary, so
  the two-line backend options need their row height relaxed globally. The
  overlay class keeps it from touching any other select in the app.
-->
<style lang="less">
.sandbox-backend-popup {
  .t-select-option {
    height: auto;
    min-height: 44px;
    padding: 6px 8px;
    line-height: 1.4;
  }
}
</style>
