import { expect, test, type Browser, type Page } from "@playwright/test";
import {
  completeOnboarding,
  createPet,
  expectAccessible,
  loginConfiguringTotp,
  signIn,
} from "../support/app";
import { runId, runTag, uniqueEmail } from "../support/env";
import { createUser } from "../support/keycloak";
import { waitForLink } from "../support/mailpit";

// PP-F2-QA-004 escenario 4: vínculo por solicitud (PRD-020, FE-008, FE-012).
// Clínica busca por nombre del tutor → solicita → el tutor aprueba desde la
// bandeja → la clínica ve la ficha → el tutor revoca. Prepara su propia clínica
// verificada para no depender del orden de las specs de F1.
test.describe.configure({ mode: "serial" });

const clinicName = `Clínica F2 ${runId}`;
const microchip = `98200${Date.now().toString().slice(-10)}`;

async function newPage(browser: Browser): Promise<Page> {
  return (await browser.newContext()).newPage();
}

async function verifiedClinic(browser: Browser) {
  const founder = await createUser(uniqueEmail("f2founder"), "Fundadora");
  const page = await newPage(browser);
  await signIn(page, founder, { path: "org" });
  await page.locator('input[name="org-type"][value="clinic"]').check();
  await page.getByTestId("org-next").click();
  await page.locator("#org-legal").fill(`${clinicName} S.A.S.`);
  await page.locator("#org-display").fill(clinicName);
  await page.locator("#org-tax-country").selectOption("MX");
  await page
    .locator("#org-tax-value")
    .fill(`F2${runId}`.toUpperCase().slice(0, 20));
  await page.getByTestId("org-next").click();
  await page.locator("#org-country").selectOption("CO");
  await page.locator("#org-city").fill("Bogotá");
  await page.locator("#org-line1").fill("Calle 1 # 2-3");
  await page.getByTestId("org-next").click();
  await page.locator("#org-phone").fill("+576015550100");
  await page.getByTestId("org-next").click();
  await page.getByTestId("org-next").click();
  await page.getByTestId("org-created").getByRole("button").click();
  await page.waitForURL(/\/org\/[0-9a-f-]{36}$/);
  const orgId = page.url().split("/").pop()!;

  const admin = await createUser(
    uniqueEmail("f2admin"),
    "Admin",
    "super_admin",
  );
  const adminPage = await newPage(browser);
  await loginConfiguringTotp(adminPage, admin);
  await completeOnboarding(adminPage, {
    displayName: "Admin E2E",
    path: "owner",
  });
  await adminPage.goto("/admin/organizations");
  await adminPage.getByTestId("tab-pending_verification").click();
  await adminPage
    .getByTestId("org-row")
    .filter({ hasText: clinicName })
    .click();
  await adminPage.getByTestId("action-verify").click();
  await adminPage.getByTestId("confirm-reason").click();
  await expect(adminPage.getByTestId("reason-dialog")).toBeHidden();
  await adminPage.context().close();
  await page.reload();
  return { page, orgId };
}

test("PP-F2-QA-004 escenario 4: solicitud de vínculo aprobada por el tutor y revocada", async ({
  browser,
}) => {
  const { page: clinicPage, orgId } = await verifiedClinic(browser);
  const owner = await createUser(uniqueEmail("f2owner"), "Marta");
  const ownerPage = await newPage(browser);
  const petName = `Kira ${runTag}`;

  await signIn(ownerPage, owner, { phone: "+573001234567" });
  const petId = await createPet(ownerPage, petName);
  expect(petId).toMatch(/^[0-9a-f-]{36}$/);
  // El microchip se asigna desde la ficha (DOM de F1).
  await ownerPage.getByTestId("edit-microchip").click();
  await ownerPage.getByLabel("Microchip", { exact: true }).fill(microchip);
  await ownerPage.getByRole("button", { name: "Guardar" }).click();
  await expect(ownerPage.getByText(microchip)).toBeVisible();

  // La clínica busca por microchip: solo ve el tutor enmascarado.
  await clinicPage.goto(`/org/${orgId}/patients`);
  const panel = clinicPage.getByTestId("carelink-request-panel");
  await panel.locator("#lookup-term").fill(microchip);
  await panel.locator('form[role="search"]').press("Enter");
  const result = panel.getByTestId("lookup-result");
  await expect(result).toContainText(petName);
  await expect(result).not.toContainText(owner.email);
  await result.click();
  await panel.locator("#request-message").fill("Atenderemos a su mascota");
  await panel.getByTestId("request-send").click();
  await expect(panel.getByTestId("request-sent")).toBeVisible();
  await expectAccessible(clinicPage, "carelink-request");

  // El tutor recibe el correo y aprueba desde la bandeja.
  await waitForLink(owner.email, /https?:\/\/[^\s"'<>]+\/app\/inbox/);
  await ownerPage.goto("/app/inbox");
  const item = ownerPage
    .getByTestId("inbox-carelink")
    .filter({ hasText: clinicName });
  await expect(item).toBeVisible();
  await expectAccessible(ownerPage, "inbox");
  await ownerPage.getByTestId("inbox-approve").first().click();
  await expect(ownerPage.getByTestId("inbox-done")).toBeVisible();

  // La clínica ya ve la ficha; el tutor revoca y deja de verla.
  await clinicPage.goto(`/org/${orgId}/patients`);
  await expect(
    clinicPage.getByTestId("patient-row").filter({ hasText: petName }),
  ).toBeVisible();

  await ownerPage.goto(`/app/pets/${petId}`);
  const links = ownerPage.getByTestId("care-links");
  await links.getByTestId("revoke-link").click();
  await links.getByTestId("confirm-revoke").click();
  await expect(links).not.toContainText(clinicName);
  await clinicPage.goto(`/org/${orgId}/patients`);
  await expect(
    clinicPage.getByTestId("patient-row").filter({ hasText: petName }),
  ).toHaveCount(0);

  await ownerPage.context().close();
  await clinicPage.context().close();
});

// Escenarios 1–3 y 5 de PP-F2-QA-004: requieren datos de agenda/consulta/carnet
// preparados por la clínica; quedan documentados y marcados para completar con
// el entorno de staging (ver docs/tracking/phase-2.md).
test.fixme("PP-F2-QA-004 escenario 1: cita reservada, propuesta alternativa y aceptación", () => {});
test.fixme("PP-F2-QA-004 escenario 2: consulta firmada y enmendada por el veterinario", () => {});
test.fixme("PP-F2-QA-004 escenario 3: vacuna en el carnet y descarga del PDF", () => {});
test.fixme("PP-F2-QA-004 escenario 5: correos de recordatorio en Mailpit y baja", () => {});
