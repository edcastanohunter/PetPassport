import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PetAlertsPanel from './PetAlertsPanel.vue'
import es from '@/app/i18n/locales/es.json'
import { ApiError, type Pet } from '@/shared/api/client'

const PUT = vi.fn()
vi.mock('@/shared/api/client', async (importOriginal) => {
  const mod = await importOriginal<typeof import('@/shared/api/client')>()
  return { ...mod, api: () => ({ PUT }) }
})

const pet = {
  id: 'pet-1',
  version: 4,
  name: 'Luna',
  alerts: [{ kind: 'allergy', text: 'Penicilina' }],
} as Pet

function mountPanel(editable = true, p: Pet = pet) {
  const i18n = createI18n({ legacy: false, locale: 'es', messages: { es } })
  return mount(PetAlertsPanel, { props: { pet: p, editable }, global: { plugins: [i18n] } })
}

describe('PetAlertsPanel (FE-005, DOM-013)', () => {
  beforeEach(() => {
    PUT.mockReset()
  })

  it('muestra las alertas con su tipo y no ofrece editar sin permiso', () => {
    const w = mountPanel(false)
    expect(w.get('[data-testid="alert-item"]').text()).toContain('Alergia')
    expect(w.get('[data-testid="alert-item"]').text()).toContain('Penicilina')
    expect(w.find('[data-testid="alerts-edit"]').exists()).toBe(false)
  })

  it('sin alertas lo dice', () => {
    const w = mountPanel(true, { ...pet, alerts: [] } as Pet)
    expect(w.text()).toContain('Sin alertas registradas')
  })

  it('edita y guarda con If-Match y descarta las filas vacías', async () => {
    PUT.mockResolvedValue({ data: { ...pet, version: 5 } })
    const w = mountPanel()
    await w.get('[data-testid="alerts-edit"]').trigger('click')
    await w.get('[data-testid="alerts-add"]').trigger('click')
    const rows = w.findAll('[data-testid="alert-row"]')
    expect(rows).toHaveLength(2)
    await rows[0].get('input').setValue('  Penicilina y sulfas ')
    await rows[1].get('select').setValue('behavior')
    // La segunda fila queda sin texto: no se envía.
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(PUT).toHaveBeenCalledWith('/pets/{petId}/alerts', {
      params: { path: { petId: 'pet-1' }, header: { 'If-Match': 4 } },
      body: { alerts: [{ kind: 'allergy', text: 'Penicilina y sulfas' }] },
    })
    expect(w.emitted('changed')?.[0]?.[0]).toMatchObject({ version: 5 })
    expect(w.find('[data-testid="alerts-save"]').exists()).toBe(false)
  })

  it('conflicto de versión: muestra el error y conserva el borrador', async () => {
    PUT.mockResolvedValue({ error: new ApiError({ code: 'CONFLICT_VERSION' }, 409) })
    const w = mountPanel()
    await w.get('[data-testid="alerts-edit"]').trigger('click')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').exists()).toBe(true)
    expect(w.find('[data-testid="alerts-save"]').exists()).toBe(true)
  })

  it('cancelar restaura las alertas originales', async () => {
    const w = mountPanel()
    await w.get('[data-testid="alerts-edit"]').trigger('click')
    await w.get('[data-testid="alert-row"] input').setValue('otra cosa')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Cancelar')!
      .trigger('click')
    expect(w.get('[data-testid="alert-item"]').text()).toContain('Penicilina')
  })
})
