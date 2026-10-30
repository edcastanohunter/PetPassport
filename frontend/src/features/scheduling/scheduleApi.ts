import {
  api,
  type Agenda,
  type AgendaDayException,
  type AgendaTimeRange,
  type Appointment,
  type AppointmentStatus,
  type AppointmentType,
  type AvailabilitySlot,
  type SlotPreference,
} from '@/shared/api/client'

// Llamadas de citas y agendas (API-007/008, M2.3). Los comandos que crean
// llevan Idempotency-Key (ARQ-008); `must` propaga el problem+json tal cual.

function must<T>(res: { data?: T; error?: unknown }): T {
  if (res.error) throw res.error
  return res.data as T
}

export interface Page<T> {
  data: T[]
  next_cursor?: string | null
}

// ---- tutor

export async function listMyAppointments(
  q: { status?: AppointmentStatus; pet_id?: string; cursor?: string } = {},
): Promise<Page<Appointment>> {
  return must(await api().GET('/appointments', { params: { query: q } }))
}

export interface RequestInput {
  pet_id: string
  organization_id: string
  type: AppointmentType
  reason?: string
  preferences: SlotPreference[]
}

export async function requestAppointment(key: string, body: RequestInput): Promise<Appointment> {
  return must(
    await api().POST('/appointments', { params: { header: { 'Idempotency-Key': key } }, body }),
  )
}

export async function acceptProposal(id: string): Promise<Appointment> {
  return must(
    await api().POST('/appointments/{appointmentId}/accept-proposal', {
      params: { path: { appointmentId: id } },
    }),
  )
}

export async function cancelMyAppointment(id: string, reason?: string): Promise<Appointment> {
  return must(
    await api().POST('/appointments/{appointmentId}/cancel', {
      params: { path: { appointmentId: id } },
      body: { reason },
    }),
  )
}

// ---- clínica

export interface OrgListQuery {
  status?: AppointmentStatus
  professional_id?: string
  pet_id?: string
  from?: string
  to?: string
  cursor?: string
}

export async function listOrgAppointments(
  orgId: string,
  query: OrgListQuery = {},
): Promise<Page<Appointment>> {
  return must(
    await api().GET('/organizations/{orgId}/appointments', {
      params: { path: { orgId }, query },
    }),
  )
}

export interface CreateInput {
  pet_id: string
  professional_id: string
  type: AppointmentType
  reason?: string
  start: string
  end: string
}

export async function createOrgAppointment(
  orgId: string,
  key: string,
  body: CreateInput,
): Promise<Appointment> {
  return must(
    await api().POST('/organizations/{orgId}/appointments', {
      params: { path: { orgId }, header: { 'Idempotency-Key': key } },
      body,
    }),
  )
}

export interface SlotInput {
  professional_id?: string
  start: string
  end: string
}

type OrgAction = 'confirm' | 'propose' | 'reject' | 'cancel' | 'check-in' | 'no-show' | 'complete'

// Acciones de la clínica sobre una cita (cada una un comando del bus).
export async function orgAction(
  orgId: string,
  id: string,
  action: OrgAction,
  body?: SlotInput | { reason?: string },
): Promise<Appointment> {
  const params = { path: { orgId, appointmentId: id } }
  switch (action) {
    case 'confirm':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/confirm', {
          params,
          body: body as SlotInput,
        }),
      )
    case 'propose':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/propose', {
          params,
          body: body as SlotInput,
        }),
      )
    case 'reject':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/reject', {
          params,
          body: body as { reason?: string },
        }),
      )
    case 'cancel':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/cancel', {
          params,
          body: body as { reason?: string },
        }),
      )
    case 'check-in':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/check-in', {
          params,
        }),
      )
    case 'no-show':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/no-show', {
          params,
        }),
      )
    case 'complete':
      return must(
        await api().POST('/organizations/{orgId}/appointments/{appointmentId}/complete', {
          params,
        }),
      )
  }
}

export async function reschedule(
  orgId: string,
  id: string,
  version: number,
  body: SlotInput,
): Promise<Appointment> {
  return must(
    await api().POST('/organizations/{orgId}/appointments/{appointmentId}/reschedule', {
      params: { path: { orgId, appointmentId: id }, header: { 'If-Match': version } },
      body,
    }),
  )
}

// ---- agendas y disponibilidad

export async function listAgendas(orgId: string): Promise<Agenda[]> {
  return must(await api().GET('/organizations/{orgId}/agendas', { params: { path: { orgId } } }))
    .data
}

export interface AgendaInput {
  time_zone?: string
  weekly_template: Record<string, AgendaTimeRange[]>
  slot_minutes: Record<string, number>
  lead_time_min_hours: number
  lead_time_max_days: number
  cancellation_policy_hours: number
  active: boolean
}

export async function putAgenda(
  orgId: string,
  professionalId: string,
  body: AgendaInput,
  version?: number,
): Promise<Agenda> {
  return must(
    await api().PUT('/organizations/{orgId}/agendas/{professionalId}', {
      params: {
        path: { orgId, professionalId },
        header: version ? { 'If-Match': version } : {},
      },
      body,
    }),
  )
}

export async function addException(
  orgId: string,
  professionalId: string,
  date: string,
  exception: AgendaDayException,
): Promise<Agenda> {
  return must(
    await api().POST('/organizations/{orgId}/agendas/{professionalId}/exceptions', {
      params: { path: { orgId, professionalId } },
      body: { date, closed: exception.closed, ranges: exception.ranges },
    }),
  )
}

export async function removeException(
  orgId: string,
  professionalId: string,
  date: string,
): Promise<Agenda> {
  return must(
    await api().DELETE('/organizations/{orgId}/agendas/{professionalId}/exceptions/{date}', {
      params: { path: { orgId, professionalId, date } },
    }),
  )
}

export async function getAvailability(
  orgId: string,
  q: { from: string; to: string; type: AppointmentType; professional_id?: string },
): Promise<AvailabilitySlot[]> {
  return must(
    await api().GET('/organizations/{orgId}/availability', {
      params: { path: { orgId }, query: q },
    }),
  ).data
}
