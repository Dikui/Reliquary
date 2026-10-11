import { get, post, put } from '@/utils/request'

export interface ChatGPTAccount {
  id: string
  email: string
  subject: string
  client_id: string
  status: string
  expires_at?: string
  scopes: string[]
  last_error?: string
  welcome_acknowledged?: boolean
}
export interface ChatGPTAccounts {
  accounts: ChatGPTAccount[]
  encryption_ready: boolean
  callback_url: string
}
export interface ChatGPTModel { slug: string; display_name: string }
export interface ChatGPTPolicy {
  daily_limit: number | null
  daily_used: number
  day: string
  paused: boolean
  reason: string
  retry_at?: string
  fallback_model_id: string
}
async function data<T>(request: Promise<any>): Promise<T> {
  const response = await request
  if (!response?.success || response.data === undefined) throw new Error(response?.message || response?.error?.message || '')
  return response.data as T
}
export const getChatGPTAccounts = () => data<ChatGPTAccounts>(get('/api/v1/chatgpt/accounts'))
export const loginChatGPT = (accountID?: string) => data<{ authorization_url: string; expires_at: string }>(post('/api/v1/chatgpt/login', accountID ? { account_id: accountID } : {}))
export const logoutChatGPT = (accountID: string) => data<{ remote_revocation_confirmed: boolean }>(post(`/api/v1/chatgpt/accounts/${encodeURIComponent(accountID)}/logout`, {}))
export const listChatGPTModels = (accountID: string) => data<ChatGPTModel[]>(get(`/api/v1/chatgpt/accounts/${encodeURIComponent(accountID)}/models`))
export const getChatGPTPolicy = () => data<ChatGPTPolicy>(get('/api/v1/chatgpt/policy'))
export const saveChatGPTPolicy = (policy: Pick<ChatGPTPolicy, 'daily_limit' | 'fallback_model_id'>) => data<ChatGPTPolicy>(put('/api/v1/chatgpt/policy', policy))
export const resumeChatGPT = () => data<ChatGPTPolicy>(post('/api/v1/chatgpt/resume', {}))
export const acknowledgeChatGPTWelcome = (accountID: string) => data<unknown>(post('/api/v1/chatgpt/welcome/ack', { account_id: accountID }))
export const chatGPTAccountLabel = (account: ChatGPTAccount) => `${account.email || account.subject} (${account.client_id.slice(-8)})`
