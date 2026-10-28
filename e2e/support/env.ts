// Endpoints del entorno de desarrollo (infra/quadlet/dev). Se pueden
// sobrescribir por entorno para apuntar a otro despliegue con Mailpit.
export const env = {
  keycloakAdminUrl: process.env.E2E_KC_ADMIN_URL ?? 'http://localhost:8180',
  keycloakAdminUser: process.env.KC_ADMIN_USER ?? 'admin',
  keycloakAdminPassword: process.env.KC_ADMIN_PASSWORD ?? 'admin',
  realm: 'petpassport',
  mailpitUrl: process.env.E2E_MAILPIT_URL ?? 'http://localhost:8025',
  password: process.env.E2E_PASSWORD ?? 'E2e-Password-2026!',
}

/** Sufijo único por ejecución: los flujos no dependen de datos previos. */
export const runId = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`

// Sufijo solo con letras para campos que las reglas de nombre restringen (PRD-002).
export const runTag = runId.replace(/[0-9]/g, (d) => 'abcdefghij'[Number(d)]!).slice(-6)

export function uniqueEmail(role: string) {
  return `e2e-${role}-${runId}@e2e.petpassport.local`
}
