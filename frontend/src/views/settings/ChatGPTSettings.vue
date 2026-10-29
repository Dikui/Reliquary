<template>
  <section class="chatgpt-settings settings-form">
    <header class="chatgpt-settings__header">
      <div><h3>{{ t('chatgpt.title') }}</h3><p>{{ t('chatgpt.description') }}</p></div>
      <Button variant="ghost" :loading="loading" :disabled="loading" @click="load">{{ t('common.refresh') }}</Button>
    </header>
    <div :aria-busy="loading">
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

              <Button variant="danger" @click="confirmSignOut(account)" :loading="logoutID === account.id" :disabled="Boolean(logoutID)">{{ t('chatgpt.logout') }}</Button>

          </div>
        </li>
      </ul>
      <p v-if="revocationWarning" class="chatgpt-settings__error" role="alert">{{ t('chatgpt.revocationUnconfirmed') }}</p>
      <ChatGPTUsageStatus v-if="accounts.length || policy?.paused || (policy?.daily_used || 0) > 0" :policy="policy" :accounts="accounts" :busy="resuming" :error="resumeError" @resume="resume" />
      <div v-if="policy" class="chatgpt-policy">
        <div class="chatgpt-policy__row">
          <label for="chatgpt-daily-limit">{{ t('chatgpt.dailyLimit') }}</label>
          <div><Input type="number" class="chatgpt-daily-limit" id="chatgpt-daily-limit" :model-value="dailyLimit ?? ''" @update:model-value="dailyLimit = $event === '' ? undefined : Number($event)" :min="1" :placeholder="t('chatgpt.unlimited')" :aria-label="t('chatgpt.dailyLimit')" /><p class="chatgpt-settings__hint">{{ t('chatgpt.dailyLimitHint') }}</p></div>
        </div>
        <div class="chatgpt-policy__row">
          <label for="chatgpt-fallback-model">{{ t('chatgpt.fallbackModel') }}</label>
          <div><Select id="chatgpt-fallback-model" v-model="fallbackID" :placeholder="t('chatgpt.noFallback')" :aria-label="t('chatgpt.fallbackModel')">
            <option value="">{{ t('chatgpt.noFallback') }}</option><option v-for="model in fallbackModels" :key="model.id" :value="model.id" >{{ model.display_name || model.name }}</option>
          </Select><p class="chatgpt-settings__hint">{{ t('chatgpt.fallbackHint') }}</p></div>
        </div>
        <p v-if="saveError" class="chatgpt-settings__error" role="alert">{{ saveError }}</p>
        <Button :loading="saving" :disabled="saving" @click="save">{{ t('chatgpt.savePolicy') }}</Button>
      </div>
    </div>
    <Dialog :model-value="Boolean(welcomeAccount)" :title="t('chatgpt.welcomeTitle')" :closable="false" :close-on-overlay-click="false" :close-on-esc-keydown="false">
      <img :src="chatGPTLogo" alt="" width="40" height="40" class="chatgpt-welcome-logo" />
      <p>{{ t('chatgpt.welcomeDescription') }}</p>
      <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{{ t('chatgpt.manageUsage') }}</a>
      <p v-if="welcomeError" class="chatgpt-settings__error" role="alert">{{ welcomeError }}</p>
    <template #footer><Button variant="primary" :loading="acknowledging" @click="acknowledgeWelcome">{{ t('chatgpt.gotIt') }}</Button></template></Dialog>
  </section>
</template>
<script setup lang="ts">
import { Button, Input, Select, Dialog, toast, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getChatGPTAccounts, getChatGPTPolicy, loginChatGPT, logoutChatGPT, saveChatGPTPolicy, resumeChatGPT, acknowledgeChatGPTWelcome, chatGPTAccountLabel, type ChatGPTAccount, type ChatGPTPolicy } from '@/api/chatgpt'
import { type ModelConfig } from '@/api/model'
import ChatGPTUsageStatus from '@/components/ChatGPTUsageStatus.vue'
import chatGPTLogo from '@/assets/img/chatgpt-logo-white.svg'
const props = withDefaults(defineProps<{ models?: ModelConfig[] }>(), { models: () => [] })
const confirm = useConfirm()
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
function confirmSignOut(account: ChatGPTAccount) {
  const dialog = confirm({ header: t('chatgpt.logout'), body: t('chatgpt.logoutConfirm', { account: chatGPTAccountLabel(account) }), confirmBtn: t('chatgpt.logout'), theme: 'danger', onConfirm: async () => { await signOut(account.id); dialog.destroy() } })
}
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
    toast.success(t('chatgpt.policySaved'))
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

<style scoped>
 .chatgpt-settings { padding-top: var(--space-4); border-top: 2px solid var(--rule); margin: var(--space-6) 0 var(--space-4); }
.chatgpt-settings__header, .chatgpt-account__actions { display: flex; flex-wrap: wrap; gap: var(--space-3); justify-content: space-between; }
.chatgpt-settings__hint, .chatgpt-settings__header p { color: var(--muted); font-size: var(--type-note-size); }
.chatgpt-settings__error { color: var(--danger); overflow-wrap: anywhere; }
.chatgpt-signin { display: inline-flex; align-items: center; justify-content: center; gap: 10px; min-height: 40px; padding: 8px 16px; border: 1px solid #000; border-radius: 0; background: #000; color: #fff; font: inherit; font-size: var(--text-sm); font-weight: 600; cursor: pointer; }
.chatgpt-signin:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.chatgpt-signin:disabled { opacity: .5; cursor: not-allowed; }
.chatgpt-welcome-logo { background: #000; padding: 6px; }
.chatgpt-accounts { list-style: none; padding: 0; }
.chatgpt-accounts li { display: flex; flex-wrap: wrap; gap: var(--space-3); justify-content: space-between; border-top: 1px solid var(--border); padding: var(--space-3) 0; }
.chatgpt-account__info { display: grid; gap: var(--space-1); min-width: 0; overflow-wrap: anywhere; }
.chatgpt-account__info strong { font-weight: 600; }
.chatgpt-account__info > span { color: var(--muted); font-size: var(--type-note-size); }
.chatgpt-policy { display: grid; gap: var(--space-4); }
.chatgpt-policy__row { display: grid; grid-template-columns: 224px minmax(0, 1fr); gap: var(--space-6); border-top: 2px solid var(--rule); padding-top: var(--space-4); }
.chatgpt-policy__row > label { font-size: var(--type-label-size); font-weight: var(--type-label-weight); }
.chatgpt-daily-limit { width: 200px; max-width: 100%; }
@container settings-content (max-width: 720px) { .chatgpt-policy__row { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
@media (max-width: 640px) { .chatgpt-policy__row { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
</style>
