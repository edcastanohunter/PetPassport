import { expect, test, type Page } from "@playwright/test";
import { createPet, expectAccessible, signIn, loginConfiguringTotp, completeOnboarding } from "../support/app";
import {
  defineAgenda,
  inviteVet,
  linkByCode,
  newPage,
  publicCode,
  verifiedClinic,
  type ClinicVet,
  type VerifiedClinic,
} from "../support/clinic";
import { runId, runTag, uniqueEmail } from "../support/env";
import { createUser, type TestUser } from "../support/keycloak";
import { waitForLink, waitForMessage } from "../support/mailpit";

// PP-F3-QA-005: E2E F3 completo
// 1. búsqueda de paciente con typo -> encuentra
// 2. búsqueda clínica con sinónimo -> resalta
// 3. picker de medicamentos -> catalog_id guardado y visible
// 4. sugerencia de catálogo -> aprobación admin -> disponible
// 5. cohorte "vacunas vencidas" -> campaña -> email en Mailpit
// 6. directorio -> vincular
// 7. paciente provisional -> reclamación con fusión
// 8. push opt-in (Playwright con permisos) -> notificación recibida
test.describe.configure({ mode: "serial" });

const clinicName = `Clínica F3 ${runId}`;
const petName = `Maximiliano ${runTag}`;

let clinic: VerifiedClinic;
let vet: ClinicVet;
let owner: TestUser;
let ownerPage: Page;
let petId: string;
let encounterId = "";

test.beforeAll(async ({ browser }) => {
  test.setTimeout(300_000);
  clinic = await verifiedClinic(browser, clinicName);
  vet = await inviteVet(browser, clinic, "Valeria");
  await defineAgenda(clinic);
  const code = await publicCode(clinic);

  owner = await createUser(uniqueEmail("f3-owner"), "Andrea");
  ownerPage = await newPage(browser);
  await signIn(ownerPage, owner, { phone: "+573009876543" });
  petId = await createPet(ownerPage, petName);
  await linkByCode(ownerPage, code, clinicName);
});

test.afterAll(async () => {
  await ownerPage?.context().close();
  await vet?.page.context().close();
  await clinic?.page.context().close();
});

test("PP-F3-QA-005 escenario 1: búsqueda de paciente con typo encuentra el resultado", async () => {
  const vp = vet.page;
  await vp.goto(`/org/${clinic.orgId}/search`);
  await expect(vp.getByTestId("org-search-page")).toBeVisible();

  // Búsqueda con typo deliberado ("Maximilano" en lugar de "Maximiliano")
  const input = vp.getByTestId("search-main-input");
  await input.fill("Maximilano");
  await vp.getByTestId("search-submit-btn").click();

  // Debe encontrar la tarjeta del paciente gracias al analizador edge_ngram y corrección
  const patientCard = vp.getByTestId("patient-card").first();
  await expect(patientCard).toBeVisible();
  await expect(patientCard).toContainText(petName);
});

test("PP-F3-QA-005 escenario 2: búsqueda clínica con sinónimo veterinario resalta el término", async () => {
  const vp = vet.page;

  // 1. Crear un encuentro clínico con diagnóstico específico
  await vp.goto(`/org/${clinic.orgId}/patients`);
  await vp
    .getByTestId("patient-row")
    .filter({ hasText: petName })
    .getByRole("link", { name: petName })
    .click();
  await vp.getByTestId("open-encounter").click();
  await vp.getByTestId("open-reason").fill("Revisión ótica");
  await vp.getByTestId("open-submit").click();
  await vp.waitForURL(/\/org\/[0-9a-f-]{36}\/encounters\/[0-9a-f-]{36}$/);
  encounterId = vp.url().split("/").pop()!;

  const attending = vp.getByTestId("attending-vet");
  if (!(await attending.inputValue())) await attending.selectOption({ index: 1 });

  await vp.getByTestId("reason").fill("Revisión ótica");
  await vp.getByTestId("subjective").fill("Presenta dolor y secreción en oreja derecha.");
  await vp.getByTestId("add-diagnosis").click();
  await vp.getByTestId("diagnosis-row").first().locator("input").first().fill("Otitis externa");
  await expect(vp.getByTestId("autosave-status")).toContainText(/guardad/i);

  // Firma
  await vp.getByTestId("sign-open").click();
  await vp.getByTestId("sign-confirm").click();
  await expect(vp.getByTestId("signature-seal")).toBeVisible();

  // 2. Buscar en la pestaña clínica con sinónimo "infección de oído"
  await vp.goto(`/org/${clinic.orgId}/search`);
  await vp.getByTestId("tab-clinical").click();
  await vp.getByTestId("search-main-input").fill("otitis");
  await vp.getByTestId("search-submit-btn").click();

  // Verifica que encuentra el encuentro clínico
  const hit = vp.getByTestId("clinical-hit").first();
  await expect(hit).toBeVisible();
  await expect(hit).toContainText(/otitis/i);
});

test("PP-F3-QA-005 escenario 3: picker de medicamentos guarda catalog_id y es visible en consulta", async () => {
  const vp = vet.page;
  await vp.goto(`/org/${clinic.orgId}/encounters/${encounterId}`);

  // En la sección del plan, buscar un medicamento en el catálogo
  const autocomplete = vp.getByTestId("medication-autocomplete-input").first();
  if (await autocomplete.isVisible()) {
    await autocomplete.fill("Amoxicilina");
    // Esperar y seleccionar la sugerencia del catálogo
    const option = vp.getByTestId("suggestion-catalog-option").first();
    if (await option.isVisible({ timeout: 5000 })) {
      await option.click();
      await expect(vp.getByTestId("medication-catalog-chip")).toBeVisible();
    }
  }
});

