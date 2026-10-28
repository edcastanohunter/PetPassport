import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'
import type { User } from 'oidc-client-ts'
import { api, ApiError, setTokenProvider, type Me } from '@/shared/api/client'
import type { AppUserManager } from '@/shared/auth/userManager'
import { currentLocale, setLocale } from '@/app/i18n'

const RETURN_TO_KEY = 'pp.returnTo'

// sessionStore (FE-002): usuario OIDC, token en memoria, `me` del backend y
// acciones login/logout/callback. La UI nunca es la autoridad: el backend repite
// cada decisión; aquí solo se enruta.
export const useSessionStore = defineStore('session', () => {
  const manager = shallowRef<AppUserManager | null>(null)
  const user = shallowRef<User | null>(null)
  const me = ref<Me | null>(null)
  const meLoading = ref(false)
  const consentOutdated = ref(false)
  const renewFailed = ref(false)

  const accessToken = computed(() => user.value?.access_token)
  const isAuthenticated = computed(() => !!user.value && !user.value.expired)
  const onboardingRequired = computed(() => me.value?.onboarding_required ?? false)
  const globalRoles = computed(() => me.value?.global_roles ?? [])
  const memberships = computed(() => me.value?.memberships ?? [])

  function hasGlobalRole(role: string) {
    return globalRoles.value.includes(role)
  }
  function membershipFor(orgId: string) {
    return memberships.value.find((m) => m.organization_id === orgId)
  }

  function init(um: AppUserManager) {
    manager.value = um
    setTokenProvider(() => accessToken.value)
    um.events.addUserLoaded((u) => {
      user.value = u
      renewFailed.value = false
    })
    um.events.addUserUnloaded(() => {
      user.value = null
      me.value = null
    })
    um.events.addSilentRenewError(() => {
      renewFailed.value = true
    })
    um.events.addAccessTokenExpired(() => {
      renewFailed.value = true
    })
  }

  /** Restaura el usuario en memoria (tras navegación interna; tras recarga completa no hay). */
  async function restore() {
    if (!manager.value) return
    const u = await manager.value.getUser()
    user.value = u && !u.expired ? u : null
  }

  async function login(returnTo = '/app') {
    if (!manager.value) throw new Error('session no inicializada')
    sessionStorage.setItem(RETURN_TO_KEY, returnTo)
    // ui_locales: login, registro y correos de Keycloak en el idioma de la SPA (FE-010, IAM-012).
    await manager.value.signinRedirect({ extraQueryParams: { ui_locales: currentLocale() } })
  }

  /** Procesa /auth/callback y devuelve la ruta a la que volver. */
  async function handleCallback(): Promise<string> {
    if (!manager.value) throw new Error('session no inicializada')
    user.value = await manager.value.signinRedirectCallback()
    const returnTo = sessionStorage.getItem(RETURN_TO_KEY) ?? '/app'
    sessionStorage.removeItem(RETURN_TO_KEY)
    return returnTo
  }

  async function logout() {
    if (!manager.value) return
    const idToken = user.value?.id_token
    user.value = null
    me.value = null
    await manager.value.signoutRedirect(idToken ? { id_token_hint: idToken } : undefined)
  }

  /** Descarta el estado local tras el callback de logout (IAM-013). */
  async function completeLogout() {
    await manager.value?.removeUser()
    user.value = null
    me.value = null
  }

  async function loadMe(force = false): Promise<Me | null> {
    if (me.value && !force) return me.value
    if (!isAuthenticated.value) return null
    meLoading.value = true
    try {
      const { data } = await api().GET('/me')
      me.value = data ?? null
      consentOutdated.value = !!data?.consent_outdated
      if (data) setLocale(data.locale)
      return me.value
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) user.value = null
      throw e
    } finally {
      meLoading.value = false
    }
  }

  function setMe(value: Me) {
    me.value = value
    consentOutdated.value = value.consent_outdated
    setLocale(value.locale)
  }

  return {
    user,
    me,
    meLoading,
    consentOutdated,
    renewFailed,
    accessToken,
    isAuthenticated,
    onboardingRequired,
    globalRoles,
    memberships,
    hasGlobalRole,
    membershipFor,
    init,
    restore,
    login,
    handleCallback,
    logout,
    completeLogout,
    loadMe,
    setMe,
  }
})
