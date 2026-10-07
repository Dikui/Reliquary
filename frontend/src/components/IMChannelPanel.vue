<template>
  <div class="im-panel">
    <div class="channels-section">
      <div class="channels-header">
        <span class="channels-title">{{ $t('agentEditor.im.channelsTitle') }}</span>
        <IntegrationsAgentFilter v-model="filterAgentId" :agents="agents" />
        <span class="channels-count">{{ channels.length }}</span>
      </div>

      <t-loading :loading="loading" size="small" class="channels-loading-wrap">
        <div v-if="!loading && channels.length === 0 && !authStore.hasRole('admin')" class="channels-empty">
          <t-empty :description="$t('agentEditor.im.empty')" />
        </div>

        <div v-else-if="!loading" class="channel-grid">
          <button v-for="channel in channels" :key="channel.id" type="button"
            class="channel-card channel-card--clickable" @click="openDrawer(channel)">
            <div class="channel-card__badge" :class="`channel-card__badge--${channel.platform}`">
              <img v-if="platformLogo(channel.platform)" :src="platformLogo(channel.platform)"
                :alt="platformLabel(channel.platform)" class="channel-card__logo" />
              <t-icon v-else name="chat-message" size="22px" />
            </div>
            <div class="channel-card__body">
              <div class="channel-card__header">
                <h3 class="channel-card__title">{{ channel.name || $t('agentEditor.im.unnamed') }}</h3>
                <t-tag v-if="!channel.enabled" size="small" variant="light" theme="warning">
                  {{ $t('agentEditor.im.disabled') }}
                </t-tag>
              </div>
              <span v-if="agentDisplayName(channel)" class="channel-card__agent-name">
                {{ agentDisplayName(channel) }}
              </span>
            </div>
            <div v-if="authStore.hasRole('admin')" class="channel-card__actions" @click.stop>
              <t-dropdown trigger="click" placement="bottom-right" attach="body" :options="channelMenuOptions(channel)"
                @click="handleChannelMenuClick($event, channel)">
                <t-button variant="text" shape="square" size="small" class="channel-card__action-btn channel-card__more"
                  @click.stop>
                  <template #icon><t-icon name="ellipsis" /></template>
                </t-button>
              </t-dropdown>
              <t-popconfirm :content="$t('agentEditor.im.deleteConfirm')"
                :confirm-btn="{ content: $t('common.delete'), theme: 'danger' }"
                :cancel-btn="{ content: $t('common.cancel') }" placement="bottom-right"
                @confirm="() => handleDelete(channel.id)">
                <t-tooltip :content="$t('common.delete')" placement="top">
                  <t-button theme="danger" shape="square" variant="text" size="small"
                    class="channel-card__action-btn channel-card__delete" @click.stop>
                    <template #icon><t-icon name="delete" /></template>
                  </t-button>
                </t-tooltip>
              </t-popconfirm>
            </div>
          </button>

          <button v-if="authStore.hasRole('admin')" type="button" class="channel-card channel-card--add"
            @click="openCreate">
            <span class="channel-card__badge" aria-hidden="true">
              <t-icon name="add" />
            </span>
            <div class="channel-card__body">
              <div class="channel-card__header">
                <span class="channel-card__title">{{ $t('agentEditor.im.addChannel') }}</span>
              </div>
            </div>
            <span class="channel-card__actions channel-card__actions--spacer" aria-hidden="true" />
          </button>
        </div>
      </t-loading>
    </div>

    <SettingDrawer v-model:visible="showCreateDialog" class="im-channel-drawer" :title="drawerTitle"
      :description="drawerStepDescription" storage-key="setting-drawer:im-channel" width="560px"
      :confirm-loading="saving" :confirm-text="drawerConfirmText" :hide-footer="!authStore.hasRole('admin')"
      @confirm="handleDrawerConfirm" @cancel="resetForm">
      <template #headerIcon>
        <img v-if="platformLogo(formData.platform)" :src="platformLogo(formData.platform)"
          :alt="platformLabel(formData.platform)" class="drawer-platform-icon" />
        <t-icon v-else name="chat-message" />
      </template>

      <template v-if="wizardStep > 0" #footer-left>
        <t-button variant="outline" @click="prevWizardStep">
          {{ $t('datasource.back') }}
        </t-button>
      </template>

      <div class="im-steps">
        <div v-for="(title, i) in stepTitles" :key="i"
          :class="['im-step', { active: wizardStep === i, done: wizardStep > i }]">
          <span class="im-step-num">
            <t-icon v-if="wizardStep > i" name="check" class="im-step-check" />
            <template v-else>{{ i + 1 }}</template>
          </span>
          <span class="im-step-title">{{ title }}</span>
        </div>
      </div>

      <!-- Step 1: Basic -->
      <div v-if="wizardStep === 0" class="im-step-body">
        <section class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionChannel') }}</h4>

          <div class="form-item">
            <label class="form-label required">{{ $t('integrations.boundAgent') }}</label>
            <div class="agent-field-row">
              <t-select v-model="formData.target_agent_id" :options="agentOptions" filterable
                :placeholder="$t('integrations.selectAgentPlaceholder')" />
            </div>
          </div>

          <div class="form-item">
            <label class="form-label required">{{ $t('agentEditor.im.platform') }}</label>
            <t-select v-model="formData.platform" :disabled="!!editingChannel" class="im-platform-select"
              @change="onPlatformChange">
              <template v-if="platformLogo(formData.platform)" #prefixIcon>
                <img :src="platformLogo(formData.platform)" :alt="platformLabel(formData.platform)"
                  class="im-platform-select-prefix" />
              </template>
              <t-option v-for="item in platformOptions" :key="item.value" :value="item.value" :label="item.label">
                <div class="im-platform-select-option">
                  <img :src="item.logo" :alt="item.label" class="im-platform-select-option__icon" />
                  <span>{{ item.label }}</span>
                </div>
              </t-option>
            </t-select>
          </div>

          <div class="form-item">
            <label class="form-label">{{ $t('agentEditor.im.channelName') }}</label>
            <t-input v-model="formData.name" :placeholder="$t('agentEditor.im.channelNamePlaceholder')"
              @focus="channelNameTouched = true" />
            <p v-if="!editingChannel" class="form-desc">{{ $t('agentEditor.im.channelNameDefaultHint') }}</p>
          </div>

          <div v-if="editingChannel && authStore.hasRole('admin')" class="setting-row setting-row--last">
            <div class="setting-info">
              <label>{{ $t('agentEditor.im.enabled') }}</label>
            </div>
            <div class="setting-control">
              <t-switch v-model="editingEnabled" size="small" />
            </div>
          </div>
        </section>
      </div>

      <!-- Step 2: Connection -->
      <div v-else-if="wizardStep === 1" class="im-step-body">
        <section  class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionAccess') }}</h4>

          <div class="form-item">
            <label class="form-label required">{{ $t('agentEditor.im.mode') }}</label>
            <div class="option-chips">
              <button type="button" class="option-chip"
                :class="{ 'option-chip--active': formData.mode === 'websocket' }"
                :disabled="false"
                @click="formData.mode = 'websocket'">
                WebSocket
              </button>
              <button type="button" class="option-chip" :class="{ 'option-chip--active': formData.mode === 'webhook' }"
                @click="formData.mode = 'webhook'">
                Webhook
              </button>
            </div>
            <p class="form-desc">
              {{ $t('agentEditor.im.modeHint') }}
            </p>
          </div>

          <div class="form-item">
            <label class="form-label required">{{ $t('agentEditor.im.outputMode') }}</label>
            <div class="option-chips">
              <button type="button" class="option-chip"
                :class="{ 'option-chip--active': formData.output_mode === 'stream' }"
                @click="formData.output_mode = 'stream'">
                {{ $t('agentEditor.im.outputStream') }}
              </button>
              <button type="button" class="option-chip"
                :class="{ 'option-chip--active': formData.output_mode === 'full' }"
                @click="formData.output_mode = 'full'">
                {{ $t('agentEditor.im.outputFull') }}
              </button>
            </div>
          </div>
        </section>

        <section class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionSession') }}</h4>
          <div class="form-item">
            <label class="form-label required">{{ $t('agentEditor.im.sessionMode') }}</label>
            <div class="option-chips">
              <button type="button" class="option-chip"
                :class="{ 'option-chip--active': formData.session_mode === 'user' }"
                @click="formData.session_mode = 'user'">
                {{ $t('agentEditor.im.sessionModeUser') }}
              </button>
              <button type="button" class="option-chip"
                :class="{ 'option-chip--active': formData.session_mode === 'thread' }"
                :disabled="!platformSupportsThread(formData.platform)" @click="formData.session_mode = 'thread'">
                {{ $t('agentEditor.im.sessionModeThread') }}
              </button>
            </div>
            <p class="form-desc">{{ $t('agentEditor.im.sessionModeHint') }}</p>
          </div>
        </section>

        <section v-if="editingChannel && formData.mode === 'webhook'"
          class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionCallback') }}</h4>
          <div class="form-item">
            <label class="form-label">{{ $t('agentEditor.im.callbackUrl') }}</label>
            <div class="callback-url-control">
              <t-input :model-value="getCallbackUrl(editingChannel)" readonly
                class="mono-text-input callback-url-input" />
              <t-button size="small" variant="text" :title="$t('common.copy')" @click="copyUrl(editingChannel)">
                <t-icon name="file-copy" />
              </t-button>
            </div>
          </div>
        </section>
      </div>

      <!-- Step 3: File knowledge base -->
      <div v-else-if="wizardStep === 2" class="im-step-body">
        <section class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionKnowledge') }}</h4>
          <div class="form-item">
            <label class="form-label">{{ $t('agentEditor.im.fileKnowledgeBase') }}</label>
            <t-select v-model="formData.knowledge_base_id"
              :placeholder="$t('agentEditor.im.fileKnowledgeBasePlaceholder')" clearable filterable>
              <t-option v-for="kb in knowledgeBases" :key="kb.id" :value="kb.id" :label="kb.name" />
            </t-select>
            <p class="form-desc">{{ $t('agentEditor.im.fileKnowledgeBaseHint') }}</p>
          </div>
        </section>
      </div>

      <!-- Step 4: Credentials -->
      <div v-else class="im-step-body">
        <section class="setting-drawer__section im-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('agentEditor.im.sectionCredentials') }}</h4>
          <div class="drawer-form">



            <!-- Feishu / Lark credentials — same fields, different open platform -->






















          </div>
        </section>
      </div>
    </SettingDrawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, onUnmounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { MessagePlugin } from 'tdesign-vue-next';
