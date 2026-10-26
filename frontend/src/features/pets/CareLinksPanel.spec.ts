import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CareLinksPanel from './CareLinksPanel.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError, type Pet } from '@/shared/api/client'

const GET = vi.fn()
const POST = vi.fn()
const DELETE = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ GET, POST, DELETE }) }
})

const pet = {
  id: 'pet-1',
  version: 2,
  guardian_id: 'g',
  name: 'Luna',
  species: 'dog',
  sex: 'female',
  neutered: 'yes',
  status: 'active',
  care_links: [
    {
      organization_id: 'org-old',
      display_name: 'Clínica Norte',
      city: 'Bogotá',
      status: 'active',
      org_suspended: true,
      granted_at: '2026-10-01T10:00:00Z',
    },
  ],
  created_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-01T10:00:00Z',
} as Pet

function mountPanel() {
  const i18n = createI18n({
    legacy: false,
    locale: 'es',
    messages: { es },
    datetimeFormats: { es: { short: { year: 'numeric', month: 'short', day: 'numeric' } } },
  })
  return mount(CareLinksPanel, { props: { pet }, global: { plugins: [i18n] } })
}

describe('CareLinksPanel (FE-008, PRD-015)', () => {
  beforeEach(() => {
    GET.mockReset()
    POST.mockReset()
    DELETE.mockReset()
  })

  it('muestra vínculos con aviso de clínica suspendida', () => {
    const w = mountPanel()
    expect(w.text()).toContain('Clínica Norte')
    expect(w.text()).toContain('Clínica suspendida')
  })

  it('código → previsualización → confirmar vínculo', async () => {
    GET.mockResolvedValue({
      data: {
        id: 'org-1',
        type: 'clinic',
        display_name: 'Clínica Sur',
        city: 'Medellín',
        country: 'CO',
        status: 'active',
      },
    })
    POST.mockResolvedValue({ data: pet })
    const w = mountPanel()
    await w.get('[data-testid="link-clinic"]').trigger('click')
    await w.get('#link-code').setValue('7k3mq2')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(GET).toHaveBeenCalledWith('/organizations/lookup', {
      params: { query: { code: '7K3MQ2' } },
    })
    expect(w.get('[data-testid="link-preview"]').text()).toContain('Clínica Sur')
    await w.get('[data-testid="confirm-link"]').trigger('click')
    await flushPromises()
    expect(POST).toHaveBeenCalledWith('/pets/{petId}/care-links', {
      params: { path: { petId: 'pet-1' } },
      body: { organization_id: 'org-1' },
    })
    expect(w.emitted('changed')).toHaveLength(1)
  })

  it('código con formato inválido no llama a la API; 404 → clínica no encontrada', async () => {
    const w = mountPanel()
    await w.get('[data-testid="link-clinic"]').trigger('click')
    await w.get('#link-code').setValue('7K3MQI')
    await w.get('form').trigger('submit')
    expect(GET).not.toHaveBeenCalled()
    GET.mockRejectedValue(new ApiError({ code: 'NOT_FOUND' }, 404))
    await w.get('#link-code').setValue('7K3MQ2')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('No encontramos una clínica activa')
  })
})
