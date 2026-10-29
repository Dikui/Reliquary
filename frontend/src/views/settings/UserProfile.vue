<template>
  <div class="user-profile settings-form">
    <div class="section-header">
      <p class="section-description">{{ $t('userProfile.description') }}</p>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="loading-inline">
      <span class="rq-spinner" aria-hidden="true" />
      <span>{{ $t('space.loadingInfo') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="error-inline">
      <div class="settings-error" role="alert">{{ error }}
        <div class="settings-actions">
          <Button @click="loadInfo">{{ $t('space.retry') }}</Button>
        </div>
      </div>
    </div>

    <!-- Content -->
    <div v-else class="settings-group">
      <!-- 用户 ID -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('space.api.userIdLabel') }}</label>
          <p class="desc">{{ $t('space.api.userIdDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value" :title="userInfo?.id">{{ userInfo?.id?.slice(0, 8) || '-' }}</span>
        </div>
      </div>

      <!-- 用户名 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('space.api.usernameLabel') }}</label>
          <p class="desc">{{ $t('space.api.usernameDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ userInfo?.username || '-' }}</span>
        </div>
      </div>

      <!-- 邮箱 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('space.api.emailLabel') }}</label>
          <p class="desc">{{ $t('space.api.emailDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ userInfo?.email || '-' }}</span>
        </div>
      </div>

      <!-- 注册时间 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('space.api.createdAtLabel') }}</label>
          <p class="desc">{{ $t('space.api.createdAtDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ formatDate(userInfo?.created_at) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Button } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, onMounted } from 'vue'
import { getCurrentUser, type UserInfo } from '@/api/auth'
import { useI18n } from 'vue-i18n'

const { t, locale } = useI18n()

const userInfo = ref<UserInfo | null>(null)
const loading = ref(true)
const error = ref('')

const loadInfo = async () => {
  try {
    loading.value = true
    error.value = ''
    const resp = await getCurrentUser()
    if ((resp as any).success && resp.data) {
      userInfo.value = resp.data.user
    } else {
      error.value = resp.message || t('space.messages.fetchFailed')
    }
  } catch (err: any) {
    error.value = err?.message || t('space.messages.networkError')
  } finally {
    loading.value = false
  }
}

const formatDate = (dateStr: string | undefined) => {
  if (!dateStr) return t('space.unknown')
  try {
    const d = new Date(dateStr)
    const fmt = new Intl.DateTimeFormat(locale.value || 'zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
    return fmt.format(d)
  } catch {
    return t('space.formatError')
  }
}

onMounted(loadInfo)
</script>
