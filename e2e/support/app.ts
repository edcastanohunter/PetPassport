import { expect, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import type { TestUser } from './keycloak'
import { totp } from './totp'

// Pasos de UI reutilizados por los flujos críticos (QA-005). Se usan ids de
// Keycloak (tema petpassport sobre keycloak.v2) y data-testid de la SPA, nunca
// textos: la suite es independiente del idioma.

/** Rellena el formulario de login de Keycloak (y la OTP si se pide). */
export async function keycloakLogin(page: Page, user: TestUser, otpSecret?: string) {
  await page.locator('#username').fill(user.email)
  await page.locator('#password').fill(user.password)
  await page.locator('#kc-login').click()
  if (otpSecret) {
    await page.locator('#otp').fill(totp(otpSecret))
    await page.locator('#kc-login').click()
  }
}

/** Desde la home: login completo hasta volver a la SPA. */
export async function login(page: Page, user: TestUser, otpSecret?: string) {
  await page.goto('/')
  await page.getByTestId('login').click()
  await keycloakLogin(page, user, otpSecret)
  await page.waitForURL((url) => url.hostname.startsWith('app.') && !url.pathname.startsWith('/auth'))
}

/**
 * Primer login de un super_admin: Keycloak exige CONFIGURE_TOTP (IAM-005). Lee el
 * secreto en modo manual, calcula el código y devuelve el secreto para logins futuros.
 */
export async function loginConfiguringTotp(page: Page, user: TestUser): Promise<string> {
  await page.goto('/')
  await page.getByTestId('login').click()
  await keycloakLogin(page, user)
  await page.locator('#mode-manual').click()
  const secret = (await page.locator('#kc-totp-secret-key').innerText()).replace(/\s/g, '')
  await page.locator('#totp').fill(totp(secret))
  await page.locator('#userLabel').fill('e2e')
  await page.locator('#saveTOTPBtn').click()
  await page.waitForURL((url) => url.hostname.startsWith('app.') && !url.pathname.startsWith('/auth'))
  return secret
}

export interface OnboardingOptions {
  displayName: string
  phone?: string
  path?: 'owner' | 'org'
}

/** Onboarding de 3 pasos (PRD-003): consentimiento, datos básicos y camino. */
export async function completeOnboarding(page: Page, o: OnboardingOptions) {
  await expect(page).toHaveURL(/\/onboarding/)
  await expect(page.getByTestId('step-consent')).toBeVisible()
  await expectAccessible(page, 'onboarding')
  await page.getByTestId('accept-terms').check()
  await page.getByTestId('accept-privacy').check()
  await page.getByTestId('next').click()
  await expect(page.getByTestId('step-basics')).toBeVisible()
  await page.locator('#display_name').fill(o.displayName)
  await page.locator('#country').selectOption('CO')
  if (o.phone) await page.locator('#phone').fill(o.phone)
  await page.getByTestId('next').click()
  await expect(page.getByTestId('step-path')).toBeVisible()
  await page.getByTestId(o.path === 'org' ? 'path-org' : 'path-owner').click()
  await expect(page).not.toHaveURL(/\/onboarding/)
}

/** Login + onboarding de un usuario recién creado. */
export async function signIn(page: Page, user: TestUser, o: Omit<OnboardingOptions, 'displayName'> = {}) {
  await login(page, user)
  await completeOnboarding(page, { displayName: user.name, ...o })
}

/** Crea una mascota desde la lista y devuelve su id (queda en el detalle). */
export async function createPet(page: Page, name: string): Promise<string> {
  await page.goto('/app/pets')
  await page.getByTestId('new-pet').click()
  await expectAccessible(page, 'pet-form')
  await page.locator('#pet-name').fill(name)
  await page.locator('#pet-species').selectOption('dog')
  await page.locator('#pet-sex').selectOption('female')
  await page.locator('#pet-neutered').selectOption('no')
  await page.getByTestId('submit-pet').click()
  await page.waitForURL(/\/app\/pets\/[0-9a-f-]{36}$/)
  return page.url().split('/').pop()!
}

/**
 * Smoke de accesibilidad (QA-005): sin violaciones serious/critical de WCAG 2.1
 * A/AA en la pantalla actual.
 */
export async function expectAccessible(page: Page, screen: string) {
  await page.waitForLoadState('networkidle')
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  const blocking = results.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id} (${v.impact}): ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)
  expect(blocking, `axe en ${screen}`).toEqual([])
}
