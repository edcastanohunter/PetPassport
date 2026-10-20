import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { RouteLocationNormalized } from 'vue-router'
import { authGuard, returnToFrom } from './guards'
import { useSessionStore } from '@/features/auth/sessionStore'
import type { Me } from '@/shared/api/client'

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET: async () => ({ data: current }) }),
    setTokenProvider: vi.fn(),
  }
})

let current: Me
const base: Me = {
  id: '0192f3a2-2222-7aaa-bbbb-dddddddddddd',
  email: 'ana@example.com',
  email_verified: true,
  display_name: 'Ana',
  locale: 'es',
  global_roles: ['pet_owner'],
  memberships: [],
  onboarding_required: false,
  consent_outdated: false,
  status: 'active',
}

function route(
  meta: Record<string, unknown>,
  fullPath = '/app',
  params: Record<string, string> = {},
) {
  return { meta, fullPath, params, query: {} } as unknown as RouteLocationNormalized
}

const run = (to: RouteLocationNormalized) =>
  (
    authGuard as unknown as (
      to: RouteLocationNormalized,
      from: RouteLocationNormalized,
    ) => Promise<unknown>
  )(to, to)

describe('authGuard', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    current = { ...base }
  })

  it('deja pasar rutas públicas sin sesión', async () => {
    expect(await run(route({}))).toBe(true)
  })

  it('redirige a login si no hay sesión', async () => {
    const s = useSessionStore()
    const login = vi.spyOn(s, 'login').mockResolvedValue()
    expect(await run(route({ requiresAuth: true }, '/app/profile'))).toBe(false)
    expect(login).toHaveBeenCalledWith('/app/profile')
  })

  it('manda a onboarding si falta', async () => {
    const s = useSessionStore()
    s.user = { access_token: 't', expired: false } as never
    current = { ...base, onboarding_required: true }
    expect(await run(route({ requiresAuth: true, requiresOnboarding: true }, '/app'))).toEqual({
      name: 'onboarding',
      query: { returnTo: '/app' },
    })
  })

  it('permite con onboarding completo y aplica rol global / membresía', async () => {
    const s = useSessionStore()
    s.user = { access_token: 't', expired: false } as never
    expect(await run(route({ requiresAuth: true, requiresOnboarding: true }))).toBe(true)
    expect(await run(route({ requiresGlobalRole: 'super_admin' }))).toEqual({ name: 'forbidden' })
    expect(await run(route({ requiresMembership: true }, '/org/x', { orgId: 'x' }))).toEqual({
      name: 'not-found',
    })
  })

  it('cuenta suspendida → 403', async () => {
    const s = useSessionStore()
    s.user = { access_token: 't', expired: false } as never
    current = { ...base, status: 'suspended' }
    expect(await run(route({ requiresAuth: true }))).toEqual({ name: 'forbidden' })
  })
})

describe('returnToFrom', () => {
  it('solo acepta rutas internas', () => {
    const r = (returnTo: unknown) => ({ query: { returnTo } }) as unknown as RouteLocationNormalized
    expect(returnToFrom(r('/app/pets'))).toBe('/app/pets')
    expect(returnToFrom(r('https://evil'))).toBe('/app')
    expect(returnToFrom(r('//evil'))).toBe('/app')
    expect(returnToFrom(r(undefined))).toBe('/app')
  })
})