import { copyWithToast } from '@/utils/clipboard';
import {
  listAllIMChannels, listAgents, listIMChannels, createIMChannel, updateIMChannel, deleteIMChannel, toggleIMChannel,
  type IMChannelOverview, type CustomAgent,
} from '@/api/agent';
import { useChatResourcesStore } from '@/stores/chatResources';
import type { IMChannel } from '@/api/agent';
import { useAuthStore } from '@/stores/auth';
import SettingDrawer from '@/components/settings/SettingDrawer.vue';
import IntegrationsAgentFilter from '@/components/IntegrationsAgentFilter.vue';
import feishuLogo from '@/assets/img/im/feishu.svg';

type IMPlatform = IMChannel['platform'];

const PLATFORM_LOGO: Record<string, string> = {
  feishu: feishuLogo,
};

const platformLogo = (platform: string): string => (platform ? PLATFORM_LOGO[platform] || '' : '');

const { t } = useI18n();
const authStore = useAuthStore();

const filterAgentId = defineModel<string>('filterAgentId', { default: '' });

const agents = ref<CustomAgent[]>([]);
const agentOptions = computed(() =>
  agents.value.map((agent) => ({ label: agent.name, value: agent.id })),
);

const allChannels = ref<Array<IMChannel | IMChannelOverview>>([]);
const channels = computed(() => {
  const filter = filterAgentId.value?.trim();
  if (!filter) return allChannels.value;
  return allChannels.value.filter((channel) => channel.agent_id === filter);
});
const loading = ref(false);
const saving = ref(false);
const showCreateDialog = ref(false);
const editingChannel = ref<IMChannel | null>(null);
const editingEnabled = ref(true);
const wizardStep = ref(0);
const channelNameTouched = ref(false);

