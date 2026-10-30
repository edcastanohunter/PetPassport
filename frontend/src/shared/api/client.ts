import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema'
import { runtimeConfig } from '@/shared/config/runtime'
import { emitFreshness, freshnessOf, resetFreshnessListeners } from './freshness'

export type Problem = components['schemas']['Problem']
export type Me = components['schemas']['Me']
export type LegalCurrent = components['schemas']['LegalCurrent']
export type OnboardingRequest = components['schemas']['OnboardingRequest']
export type UpdateMeRequest = components['schemas']['UpdateMeRequest']
export type Pet = components['schemas']['Pet']
export type PetListItem = components['schemas']['PetListItem']
export type PetPage = components['schemas']['PetPage']
export type Organization = components['schemas']['Organization']
export type OrgSummary = components['schemas']['OrgSummary']
export type Member = components['schemas']['Member']
export type Invitation = components['schemas']['Invitation']
export type InvitationPreview = components['schemas']['InvitationPreview']
export type TransferPreview = components['schemas']['TransferPreview']
export type PatientRow = components['schemas']['PatientRow']
export type PatientPage = components['schemas']['PatientPage']
export type OrgRole = components['schemas']['OrgRole']
export type Membership = components['schemas']['Membership']
export type AdminUserRow = components['schemas']['AdminUserRow']
export type AuditEntry = components['schemas']['AuditEntry']
export type Encounter = components['schemas']['Encounter']
export type EncounterSummary = components['schemas']['EncounterSummary']
export type EncounterType = components['schemas']['EncounterType']
export type Vitals = components['schemas']['Vitals']
export type VitalWarning = components['schemas']['VitalWarning']
export type Objective = components['schemas']['Objective']
export type Finding = components['schemas']['Finding']
export type Diagnosis = components['schemas']['Diagnosis']
export type MedicationItem = components['schemas']['MedicationItem']
export type Plan = components['schemas']['Plan']
export type FollowUp = components['schemas']['FollowUp']
export type Attachment = components['schemas']['Attachment']
export type Addendum = components['schemas']['Addendum']
export type AddendumReason = components['schemas']['AddendumReason']
export type DocumentJob = components['schemas']['DocumentJob']
export type DownloadLink = components['schemas']['DownloadLink']
export type TimelineItem = components['schemas']['TimelineItem']
export type TimelinePage = components['schemas']['TimelinePage']
export type WeightPoint = components['schemas']['WeightPoint']
export type WeightHistory = components['schemas']['WeightHistory']
export type PetClinicalSummary = components['schemas']['PetClinicalSummary']
export type EncounterVerification = components['schemas']['EncounterVerification']
export type Card = components['schemas']['Card']
export type CardEntry = components['schemas']['CardEntry']
export type CardEntryKind = components['schemas']['CardEntryKind']
export type NextDueItem = components['schemas']['NextDueItem']
export type NextDueProposal = components['schemas']['NextDueProposal']
export type Vaccine = components['schemas']['Vaccine']
export type Protocol = components['schemas']['Protocol']
export type Share = components['schemas']['Share']
export type CreatedShare = components['schemas']['CreatedShare']
export type SharedRecord = components['schemas']['SharedRecord']
export type CardVerification = components['schemas']['CardVerification']
export type UnlockedShare = components['schemas']['UnlockedShare']
export type CareLink = components['schemas']['CareLink']
export type Appointment = components['schemas']['Appointment']
export type NotificationPreferences = components['schemas']['NotificationPreferences']
export type AppointmentType = components['schemas']['AppointmentType']
export type AppointmentStatus = components['schemas']['AppointmentStatus']
export type SlotPreference = components['schemas']['SlotPreference']
export type Agenda = components['schemas']['Agenda']
export type AgendaTimeRange = components['schemas']['AgendaTimeRange']
export type AgendaDayException = components['schemas']['AgendaDayException']
export type AvailabilitySlot = components['schemas']['AvailabilitySlot']

