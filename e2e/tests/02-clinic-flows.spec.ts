import { expect, test, type Browser, type Page } from '@playwright/test'
import {
  completeOnboarding,
  createPet,
  expectAccessible,
  loginConfiguringTotp,
  signIn,
} from '../support/app'
import { runId, uniqueEmail } from '../support/env'
import { createUser, type TestUser } from '../support/keycloak'
import { waitForLink } from '../support/mailpit'

// PP-F1-QA-005 flujos 2–4. Comparten la clínica verificada, como en la spec:
// 2. registrar clínica → SuperAdmin verifica → código público visible;
// 3. invitar veterinario → aceptar (segunda sesión) → aparece como miembro;
// 4. tutor vincula por código → el veterinario ve paciente y teléfono → el
//    tutor revoca → el veterinario deja de verlo.
test.describe.configure({ mode: 'serial' })

const clinicName = `Clínica E2E ${runId}`
const ownerPhone = '+573001234567'
let founderPage: Page
let vetPage: Page
let orgId = ''
let publicCode = ''
let vet: TestUser

async function newPage(browser: Browser) {
  return (await browser.newContext()).newPage()
}

test.afterAll(async () => {
  await founderPage?.context().close()
  await vetPage?.context().close()
})

test('PP-F1-QA-005 flujo 2: registro de clínica y verificación por SuperAdmin', async ({
  browser,
}) => {
  const founder = await createUser(uniqueEmail('founder'), 'Fundadora')
  founderPage = await newPage(browser)
  const page = founderPage
  await signIn(page, founder, { path: 'org' })
  await expect(page).toHaveURL(/\/org\/new/)
  await expectAccessible(page, 'org-register')

  // Asistente de 5 pasos (PRD-011).
  await page.locator('input[name="org-type"][value="clinic"]').check()
  await page.getByTestId('org-next').click()
  await page.locator('#org-legal').fill(`${clinicName} S.A.S.`)
  await page.locator('#org-display').fill(clinicName)
  await page.locator('#org-tax-country').selectOption('MX')
  await page.locator('#org-tax-value').fill(`E2E${runId}`.toUpperCase().slice(0, 20))
  await page.getByTestId('org-next').click()
  await page.locator('#org-country').selectOption('CO')
  await page.locator('#org-city').fill('Bogotá')
  await page.locator('#org-line1').fill('Calle 1 # 2-3')
  await page.getByTestId('org-next').click()
  await page.locator('#org-phone').fill('+576015550100')
  await page.getByTestId('org-next').click()
  await expect(page.getByTestId('org-review')).toBeVisible()
  await page.getByTestId('org-next').click()
  await expect(page.getByTestId('org-created')).toBeVisible()
  await page.getByTestId('org-created').getByRole('button').click()
  await page.waitForURL(/\/org\/[0-9a-f-]{36}$/)
  orgId = page.url().split('/').pop()!
  await expect(page.getByTestId('org-status-banner')).toBeVisible()

  // SuperAdmin con OTP obligatoria (IAM-005) verifica la organización (PRD-017).
  const admin = await createUser(uniqueEmail('admin'), 'Admin', 'super_admin')
  const adminPage = await newPage(browser)
  await loginConfiguringTotp(adminPage, admin)
  await completeOnboarding(adminPage, { displayName: 'Admin E2E', path: 'owner' })
  await adminPage.goto('/admin/organizations')
  await adminPage.getByTestId('tab-pending_verification').click()
  await adminPage.getByTestId('org-row').filter({ hasText: clinicName }).click()
  await expect(adminPage.getByTestId('org-detail')).toBeVisible()
  await expectAccessible(adminPage, 'admin-organizations')
  await adminPage.getByTestId('action-verify').click()
  await expect(adminPage.getByTestId('reason-dialog')).toBeVisible()
  await adminPage.getByTestId('confirm-reason').click()
  await expect(adminPage.getByTestId('reason-dialog')).toBeHidden()
  await adminPage.context().close()

  // El fundador ve la organización activa y su código público.
  await page.reload()
  await page.waitForURL(new RegExp(`/org/${orgId}$`))
  await expect(page.getByTestId('public-code-card')).toBeVisible()
  publicCode = (await page.getByTestId('public-code-card').locator('.font-mono').innerText()).trim()
  expect(publicCode).toMatch(/^[0-9A-Z-]{4,}$/)
  await expectAccessible(page, 'org-summary')
})

