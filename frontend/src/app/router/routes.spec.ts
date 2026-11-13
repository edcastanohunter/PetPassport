import { describe, expect, it, vi } from 'vitest'
import { createAppRouter } from './index'
import { fullPageLoadGuard } from './guards'

describe('rutas admin (FE-009, docs/phase-1/07 §4)', () => {
  it('toda ruta /admin/* exige super_admin y usa el layout admin', () => {
    const admin = createAppRouter()
      .getRoutes()
      .filter((r) => r.path.startsWith('/admin') && !r.redirect)
    expect(admin.map((r) => r.path).sort()).toEqual([
      '/admin/ai',
      '/admin/audit',
      '/admin/billing',
      '/admin/catalogs/medications',
      '/admin/credentials',
      '/admin/ocr',
      '/admin/organizations',
      '/admin/partners',
      '/admin/platform',
      '/admin/regulatory',
      '/admin/users',
    ])
    for (const r of admin) {
      expect(r.meta.requiresGlobalRole, r.path).toBe('super_admin')
      expect(r.meta.requiresAuth, r.path).toBe(true)
      expect(r.meta.layout, r.path).toBe('admin')
    }
  })

  it('rutas de farmacia (FE-007, FE-008, FE-009) exigen tipo pharmacy y layout org', () => {
    const pharmacy = createAppRouter()
      .getRoutes()
      .filter((r) => r.path.startsWith('/pharmacy/:orgId'))
    expect(pharmacy.map((r) => r.path).sort()).toEqual([
      '/pharmacy/:orgId/inbox',
      '/pharmacy/:orgId/orders',
      '/pharmacy/:orgId/orders/:orderId',
      '/pharmacy/:orgId/settings',
    ])
    for (const r of pharmacy) {
      expect(r.meta.requiresOrgType, r.path).toBe('pharmacy')
      expect(r.meta.requiresAuth, r.path).toBe(true)
      expect(r.meta.layout, r.path).toBe('org')
    }
  })

  it('rutas de cotizaciones y pedidos del tutor (FE-004, FE-005, FE-006) usan layout owner', () => {
    const tutorRoutes = createAppRouter()
      .getRoutes()
      .filter(
        (r) =>
          r.path.startsWith('/app/') && (r.path.includes('quote') || r.path.includes('orders')),
      )
    expect(tutorRoutes.length).toBeGreaterThan(0)
    for (const r of tutorRoutes) {
      expect(r.meta.requiresAuth, r.path).toBe(true)
      expect(r.meta.layout, r.path).toBe('owner')
    }
  })
})

describe('facturación de la organización (PP-F5-FE-003, SEC-001)', () => {
  it('exige rol admin y se abre con carga completa (CSP propia del widget de pagos)', () => {
    const r = createAppRouter()
      .getRoutes()
      .find((x) => x.path === '/org/:orgId/settings/billing')
    expect(r?.meta.requiresOrgRole).toEqual(
      expect.arrayContaining(['clinic_admin', 'pharmacy_admin']),
    )
    expect(r?.meta.fullPageLoad).toBe(true)
  })

  it('fullPageLoadGuard recarga el documento solo al navegar dentro del SPA', () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    const to = { meta: { fullPageLoad: true }, fullPath: '/org/o1/settings/billing' }
    const inApp = { matched: [{}] }
    const firstLoad = { matched: [] }
    // @ts-expect-error -- rutas mínimas para el guard
    expect(fullPageLoadGuard.call(undefined, to, firstLoad, () => {})).toBe(true)
    // @ts-expect-error -- rutas mínimas para el guard
    expect(fullPageLoadGuard.call(undefined, to, inApp, () => {})).toBe(false)
    expect(assign).toHaveBeenCalledWith('/org/o1/settings/billing')
    // @ts-expect-error -- rutas mínimas para el guard
    expect(fullPageLoadGuard.call(undefined, { meta: {} }, inApp, () => {})).toBe(true)
    vi.unstubAllGlobals()
  })
})