const stepTitles = computed(() => [
  t('agentEditor.im.stepBasic'),
  t('agentEditor.im.stepConnection'),
  t('agentEditor.im.stepKnowledge'),
  t('agentEditor.im.stepCredentials'),
]);

const drawerStepDescription = computed(() => stepTitles.value[wizardStep.value] ?? '');

const drawerConfirmText = computed(() =>
  wizardStep.value < stepTitles.value.length - 1 ? t('common.next') : t('common.save'),
);

const platformOptions = computed(() => ([
  { value: 'feishu' as IMPlatform, label: t('agentEditor.im.feishu'), logo: feishuLogo },
]));

// Feishu and Lark are the same product on separate clouds, so each has its own
// open platform console. Bots must be created on the one matching the channel.
const openPlatformConsole = computed(() => ({ url: 'https://open.feishu.cn/', labelKey: 'agentEditor.im.feishuConsole' }));

const drawerTitle = computed(() => {
  if (editingChannel.value) {
    return formData.value.name?.trim() || t('agentEditor.im.unnamed');
  }
  return t('agentEditor.im.addChannel');
});

function validateWizardStep(step: number): boolean {
  if (step === 0 && !formData.value.target_agent_id) {
    MessagePlugin.warning(t('integrations.selectAgentHint'));
    return false;
  }
  return true;
}

