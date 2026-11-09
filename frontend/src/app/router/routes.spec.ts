import { describe, expect, it } from 'vitest'
import { createAppRouter } from './index'

describe('rutas admin (FE-009, docs/phase-1/07 §4)', () => {
  it('toda ruta /admin/* exige super_admin y usa el layout admin', () => {
    const admin = createAppRouter()
      .getRoutes()
      .filter((r) => r.path.startsWith('/admin') && !r.redirect)
    expect(admin.map((r) => r.path).sort()).toEqual([
      '/admin/audit',
      '/admin/catalogs/medications',
      '/admin/credentials',
      '/admin/organizations',
      '/admin/platform',
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
