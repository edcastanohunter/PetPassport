import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type AdminUserRow, type AuditEntry, type Organization } from '@/shared/api/client'

export type OrgStatus = Organization['status']
export type OrgAction = 'verify' | 'reject' | 'suspend' | 'reactivate'
export type UserAction = 'suspend' | 'reactivate'

/** Motivo mínimo de las acciones admin (PRD-017/018, AdminReasonRequest). */
export const MIN_REASON = 10

/** Acciones disponibles por estado (máquina de estados de docs/phase-1/03 §2). */
export const ORG_ACTIONS: Record<OrgStatus, readonly OrgAction[]> = {
  pending_verification: ['verify', 'reject'],
  active: ['suspend'],
  suspended: ['reactivate'],
  rejected: [],
}

export interface AuditFilter {
  actor_id?: string
  target_id?: string
  from?: string
  to?: string
}

// adminStore (FE-009): colas y búsquedas de la consola de super_admin con
// paginación por cursor. Cada acción la autoriza el backend (admin:*) y la
// registra en audit_log; el store solo guarda las vistas.
export const useAdminStore = defineStore('admin', () => {
  const orgs = ref<Organization[]>([])
  const orgsCursor = ref<string | null>(null)
  const users = ref<AdminUserRow[]>([])
  const usersCursor = ref<string | null>(null)
  const audit = ref<AuditEntry[]>([])
  const auditCursor = ref<string | null>(null)

  async function loadOrgs(status: OrgStatus, q = '', more = false) {
    const { data, error } = await api().GET('/admin/organizations', {
      params: {
        query: {
          status,
          q: q.trim() || undefined,
          cursor: more ? (orgsCursor.value ?? undefined) : undefined,
        },
      },
    })
    if (error) throw error
    orgs.value = more ? [...orgs.value, ...(data?.data ?? [])] : (data?.data ?? [])
    orgsCursor.value = data?.next_cursor ?? null
    return orgs.value
  }

  async function orgAction(orgId: string, action: OrgAction, reason: string) {
    const params = { path: { orgId } }
    const body = { reason: reason.trim() }
    const c = api()
    const { data, error } =
      action === 'verify'
        ? await c.POST('/admin/organizations/{orgId}/verify', {
            params,
            body: body.reason ? body : {},
          })
        : action === 'reject'
          ? await c.POST('/admin/organizations/{orgId}/reject', { params, body })
          : action === 'suspend'
            ? await c.POST('/admin/organizations/{orgId}/suspend', { params, body })
            : await c.POST('/admin/organizations/{orgId}/reactivate', { params, body })
    if (error) throw error
    // Sale de la pestaña actual: la cola refleja el nuevo estado al recargar.
    orgs.value = orgs.value.filter((o) => o.id !== orgId)
    return data as Organization
  }

  async function loadUsers(q = '', status?: AdminUserRow['status'], more = false) {
    const { data, error } = await api().GET('/admin/users', {
      params: {
        query: {
          q: q.trim() || undefined,
          status,
          cursor: more ? (usersCursor.value ?? undefined) : undefined,
        },
      },
    })
    if (error) throw error
    users.value = more ? [...users.value, ...(data?.data ?? [])] : (data?.data ?? [])
    usersCursor.value = data?.next_cursor ?? null
    return users.value
  }

  async function userAction(userId: string, action: UserAction, reason: string) {
    const init = { params: { path: { userId } }, body: { reason: reason.trim() } }
    const { data, error } =
      action === 'suspend'
        ? await api().POST('/admin/users/{userId}/suspend', init)
        : await api().POST('/admin/users/{userId}/reactivate', init)
    if (error) throw error
    const row = data as AdminUserRow
    users.value = users.value.map((u) => (u.id === userId ? row : u))
    return row
  }

  async function loadAudit(filter: AuditFilter, more = false) {
    const clean = Object.fromEntries(
      Object.entries(filter).filter(([, v]) => typeof v === 'string' && v.trim() !== ''),
    ) as AuditFilter
    const { data, error } = await api().GET('/admin/audit-log', {
      params: {
        query: { ...clean, cursor: more ? (auditCursor.value ?? undefined) : undefined },
      },
    })
    if (error) throw error
    audit.value = more ? [...audit.value, ...(data?.data ?? [])] : (data?.data ?? [])
    auditCursor.value = data?.next_cursor ?? null
    return audit.value
  }

  return {
    orgs,
    orgsCursor,
    users,
    usersCursor,
    audit,
    auditCursor,
    loadOrgs,
    orgAction,
    loadUsers,
    userAction,
    loadAudit,
  }
})
