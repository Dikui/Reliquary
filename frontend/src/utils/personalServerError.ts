type Translate = (key: string) => string

export function translatePersonalServerMessage(message: string | undefined, t: Translate): string | undefined {
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
