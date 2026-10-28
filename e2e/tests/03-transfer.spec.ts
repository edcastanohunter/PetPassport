import { expect, test } from '@playwright/test'
import { createPet, expectAccessible, signIn } from '../support/app'
import { runId, uniqueEmail } from '../support/env'
import { createUser } from '../support/keycloak'
import { waitForLink } from '../support/mailpit'

// PP-F1-QA-005 flujo 5: transferencia de tutela entre dos usuarios (PRD-010).
test('PP-F1-QA-005 flujo 5: transferencia de tutela con aceptación', async ({ browser }) => {
  const from = await createUser(uniqueEmail('from'), 'Lucía')
  const to = await createUser(uniqueEmail('to'), 'Pedro')
  const petName = `Nala ${runId.slice(0, 4)}`

  const fromPage = await (await browser.newContext()).newPage()
  await signIn(fromPage, from)
  const petId = await createPet(fromPage, petName)
  const panel = fromPage.getByTestId('transfer')
  await panel.getByTestId('start-transfer').click()
  await fromPage.locator('#transfer-email').fill(to.email)
  await panel.getByTestId('send-transfer').click()
  await expect(panel.getByTestId('transfer-pending')).toBeVisible()

  const link = await waitForLink(to.email, /https?:\/\/[^\s"'<>]+\/app\/transfers\/[A-Za-z0-9_-]+/)
  const toPage = await (await browser.newContext()).newPage()
  await signIn(toPage, to)
  await toPage.goto(link)
  await expectAccessible(toPage, 'accept-transfer')
  await toPage.getByTestId('accept-transfer').click()
  await toPage.waitForURL(new RegExp(`/app/pets/${petId}$`))

  // El receptor la tiene en su lista; el tutor anterior ya no.
  await toPage.goto('/app/pets')
  await expect(toPage.getByTestId('pet-card').filter({ hasText: petName })).toBeVisible()
  await fromPage.goto('/app/pets')
  await expect(fromPage.getByTestId('pet-card').filter({ hasText: petName })).toHaveCount(0)

  await fromPage.context().close()
  await toPage.context().close()
})
