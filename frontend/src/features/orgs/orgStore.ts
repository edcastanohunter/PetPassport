import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type Invitation, type Member, type Organization } from '@/shared/api/client'

// orgStore (FE-007): organización activa del contexto Org, miembros e
// invitaciones. La autoridad es el backend (org:read / org:member:*); el
// store guarda las vistas y la versión para If-Match.
export const useOrgStore = defineStore('orgs', () => {
  const current = ref<Organization | null>(null)
  const members = ref<Member[]>([])
  const invitations = ref<Invitation[]>([])

  async function load(orgId: string): Promise<Organization> {
    const { data, error } = await api().GET('/organizations/{orgId}', {
      params: { path: { orgId } },
    })
    if (error) throw error
    // Los miembros reciben la vista completa (con public_code y version).
    current.value = data as Organization
    return current.value
  }

  async function loadMembers(orgId: string) {
    const { data, error } = await api().GET('/organizations/{orgId}/members', {
      params: { path: { orgId } },
    })
    if (error) throw error
    members.value = data?.data ?? []
    return members.value
  }

  async function loadInvitations(orgId: string) {
    const { data, error } = await api().GET('/organizations/{orgId}/invitations', {
      params: { path: { orgId } },
    })
    if (error) throw error
    invitations.value = data?.data ?? []
    return invitations.value
  }

  /** Número de admins activos (O2: el último no se puede retirar ni degradar). */
  function adminCount() {
    return members.value.filter((m) => m.role === 'clinic_admin' || m.role === 'pharmacy_admin')
      .length
  }

  return { current, members, invitations, load, loadMembers, loadInvitations, adminCount }
})
