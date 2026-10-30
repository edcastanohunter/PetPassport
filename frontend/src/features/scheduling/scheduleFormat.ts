import type { Appointment, AppointmentStatus } from '@/shared/api/client'
import { isApiError } from '@/shared/api/client'

// Utilidades de fecha/hora de la agenda. Las franjas viajan en UTC; se
// muestran en la zona horaria de la agenda (la de la clínica), no en la del
// navegador, para que tutor y clínica vean la misma hora (A6, ADR-011).

export function fmtTime(iso: string, tz: string | undefined, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    hour: '2-digit',
    minute: '2-digit',
    timeZone: tz || undefined,
  }).format(new Date(iso))
}

export function fmtDay(iso: string, tz: string | undefined, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    timeZone: tz || undefined,
  }).format(new Date(iso))
}

export function fmtDateTime(iso: string, tz: string | undefined, locale: string): string {
  return `${fmtDay(iso, tz, locale)} ${fmtTime(iso, tz, locale)}`
}

// Clave de día local YYYY-MM-DD en la zona dada (para agrupar).
export function dayKey(iso: string, tz: string | undefined): string {
  return new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    timeZone: tz || undefined,
  }).format(new Date(iso))
}

export function startOfWeek(d: Date): Date {
  const x = new Date(d)
  const day = (x.getDay() + 6) % 7 // lunes = 0
  x.setHours(0, 0, 0, 0)
  x.setDate(x.getDate() - day)
  return x
}

export function addDays(d: Date, n: number): Date {
  const x = new Date(d)
  x.setDate(x.getDate() + n)
  return x
}

export const ACTIVE_STATUSES: AppointmentStatus[] = ['proposed', 'confirmed', 'checked_in']

// Mensaje traducible para un error de la API de citas: usa `errors.<CODE>` si
// existe y cae en el genérico.
export function scheduleErrorKey(e: unknown, te: (k: string) => boolean): string {
  if (isApiError(e) && te(`errors.${e.code}`)) return `errors.${e.code}`
  return 'errors.GENERIC'
}

export function isConflict(e: unknown): boolean {
  return isApiError(e) && (e.code === 'SLOT_UNAVAILABLE' || e.code === 'CONFLICT_VERSION')
}

// La cita tiene franja asignada (confirmada, propuesta o posterior).
export function slotOf(a: Appointment): { start: string; end: string } | null {
  return a.start && a.end ? { start: a.start, end: a.end } : null
}

// Cancelar dentro de la ventana de política es «tardía» (PRD-018, sin penalización).
export function wouldBeLate(a: Appointment, now = new Date()): boolean {
  if (!a.start || !a.cancellation_policy_hours) return false
  return new Date(a.start).getTime() - now.getTime() < a.cancellation_policy_hours * 3600_000
}
