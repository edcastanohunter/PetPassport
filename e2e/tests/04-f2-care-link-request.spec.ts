import { expect, test } from "@playwright/test";
import { createPet, expectAccessible, signIn } from "../support/app";
import { newPage, verifiedClinic } from "../support/clinic";
import { runId, runTag, uniqueEmail } from "../support/env";
import { createUser } from "../support/keycloak";
import { waitForLink } from "../support/mailpit";

// PP-F2-QA-004 escenario 4: vÃ­nculo por solicitud (PRD-020, FE-008, FE-012).
// ClÃ­nica busca por nombre del tutor â†’ solicita â†’ el tutor aprueba desde la
// bandeja â†’ la clÃ­nica ve la ficha â†’ el tutor revoca. Prepara su propia clÃ­nica
// verificada para no depender del orden de las specs de F1.
test.describe.configure({ mode: "serial" });

const clinicName = `ClÃ­nica F2 ${runId}`;
const microchip = `98200${Date.now().toString().slice(-10)}`;

test("PP-F2-QA-004 escenario 4: solicitud de vÃ­nculo aprobada por el tutor y revocada", async ({
  browser,
}) => {
  const { page: clinicPage, orgId } = await verifiedClinic(browser, clinicName);
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

  // La clÃ­nica busca por microchip: solo ve el tutor enmascarado.
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

  // La clÃ­nica ya ve la ficha; el tutor revoca y deja de verla.
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

// Escenarios 1â€“3 y 5 de PP-F2-QA-004: requieren datos de agenda/consulta/carnet
// preparados por la clÃ­nica; quedan documentados y marcados para completar con
// el entorno de staging (ver docs/tracking/phase-2.md).