function prevWizardStep() {
  if (wizardStep.value > 0) wizardStep.value -= 1;
}

async function handleDrawerConfirm() {
  if (wizardStep.value < stepTitles.value.length - 1) {
    if (!validateWizardStep(wizardStep.value)) return;
    wizardStep.value += 1;
    return;
  }
  await handleSave();
}

// Knowledge base options for file-to-KB feature
const knowledgeBases = ref<{ id: string; name: string }[]>([]);

const defaultCredentials = (): Record<string, any> => ({});

const formData = ref({
  target_agent_id: '',
  platform: 'feishu' as IMPlatform,
  name: '',
  mode: 'websocket' as 'webhook' | 'websocket',
  output_mode: 'stream' as 'stream' | 'full',
  session_mode: 'user' as 'user' | 'thread',
  knowledge_base_id: '',
  credentials: defaultCredentials(),
});

const channelMenuOptions = (channel: IMChannel | IMChannelOverview) => ([
  {
    content: channel.enabled ? t('common.off') : t('common.on'),
    value: 'toggle',
  },
]);

function handleChannelMenuClick(
  data: { value?: string },
  channel: IMChannel | IMChannelOverview,
) {
  if (data.value === 'toggle') {
    void handleToggle(channel);
  }
}

function agentDisplayName(channel: IMChannel | IMChannelOverview): string {
  return agentForChannel(channel)?.name || '';
}

function agentForChannel(channel: IMChannel | IMChannelOverview): CustomAgent | undefined {
  const found = agents.value.find((agent) => agent.id === channel.agent_id);
  if (found) return found;
  const overviewName = (channel as IMChannelOverview).agent_name;
  if (!overviewName) return undefined;
  return {
    id: channel.agent_id,
    name: overviewName,
    is_builtin: false,
    config: {},
  };
}

function platformLabel(platform: string): string {
  const key = `agentEditor.im.${platform}`;
  return t(key);
}

function defaultChannelName(platform: string = formData.value.platform): string {
  return platformLabel(platform);
}

function resolvedChannelName(): string {
  return formData.value.name.trim() || defaultChannelName();
}
function platformSupportsThread(platform: string): boolean { return platform === 'feishu'; }


watch(
  () => formData.value.platform,
  (p) => {

    if (!platformSupportsThread(p)) {
      formData.value.session_mode = 'user';
    }
  },
);
function onPlatformChange(val: string | number | boolean) { if (editingChannel.value) return; formData.value.credentials = defaultCredentials(); formData.value.mode = 'websocket'; formData.value.output_mode = 'stream'; if (!channelNameTouched.value) formData.value.name = defaultChannelName(String(val)); }

async function loadChannels() {
  loading.value = true;
  try {
    const chatResources = useChatResourcesStore();
    const [channelRes, agentRes] = await Promise.all([
      listAllIMChannels(),
      listAgents(),
      chatResources.ensureKnowledgeBases(),
    ]);
    allChannels.value = channelRes.data || [];
    agents.value = agentRes?.data || [];
    knowledgeBases.value = chatResources.rawKnowledgeBases.map((kb: any) => ({ id: kb.id, name: kb.name }));
  } catch {
    allChannels.value = [];
  } finally {
    loading.value = false;
  }
}

