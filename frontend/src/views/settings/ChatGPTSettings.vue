<template>
  <section class="chatgpt-settings">
    <header class="chatgpt-settings__header">
      <div><h3>{{ t('chatgpt.title') }}</h3><p>{{ t('chatgpt.description') }}</p></div>
      <t-button variant="text" size="small" :loading="loading" :disabled="loading" @click="load">{{ t('common.refresh') }}</t-button>
    </header>
    <t-loading :loading="loading" size="small">
      <p v-if="error" class="chatgpt-settings__error" role="alert">{{ error }}</p>
      <p v-if="loaded && !encryptionReady" class="chatgpt-settings__error" role="alert">{{ t('chatgpt.encryptionRequired') }}</p>
      <p class="chatgpt-settings__hint">{{ t('chatgpt.browserHint') }}</p>
      <button class="chatgpt-signin" type="button" :disabled="!loaded || !encryptionReady || signingIn" :aria-busy="signingIn" @click="signIn()">
        <img :src="chatGPTLogo" alt="" width="20" height="20" />Continue with ChatGPT
      </button>
      <p v-if="loginPending" class="chatgpt-settings__hint" role="status">{{ t('chatgpt.awaitingLogin') }}</p>
      <a v-if="authorizationURL" :href="authorizationURL" target="_blank" rel="noopener noreferrer">{{ t('chatgpt.openAuthorization') }}</a>
      <p v-if="accounts.length === 0 && loaded" class="chatgpt-settings__hint">{{ t('chatgpt.noAccounts') }}</p>
      <ul v-else class="chatgpt-accounts">
        <li v-for="account in accounts" :key="account.id">
          <div class="chatgpt-account__info"><strong>{{ chatGPTAccountLabel(account) }}</strong><span>{{ accountStatus(account) }}</span><p v-if="account.last_error" class="chatgpt-settings__error">{{ accountError(account.last_error) }}</p></div>
          <div class="chatgpt-account__actions">
            <button class="chatgpt-signin" type="button" :disabled="!encryptionReady || signingIn" @click="signIn(account.id)"><img :src="chatGPTLogo" alt="" width="20" height="20" />Continue with ChatGPT</button>
            <t-popconfirm :content="t('chatgpt.logoutConfirm', { account: chatGPTAccountLabel(account) })" :confirm-btn="{ content: t('chatgpt.logout'), theme: 'danger' }" :cancel-btn="{ content: t('common.cancel') }" @confirm="signOut(account.id)">
              <t-button theme="danger" variant="text" size="small" :loading="logoutID === account.id" :disabled="Boolean(logoutID)">{{ t('chatgpt.logout') }}</t-button>
            </t-popconfirm>
          </div>
        </li>
      </ul>
      <p v-if="revocationWarning" class="chatgpt-settings__error" role="alert">{{ t('chatgpt.revocationUnconfirmed') }}</p>
      <ChatGPTUsageStatus v-if="accounts.length || policy?.paused || (policy?.daily_used || 0) > 0" :policy="policy" :accounts="accounts" :busy="resuming" :error="resumeError" @resume="resume" />
      <div v-if="policy" class="chatgpt-policy">
        <div class="chatgpt-policy__row">
          <label for="chatgpt-daily-limit">{{ t('chatgpt.dailyLimit') }}</label>
          <div><t-input-number id="chatgpt-daily-limit" class="chatgpt-daily-limit" v-model="dailyLimit" :min="1" :decimal-places="0" :placeholder="t('chatgpt.unlimited')" :aria-label="t('chatgpt.dailyLimit')" /><p class="chatgpt-settings__hint">{{ t('chatgpt.dailyLimitHint') }}</p></div>
        </div>
        <div class="chatgpt-policy__row">
          <label for="chatgpt-fallback-model">{{ t('chatgpt.fallbackModel') }}</label>
          <div><t-select id="chatgpt-fallback-model" v-model="fallbackID" clearable :placeholder="t('chatgpt.noFallback')" :aria-label="t('chatgpt.fallbackModel')">
            <t-option v-for="model in fallbackModels" :key="model.id" :value="model.id" :label="model.display_name || model.name" />
          </t-select><p class="chatgpt-settings__hint">{{ t('chatgpt.fallbackHint') }}</p></div>
        </div>
        <p v-if="saveError" class="chatgpt-settings__error" role="alert">{{ saveError }}</p>
        <t-button :loading="saving" :disabled="saving" @click="save">{{ t('chatgpt.savePolicy') }}</t-button>
      </div>
    </t-loading>
    <t-dialog :visible="Boolean(welcomeAccount)" :header="t('chatgpt.welcomeTitle')" :confirm-btn="{ content: t('chatgpt.gotIt'), loading: acknowledging }" :cancel-btn="null" :close-btn="false" :close-on-overlay-click="false" :close-on-esc-keydown="false" @confirm="acknowledgeWelcome">
      <img :src="chatGPTLogo" alt="" width="40" height="40" class="chatgpt-welcome-logo" />
      <p>{{ t('chatgpt.welcomeDescription') }}</p>
      <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{{ t('chatgpt.manageUsage') }}</a>
      <p v-if="welcomeError" class="chatgpt-settings__error" role="alert">{{ welcomeError }}</p>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { getChatGPTAccounts, getChatGPTPolicy, loginChatGPT, logoutChatGPT, saveChatGPTPolicy, resumeChatGPT, acknowledgeChatGPTWelcome, chatGPTAccountLabel, type ChatGPTAccount, type ChatGPTPolicy } from '@/api/chatgpt'
