type Translator = (key: string) => string

// Translate server defaults for display while retaining user-supplied values.
export function workspaceName(value: string | null | undefined, t: Translator): string {
  return value === 'Personal workspace' ? t('tenant.defaultName') : value || ''
}

export function workspaceDescription(value: string | null | undefined, t: Translator): string {
  return value === 'Personal knowledge' ? t('tenant.defaultDescription') : value || ''
}
