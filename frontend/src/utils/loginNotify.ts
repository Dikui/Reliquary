import { NotifyPlugin } from 'tdesign-vue-next'
import { renderWorkspaceNotifyContent } from './workspaceNotifyContent'

type Translator = (key: string, params?: Record<string, unknown>) => string
// TemplateResolver returns the raw i18n message verbatim — placeholders
// like `{name}` are left untouched. Pass `tm` from useI18n (which does
// no interpolation), not `t` (which would replace unspecified named
// placeholders with empty strings and strand the renderer with nothing
// to split on).
type TemplateResolver = (key: string) => unknown
interface LoginResponseLike {
  // response uses `tenant` (legacy backward-compat name on the Go side).
  // Accept either so callers don't have to normalise.
  active_tenant?: { id?: number | string; name?: string } | null
  tenant?: { id?: number | string; name?: string } | null
}

export function notifyLoginSuccess(
  response: LoginResponseLike | null | undefined,
  t: Translator,
  tm: TemplateResolver,
): void {
  const activeTenant = response?.active_tenant || response?.tenant
  if (!activeTenant) return

  const tenantName = activeTenant.name || String(activeTenant.id || '')
  const rawTemplate = tm('auth.loginSuccessContent')
  const template = typeof rawTemplate === 'string' ? rawTemplate : ''

  NotifyPlugin.success({
    title: t('auth.loginSuccessTitle'),
    content: renderWorkspaceNotifyContent({
      template,
      name: tenantName,
    }),
    duration: 6000,
    closeBtn: true,
  })
}