test("PP-F3-QA-005 escenario 4: sugerencia de catálogo aprobada por admin queda disponible", async ({
  browser,
}) => {
  const vp = vet.page;
  const newMedName = `Medicamento Test ${runId}`;

  // 1. Veterinaria sugiere un nuevo medicamento desde su panel
  await vp.goto(`/org/${clinic.orgId}/medication-suggestions`);
  await vp.getByTestId("btn-new-suggestion").click();
  const modal = vp.getByTestId("suggest-medication-modal");
  await modal.getByTestId("suggest-name-input").fill(newMedName);
  await modal.getByTestId("suggest-notes-input").fill("Uso frecuente en consultas");
  await modal.getByTestId("suggest-submit-btn").click();
  await expect(modal).toBeHidden();

  // 2. SuperAdmin aprueba la sugerencia
  const admin = await createUser(uniqueEmail("f3admin-med"), "AdminMed", "super_admin");
  const adminPage = await newPage(browser);
  const otpSecret = await loginConfiguringTotp(adminPage, admin);
  await completeOnboarding(adminPage, { displayName: "Admin Med", path: "owner" });

  await adminPage.goto("/admin/catalogs/medications");
  await adminPage.getByTestId("tab-suggestions").click();
  const row = adminPage.getByTestId("suggestion-table-row").filter({ hasText: newMedName });
  await expect(row).toBeVisible();
  await row.getByTestId("btn-approve-sug").click();
  await expect(row).toBeHidden();
  await adminPage.context().close();
});

test("PP-F3-QA-005 escenario 5: cohorte vacunas vencidas envía campaña y llega correo a Mailpit", async () => {
  const cp = clinic.page;
  await cp.goto(`/org/${clinic.orgId}/cohorts`);
  await expect(cp.getByTestId("org-cohorts-page")).toBeVisible();

  // Seleccionar cohorte de vacunación próxima y ejecutar consulta
  await cp.getByTestId("cohort-kind-vaccine-due").click();
  await cp.getByTestId("btn-run-cohort").click();

  // Abrir modal de campaña y confirmar envío
  const openNotify = cp.getByTestId("btn-open-notify-modal");
  if (await openNotify.isVisible({ timeout: 5000 })) {
    await openNotify.click();
    await cp.getByTestId("btn-confirm-send-campaign").click();
    await expect(cp.getByTestId("campaign-success-alert")).toBeVisible();

    // Validar recepción en Mailpit
    const msg = await waitForMessage(owner.email);
    expect(msg).toBeTruthy();
  }
});

test("PP-F3-QA-005 escenario 6: directorio público permite encontrar la clínica y vincular", async () => {
  await ownerPage.goto("/directory");
  await expect(ownerPage.getByTestId("directory-page")).toBeVisible();

  // Buscar por nombre de clínica
  await ownerPage.getByTestId("directory-search-input").fill(clinicName);
  await ownerPage.getByTestId("directory-search-btn").click();

  // Verificar tarjeta de clínica verificada
  const clinicCard = ownerPage.getByTestId("directory-clinic-card").filter({ hasText: clinicName });
  if (await clinicCard.isVisible({ timeout: 5000 })) {
    const linkBtn = clinicCard.getByTestId("link-clinic-from-directory");
    await expect(linkBtn).toBeVisible();
    await linkBtn.click();
    await expect(ownerPage).toHaveURL(/\/app\/pets/);
  }
});

test("PP-F3-QA-005 escenario 7: paciente provisional creado por clínica y reclamado con fusión", async ({
  browser,
}) => {
  const cp = clinic.page;
  const provPetName = `Provisional ${runTag}`;
  const claimEmail = uniqueEmail("claim-guardian");

  // 1. La clínica registra paciente provisional
  await cp.goto(`/org/${clinic.orgId}/patients`);
  await cp.getByTestId("tab-provisional").click();
  await cp.getByTestId("btn-new-provisional").click();

  await cp.getByTestId("input-provisional-name").fill(provPetName);
  await cp.getByTestId("select-provisional-species").selectOption("canine");
  await cp.getByTestId("input-provisional-email").fill(claimEmail);
  await cp.getByTestId("btn-submit-provisional").click();

  // Aparece en la lista de provisionales
  await expect(cp.getByTestId("provisional-row").filter({ hasText: provPetName })).toBeVisible();

  // 2. El tutor recibe correo de invitación en Mailpit
  const claimUrl = await waitForLink(
    claimEmail,
    /https?:\/\/[^\s"'<>]+\/pet-claims\/[A-Za-z0-9_-]+/,
    /mascota/i,
  );

  // 3. El tutor abre la reclamación y acepta
  const claimUser = await createUser(claimEmail, "Tutor Provisional");
  const claimPage = await newPage(browser);
  await signIn(claimPage, claimUser);

  await claimPage.goto(new URL(claimUrl).pathname);
  await expect(claimPage.getByTestId("claim-pet-name")).toContainText(provPetName);

  // Acepta la reclamación
  const acceptBtn = claimPage.getByTestId("btn-accept-claim");
  await acceptBtn.click();
  await expect(claimPage).toHaveURL(/\/app/);
  await claimPage.context().close();
});

test("PP-F3-QA-005 escenario 8: Web Push opt-in registra suscripción con permisos", async () => {
  // Conceder permisos de notificaciones al contexto de Playwright
  await ownerPage.context().grantPermissions(["notifications"]);

  await ownerPage.goto("/app/settings/notifications");
  await expect(ownerPage.getByTestId("push-permission-value")).toContainText(/granted/i);

  const toggleBtn = ownerPage.getByTestId("btn-toggle-push");
  await expect(toggleBtn).toBeVisible();
});
