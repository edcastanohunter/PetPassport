import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSessionStore } from './sessionStore'
import type { AppUserManager } from '@/shared/auth/userManager'
import type { Me } from '@/shared/api/client'
import { i18n, setLocale } from '@/app/i18n'

const getMock = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET: getMock }), setTokenProvider: vi.fn() }
})

function fakeManager() {
  const user = { access_token: 'tok', id_token: 'id', expired: false }
  const handlers: Record<string, ((...args: unknown[]) => void)[]> = {}
  const on = (name: string) => (fn: (...args: unknown[]) => void) => {
    ;(handlers[name] ??= []).push(fn)
  }
  return {
    user,
    handlers,
    manager: {
      events: {
        addUserLoaded: on('loaded'),
        addUserUnloaded: on('unloaded'),
        addSilentRenewError: on('renewError'),
        addAccessTokenExpired: on('expired'),
      },
      getUser: vi.fn(async () => user),
      signinRedirect: vi.fn(async () => undefined),
      signinRedirectCallback: vi.fn(async () => user),
      signoutRedirect: vi.fn(async () => undefined),
      removeUser: vi.fn(async () => undefined),
    } as unknown as AppUserManager,
  }
}

const me: Me = {
  id: '0192f3a2-2222-7aaa-bbbb-dddddddddddd',
  email: 'ana@example.com',
  email_verified: true,
  display_name: 'Ana',
  locale: 'es',
  global_roles: ['pet_owner'],
  memberships: [],
  onboarding_required: true,
  consent_outdated: true,
  status: 'active',
}

describe('sessionStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getMock.mockReset()
    sessionStorage.clear()
  })

  it('login guarda returnTo y redirige a Keycloak', async () => {
    const f = fakeManager()
    const s = useSessionStore()
    s.init(f.manager)
    await s.login('/app/profile')
    expect(sessionStorage.getItem('pp.returnTo')).toBe('/app/profile')
    expect(f.manager.signinRedirect).toHaveBeenCalled()
  })

  it('PP-F1-FE-010 login envía ui_locales y me.locale fija idioma y <html lang>', async () => {
    setLocale('en', false)
    const f = fakeManager()
    const s = useSessionStore()
    s.init(f.manager)
    await s.login()
    expect(f.manager.signinRedirect).toHaveBeenCalledWith({
      extraQueryParams: { ui_locales: 'en' },
    })
    await s.handleCallback()
    getMock.mockResolvedValue({ data: { ...me, locale: 'es' } })
    await s.loadMe()
    expect(i18n.global.locale.value).toBe('es')
    expect(document.documentElement.lang).toBe('es')
    s.setMe({ ...me, locale: 'en' })
    expect(document.documentElement.lang).toBe('en')
  })

  it('handleCallback restaura returnTo y loadMe carga /me', async () => {
    const f = fakeManager()
    const s = useSessionStore()
    s.init(f.manager)
    sessionStorage.setItem('pp.returnTo', '/app/x')
    expect(await s.handleCallback()).toBe('/app/x')
    expect(s.isAuthenticated).toBe(true)
    expect(s.accessToken).toBe('tok')
    getMock.mockResolvedValue({ data: me })
    await s.loadMe()
    expect(s.me?.display_name).toBe('Ana')
    expect(s.onboardingRequired).toBe(true)
    expect(s.consentOutdated).toBe(true)
    // Cacheado: no vuelve a llamar.
    await s.loadMe()
    expect(getMock).toHaveBeenCalledTimes(1)
  })

  it('logout descarta tokens y usa id_token_hint', async () => {
    const f = fakeManager()
    const s = useSessionStore()
    s.init(f.manager)
    await s.handleCallback()
    await s.logout()
    expect(s.isAuthenticated).toBe(false)
    expect(f.manager.signoutRedirect).toHaveBeenCalledWith({ id_token_hint: 'id' })
  })

  it('silentRenewError marca la sesión como caducada', async () => {
    const f = fakeManager()
    const s = useSessionStore()
    s.init(f.manager)
    f.handlers.renewError[0]()
    expect(s.renewFailed).toBe(true)
  })
})
