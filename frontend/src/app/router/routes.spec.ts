import { describe, expect, it } from 'vitest'
import { createAppRouter } from './index'

describe('rutas admin (FE-009, docs/phase-1/07 §4)', () => {
  it('toda ruta /admin/* exige super_admin y usa el layout admin', () => {
    const admin = createAppRouter()
      .getRoutes()
      .filter((r) => r.path.startsWith('/admin') && !r.redirect)
    expect(admin.map((r) => r.path).sort()).toEqual([
      '/admin/audit',
      '/admin/organizations',
      '/admin/users',
    ])
    for (const r of admin) {
      expect(r.meta.requiresGlobalRole, r.path).toBe('super_admin')
      expect(r.meta.requiresAuth, r.path).toBe(true)
      expect(r.meta.layout, r.path).toBe('admin')
    }
  })
})
