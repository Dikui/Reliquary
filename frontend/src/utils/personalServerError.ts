type Translate = (key: string) => string

export function translatePersonalServerMessage(message: string | undefined, t: Translate, code?: number): string | undefined {
  const imageModelKeys: Record<number, string> = {
    2301: 'model.imageProcessingUnsupported',
    2302: 'model.imageProcessingNotFound',
    2303: 'model.imageProcessingUnavailable',
  }
  if (typeof code === 'number' && imageModelKeys[code]) return t(imageModelKeys[code])
  if (message === 'Registration is closed') return t('auth.registrationClosed')
  if (message === 'expires_at_unix must be in the future') return t('integrations.api.apiKeyExpiresAtInvalid')
  if (message === 'name is required') return t('integrations.api.apiKeyNameRequired')
  return message
}

export function apiKeyErrorMessage(error: { message?: string }, t: Translate, fallback: string): string {
  const translated = translatePersonalServerMessage(error?.message, t)
  const knownKeys = ['auth.registrationClosed', 'integrations.api.apiKeyExpiresAtInvalid', 'integrations.api.apiKeyNameRequired']
  return translated && (translated !== error?.message || knownKeys.some(key => translated === t(key))) ? translated : t(fallback)
}
