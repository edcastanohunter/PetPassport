import { describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import es from '@/app/i18n/locales/es.json'
import FieldError from './FieldError.vue'
import ContextSwitcher from './ContextSwitcher.vue'
import { countries } from '@/shared/i18n/countries'

// PP-F1-FE-010: errores asociados a su campo, menú de contexto accesible por
// teclado en móvil y nombres de país localizados.
const i18n = () => createI18n({ legacy: false, locale: 'es', messages: { es } })

describe('PP-F1-FE-010 accesibilidad', () => {
  it('FieldError asocia el mensaje al control con aria-describedby y aria-invalid', async () => {
    const code = ref<string | undefined>(undefined)
    const Form = defineComponent({
      setup: () => () =>
        h('div', [
          h('input', { id: 'email', 'aria-describedby': 'email-hint' }),
          h(FieldError, { field: 'email', code: code.value }),
        ]),
    })
    const w = mount(Form, { attachTo: document.body, global: { plugins: [i18n()] } })
    const input = document.getElementById('email')!
    expect(input.getAttribute('aria-invalid')).toBeNull()

    code.value = 'REQUIRED'
    await nextTick()
    await nextTick()
    expect(input.getAttribute('aria-invalid')).toBe('true')
    expect(input.getAttribute('aria-describedby')).toBe('email-hint email-error')
    expect(document.getElementById('email-error')?.getAttribute('role')).toBe('alert')

    code.value = undefined
    await nextTick()
    await nextTick()
    expect(input.getAttribute('aria-invalid')).toBeNull()
    expect(input.getAttribute('aria-describedby')).toBe('email-hint')
    w.unmount()
  })

  it('FieldError muestra el mensaje propio de la pantalla', () => {
    const w = mount(FieldError, {
      props: { field: 'x', message: 'Nombre inválido' },
      global: { plugins: [i18n()] },
    })
    expect(w.text()).toBe('Nombre inválido')
  })

  it('ContextSwitcher abre con aria-expanded y cierra con Escape devolviendo el foco', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:p(.*)*', component: { render: () => null } }],
    })
    await router.push('/app')
    const w = mount(ContextSwitcher, {
      props: { items: [{ to: '/app', label: 'Mis mascotas' }] },
      attachTo: document.body,
      global: { plugins: [i18n(), router] },
    })
    const toggle = w.get('[data-testid="context-menu-toggle"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(w.get('#context-switcher').classes()).toContain('flex')
    await w.get('#context-switcher').trigger('keydown', { key: 'Escape' })
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(toggle.element)
    w.unmount()
  })

  it('los países se nombran y ordenan en el idioma activo', () => {
    const es = countries('es')
    const en = countries('en')
    expect(es.find((c) => c.code === 'ES')?.name).toBe('España')
    expect(en.find((c) => c.code === 'ES')?.name).toBe('Spain')
    expect(en.map((c) => c.name)).toEqual(
      [...en.map((c) => c.name)].sort((a, b) => a.localeCompare(b, 'en')),
    )
  })
})
