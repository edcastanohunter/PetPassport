import type { components } from '@/shared/api/schema'

// Tokenización del método de pago en el proveedor (PP-F5-SEC-001): la SPA nunca
// muestra un formulario de tarjeta propio; abre el widget del proveedor que
// devuelve `POST /billing-accounts/{id}/payment-methods/setup` y entrega al
// backend solo el token de un solo uso.

export type PaymentMethodSetup = components['schemas']['PaymentMethodSetup']

export interface TokenizeResult {
  providerToken: string
}

export class WidgetError extends Error {
  constructor(readonly code: 'WIDGET_LOAD_FAILED' | 'WIDGET_UNSUPPORTED' | 'WIDGET_CLOSED') {
    super(code)
    this.name = 'WidgetError'
  }
}

// Único script de proveedor que la SPA carga; la CSP de la página de
// facturación (frontend/nginx.conf) solo admite este origen.
export const WOMPI_WIDGET_URL = 'https://checkout.wompi.co/widget.js'

/** Indica si el proveedor exige aceptar sus términos antes de tokenizar. */
export function requiresAcceptance(setup: PaymentMethodSetup): boolean {
  return Boolean(setup.client_config.acceptance_permalink)
}

/**
 * Abre el widget del proveedor y resuelve con el token de un solo uso, o null
 * si la persona lo cierra sin terminar.
 */
export async function tokenize(setup: PaymentMethodSetup): Promise<TokenizeResult | null> {
  switch (setup.provider) {
    case 'fake':
      // Proveedor simulado de dev/CI: el backend acepta tok_ok (providers/fake).
      return { providerToken: 'tok_ok' }
    case 'wompi':
      return tokenizeWompi(setup.client_config)
    default:
      throw new WidgetError('WIDGET_UNSUPPORTED')
  }
}

type WompiWidget = new (options: Record<string, unknown>) => {
  open: (cb: (result: unknown) => void) => void
}

let wompiScript: Promise<void> | null = null

function loadWompi(url: string): Promise<void> {
  if (url !== WOMPI_WIDGET_URL) return Promise.reject(new WidgetError('WIDGET_UNSUPPORTED'))
  wompiScript ??= new Promise<void>((resolve, reject) => {
    const s = document.createElement('script')
    s.src = url
    s.async = true
    s.onload = () => resolve()
    s.onerror = () => {
      wompiScript = null
      reject(new WidgetError('WIDGET_LOAD_FAILED'))
    }
    document.head.appendChild(s)
  })
  return wompiScript
}

/**
 * Extrae el token del resultado del widget en modo tokenización.
 *
 * SIN VERIFICAR: la documentación pública de Wompi describe el widget de
 * compra (`new WidgetCheckout({...}).open(cb)` con `result.transaction`) y
 * confirma que existe un modo de tokenización, pero no documenta la opción ni
 * la forma del resultado. Se asume la forma de `POST /v1/tokens/cards`
 * (`{ data: { id } }`) o un `token` plano. Si no aparece, se informa
 * WIDGET_UNSUPPORTED en lugar de adivinar. Verificar contra el sandbox (QA-002).
 */
export function wompiToken(result: unknown): string | null {
  if (!result || typeof result !== 'object') return null
  const r = result as { token?: unknown; data?: { id?: unknown } }
  if (typeof r.token === 'string' && r.token) return r.token
  if (r.data && typeof r.data.id === 'string' && r.data.id) return r.data.id
  return null
}

async function tokenizeWompi(cfg: Record<string, string>): Promise<TokenizeResult | null> {
  await loadWompi(cfg.widget_url ?? '')
  const Widget = (window as unknown as { WidgetCheckout?: WompiWidget }).WidgetCheckout
  if (!Widget) throw new WidgetError('WIDGET_LOAD_FAILED')
  return new Promise((resolve, reject) => {
    // SIN VERIFICAR: nombre de la opción de tokenización (ver wompiToken).
    const widget = new Widget({
      publicKey: cfg.public_key,
      widgetOperation: cfg.data_widget_operation ?? 'tokenize',
    })
    widget.open((result) => {
      if (result == null) {
        resolve(null)
        return
      }
      const token = wompiToken(result)
      if (token) resolve({ providerToken: token })
      else reject(new WidgetError('WIDGET_UNSUPPORTED'))
    })
  })
}