function getCallbackUrl(channel: IMChannel): string {
  const base = window.location.origin;
  return `${base}/api/v1/im/callback/${channel.id}`;
}

async function copyUrl(channel: IMChannel) {
  await copyWithToast(getCallbackUrl(channel), 'common.copySuccess');
}

function openCreate() {
  resetForm();
  if (filterAgentId.value) {
    formData.value.target_agent_id = filterAgentId.value;
  }
  showCreateDialog.value = true;
}

function openDrawer(channel: IMChannel | IMChannelOverview) {
  void editChannel(channel);
}

async function editChannel(channel: IMChannel | IMChannelOverview) {
  wizardStep.value = 0;
  let fullChannel: IMChannel | null = null;
  if (!('credentials' in channel)) {
    try {
      const res = await listIMChannels(channel.agent_id);
      fullChannel = (res.data || []).find((item) => item.id === channel.id) || null;
    } catch {
      fullChannel = null;
    }
  } else {
    fullChannel = channel as IMChannel;
  }
  if (!fullChannel) {
    MessagePlugin.error(t('common.operationFailed'));
    return;
  }
  editingChannel.value = fullChannel;
  editingEnabled.value = fullChannel.enabled;
  channelNameTouched.value = true;
  formData.value = {
    target_agent_id: fullChannel.agent_id,
    platform: fullChannel.platform,
    name: fullChannel.name,
    mode: fullChannel.mode,
    output_mode: fullChannel.output_mode,
    session_mode: fullChannel.session_mode || 'user',
    knowledge_base_id: fullChannel.knowledge_base_id || '',
    credentials: { ...fullChannel.credentials },
  };
  showCreateDialog.value = true;
}

function resetForm() {
  editingChannel.value = null;
  editingEnabled.value = true;
  wizardStep.value = 0;
  channelNameTouched.value = false;
  formData.value = {
    target_agent_id: filterAgentId.value || '',
    platform: 'feishu',
    name: defaultChannelName('feishu'),
    mode: 'websocket',
    output_mode: 'stream',
    session_mode: 'user',
    knowledge_base_id: '',
    credentials: defaultCredentials(),
  };
}

async function handleSave() {
  saving.value = true;
  try {



    if (editingChannel.value) {
      await updateIMChannel(editingChannel.value.id, {
        name: resolvedChannelName(),
        mode: formData.value.mode,
        output_mode: formData.value.output_mode,
        session_mode: formData.value.session_mode,
        knowledge_base_id: formData.value.knowledge_base_id,
        credentials: formData.value.credentials,
        enabled: editingEnabled.value,
        ...(formData.value.target_agent_id ? { agent_id: formData.value.target_agent_id } : {}),
      });
      MessagePlugin.success(t('common.updateSuccess'));
    } else {
      const targetAgentId = formData.value.target_agent_id;
      if (!targetAgentId) {
        MessagePlugin.warning(t('integrations.selectAgentHint'));
        return;
      }
      await createIMChannel(targetAgentId, {
        platform: formData.value.platform,
        name: resolvedChannelName(),
        mode: formData.value.mode,
        output_mode: formData.value.output_mode,
        session_mode: formData.value.session_mode,
        knowledge_base_id: formData.value.knowledge_base_id,
        credentials: formData.value.credentials,
      });
      MessagePlugin.success(t('common.createSuccess'));
    }
    showCreateDialog.value = false;
    resetForm();
    await loadChannels();
  } catch (e: any) {
    const msg = e?.message || (typeof e?.error === 'string' ? e.error : null) || t('common.operationFailed');
    MessagePlugin.error(msg);
  } finally {
    saving.value = false;
  }
}

async function handleToggle(channel: IMChannel | IMChannelOverview) {
  try {
    await toggleIMChannel(channel.id);
    await loadChannels();
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('common.operationFailed'));
  }
}

async function handleDelete(id: string) {
  try {
    await deleteIMChannel(id);
    MessagePlugin.success(t('common.deleteSuccess'));
    await loadChannels();
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('common.operationFailed'));
  }
}

onMounted(() => {
  loadChannels();
});

watch(filterAgentId, (id) => {
  if (!showCreateDialog.value && !editingChannel.value && id) {
    formData.value.target_agent_id = id;
  }
});

onUnmounted(() => {
});
</script>

