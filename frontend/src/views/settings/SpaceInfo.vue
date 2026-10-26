<template>
  <div class="space-info">
    <div class="section-header">
      <h2>{{ $t('space.title') }}</h2>
      <p class="section-description">{{ $t('space.sectionDescription') }}</p>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="loading-inline">
      <t-loading size="small" />
      <span>{{ $t('space.loadingInfo') }}</span>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="error-inline">
      <t-alert theme="error" :message="error">
        <template #operation>
          <t-button size="small" @click="loadInfo">{{ $t('space.retry') }}</t-button>
        </template>
      </t-alert>
    </div>

    <!-- 空间信息列表 -->
    <div v-else class="space-info-body">
      <div class="settings-group">
        <!-- Space ID -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.idLabel') }}</label>
            <p class="desc">{{ $t('space.details.idDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ spaceInfo?.id || '-' }}</span>
          </div>
        </div>

        <!-- Space name -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.nameLabel') }}</label>
            <p class="desc">{{ $t('space.details.nameDescription') }}</p>
          </div>
          <div class="setting-control">
            <!-- 只读态：显示名称 + 编辑按钮。
               原地编辑取代弹窗：少一层视觉打断，与其它行的展示节奏一致。 -->
            <template v-if="!editing">
              <span class="info-value">{{ displayName || '-' }}</span>
              <t-button theme="default" variant="text" shape="square" size="small"
                class="edit-btn" :title="$t('space.details.editName')" :aria-label="$t('space.details.editName')"
                @click="startEditName">
                <template #icon>
                  <t-icon name="edit" />
                </template>
              </t-button>
            </template>
            <!-- 编辑态：输入框 + 保存/取消。回车保存，Esc 取消。 -->
            <div v-else class="inline-edit">
              <t-input v-model="editName" :placeholder="$t('space.details.editNamePlaceholder')" :maxlength="64"
                :disabled="saving" autofocus class="inline-edit-input" @enter="saveSpaceName"
                @keydown="onEditKeydown" />
              <t-button theme="primary" size="small" :loading="saving" :disabled="!canSubmit" @click="saveSpaceName">
                {{ $t('space.details.editNameConfirm') }}
              </t-button>
              <t-button theme="default" variant="outline" size="small" :disabled="saving" @click="cancelEditName">
                {{ $t('space.details.editNameCancel') }}
              </t-button>
            </div>
          </div>
        </div>

        <!-- Space description -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.descriptionLabel') }}</label>
            <p class="desc">{{ $t('space.details.descriptionDescription') }}</p>
          </div>
          <div class="setting-control">
            <!-- 只读态：显示描述（空时给占位）+ 编辑按钮。
               与名称同款"原地编辑"模式，少一层弹窗打断。 -->
            <template v-if="!editingDescription">
              <span class="info-value description-value" :class="{ 'is-empty': !spaceInfo?.description }">
                {{ displayDescription || $t('space.details.descriptionEmptyPlaceholder') }}
              </span>
              <t-button theme="default" variant="text" shape="square" size="small"
                class="edit-btn" :title="$t('space.details.editDescription')"
                :aria-label="$t('space.details.editDescription')" @click="startEditDescription">
                <template #icon>
                  <t-icon name="edit" />
                </template>
              </t-button>
            </template>
            <!-- 编辑态：textarea + 保存/取消。Esc 取消、Ctrl/⌘+Enter 保存；
               textarea 上 Enter 默认换行更顺手，不接管 Enter 提交。 -->
            <div v-else class="inline-edit inline-edit-description">
              <SafeTextarea v-model="editDescription"
                :placeholder="$t('space.details.editDescriptionPlaceholder')" :maxlength="512"
                :autosize="{ minRows: 2, maxRows: 6 }" :disabled="savingDescription" autofocus
                class="inline-edit-textarea" @keydown="onEditDescriptionKeydown" />
              <div class="inline-edit-actions">
                <t-button theme="primary" size="small" :loading="savingDescription"
                  :disabled="!canSubmitDescription" @click="saveSpaceDescription">
                  {{ $t('space.details.editNameConfirm') }}
                </t-button>
                <t-button theme="default" variant="outline" size="small" :disabled="savingDescription"
                  @click="cancelEditDescription">
                  {{ $t('space.details.editNameCancel') }}
                </t-button>
              </div>
            </div>
          </div>
        </div>

        <!-- Space business -->
        <div v-if="spaceInfo?.business" class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.businessLabel') }}</label>
            <p class="desc">{{ $t('space.details.businessDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ spaceInfo.business }}</span>
          </div>
        </div>

        <!-- Space status -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.statusLabel') }}</label>
            <p class="desc">{{ $t('space.details.statusDescription') }}</p>
          </div>
          <div class="setting-control">
            <t-tag :theme="getStatusTheme(spaceInfo?.status)" variant="light" size="small">
              {{ getStatusText(spaceInfo?.status) }}
            </t-tag>
          </div>
        </div>

        <!-- Space creation time -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.createdAtLabel') }}</label>
            <p class="desc">{{ $t('space.details.createdAtDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ formatDate(spaceInfo?.created_at) }}</span>
          </div>
        </div>

        <!-- Used storage -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.storage.usedLabel') }}</label>
            <p class="desc">{{ $t('space.storage.usedDescription') }}</p>
          </div>
          <div class="setting-control">
            <span class="info-value">{{ formatBytes(spaceInfo?.storage_used || 0) }}</span>
          </div>
        </div>

      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'
import { ref, computed, onMounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { getSpaceInfo, updateSpace as updateSpaceApi, type SpaceInfo } from '@/api/space'
import { useSpaceStore } from '@/stores/space'
import { useI18n } from 'vue-i18n'
import { workspaceName, workspaceDescription } from '@/utils/workspaceCopy'

defineExpose({ confirmLeave: () => Promise.resolve(true) })
const { t, locale } = useI18n()
const spaceStore = useSpaceStore()

// Reactive state
const spaceInfo = ref<SpaceInfo | null>(null)
const displayName = computed(() => workspaceName(spaceInfo.value?.name, t))
const displayDescription = computed(() => workspaceDescription(spaceInfo.value?.description, t))
const loading = ref(true)
const error = ref('')

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
  () => !saving.value && !!editNameTrimmed.value && editNameTrimmed.value !== displayName.value,
)

const startEditName = () => {
  editName.value = displayName.value
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
  () => !savingDescription.value && editDescriptionTrimmed.value !== displayDescription.value,
)

const startEditDescription = () => {
  editDescription.value = displayDescription.value
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
    void saveSpaceDescription()
  }
}

const saveSpaceDescription = async () => {
  if (!spaceInfo.value?.id) return
  const newDesc = editDescriptionTrimmed.value
  if (newDesc === displayDescription.value) {
    editingDescription.value = false
    return
  }

  try {
    savingDescription.value = true
    const resp = await updateSpaceApi({ description: newDesc })
    if (resp.success) {
      // 本地立即回显。
      if (spaceInfo.value) {
        spaceInfo.value = { ...spaceInfo.value, description: newDesc }
      }
      MessagePlugin.success(t('space.details.editDescriptionSuccess'))
      editingDescription.value = false
    } else {
      MessagePlugin.error(resp.message || t('space.details.editDescriptionFailed'))
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('space.details.editDescriptionFailed'))
  } finally {
    savingDescription.value = false
  }
}

const saveSpaceName = async () => {
  const newName = editNameTrimmed.value
  if (!newName) {
    MessagePlugin.warning(t('space.details.editNameRequired'))
    return
  }
  if (!spaceInfo.value?.id) return
  if (newName === displayName.value) {
    editing.value = false
    return
  }

  try {
    saving.value = true
    const resp = await updateSpaceApi({ name: newName })
    if (resp.success) {
      // 本地立即回显，并同步工作区名称。
      if (spaceInfo.value) {
        spaceInfo.value = { ...spaceInfo.value, name: newName }
      }
      if (spaceStore.space && String(spaceStore.space.id) === String(spaceInfo.value?.id)) {
        spaceStore.setSpace({ ...spaceStore.space, name: newName })
      }

      MessagePlugin.success(t('space.details.editNameSuccess'))
      editing.value = false
    } else {
      MessagePlugin.error(resp.message || t('space.details.editNameFailed'))
    }
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('space.details.editNameFailed'))
  } finally {
    saving.value = false
  }
}

// Methods
const loadInfo = async () => {
  try {
    loading.value = true
    error.value = ''

    const spaceResponse = await getSpaceInfo()

    if (spaceResponse.success && spaceResponse.data) {
      spaceInfo.value = spaceResponse.data
    } else {
      error.value = spaceResponse.message || t('space.messages.fetchFailed')
    }
  } catch (err: any) {
    error.value = err?.message || t('space.messages.networkError')
  } finally {
    loading.value = false
  }
}

const getStatusText = (status: string | undefined) => {
  switch (status) {
    case 'active':
      return t('space.statusActive')
    case 'inactive':
      return t('space.statusInactive')
    case 'suspended':
      return t('space.statusSuspended')
    default:
      return t('space.statusUnknown')
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
  if (!dateStr) return t('space.unknown')

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
    return t('space.formatError')
  }
}

const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'

  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))

  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

// Lifecycle
onMounted(() => {
  loadInfo()
})
</script>

<style lang="less" scoped>
@import '../../assets/settings-layout.less';

.space-info {
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

.space-info-body {
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

.settings-responsive-rows();

@container settings-content (max-width: @settings-narrow-width) {
  .setting-control .info-value { text-align: left; }
  .inline-edit, .inline-edit-actions { justify-content: flex-start; flex-wrap: wrap; }
  .inline-edit-input { min-width: 0; max-width: 100%; }
  .inline-edit-description { max-width: 100%; }
}

</style>
