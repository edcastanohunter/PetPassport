import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useStatusStore } from './statusStore'
import type { SystemStatusData } from './statusTypes'

describe('statusStore (PP-F5-OPS-007, ARQ-012, FE-009)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
  })

  afterEach(() => {
    const store = useStatusStore()
    store.stopPolling()
  })

  it('initializes with operational status and no banners', () => {
    const store = useStatusStore()
    expect(store.isOperational).toBe(true)
    expect(store.hasActiveIncident).toBe(false)
    expect(store.hasActiveMaintenance).toBe(false)
    expect(store.bannerSeverity).toBeNull()
  })

  it('detects degraded performance status', () => {
    const store = useStatusStore()
    const mockData: SystemStatusData = {
      status: 'degraded_performance',
      updated_at: new Date().toISOString(),
      components: [
        { id: 'api', name: 'API', status: 'degraded_performance', uptime_percentage_90d: 98.5 },
      ],
      active_incidents: [],
      scheduled_maintenances: [],
    }
    store.setMockStatus(mockData)

    expect(store.isOperational).toBe(false)
    expect(store.bannerSeverity).toBe('warning')
  })

  it('detects active critical S1 incident and sets critical banner', () => {
    const store = useStatusStore()
    const mockData: SystemStatusData = {
      status: 'major_outage',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [
        {
          id: 'inc-1',
          name: 'Caída de base de datos',
          status: 'investigating',
          severity: 'S1',
          impact: 'Interrupción en todas las consultas y citas',
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          updates: [],
        },
      ],
      scheduled_maintenances: [],
    }
    store.setMockStatus(mockData)

    expect(store.hasActiveIncident).toBe(true)
    expect(store.bannerSeverity).toBe('critical')
    expect(store.activeIncident?.name).toBe('Caída de base de datos')
  })

  it('detects active maintenance and sets info banner', () => {
    const store = useStatusStore()
    const mockData: SystemStatusData = {
      status: 'maintenance',
      updated_at: new Date().toISOString(),
      components: [],
      active_incidents: [],
      scheduled_maintenances: [
        {
          id: 'maint-1',
          name: 'Mantenimiento de infraestructura K8s',
          status: 'in_progress',
          scheduled_for: new Date().toISOString(),
          scheduled_until: new Date(Date.now() + 3600000).toISOString(),
          impact: 'Posible degradación temporal',
          description: 'Actualización de nodos',
        },
      ],
    }
    store.setMockStatus(mockData)

    expect(store.hasActiveMaintenance).toBe(true)
    expect(store.bannerSeverity).toBe('info')
    expect(store.activeMaintenance?.name).toBe('Mantenimiento de infraestructura K8s')
  })

  it('fetches status from endpoint successfully', async () => {
    const mockData: SystemStatusData = {
      status: 'operational',
      updated_at: '2026-11-10T12:00:00Z',
      components: [
        { id: 'web-app', name: 'Web App', status: 'operational', uptime_percentage_90d: 99.99 },
      ],
      active_incidents: [],
      scheduled_maintenances: [],
    }

    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(mockData),
    })

    const store = useStatusStore()
    await store.fetchStatus('/status.json')

    expect(store.statusData).toEqual(mockData)
    expect(store.isOperational).toBe(true)
    expect(store.error).toBeNull()
  })

  it('handles fetch failure gracefully without throwing', async () => {
    global.fetch = vi.fn().mockRejectedValue(new Error('Network offline'))

    const store = useStatusStore()
    await store.fetchStatus('/status.json')

    expect(store.statusData).toBeNull()
    expect(store.error).toContain('Network offline')
  })
})
