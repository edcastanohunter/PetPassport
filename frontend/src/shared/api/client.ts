import createClient from 'openapi-fetch'
import type { paths } from './schema'
import { runtimeConfig } from '@/shared/config/runtime'

// Cliente tipado desde el contrato OpenAPI (ADR-005). En M1.1 se añaden los
// middlewares de Authorization, X-Correlation-ID e Idempotency-Key (FE-003).
export function createApiClient() {
  return createClient<paths>({ baseUrl: runtimeConfig().apiBaseUrl })
}

export type ApiClient = ReturnType<typeof createApiClient>