// ApiError envuelve un application/problem+json (docs/phase-1/07 §3, FE-003).
export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly detail?: string
  readonly errors: { field: string; code: string }[]
  readonly correlationId?: string
  readonly retryAfter?: number

  constructor(problem: Partial<Problem>, status: number, retryAfter?: string | null) {
    super(problem.detail ?? problem.title ?? `HTTP ${status}`)
    this.name = 'ApiError'
    this.code = problem.code ?? (status === 401 ? 'UNAUTHENTICATED' : 'INTERNAL')
    this.status = status
    this.detail = problem.detail
    this.errors = problem.errors ?? []
    this.correlationId = problem.correlation_id
    this.retryAfter = retryAfter ? Number(retryAfter) : undefined
  }

  /** Mapa campo → código, para volcar a formularios. */
  fieldErrors(): Record<string, string> {
    return Object.fromEntries(this.errors.map((e) => [e.field, e.code]))
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

// Hooks de la app para errores transversales (401, onboarding, consentimiento, 429).
export type ApiErrorHandler = (error: ApiError) => void
const handlers: ApiErrorHandler[] = []
export function onApiError(handler: ApiErrorHandler) {
  handlers.push(handler)
  return () => handlers.splice(handlers.indexOf(handler), 1)
}

export interface ClientDeps {
  getToken: () => string | undefined
  baseUrl?: string
}

// Correlación: UUID por pestaña + sufijo por request.
const tabId = crypto.randomUUID().slice(0, 8)
let seq = 0
function correlationId() {
  seq += 1
  const suffix = seq.toString(16).padStart(4, '0')
  // UUID v4-like estable en formato: 8-4-4-4-12 (el backend exige UUID válido).
  return `${tabId}-0000-4000-8000-${suffix}00000000`
}

// Cliente tipado desde el contrato OpenAPI (ADR-005). Middlewares: Authorization,
// X-Correlation-ID e Idempotency-Key (si el llamador la pasa en headers),
// cabecera X-PP-Freshness de vistas derivadas (FE-014) y conversión de
// problem+json a ApiError.
export function createApiClient(deps: ClientDeps) {
  const client = createClient<paths>({ baseUrl: deps.baseUrl ?? runtimeConfig().apiBaseUrl })
  const auth: Middleware = {
    onRequest({ request }) {
      const token = deps.getToken()
      if (token) request.headers.set('Authorization', `Bearer ${token}`)
      if (!request.headers.has('X-Correlation-ID')) {
        request.headers.set('X-Correlation-ID', correlationId())
      }
      return request
    },
    async onResponse({ response }) {
      if (response.ok) {
        const lagSeconds = freshnessOf(response)
        if (lagSeconds !== null) emitFreshness({ url: response.url, lagSeconds })
        return response
      }
      let problem: Partial<Problem> = {}
      const type = response.headers.get('Content-Type') ?? ''
      if (type.includes('json')) {
        try {
          problem = (await response.clone().json()) as Partial<Problem>
        } catch {
          problem = {}
        }
      }
      const error = new ApiError(problem, response.status, response.headers.get('Retry-After'))
      for (const h of handlers) h(error)
      throw error
    },
  }
  client.use(auth)
  return client
}

export type ApiClient = ReturnType<typeof createApiClient>

let shared: ApiClient | null = null
let tokenProvider: () => string | undefined = () => undefined

/** Registra el proveedor de token (lo hace el sessionStore al arrancar). */
export function setTokenProvider(fn: () => string | undefined) {
  tokenProvider = fn
}

/** Cliente compartido de la app. */
export function api(): ApiClient {
  if (!shared) shared = createApiClient({ getToken: () => tokenProvider() })
  return shared
}

/** Solo pruebas: reinicia el cliente compartido. */
export function resetApiClient() {
  shared = null
  handlers.length = 0
  resetFreshnessListeners()
}