test('PP-F1-QA-005 flujo 3: invitar veterinario y aceptar la invitación', async ({ browser }) => {
  test.skip(!orgId, 'requiere la clínica del flujo 2')
  vet = await createUser(uniqueEmail('vet'), 'Carlos')

  const page = founderPage
  await page.goto(`/org/${orgId}/members`)
  await page.getByTestId('tab-invitations').click()
  await page.locator('#invite-email').fill(vet.email)
  await page.locator('#invite-role').selectOption('veterinarian')
  await page.getByTestId('send-invitation').click()
  await expect(page.getByTestId('invitation-row').filter({ hasText: vet.email })).toBeVisible()
  await expectAccessible(page, 'org-members')

  // Segunda sesión: el veterinario acepta desde el enlace del correo (PRD-013).
  const link = await waitForLink(vet.email, /https?:\/\/[^\s"'<>]+\/invitations\/[A-Za-z0-9_-]+/)
  vetPage = await newPage(browser)
  await signIn(vetPage, vet)
  await vetPage.goto(link)
  await vetPage.getByTestId('accept-invitation').click()
  await vetPage.waitForURL(new RegExp(`/org/${orgId}$`))

  await page.goto(`/org/${orgId}/members`)
  await page.getByTestId('tab-members').click()
  await expect(page.getByTestId('member-row').filter({ hasText: vet.email })).toBeVisible()
})

test('PP-F1-QA-005 flujo 4: vincular mascota por código, ver como clínica y revocar', async ({
  browser,
}) => {
  test.skip(!publicCode || !vetPage, 'requiere los flujos 2 y 3')
  const owner = await createUser(uniqueEmail('linkowner'), 'Marta')
  const ownerPage = await newPage(browser)
  const petName = `Toby ${runId.slice(0, 4)}`

  await signIn(ownerPage, owner, { phone: ownerPhone })
  await createPet(ownerPage, petName)
  const links = ownerPage.getByTestId('care-links')
  await links.getByTestId('link-clinic').click()
  await ownerPage.locator('#link-code').fill(publicCode)
  await ownerPage.locator('#link-code').press('Enter')
  await expect(ownerPage.getByTestId('link-preview')).toContainText(clinicName)
  await ownerPage.getByTestId('confirm-link').click()
  await expect(links).toContainText(clinicName)

  // El veterinario ve el paciente y el teléfono del tutor (PRD-016).
  await vetPage.goto(`/org/${orgId}/patients`)
  const row = vetPage.getByTestId('patient-row').filter({ hasText: petName })
  await expect(row).toBeVisible()
  await expectAccessible(vetPage, 'patients')
  await row.getByRole('link', { name: petName }).click()
  await expect(vetPage.getByTestId('guardian-contact')).toContainText(ownerPhone.slice(-4))
  await expectAccessible(vetPage, 'patient-detail')
  const patientUrl = vetPage.url()

  // El tutor revoca (PRD-015) y la clínica deja de verlo.
  await links.getByTestId('revoke-link').click()
  await links.getByTestId('confirm-revoke').click()
  await expect(links).not.toContainText(clinicName)

  await vetPage.goto(`/org/${orgId}/patients`)
  await expect(vetPage.getByTestId('patient-row').filter({ hasText: petName })).toHaveCount(0)
  await vetPage.goto(patientUrl)
  await expect(vetPage.getByTestId('guardian-contact')).toHaveCount(0)
  await ownerPage.context().close()
})
