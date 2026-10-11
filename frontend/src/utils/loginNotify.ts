import { NotifyPlugin } from 'tdesign-vue-next'
import { renderWorkspaceNotifyContent } from './workspaceNotifyContent'
import { workspaceName } from './workspaceCopy'

type Translator = (key: string, params?: Record<string, unknown>) => string
// TemplateResolver returns the raw i18n message verbatim — placeholders
// like `{name}` are left untouched. Pass `tm` from useI18n (which does
// no interpolation), not `t` (which would replace unspecified named
// placeholders with empty strings and strand the renderer with nothing
// to split on).
type TemplateResolver = (key: string) => unknown
interface LoginResponseLike {
  // Auth responses expose the personal space snapshot.
  // Accept either so callers don't have to normalise.
  active_space?: { id?: number | string; name?: string } | null
  space?: { id?: number | string; name?: string } | null
}

export function notifyLoginSuccess(
  response: LoginResponseLike | null | undefined,
  t: Translator,
  tm: TemplateResolver,
): void {
  const activeSpace = response?.active_space || response?.space
  if (!activeSpace) return

  const spaceName = workspaceName(activeSpace.name, t) || String(activeSpace.id || '')
  const rawTemplate = tm('auth.loginSuccessContent')
  const template = typeof rawTemplate === 'string' ? rawTemplate : ''

  NotifyPlugin.success({
    title: t('auth.loginSuccessTitle'),
    content: renderWorkspaceNotifyContent({
      template,
      name: spaceName,
    }),
    duration: 6000,
    closeBtn: true,
  })
}
