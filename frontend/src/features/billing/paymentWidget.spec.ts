import { describe, expect, it } from 'vitest'
import { requiresAcceptance, tokenize, wompiToken, WidgetError } from './paymentWidget'

describe('paymentWidget (PP-F5-SEC-001)', () => {
  it('el proveedor simulado devuelve el token que acepta el fake del backend', async () => {
    await expect(tokenize({ provider: 'fake', client_config: { mode: 'fake' } })).resolves.toEqual({
      providerToken: 'tok_ok',
    })
  })

  it('un proveedor desconocido no abre nada', async () => {
    await expect(tokenize({ provider: 'otro', client_config: {} })).rejects.toBeInstanceOf(
      WidgetError,
    )
  })

  it('solo carga el script del widget desde el origen permitido por la CSP', async () => {
    await expect(
      tokenize({ provider: 'wompi', client_config: { widget_url: 'https://evil.example/w.js' } }),
    ).rejects.toMatchObject({ code: 'WIDGET_UNSUPPORTED' })
    expect(document.querySelector('script[src="https://evil.example/w.js"]')).toBeNull()
  })

  it('pide aceptación de términos solo si el proveedor la publica', () => {
    expect(requiresAcceptance({ provider: 'fake', client_config: {} })).toBe(false)
    expect(
      requiresAcceptance({
        provider: 'wompi',
        client_config: { acceptance_permalink: 'https://x/terms.pdf' },
      }),
    ).toBe(true)
  })

  it('extrae el token sin inventar: forma desconocida → null', () => {
    expect(wompiToken({ token: 'tok_1' })).toBe('tok_1')
    expect(wompiToken({ data: { id: 'tok_2' } })).toBe('tok_2')
    expect(wompiToken({ transaction: { id: 'tx' } })).toBeNull()
    expect(wompiToken(null)).toBeNull()
  })
})