import { type ModelConfig } from '@/api/model'
import ChatGPTUsageStatus from '@/components/ChatGPTUsageStatus.vue'
import chatGPTLogo from '@/assets/img/chatgpt-logo-white.svg'
const props = withDefaults(defineProps<{ models?: ModelConfig[] }>(), { models: () => [] })
const { t } = useI18n()
const accounts = ref<ChatGPTAccount[]>([]), policy = ref<ChatGPTPolicy | null>(null)
const loading = ref(false), loaded = ref(false), encryptionReady = ref(false), error = ref('')
const revocationWarning = ref(false)
const signingIn = ref(false), logoutID = ref(''), dailyLimit = ref<number | undefined>(), fallbackID = ref('')
const saving = ref(false), saveError = ref(''), resuming = ref(false), resumeError = ref('')
const welcomeAccount = ref<ChatGPTAccount | null>(null), acknowledging = ref(false), welcomeError = ref('')
const loginPending = ref(false), authorizationURL = ref('')
let requestVersion = 0, disposed = false, loginPoll: ReturnType<typeof setInterval> | null = null
let pollingAccounts = false
const fallbackModels = computed(() => props.models.filter(model => model.type === 'KnowledgeQA' && model.source === 'local' && model.status === 'active'))
const accountStatus = (account: ChatGPTAccount) => t(account.status === 'connected' ? 'chatgpt.connected' : account.status === 'needs_login' || account.status === 'reauth_required' ? 'chatgpt.needsLogin' : 'chatgpt.signedOut')
function accountError(code: string) {
  if (code === 'revocation_unconfirmed') return t('chatgpt.revocationUnconfirmed')
  if (['invalid_grant', 'invalid_refresh_token', 'refresh_token_reused', 'missing_refresh_token'].includes(code)) return t('chatgpt.refreshInvalid')
  if (['temporarily_unavailable', 'refresh_not_yet_available'].includes(code)) return t('chatgpt.refreshUnavailable')
  return t('chatgpt.accountError', { code })
}
async function load() {
  if (loading.value) return
  const version = ++requestVersion; loading.value = true
  try {
    const [result, nextPolicy] = await Promise.all([getChatGPTAccounts(), getChatGPTPolicy()])
    if (disposed || version !== requestVersion) return
    accounts.value = result.accounts; encryptionReady.value = result.encryption_ready
    policy.value = nextPolicy; dailyLimit.value = nextPolicy.daily_limit ?? undefined; fallbackID.value = nextPolicy.fallback_model_id || ''
    welcomeAccount.value = result.accounts.find(account => account.status === 'connected' && account.welcome_acknowledged === false) || null
    loaded.value = true; error.value = ''
  } catch (err: any) { if (!disposed && version === requestVersion) error.value = err?.message || t('chatgpt.loadFailed') }
  finally { if (!disposed && version === requestVersion) loading.value = false }
}
async function signIn(accountID?: string) {
  if (signingIn.value || !encryptionReady.value) return
  const tab = window.open('about:blank', '_blank')
  if (tab) tab.opener = null
  signingIn.value = true; error.value = ''
  try {
    const result = await loginChatGPT(accountID)
    if (!disposed) {
      authorizationURL.value = result.authorization_url
      if (tab) tab.location.replace(result.authorization_url)
      loginPending.value = true
      if (loginPoll) clearInterval(loginPoll)
      const expires = Date.parse(result.expires_at)
      loginPoll = setInterval(() => pollLogin(expires), 2000)
    } else tab?.close()
  } catch (err: any) { tab?.close(); error.value = err?.message || t('chatgpt.loginFailed') }
  finally { signingIn.value = false }
}
async function pollLogin(expires: number) {
  if (pollingAccounts || disposed) return
  if (Date.now() >= expires) { stopLoginPoll(); error.value = t('chatgpt.loginExpired'); return }
  pollingAccounts = true
  try {
    const result = await getChatGPTAccounts()
    if (disposed) return
    const connected = result.accounts.find(account => account.status === 'connected' && !accounts.value.some(previous => previous.id === account.id && previous.status === 'connected' && previous.expires_at === account.expires_at))
    accounts.value = result.accounts
    if (connected) {
      stopLoginPoll()
      if (connected.welcome_acknowledged === false) welcomeAccount.value = connected
    }
  } catch (err: any) { if (!disposed) error.value = err?.message || t('chatgpt.loadFailed') }
  finally { pollingAccounts = false }
}
function stopLoginPoll() { if (loginPoll) clearInterval(loginPoll); loginPoll = null; loginPending.value = false; authorizationURL.value = '' }
async function signOut(accountID: string) {
  if (logoutID.value) return
  logoutID.value = accountID; error.value = ''; revocationWarning.value = false
  try {
    const result = await logoutChatGPT(accountID)
    await load()
    revocationWarning.value = result.remote_revocation_confirmed === false && !accounts.value.some(account => account.id === accountID && account.last_error === 'revocation_unconfirmed')
  }
  catch (err: any) { error.value = err?.message || t('chatgpt.logoutFailed') }
  finally { logoutID.value = '' }
}
async function save() {
  if (saving.value) return
  const value = dailyLimit.value
  if (value !== undefined && value !== null && (!Number.isInteger(value) || value < 1)) { saveError.value = t('chatgpt.invalidLimit'); return }
  saving.value = true; saveError.value = ''
  try {
    policy.value = await saveChatGPTPolicy({ daily_limit: value == null ? null : value, fallback_model_id: fallbackID.value || '' })
    MessagePlugin.success(t('chatgpt.policySaved'))
  } catch (err: any) { saveError.value = err?.message || t('chatgpt.saveFailed') }
  finally { saving.value = false }
}
async function resume() {
  if (resuming.value) return
  resuming.value = true; resumeError.value = ''
  try { policy.value = await resumeChatGPT() }
  catch (err: any) { resumeError.value = err?.message || t('chatgpt.resumeFailed') }
  finally { resuming.value = false }
}
async function acknowledgeWelcome() {
  if (acknowledging.value || !welcomeAccount.value) return
  acknowledging.value = true; welcomeError.value = ''
  try { await acknowledgeChatGPTWelcome(welcomeAccount.value.id); welcomeAccount.value = null }
  catch (err: any) { welcomeError.value = err?.message || t('chatgpt.saveFailed') }
  finally { acknowledging.value = false }
}
onMounted(load)
onUnmounted(() => { disposed = true; requestVersion++; stopLoginPoll() })
</script>
<style scoped lang="less">
.chatgpt-settings { margin: 20px 0; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-default); }
.chatgpt-settings__header { display: flex; align-items: start; gap: 12px; justify-content: space-between; }
.chatgpt-settings h3 { margin: 0; font-size: 16px; }
.chatgpt-settings p { line-height: 1.6; font-size: 13px; }
.chatgpt-settings__header p { color: var(--td-text-color-secondary); }
.chatgpt-settings__hint { margin: 6px 0; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.chatgpt-settings__error { color: var(--td-error-color); overflow-wrap: anywhere; }
.chatgpt-signin { display: inline-flex; align-items: center; justify-content: center; gap: 10px; min-height: 40px; padding: 8px 16px; border: 1px solid #000; border-radius: 8px; background: #000; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; }
.chatgpt-welcome-logo { background: #000; border-radius: 8px; padding: 6px; }
.chatgpt-signin:hover:not(:disabled) { background: #222; }
.chatgpt-signin:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
.chatgpt-signin:disabled { cursor: not-allowed; opacity: .5; }
.chatgpt-accounts { list-style: none; padding: 0; }
.chatgpt-accounts li { display: flex; flex-wrap: wrap; gap: 12px; justify-content: space-between; align-items: center; padding: 12px 0; border-top: 1px solid var(--td-component-stroke); }
.chatgpt-account__info { display: flex; flex-direction: column; gap: 5px; min-width: 0; overflow-wrap: anywhere; }
.chatgpt-account__info span { font-size: 13px; color: var(--td-text-color-secondary); }
.chatgpt-account__actions { display: flex; flex-wrap: wrap; gap: 8px; }
.chatgpt-policy { display: flex; flex-direction: column; gap: 16px; margin-top: 20px; }
.chatgpt-policy__row { display: grid; grid-template-columns: minmax(120px, 1fr) minmax(0, 2fr); gap: 16px; align-items: start; }
.chatgpt-policy__row label { font-size: 13px; padding-top: 6px; }
.chatgpt-daily-limit { width: 200px; max-width: 100%; }
@container settings-content (max-width: 720px) { .chatgpt-policy__row { grid-template-columns: minmax(0, 1fr); gap: 6px; } }
</style>
