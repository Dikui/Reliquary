<template>
  <section v-if="policy || reconnectAccounts.length" class="chatgpt-usage" :class="{ 'chatgpt-usage--paused': policy?.paused || reconnectAccounts.length }" aria-live="polite">
    <div class="chatgpt-usage__copy">
      <strong>{{ t(policy?.paused ? 'chatgpt.pausedTitle' : reconnectAccounts.length ? 'chatgpt.reloginTitle' : accounts.some(account => account.status === 'connected') ? 'chatgpt.usingPlan' : 'chatgpt.usageTitle') }}</strong>
      <p v-if="policy?.paused">{{ pauseReason }}</p>
      <p v-if="policy?.paused && policy.retry_at">{{ t('chatgpt.retryAt', { time: formatTime(policy.retry_at) }) }}</p>
      <p v-else-if="policy?.paused">{{ t(policy.reason === 'daily_limit' || policy.reason === 'daily_call_limit' ? 'chatgpt.dailyRecovery' : 'chatgpt.manualRecovery') }}</p>
      <p v-if="policy">{{ t('chatgpt.dailyUsage', { count: policy.daily_used, limit: policy.daily_limit ?? t('chatgpt.unlimited'), day: policy.day }) }}</p>
      <p v-for="account in reconnectAccounts" :key="account.id">{{ t('chatgpt.reloginAccount', { account: chatGPTAccountLabel(account) }) }}</p>
      <p v-if="policy?.paused && policy.fallback_model_id">{{ t('chatgpt.fallbackActive') }}</p>
      <p v-if="error" role="alert" class="chatgpt-usage__error">{{ error }}</p>
    </div>
    <div class="chatgpt-usage__actions">
      <a class="chatgpt-usage__manage" href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{{ t('chatgpt.manageUsage') }}</a>
      <t-button v-if="policy?.paused" variant="outline" size="small" :loading="busy" :disabled="busy" @click="$emit('resume')">{{ t('chatgpt.resume') }}</t-button>
      <a v-if="reconnectAccounts.length && settingsLink" href="/platform/settings?section=models">{{ t('chatgpt.openSettings') }}</a>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { chatGPTAccountLabel, type ChatGPTAccount, type ChatGPTPolicy } from '@/api/chatgpt'
const props = withDefaults(defineProps<{ policy: ChatGPTPolicy | null; accounts?: ChatGPTAccount[]; busy?: boolean; error?: string; settingsLink?: boolean }>(), { accounts: () => [], busy: false, error: '', settingsLink: false })
defineEmits<{ resume: [] }>()
const { t, locale } = useI18n()
const reconnectAccounts = computed(() => props.accounts.filter(account => account.status === 'needs_login' || account.status === 'reauth_required'))
const pauseReason = computed(() => {
  const reason = props.policy?.reason || ''
  const key = reason === 'subscription_sharing_usage_unavailable' ? 'unavailable' : reason === 'subscription_sharing_usage_limit_exceeded' ? 'exceeded' : reason === 'daily_limit' || reason === 'daily_call_limit' ? 'daily' : 'unknown'
  return t(`chatgpt.pauseReasons.${key}`)
})
const formatTime = (value: string) => new Date(value).toLocaleString(locale.value)
</script>
<style scoped lang="less">
.chatgpt-usage { display: flex; flex-wrap: wrap; gap: 16px; align-items: start; padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-default); background: var(--td-bg-color-container); }
.chatgpt-usage--paused { border-color: var(--td-warning-color); background: var(--td-warning-color-light); }
.chatgpt-usage__copy { flex: 1; min-width: min(100%, 220px); }
.chatgpt-usage p { margin: 6px 0 0; font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.chatgpt-usage__actions { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
.chatgpt-usage a { color: var(--td-brand-color); text-decoration: none; cursor: pointer; }
.chatgpt-usage a:hover { text-decoration: underline; }
.chatgpt-usage a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
.chatgpt-usage .chatgpt-usage__manage { display: inline-flex; align-items: center; min-height: 32px; padding: 4px 12px; font-weight: 600; color: var(--td-text-color-anti); background: var(--td-brand-color); border-radius: var(--td-radius-default); }
.chatgpt-usage .chatgpt-usage__manage:hover { background: var(--td-brand-color-hover); text-decoration: none; }
.chatgpt-usage__error { color: var(--td-error-color); }
</style>
