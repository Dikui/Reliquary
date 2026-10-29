<template>
  <div class="space-info settings-form">
    <div class="section-header">
      <p class="section-description">{{ $t('space.sectionDescription') }}</p>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="loading-inline">
      <span class="rq-spinner" aria-hidden="true" />
      <span>{{ $t('space.loadingInfo') }}</span>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="error-inline">
      <div class="settings-error" role="alert">{{ error }}
        <div class="settings-actions">
          <Button @click="loadInfo">{{ $t('space.retry') }}</Button>
        </div>
      </div>
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
            <span class="info-value" :title="String(spaceInfo?.id ?? '')">{{ spaceInfo?.id || '-' }}</span>
          </div>
        </div>

        <!-- Space name -->
        <div class="setting-row">
          <div class="setting-info">
            <label>{{ $t('space.details.nameLabel') }}</label>
            <p class="desc">{{ $t('space.details.nameDescription') }}</p>
          </div>
          <div class="setting-control">
            <!-- 只读态：显示名称 + 编辑按钮（登录后可编辑）。
               原地编辑取代弹窗：少一层视觉打断，与其它行的展示节奏一致。 -->
            <template v-if="!editing">
              <span class="info-value">{{ displayName || '-' }}</span>
              <Button variant="ghost"
                class="edit-btn" :title="$t('space.details.editName')" :aria-label="$t('space.details.editName')"
                @click="startEditName">{{ $t('space.details.editName') }}

              </Button>
            </template>
            <!-- 编辑态：输入框 + 保存/取消。回车保存，Esc 取消。 -->
            <div v-else class="inline-edit">
              <Input v-model="editName" :aria-label="$t('space.details.nameLabel')" :placeholder="$t('space.details.editNamePlaceholder')" :maxlength="64"
                :disabled="saving" autofocus class="inline-edit-input" @keydown.enter="!$event.isComposing && saveSpaceName()"
                @keydown="onEditKeydown" />
              <Button :loading="saving" :disabled="!canSubmit" @click="saveSpaceName">
                {{ $t('space.details.editNameConfirm') }}
              </Button>
              <Button variant="default" :disabled="saving" @click="cancelEditName">
                {{ $t('space.details.editNameCancel') }}
              </Button>
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
            <!-- 只读态：显示描述（空时给占位）+ 编辑按钮（登录后可编辑）。
               与名称同款"原地编辑"模式，少一层弹窗打断。 -->
            <template v-if="!editingDescription">
              <span class="info-value description-value" :class="{ 'is-empty': !spaceInfo?.description }">
                {{ displayDescription || $t('space.details.descriptionEmptyPlaceholder') }}
              </span>
              <Button variant="ghost"
                class="edit-btn" :title="$t('space.details.editDescription')"
                :aria-label="$t('space.details.editDescription')" @click="startEditDescription">{{ $t('space.details.editDescription') }}

              </Button>
            </template>
            <!-- 编辑态：textarea + 保存/取消。Esc 取消、Ctrl/⌘+Enter 保存；
               textarea 上 Enter 默认换行更顺手，不接管 Enter 提交。 -->
            <div v-else class="inline-edit inline-edit-description">
              <Textarea v-model="editDescription" :aria-label="$t('space.details.descriptionLabel')"
                :placeholder="$t('space.details.editDescriptionPlaceholder')" :maxlength="512"
                rows="4" :disabled="savingDescription" autofocus
                class="inline-edit-textarea" @keydown="onEditDescriptionKeydown" />
              <div class="inline-edit-actions">
                <Button :loading="savingDescription"
                  :disabled="!canSubmitDescription" @click="saveSpaceDescription">
                  {{ $t('space.details.editNameConfirm') }}
                </Button>
                <Button variant="default" :disabled="savingDescription"
                  @click="cancelEditDescription">
                  {{ $t('space.details.editNameCancel') }}
                </Button>
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
            <Tag :variant="spaceInfo?.status === 'active' ? 'success' : 'plain'">
              {{ getStatusText(spaceInfo?.status) }}
            </Tag>
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
import { Button, Input, Tag, Textarea, toast } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, computed, onMounted } from 'vue'
import { getSpaceInfo, updateSpace as updateSpaceApi, type SpaceInfo } from '@/api/space'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import { workspaceName, workspaceDescription } from '@/utils/workspaceCopy'

defineExpose({ confirmLeave: () => Promise.resolve(true) })
const { t, locale } = useI18n()
const authStore = useAuthStore()

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
      // 本地立即回显，避免等 /auth/me 往返。
      if (spaceInfo.value) {
        spaceInfo.value = { ...spaceInfo.value, description: newDesc }
      }
      toast.success(t('space.details.editDescriptionSuccess'))
      editingDescription.value = false
    } else {
      toast.error(resp.message || t('space.details.editDescriptionFailed'))
    }
  } catch (err: any) {
    toast.error(err?.message || t('space.details.editDescriptionFailed'))
  } finally {
    savingDescription.value = false
  }
}

const saveSpaceName = async () => {
  const newName = editNameTrimmed.value
  if (!newName) {
    toast.warning(t('space.details.editNameRequired'))
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
      // 本地立即回显，并同步登录态中的空间名称。
      if (spaceInfo.value) {
        spaceInfo.value = { ...spaceInfo.value, name: newName }
      }
      if (authStore.space && String(authStore.space.id) === String(spaceInfo.value?.id)) {
        authStore.setSpace({ ...authStore.space, name: newName })
      }

      toast.success(t('space.details.editNameSuccess'))
      editing.value = false
    } else {
      toast.error(resp.message || t('space.details.editNameFailed'))
    }
  } catch (err: any) {
    toast.error(err?.message || t('space.details.editNameFailed'))
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
