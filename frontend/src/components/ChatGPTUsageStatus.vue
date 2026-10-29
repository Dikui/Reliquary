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
      <Button v-if="policy?.paused" variant="default" :loading="busy" :disabled="busy" @click="$emit('resume')">{{ t('chatgpt.resume') }}</Button>
      <a v-if="reconnectAccounts.length && settingsLink" href="/platform/settings?section=models">{{ t('chatgpt.openSettings') }}</a>
    </div>
  </section>
</template>
<script setup lang="ts">
import { Button } from '@/components/ui'
import '@/styles/settings-form.css'
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

<style scoped>
 .chatgpt-usage { display: flex; flex-wrap: wrap; align-items: start; gap: var(--space-4); padding: var(--space-4); border-top: 2px solid var(--rule); background: var(--surface-soft); color: var(--text); }
.chatgpt-usage--paused { border-top-color: var(--danger); }
.chatgpt-usage__copy { flex: 1; min-width: min(100%, 220px); }
.chatgpt-usage p { margin: var(--space-2) 0 0; font-size: var(--text-sm); overflow-wrap: anywhere; }
.chatgpt-usage__actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); }
.chatgpt-usage a { color: var(--text); text-decoration: underline; }
.chatgpt-usage a:hover { color: var(--accent); }
.chatgpt-usage__error { color: var(--danger); }

</style>