<style scoped lang="less">
@import './css/channel-panel-list.less';

.im-panel {
  display: flex;
  flex-direction: column;
}

.drawer-platform-icon {
  width: 16px;
  height: 16px;
  object-fit: contain;
}

.im-steps {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--td-component-stroke);
  padding-bottom: 12px;
}

.im-step {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.im-step-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.im-step.active {
  color: var(--td-brand-color);
  font-weight: 500;
}

.im-step.done {
  color: var(--td-text-color-secondary);
  font-weight: 500;
}

.im-step-num {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  border: 1px solid var(--td-component-stroke);
  color: var(--td-text-color-placeholder);
  background: transparent;
}

.im-step.active .im-step-num {
  background: var(--td-brand-color);
  color: #fff;
  border-color: var(--td-brand-color);
}

.im-step.done .im-step-num {
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-brand-color);
  border-color: var(--td-component-stroke);
}

.im-step-check {
  font-size: 12px;
}

.im-step-body {
  display: flex;
  flex-direction: column;
  gap: 0;
}

:deep(.im-drawer__section.setting-drawer__section) {
  gap: 10px;
  padding: 10px 0 14px;
}

.callback-url-control {
  display: flex;
  align-items: center;
  gap: 4px;
}

.callback-url-input {
  flex: 1;
  min-width: 0;
}

.mono-text-input :deep(input) {
  font-family: var(--app-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 12px;
}

.drawer-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.form-item {
  margin-bottom: 0;
}

.form-label {
  display: block;
  margin-bottom: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  line-height: 1.4;

  &--inline {
    margin-bottom: 0;
  }

  &.required::after {
    content: '*';
    color: var(--td-error-color);
    margin-left: 4px;
  }
}

.im-platform-select-prefix {
  width: 16px;
  height: 16px;
  object-fit: contain;
}

.im-platform-select-option {
  display: flex;
  align-items: center;
  gap: 8px;

  &__icon {
    width: 16px;
    height: 16px;
    object-fit: contain;
    flex-shrink: 0;
  }
}

.form-desc {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.45;
  color: var(--td-text-color-placeholder);

  .doc-link {
    margin-left: 4px;
    color: var(--td-brand-color);
  }
}

.option-chips {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 3px;
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.option-chip {
  border: none;
  background: transparent;
  color: var(--td-text-color-secondary);
  font: inherit;
  font-size: 12px;
  line-height: 1.3;
  padding: 5px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
  white-space: nowrap;

  &:hover:not(:disabled) {
    color: var(--td-text-color-primary);
  }

  &:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  &--active {
    background: var(--td-bg-color-container);
    color: var(--td-brand-color);
    font-weight: 500;
    box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
  }
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &--last {
    border-bottom: none;
    padding-bottom: 0;
  }
}

.setting-info {
  flex: 1;
  min-width: 0;
  max-width: 72%;
  padding-right: 8px;

  label {
    display: block;
    margin: 0 0 4px;
    font-size: 13px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    line-height: 1.4;
  }

  .desc {
    margin: 0;
    font-size: 12px;
    line-height: 1.45;
    color: var(--td-text-color-placeholder);
  }
}

.setting-control {
  flex-shrink: 0;
  padding-top: 2px;
}

.platform-link-hint {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--td-text-color-placeholder);

  .doc-link {
    white-space: nowrap;
  }

  .hint-text {
    color: var(--td-text-color-placeholder);
  }
}









.qr-container {
  position: relative;
  width: 200px;
  height: 200px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  overflow: hidden;
  // QR code images are always black-on-white; force white background
  // so the code remains scannable in dark mode.
  background: #fff;

  .qr-image {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
}

.qr-expired-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  cursor: pointer;
  font-size: 12px;

  .refresh-icon {
    font-size: 24px;
  }
}

.qr-hint {
  font-size: 13px;
  color: var(--td-text-color-secondary);
  text-align: center;
}
</style>

<style lang="less">
.im-channel-drawer .setting-drawer__header-icon:has(.drawer-platform-icon) {
  background: var(--td-bg-color-container, #fff);
  box-shadow: inset 0 0 0 1px var(--td-component-stroke);
}

.im-channel-drawer .drawer-platform-icon {
  display: block;
  width: 20px;
  height: 20px;
  object-fit: contain;
}
</style>
