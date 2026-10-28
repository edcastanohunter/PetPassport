import { expect, test } from '@playwright/test'
import { completeOnboarding, createPet, expectAccessible } from '../support/app'
import { env, runId, uniqueEmail } from '../support/env'
import { waitForLink } from '../support/mailpit'

// PP-F1-QA-005 flujo 1: registro de tutor → verificación por email (Mailpit) →
// onboarding → crear mascota → verla en la lista.
test('PP-F1-QA-005 flujo 1: registro de tutor hasta su primera mascota', async ({ page }) => {
  const email = uniqueEmail('owner')
  const petName = `Luna ${runId.slice(0, 4)}`

  await page.goto('/')
  await expectAccessible(page, 'home')
  await page.getByTestId('login').click()

  // Registro en Keycloak (PRD-001).
  await page.locator('a[href*="/login-actions/registration"]').click()
  await page.locator('#email').fill(email)
  await page.locator('#password').fill(env.password)
  await page.locator('#password-confirm').fill(env.password)
  await page.locator('#firstName').fill('Ana')
  await page.locator('#lastName').fill('E2E')
  await page.locator('#kc-register-form [type="submit"]').click()

  // Verificación obligatoria: el enlace llega por correo.
  const verify = await waitForLink(
    email,
    /https?:\/\/[^\s"'<>]+\/login-actions\/action-token\?[^\s"'<>]+/,
  )
  await page.goto(verify)
  // Keycloak continúa el flujo en el mismo navegador; si muestra la página
  // intermedia ("volver a la aplicación"), se sigue el enlace.
  const back = page.locator('#kc-info-message a, #kc-content a[href*="app."]')
  await Promise.race([
    page.waitForURL(/\/onboarding/, { timeout: 30_000 }),
    back.first().waitFor({ timeout: 30_000 }).then(() => back.first().click()),
  ])
  await page.waitForURL(/\/onboarding/)

  await completeOnboarding(page, { displayName: 'Ana E2E', path: 'owner' })

  await createPet(page, petName)
  await expectAccessible(page, 'pet-detail')
  await page.goto('/app/pets')
  await expect(page.getByTestId('pet-card').filter({ hasText: petName })).toBeVisible()
  await expectAccessible(page, 'pets')
})
