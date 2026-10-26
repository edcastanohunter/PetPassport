import type { NavigationGuardWithThis, RouteLocationNormalized } from 'vue-router'
import { useSessionStore } from '@/features/auth/sessionStore'
import { ApiError } from '@/shared/api/client'

// Guards de docs/phase-1/07 §4 (FE-004). Leen sessionStore.me; si no está cargado,
// lo cargan antes de resolver. El backend repite la decisión en cada request.
declare module 'vue-router' {
  interface RouteMeta {
    layout?: 'public' | 'owner' | 'org' | 'admin'
    requiresAuth?: boolean
    requiresOnboarding?: boolean
    requiresMembership?: boolean
    requiresGlobalRole?: string
    /** Con requiresMembership: rol de organización requerido (alguno de la lista). */
    requiresOrgRole?: readonly string[]
    /** Con requiresMembership: tipo de organización requerido. */
    requiresOrgType?: 'clinic' | 'pharmacy'
  }
}

export const authGuard: NavigationGuardWithThis<undefined> = async (to) => {
  const meta = to.meta
  if (
    !meta.requiresAuth &&
    !meta.requiresOnboarding &&
    !meta.requiresMembership &&
    !meta.requiresGlobalRole
  ) {
    return true
  }
  const session = useSessionStore()
  if (!session.isAuthenticated) {
    await session.login(to.fullPath)
    return false
  }
  try {
    const me = await session.loadMe()
    if (!me) {
      await session.login(to.fullPath)
      return false
    }
    if (me.status === 'suspended') return { name: 'forbidden' }
    if (meta.requiresOnboarding && me.onboarding_required) {
      return { name: 'onboarding', query: { returnTo: to.fullPath } }
    }
    if (meta.requiresGlobalRole && !session.hasGlobalRole(meta.requiresGlobalRole)) {
      return { name: 'forbidden' }
    }
    if (meta.requiresMembership) {
      const orgId = String(to.params.orgId ?? '')
      const m = session.membershipFor(orgId)
      if (!m) return { name: 'not-found' }
      if (meta.requiresOrgType && m.type !== meta.requiresOrgType) return { name: 'forbidden' }
      if (meta.requiresOrgRole && !meta.requiresOrgRole.includes(m.role)) {
        return { name: 'forbidden' }
      }
    }
    return true
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      await session.login(to.fullPath)
      return false
    }
    if (e instanceof ApiError && e.code === 'ACCOUNT_SUSPENDED') return { name: 'forbidden' }
    throw e
  }
}

export function returnToFrom(route: RouteLocationNormalized, fallback = '/app') {
  const raw = route.query.returnTo
  const value = Array.isArray(raw) ? raw[0] : raw
  // Solo rutas internas: evita open redirect.
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')
    ? value
    : fallback
}
