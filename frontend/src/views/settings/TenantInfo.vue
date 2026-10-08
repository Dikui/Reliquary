<template>
  <div class="tenant-info">
    <div class="section-header">
      <h2>{{ $t('tenant.title') }}</h2>
      <p class="section-description">{{ $t('tenant.sectionDescription') }}</p>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="loading-inline">
      <t-loading size="small" />
      <span>{{ $t('tenant.loadingInfo') }}</span>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="error-inline">
      <t-alert theme="error" :message="error">
        <template #operation>
          <t-button size="small" @click="loadInfo">{{ $t('tenant.retry') }}</t-button>
        </template>
      </t-alert>
    </div>

    <!-- 团队信息列表 -->
    <div v-else class="tenant-info-body">
      <div class="settings-group">
        <!-- Tenant ID -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.idLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.idDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ tenantInfo?.id || '-' }}</span>
          </div>
        </div>

        <!-- Tenant name -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.nameLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.nameDescription') }}</p>
          </div>
          <div class="setting-control">
            <!-- 只读态：显示名称 + 编辑按钮（owner 才看得见编辑入口）。
               原地编辑取代弹窗：少一层视觉打断，与其它行的展示节奏一致。 -->
            <template v-if="!editing">
              <span class="info-value">{{ tenantInfo?.name || '-' }}</span>
              <t-button v-if="canEditTenant" theme="default" variant="text" shape="square" size="small"
                class="edit-btn" :title="$t('tenant.details.editName')" :aria-label="$t('tenant.details.editName')"
                @click="startEditName">
                <template #icon>
                  <t-icon name="edit" />
                </template>
              </t-button>
            </template>
            <!-- 编辑态：输入框 + 保存/取消。回车保存，Esc 取消。 -->
            <div v-else class="inline-edit">
              <t-input v-model="editName" :placeholder="$t('tenant.details.editNamePlaceholder')" :maxlength="64"
                :disabled="saving" autofocus class="inline-edit-input" @enter="saveTenantName"
                @keydown="onEditKeydown" />
              <t-button theme="primary" size="small" :loading="saving" :disabled="!canSubmit" @click="saveTenantName">
                {{ $t('tenant.details.editNameConfirm') }}
              </t-button>
              <t-button theme="default" variant="outline" size="small" :disabled="saving" @click="cancelEditName">
                {{ $t('tenant.details.editNameCancel') }}
              </t-button>
            </div>
          </div>
        </div>

        <!-- Tenant description -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.descriptionLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.descriptionDescription') }}</p>
          </div>
          <div class="setting-control">
            <!-- 只读态：显示描述（空时给占位）+ 编辑按钮（owner 才看得见编辑入口）。
               与名称同款"原地编辑"模式，少一层弹窗打断。 -->
            <template v-if="!editingDescription">
              <span class="info-value description-value" :class="{ 'is-empty': !tenantInfo?.description }">
                {{ tenantInfo?.description || $t('tenant.details.descriptionEmptyPlaceholder') }}
              </span>
              <t-button v-if="canEditTenant" theme="default" variant="text" shape="square" size="small"
                class="edit-btn" :title="$t('tenant.details.editDescription')"
                :aria-label="$t('tenant.details.editDescription')" @click="startEditDescription">
                <template #icon>
                  <t-icon name="edit" />
                </template>
              </t-button>
            </template>
            <!-- 编辑态：textarea + 保存/取消。Esc 取消、Ctrl/⌘+Enter 保存；
               textarea 上 Enter 默认换行更顺手，不接管 Enter 提交。 -->
            <div v-else class="inline-edit inline-edit-description">
              <t-textarea v-model="editDescription"
                :placeholder="$t('tenant.details.editDescriptionPlaceholder')" :maxlength="512"
                :autosize="{ minRows: 2, maxRows: 6 }" :disabled="savingDescription" autofocus
                class="inline-edit-textarea" @keydown="onEditDescriptionKeydown" />
              <div class="inline-edit-actions">
                <t-button theme="primary" size="small" :loading="savingDescription"
                  :disabled="!canSubmitDescription" @click="saveTenantDescription">
                  {{ $t('tenant.details.editNameConfirm') }}
                </t-button>
                <t-button theme="default" variant="outline" size="small" :disabled="savingDescription"
                  @click="cancelEditDescription">
                  {{ $t('tenant.details.editNameCancel') }}
                </t-button>
              </div>
            </div>
          </div>
        </div>

        <!-- Tenant business -->
        <div v-if="tenantInfo?.business" class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.businessLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.businessDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ tenantInfo.business }}</span>
          </div>
        </div>

        <!-- Tenant status -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.statusLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.statusDescription') }}</p>
          </div>
          <div class="setting-control">
            <t-tag :theme="getStatusTheme(tenantInfo?.status)" variant="light" size="small">
              {{ getStatusText(tenantInfo?.status) }}
            </t-tag>
          </div>
        </div>

        <!-- Tenant creation time -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.details.createdAtLabel') }}</label>
            <p class="desc">{{ $t('tenant.details.createdAtDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ formatDate(tenantInfo?.created_at) }}</span>
          </div>
        </div>

        <!-- Storage quota -->
        <div v-if="tenantInfo?.storage_quota !== undefined" class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.storage.quotaLabel') }}</label>
            <p class="desc">{{ $t('tenant.storage.quotaDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ formatBytes(tenantInfo.storage_quota) }}</span>
          </div>
        </div>

        <!-- Used storage -->
        <div v-if="tenantInfo?.storage_quota !== undefined" class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.storage.usedLabel') }}</label>
            <p class="desc">{{ $t('tenant.storage.usedDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ formatBytes(tenantInfo.storage_used || 0) }}</span>
          </div>
        </div>

        <!-- Storage usage -->
        <div v-if="tenantInfo?.storage_quota !== undefined" class="setting-row">
          <div class="setting-info">
            <label>{{ $t('tenant.storage.usageLabel') }}</label>
            <p class="desc">{{ $t('tenant.storage.usageDescription') }}</p>
          </div>
          <div class="setting-control">
            <div class="usage-control">
              <span class="usage-text">{{ getUsagePercentage() }}%</span>
              <!-- t-progress: theme = 形态（line/plump/circle）；颜色用 status -->
              <t-progress :percentage="getUsagePercentage()" :show-info="false" size="small"
                :status="getUsagePercentage() > 80 ? 'warning' : 'success'" style="flex: 1;" />
            </div>
          </div>
        </div>

      </div>





    </div>
    <TeamModelTierSettings ref="modelTierSettings" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import TeamModelTierSettings from './TeamModelTierSettings.vue'
import { getCurrentUser, type TenantInfo } from '@/api/auth'
import { updateTenant as updateTenantApi } from '@/api/tenant'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

const modelTierSettings = ref<InstanceType<typeof TeamModelTierSettings>>()
defineExpose({ confirmLeave: () => modelTierSettings.value?.confirmLeave() ?? Promise.resolve(true) })
const { t, locale } = useI18n()
const authStore = useAuthStore()

// Reactive state
const tenantInfo = ref<TenantInfo | null>(null)
const loading = ref(true)
const error = ref('')

// 仅 owner 可改空间名（与后端 router.go 中 g.Owner() 守卫一致；
// 服务端始终是权限的最终裁判，这里只决定 UI 是否露出入口）。
const canEditTenant = computed(() => authStore.isLoggedIn)


// 原地编辑空间名称：editing 控制行内只读 / 编辑两种形态切换。
// 不沿用 dialog 是因为这里只有一个字段，弹窗反而打断了配置浏览节奏。
const editing = ref(false)
const editName = ref('')
const saving = ref(false)
const editNameTrimmed = computed(() => editName.value.trim())
// 保存按钮可点条件：非空、改了内容、不在保存中。
// 后端 name 字段没有 uniqueIndex 也没有重名校验，所以这里不做"是否已存在"的判断；
// 后端 service 也只在 create 时拒空，update 时不校验，保持前端兜底非空即可。
const canSubmit = computed(
  () => !saving.value && !!editNameTrimmed.value && editNameTrimmed.value !== tenantInfo.value?.name,
)

const startEditName = () => {
  editName.value = tenantInfo.value?.name || ''
  editing.value = true
}

const cancelEditName = () => {
  if (saving.value) return
  editing.value = false
  editName.value = ''
}

// t-input 自身不冒泡 esc，这里手动处理（与 enter 的体验对称）。
const onEditKeydown = (_value: any, ctx: { e: KeyboardEvent }) => {
  if (ctx?.e?.key === 'Escape') {
    cancelEditName()
  }
}

// 原地编辑空间描述：与名称对称的 editing / editValue / saving 三态。
// 描述允许为空（业务上是可选字段），所以可提交条件不要求非空，只要内容变了即可。
const editingDescription = ref(false)
const editDescription = ref('')
const savingDescription = ref(false)
const editDescriptionTrimmed = computed(() => editDescription.value.trim())
const canSubmitDescription = computed(
  () => !savingDescription.value && editDescriptionTrimmed.value !== (tenantInfo.value?.description || ''),
)

const startEditDescription = () => {
  editDescription.value = tenantInfo.value?.description || ''
  editingDescription.value = true
}

const cancelEditDescription = () => {
  if (savingDescription.value) return
  editingDescription.value = false
  editDescription.value = ''
}

// textarea 上 Enter 默认走换行，提交走 Ctrl/⌘+Enter；Esc 取消。
const onEditDescriptionKeydown = (_value: any, ctx: { e: KeyboardEvent }) => {
  const e = ctx?.e
  if (!e) return
  if (e.key === 'Escape') {
    cancelEditDescription()
    return
  }
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault()
    void saveTenantDescription()
  }
}

const saveTenantDescription = async () => {
  if (!tenantInfo.value?.id) return
  const newDesc = editDescriptionTrimmed.value
  if (newDesc === (tenantInfo.value.description || '')) {
    editingDescription.value = false
    return
  }

  try {
    savingDescription.value = true
    const resp = await updateTenantApi(Number(tenantInfo.value.id), { description: newDesc })
    if (resp.success) {
      // 本地立即回显，避免等 /auth/me 往返。
      if (tenantInfo.value) {
        tenantInfo.value = { ...tenantInfo.value, description: newDesc }
      }
      MessagePlugin.success(t('tenant.details.editDescriptionSuccess'))
      editingDescription.value = false
    } else {
      MessagePlugin.error(resp.message || t('tenant.details.editDescriptionFailed'))
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('tenant.details.editDescriptionFailed'))
  } finally {
    savingDescription.value = false
  }
}

const saveTenantName = async () => {
  const newName = editNameTrimmed.value
  if (!newName) {
    MessagePlugin.warning(t('tenant.details.editNameRequired'))
    return
  }
  if (!tenantInfo.value?.id) return
  if (newName === tenantInfo.value.name) {
    editing.value = false
    return
  }

  try {
    saving.value = true
    const resp = await updateTenantApi(Number(tenantInfo.value.id), { name: newName })
    if (resp.success) {
      // 本地立即回显，并同步登录态中的团队名称。
      if (tenantInfo.value) {
        tenantInfo.value = { ...tenantInfo.value, name: newName }
      }
      if (authStore.tenant && String(authStore.tenant.id) === String(tenantInfo.value?.id)) {
        authStore.setTenant({ ...authStore.tenant, name: newName })
      }
      // 同步成员记录中的团队名称。

      MessagePlugin.success(t('tenant.details.editNameSuccess'))
      editing.value = false
    } else {
      MessagePlugin.error(resp.message || t('tenant.details.editNameFailed'))
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('tenant.details.editNameFailed'))
  } finally {
    saving.value = false
  }
}

// Methods
const loadInfo = async () => {
  try {
    loading.value = true
    error.value = ''

    const userResponse = await getCurrentUser()

    const data = userResponse?.data as { tenant?: TenantInfo } | undefined
    if ((userResponse as any).success && data?.tenant) {
      tenantInfo.value = data.tenant
    } else {
      error.value = userResponse.message || t('tenant.messages.fetchFailed')
    }
  } catch (err: any) {
    error.value = err?.message || t('tenant.messages.networkError')
  } finally {
    loading.value = false
  }
}

const getStatusText = (status: string | undefined) => {
  switch (status) {
    case 'active':
      return t('tenant.statusActive')
    case 'inactive':
      return t('tenant.statusInactive')
    case 'suspended':
      return t('tenant.statusSuspended')
    default:
      return t('tenant.statusUnknown')
  }
}

const getStatusTheme = (status: string | undefined) => {
  switch (status) {
    case 'active':
      return 'success'
    case 'inactive':
      return 'warning'
    case 'suspended':
      return 'danger'
    default:
      return 'default'
  }
}

const formatDate = (dateStr: string | undefined) => {
  if (!dateStr) return t('tenant.unknown')

  try {
    const date = new Date(dateStr)
    const formatter = new Intl.DateTimeFormat(locale.value || 'zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
    return formatter.format(date)
  } catch {
    return t('tenant.formatError')
  }
}

const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'

  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))

  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const getUsagePercentage = () => {
  if (!tenantInfo.value?.storage_quota || tenantInfo.value.storage_quota === 0) {
    return 0
  }

  const used = tenantInfo.value.storage_used || 0
  const percentage = (used / tenantInfo.value.storage_quota) * 100
  return Math.min(Math.round(percentage * 100) / 100, 100)
}

// Lifecycle
onMounted(() => {
  loadInfo()
})
</script>

<style lang="less" scoped>
.tenant-info {
  width: 100%;
}

.section-header {
  margin-bottom: 32px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 8px 0;
  }

  .section-description {
    font-size: 14px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.loading-inline {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 40px 0;
  justify-content: center;
  color: var(--td-text-color-secondary);
  font-size: 14px;
}

.error-inline {
  padding: 20px 0;
}

.tenant-info-body {
  display: flex;
  flex-direction: column;
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
  padding: 20px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }
}

.setting-info {
  /* 不再 flex:1：标签列固定到 max-content 的合理范围内（CJK label 一般 4~6 字，
     再加 desc 文案撑宽），不参与剩余空间分配，避免被长内容挤到单字纵向换行。
     min-width 兜底，desc 字数稍多时也不会被压缩到一字一行。 */
  flex: 0 0 auto;
  width: max-content;
  min-width: 140px;
  max-width: 40%;
  padding-right: 24px;

  label {
    font-size: 15px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    display: block;
    margin-bottom: 4px;
  }

  .desc {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.setting-control {
  /* 反过来：内容列吃掉剩余空间，并允许收缩 + 内部换行，长字符串不会再撑爆行。
     去掉原先的 min-width:280px 硬约束（短内容也不需要那么宽的展示槽）。 */
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;

  .info-value {
    font-size: 14px;
    color: var(--td-text-color-primary);
    text-align: right;
    /* anywhere 比 break-word 激进：连无空格的长串（"WorkspaceDefault..." 这种）
       也能强制断行，避免单条内容把整行撑出。 */
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .edit-btn {
    flex-shrink: 0;
  }
}

.inline-edit {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  justify-content: flex-end;
}

.inline-edit-input {
  /* 行内编辑场景下输入框不能撑满整行，否则右侧两个按钮会贴边；
     给一个合理上限即可，超出走 t-input 自己的省略。 */
  max-width: 220px;
  flex: 1;
}

/* 描述行的原地编辑：textarea 自身可换行展开，按钮换到下方右对齐，
   避免名称行那样横向把按钮挤窄。 */
.inline-edit-description {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  width: 100%;
  max-width: 360px;
}

.inline-edit-textarea {
  width: 100%;
}

.inline-edit-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

/* 只读态的描述：多行可换行；空描述用占位色提示用户可点编辑写入。 */
.description-value {
  white-space: pre-wrap;
  word-break: break-word;

  &.is-empty {
    color: var(--td-text-color-placeholder);
  }
}

.usage-control {
  //   width: 100%;
  //   display: flex;
  //   align-items: center;
  //   gap: 12px;

  .usage-text {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    min-width: 50px;
    text-align: right;
  }
}

</style>
