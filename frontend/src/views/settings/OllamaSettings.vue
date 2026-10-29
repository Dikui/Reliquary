<template>
  <div class="ollama-settings settings-form">
    <div class="section-header">
      <p class="section-description">{{ $t('ollamaSettings.description') }}</p>
    </div>

    <div class="settings-group">
      <!-- Ollama 服务状态 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('ollamaSettings.status.label') }}</label>
          <p class="desc">{{ $t('ollamaSettings.status.desc') }}</p>
        </div>
        <div class="setting-control">
          <div class="status-display">
            <Tag
              v-if="testing"
            >

              {{ $t('ollamaSettings.status.testing') }}
            </Tag>
            <Tag
              v-else-if="connectionStatus === true"
            >

              {{ $t('ollamaSettings.status.available') }}
            </Tag>
            <Tag
              v-else-if="connectionStatus === false"
              variant="danger"
            >

              {{ $t('ollamaSettings.status.unavailable') }}
            </Tag>
            <Tag
              v-else
            >

              {{ $t('ollamaSettings.status.untested') }}
            </Tag>
            <Button
              variant="ghost"
              :loading="testing"
              @click="testConnection"
            >

              {{ $t('ollamaSettings.status.retest') }}
            </Button>
          </div>
        </div>
      </div>

      <!-- Ollama 服务地址 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('ollamaSettings.address.label') }}</label>
          <p class="desc">{{ $t('ollamaSettings.address.desc') }}</p>
        </div>
        <div class="setting-control">
          <div class="url-control-group">
            <Input
              v-model="localBaseUrl" :aria-label="$t('ollamaSettings.address.label')"
              :placeholder="$t('ollamaSettings.address.placeholder')"
              disabled
              style="flex: 1;"
            />
          </div>
          <p
            v-if="connectionStatus === false"
            style="margin-top: 8px;"
           class="settings-error" role="alert">{{ $t('ollamaSettings.address.failed') }}</p>
        </div>
      </div>

    </div>

    <!-- 下载新模型 -->
    <div v-if="connectionStatus === true" class="model-category-section setting-row">
      <div class="category-header setting-info">
        <div class="header-info">
          <h3>{{ $t('ollamaSettings.download.title') }}</h3>
          <p>
            {{ $t('ollamaSettings.download.descPrefix') }}
            <a href="https://ollama.com/search" target="_blank" rel="noopener noreferrer" class="doc-link">
              {{ $t('ollamaSettings.download.browse') }}

            </a>
          </p>
        </div>
      </div>

      <div class="download-content setting-control">
        <div class="input-group">
          <Input
            v-model="downloadModelName" :disabled="downloading" :aria-label="$t('ollamaSettings.download.title')"
            :placeholder="$t('ollamaSettings.download.placeholder')"
            style="flex: 1;"
          />
          <Button
            variant="default"
            class="download-btn"
            :loading="downloading"
            :disabled="!downloadModelName.trim()"
            @click="downloadModel"
          >

            {{ $t('ollamaSettings.download.download') }}
          </Button>
        </div>

        <div v-if="downloadProgress > 0" class="download-progress">
          <div class="progress-info">
            <span>{{ $t('ollamaSettings.download.downloading', { name: downloadModelName }) }}</span>
            <span>{{ downloadProgress.toFixed(2) }}%</span>
          </div>
          <progress :value="downloadProgress" max="100" :aria-label="$t('ollamaSettings.download.title')" />
        </div>
      </div>
    </div>

    <!-- 已下载的模型 -->
    <div v-if="connectionStatus === true" class="model-category-section setting-row">
      <div class="category-header setting-info">
        <div class="header-info">
          <h3>{{ $t('ollamaSettings.installed.title') }}</h3>
          <p>{{ $t('ollamaSettings.installed.desc') }}</p>
        </div>
        <Button
          variant="ghost"
          :loading="loadingModels"
          @click="refreshModels"
        >

          {{ $t('common.refresh') }}
        </Button>
      </div>

      <div v-if="loadingModels" class="loading-state">
        <span class="rq-spinner" aria-hidden="true" />
        <span>{{ $t('common.loading') }}</span>
      </div>
      <div v-else-if="downloadedModels.length > 0" class="model-list-container">
        <div v-for="model in downloadedModels" :key="model.name" class="model-card">
          <div class="model-info">
            <div class="model-name">{{ model.name }}</div>
            <div class="model-meta">
              <span class="model-size">{{ formatSize(model.size) }}</span>
              <span class="model-modified">{{ formatDate(model.modified_at) }}</span>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="empty-state">
        <p class="empty-text">{{ $t('ollamaSettings.installed.empty') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Button, Input, Tag, toast } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { useI18n } from 'vue-i18n'
import { checkOllamaStatus, listOllamaModels, downloadOllamaModel, getDownloadProgress, type OllamaModelInfo } from '@/api/initialization'

const settingsStore = useSettingsStore()
const { t } = useI18n()

const localBaseUrl = ref(settingsStore.settings.ollamaConfig?.baseUrl ?? '')

const testing = ref(false)
const connectionStatus = ref<boolean | null>(null)
const loadingModels = ref(false)
const downloadedModels = ref<OllamaModelInfo[]>([])
const downloading = ref(false)
const downloadModelName = ref('')
const downloadProgress = ref(0)

let progressInterval: ReturnType<typeof setInterval> | undefined
onBeforeUnmount(() => clearInterval(progressInterval))

// 测试连接
const testConnection = async () => {
  testing.value = true
  connectionStatus.value = null

  try {
    // 保存配置
    settingsStore.updateOllamaConfig({ baseUrl: localBaseUrl.value })

    // 调用真实 Ollama API 测试连接
    const result = await checkOllamaStatus()

    // 如果接口返回了 baseUrl 且与当前输入框的值不同，更新为接口返回的值
    if (result.baseUrl && result.baseUrl !== localBaseUrl.value) {
      localBaseUrl.value = result.baseUrl
      settingsStore.updateOllamaConfig({ baseUrl: result.baseUrl })
    }

    connectionStatus.value = result.available

    if (connectionStatus.value) {
      toast.success(t('ollamaSettings.toasts.connected'))
      refreshModels()
    } else {
      toast.error(result.error || t('ollamaSettings.toasts.connectFailed'))
    }
  } catch (error: any) {
    connectionStatus.value = false
    toast.error(error.message || t('ollamaSettings.toasts.connectFailed'))
  } finally {
    testing.value = false
  }
}

// 刷新模型列表
const refreshModels = async () => {
  loadingModels.value = true

  try {
    // 调用真实 Ollama API 获取模型列表（现在返回完整的模型信息）
    const models = await listOllamaModels()
    downloadedModels.value = models
  } catch (error: any) {
    console.error('获取模型列表失败:', error)
    toast.error(error.message || t('ollamaSettings.toasts.listFailed'))
  } finally {
    loadingModels.value = false
  }
}

// 格式化文件大小
const formatSize = (bytes: number): string => {
  if (!bytes || bytes === 0 || isNaN(bytes)) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
}

// 格式化日期
const formatDate = (dateStr: string): string => {
  if (!dateStr) return t('ollama.unknown')

  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return t('ollama.unknown')

  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const days = Math.floor(diff / (1000 * 60 * 60 * 24))

  if (days === 0) return t('ollama.today')
  if (days === 1) return t('ollama.yesterday')
  if (days < 7) return t('ollama.daysAgo', { days }, days)
  return date.toLocaleDateString()
}

// 下载模型
const downloadModel = async () => {
  if (downloading.value || !downloadModelName.value.trim()) return

  downloading.value = true
  downloadProgress.value = 0

  try {
    // 调用真实 Ollama API 下载模型
    const result = await downloadOllamaModel(downloadModelName.value)

    if (result.status === 'failed') {
      toast.error(t('ollamaSettings.toasts.downloadFailed'))
      downloading.value = false
      downloadProgress.value = 0
      return
    }

    toast.success(t('ollamaSettings.toasts.downloadStarted', { name: downloadModelName.value }))

    // 查询下载进度
    const taskId = result.taskId
    progressInterval = setInterval(async () => {
      try {
        const task = await getDownloadProgress(taskId)
        downloadProgress.value = task.progress

        if (task.status === 'completed') {
          clearInterval(progressInterval)
          toast.success(t('ollamaSettings.toasts.downloadCompleted', { name: downloadModelName.value }))
          downloadModelName.value = ''
          downloadProgress.value = 0
          downloading.value = false
          refreshModels()
        } else if (task.status === 'failed') {
          clearInterval(progressInterval)
          toast.error(task.message || t('ollamaSettings.toasts.downloadFailed'))
          downloading.value = false
          downloadProgress.value = 0
        }
      } catch (error) {
        clearInterval(progressInterval)
        toast.error(t('ollamaSettings.toasts.progressFailed'))
        downloading.value = false
        downloadProgress.value = 0
      }
    }, 1000)
  } catch (error: any) {
    console.error('下载失败:', error)
    toast.error(error.message || t('ollamaSettings.toasts.downloadFailed'))
    downloading.value = false
    downloadProgress.value = 0
  }
}

// 初始化 Ollama 服务地址
const initOllamaBaseUrl = async () => {
  try {
    const result = await checkOllamaStatus()
    // 如果接口返回了 baseUrl，优先使用接口返回的值
    if (result.baseUrl) {
      localBaseUrl.value = result.baseUrl
      // 如果 store 中没有保存过，也保存到 store 中
      if (!settingsStore.settings.ollamaConfig?.baseUrl) {
        settingsStore.updateOllamaConfig({ baseUrl: result.baseUrl })
      }
    } else if (!localBaseUrl.value) {
      // 如果接口没返回且 store 中也没有，使用默认值
      localBaseUrl.value = 'http://localhost:11434'
    }

    // 直接使用初始化时获取的状态，避免重复调用
      connectionStatus.value = result.available
      if (result.available) {
        refreshModels()
    }

    return result
  } catch (error) {
    console.error('初始化 Ollama 地址失败:', error)
    // 如果获取失败，使用默认值或 store 中的值
    if (!localBaseUrl.value) {
      localBaseUrl.value = 'http://localhost:11434'
    }
    return null
  }
}

// 组件挂载时自动检查连接
onMounted(async () => {
  // 初始化服务地址，如果启用则直接使用返回的状态，避免重复调用
  await initOllamaBaseUrl()
})
</script>

<style scoped>
.status-display, .url-control-group, .input-group { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); width: 100%; }
.input-group .rq-control { flex: 1; min-width: 160px; }
.download-content, .download-progress { width: 100%; }
.progress-info { display: flex; gap: var(--space-2); justify-content: space-between; }
progress { width: 100%; accent-color: var(--text); }
.model-list-container { display: grid; gap: var(--space-2); min-width: 0; }
.model-card { padding: var(--space-3) 0; border-bottom: 1px solid var(--border); }
.model-name { font-family: var(--mono); overflow-wrap: anywhere; }
.model-meta { display: flex; flex-wrap: wrap; gap: var(--space-2); color: var(--muted); font-size: var(--type-note-size); }
.loading-state { display: flex; gap: var(--space-2); align-items: center; }
.category-header h3, .category-header p { margin-top: 0; }
</style>
