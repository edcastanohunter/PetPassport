import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PharmacySettingsView from './PharmacySettingsView.vue'

const GET = vi.fn()
const POST = vi.fn()
const PUT = vi.fn()
const DELETE = vi.fn()

vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return {
    ...mod,
    api: () => ({ GET, POST, PUT, DELETE }),
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { orgId: '01930000-0000-7000-8000-00000000c004' } }),
  RouterLink: { template: '<a><slot /></a>' },
}))

const mockIntegration = {
  level: 'portal' as const,
  catalog_mapping_enabled: true,
  delivery_areas: [{ city: 'Bogotá', postal_codes: ['110111'] }],
}

const mockCredentials = {
  has_credentials: true,
  client_id: 'client_pharmacy_demo_123',
  rate_limit_rpm: 600,
  scopes: ['quotes:read', 'quotes:submit'],
}

const mockWebhookConfig = {
  configured: true,
  url: 'https://farmacia.com/webhook',
  events: ['order.placed'],
  has_secret: true,
}

const mockDeliveries = {
  items: [
    {
      id: '01930000-0000-7000-8000-00000000d001',
      event_type: 'order.placed',
      status: 'failed',
      status_code: 500,
      attempts: 2,
      created_at: '2026-11-09T10:00:00Z',
    },
  ],
  total: 1,
}

const mockStats = {
  quotes_received: 15,
  quotes_submitted: 12,
  orders_placed: 8,
  orders_completed: 8,
  avg_response_minutes: 24,
  conversion_rate_percent: 66.7,
}

describe('PharmacySettingsView (FE-010, PTN-002, PTN-004, PTN-008)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    GET.mockImplementation((path: string) => {
      if (path.includes('/integration'))
        return Promise.resolve({ data: mockIntegration, response: { status: 200 } })
      if (path.includes('/product-mappings'))
        return Promise.resolve({ data: { items: [], total: 0 }, response: { status: 200 } })
      if (path.includes('/api-credentials'))
        return Promise.resolve({ data: mockCredentials, response: { status: 200 } })
      if (path.includes('/webhook/deliveries'))
        return Promise.resolve({ data: mockDeliveries, response: { status: 200 } })
      if (path.includes('/webhook'))
        return Promise.resolve({ data: mockWebhookConfig, response: { status: 200 } })
      if (path.includes('/stats'))
        return Promise.resolve({ data: mockStats, response: { status: 200 } })
      return Promise.resolve({ data: {}, response: { status: 200 } })
    })

    POST.mockImplementation((path: string) => {
      if (path.includes('/api-credentials/rotate')) {
        return Promise.resolve({
          data: {
            client_id: 'client_pharmacy_demo_123',
            client_secret: 'sec_new_rotated_secret_999',
            rotated_at: '2026-11-09T11:00:00Z',
          },
          response: { status: 200 },
        })
      }
      if (path.includes('/api-credentials/sandbox')) {
        return Promise.resolve({
          data: {
            client_id: 'sbx_client_456',
            client_secret: 'sbx_sec_789',
            token_endpoint: 'https://keycloak.petpassport.local/token',
            api_base_url: 'https://api.sandbox.petpassport.local',
          },
          response: { status: 200 },
        })
      }
      if (path.includes('/webhook/test')) {
        return Promise.resolve({
          data: { delivery_id: 'test-123', status: 'delivered', status_code: 200 },
          response: { status: 200 },
        })
      }
      if (path.includes('/retry')) {
        return Promise.resolve({
          data: {
            delivery_id: '01930000-0000-7000-8000-00000000d001',
            status: 'delivered',
            status_code: 200,
          },
          response: { status: 200 },
        })
      }
      return Promise.resolve({ data: {}, response: { status: 200 } })
    })
  })

  it('cambia entre pestañas y muestra credenciales API', async () => {
    const w = mount(PharmacySettingsView)
    await flushPromises()

    // Inicialmente pestaña general
    expect(w.text()).toContain('Modalidad y Cobertura')

    // Cambiar a pestaña de credenciales
    const credTabBtn = w.findAll('button').find((b) => b.text().includes('Credenciales API'))
    expect(credTabBtn).toBeDefined()
    await credTabBtn?.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('client_pharmacy_demo_123')
    expect(w.text()).toContain('600 req/min')
  })

  it('rota secretos de API correctamente', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const w = mount(PharmacySettingsView)
    await flushPromises()

    const credTabBtn = w.findAll('button').find((b) => b.text().includes('Credenciales API'))
    await credTabBtn?.trigger('click')
    await flushPromises()

    const rotateBtn = w.findAll('button').find((b) => b.text().includes('Rotar Secreto API'))
    expect(rotateBtn).toBeDefined()
    await rotateBtn?.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/pharmacies/{orgId}/api-credentials/rotate',
      expect.objectContaining({
        params: { path: { orgId: '01930000-0000-7000-8000-00000000c004' } },
      }),
    )
    expect(w.text()).toContain('sec_new_rotated_secret_999')
  })

  it('prueba el webhook y muestra resultado exitoso', async () => {
    const w = mount(PharmacySettingsView)
    await flushPromises()

    const webhookTabBtn = w.findAll('button').find((b) => b.text().includes('Webhooks y Entregas'))
    await webhookTabBtn?.trigger('click')
    await flushPromises()

    const testBtn = w.findAll('button').find((b) => b.text().includes('Probar Webhook'))
    expect(testBtn).toBeDefined()
    await testBtn?.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/pharmacies/{orgId}/webhook/test',
      expect.objectContaining({
        params: { path: { orgId: '01930000-0000-7000-8000-00000000c004' } },
      }),
    )
    expect(w.text()).toContain('HTTP 200')
  })

  it('reintenta entrega de webhook fallida', async () => {
    const w = mount(PharmacySettingsView)
    await flushPromises()

    const webhookTabBtn = w.findAll('button').find((b) => b.text().includes('Webhooks y Entregas'))
    await webhookTabBtn?.trigger('click')
    await flushPromises()

    const retryBtn = w.findAll('button').find((b) => b.text().includes('Reintentar'))
    expect(retryBtn).toBeDefined()
    await retryBtn?.trigger('click')
    await flushPromises()

    expect(POST).toHaveBeenCalledWith(
      '/pharmacies/{orgId}/webhook/deliveries/{deliveryId}/retry',
      expect.objectContaining({
        params: {
          path: {
            orgId: '01930000-0000-7000-8000-00000000c004',
            deliveryId: '01930000-0000-7000-8000-00000000d001',
          },
        },
      }),
    )
  })

  it('muestra métricas y SLA de la farmacia', async () => {
    const w = mount(PharmacySettingsView)
    await flushPromises()

    const statsTabBtn = w.findAll('button').find((b) => b.text().includes('Métricas y SLA'))
    await statsTabBtn?.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('15') // quotes_received
    expect(w.text()).toContain('12') // quotes_submitted
    expect(w.text()).toContain('24 min') // avg_response_minutes
    expect(w.text()).toContain('66.7%') // conversion_rate_percent
  })
})
