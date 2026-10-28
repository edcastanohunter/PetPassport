import { env } from './env'

// Admin API de Keycloak para preparar usuarios ya verificados: los flujos que no
// prueban el registro no lo repiten (el flujo 1 sí registra por la UI).
async function adminToken(): Promise<string> {
  const res = await fetch(`${env.keycloakAdminUrl}/realms/master/protocol/openid-connect/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'password',
      client_id: 'admin-cli',
      username: env.keycloakAdminUser,
      password: env.keycloakAdminPassword,
    }),
  })
  if (!res.ok) throw new Error(`keycloak admin token: ${res.status}`)
  return ((await res.json()) as { access_token: string }).access_token
}

async function admin(path: string, init: RequestInit = {}) {
  const token = await adminToken()
  const res = await fetch(`${env.keycloakAdminUrl}/admin/realms/${env.realm}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      ...init.headers,
    },
  })
  if (!res.ok) {
    throw new Error(`keycloak ${init.method ?? 'GET'} ${path}: ${res.status} ${await res.text()}`)
  }
  return res
}

export interface TestUser {
  email: string
  password: string
  name: string
}

/** Crea un usuario verificado con contraseña y, opcionalmente, un rol de realm. */
export async function createUser(email: string, name: string, realmRole?: string): Promise<TestUser> {
  await admin('/users', {
    method: 'POST',
    body: JSON.stringify({
      username: email,
      email,
      emailVerified: true,
      enabled: true,
      firstName: name,
      lastName: 'E2E',
      credentials: [{ type: 'password', value: env.password, temporary: false }],
    }),
  })
  if (realmRole) {
    const users = (await (
      await admin(`/users?email=${encodeURIComponent(email)}&exact=true`)
    ).json()) as { id: string }[]
    const role = await (await admin(`/roles/${realmRole}`)).json()
    await admin(`/users/${users[0]!.id}/role-mappings/realm`, {
      method: 'POST',
      body: JSON.stringify([role]),
    })
  }
  return { email, password: env.password, name }
}
