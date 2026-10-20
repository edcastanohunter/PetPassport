export interface RuntimeConfig {
  apiBaseUrl: string
  oidc: { issuer: string; clientId: string }
  env: 'dev' | 'staging' | 'prod'
}

let cached: RuntimeConfig | null = null

// /config.json se monta por entorno (docs/phase-1/07 §9): misma imagen, distinta configuración.
export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  if (cached) return cached
  const res = await fetch('/config.json', { cache: 'no-cache' })
  if (!res.ok) throw new Error(`No se pudo cargar /config.json (${res.status})`)
  cached = (await res.json()) as RuntimeConfig
  return cached
}

export function runtimeConfig(): RuntimeConfig {
  if (!cached) throw new Error('runtime config no cargada')
  return cached
}
