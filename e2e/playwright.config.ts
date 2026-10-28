import { defineConfig, devices } from '@playwright/test'

// E2E de flujos críticos (PP-F1-QA-005) contra el entorno completo de desarrollo
// (Keycloak real, Mailpit para leer correos). Ver e2e/README.md.
export default defineConfig({
  testDir: './tests',
  // Keycloak + correo: los flujos son lentos y 2→3→4 comparten la clínica.
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never' }], ['junit', { outputFile: 'results/junit.xml' }]]
    : [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_APP_URL ?? 'https://app.pp.localhost',
    // Certificados mkcert de desarrollo: no se depende de que la CA esté instalada.
    ignoreHTTPSErrors: true,
    locale: 'es-CO',
    timezoneId: 'America/Bogota',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
