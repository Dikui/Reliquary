import type { FormRule } from 'tdesign-vue-next'

type Translate = (key: string, params?: Record<string, unknown>) => string

export function newPasswordRules(t: Translate, extra: FormRule[] = []): FormRule[] {
  return [
    { required: true, message: t('auth.passwordRequired'), type: 'error' },
    { min: 8, message: t('auth.passwordMinLength'), type: 'error' },
    { max: 72, message: t('auth.passwordMaxLength'), type: 'error' },
    ...extra,
  ]
}
