import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

/**
 * Format a tenant role enum value ('viewer' | 'contributor' | 'admin' | 'owner')
 * into the user-facing label declared under tenantMember.role.* in the i18n
 * bundle. Falls back to the raw role string when the locale does not carry
 * the key — same behaviour as the inline helper this used to live in
 * (UserMenu.vue), now extracted so role-aware UI gates across views can
 * share one implementation.
 *
 * `roleIcon(role)` returns a TDesign icon name suitable for prefixing the
 * role label. The mapping is intentionally
 * limited to icons already known to ship in this codebase to avoid a
 * runtime "icon not found" footgun on uncommon role names.
 */
export function useRoleLabel() {
  const { t } = useI18n()
  const formatRole = (role: string | null | undefined): string => {
    if (!role) return ''
    const key = `tenantMember.role.${role}`
    const label = t(key)
    return label === key ? role : label
  }
  const ROLE_ICONS: Record<string, string> = {
    owner: 'secured',
    admin: 'user-circle',
    contributor: 'edit',
    viewer: 'browse',
  }
  const roleIcon = (role: string | null | undefined): string =>
    (role && ROLE_ICONS[role]) || ''
  return { formatRole, roleIcon }
}
