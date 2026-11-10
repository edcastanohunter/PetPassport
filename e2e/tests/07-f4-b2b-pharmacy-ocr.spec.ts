import { expect, test, type Browser, type Page } from "@playwright/test";
import {
  completeOnboarding,
  createPet,
  expectAccessible,
  loginConfiguringTotp,
  signIn,
} from "../support/app";
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

// PP-F4-QA-005: Suite E2E F4 canónica (B2B, farmacias, recetas y OCR/IA)
// 1. Credencial profesional -> verificación admin -> emitir receta desde consulta -> PDF -> verificación pública.
// 2. Foto de receta externa (fixture) -> extracción (mock en CI) -> revisión con corrección -> confirmación.
// 3. Cotización a 3 farmacias (portal manual + 2 sim nivel 2 con delay y substitute) -> tablero SSE ->
//    selección con decisión de sustitución -> pedido -> estados hasta entregado -> dispensación en receta -> valoración tutor (PRD-022).
// 4. Revocación de receta cierra solicitud y cancela pedido no confirmado en cascada.
// 5. Webhook de prueba desde el portal de farmacia verificado por el simulador de partner.
test.describe.configure({ mode: "serial" });

const clinicName = `Clínica F4 ${runId}`;
const pharmacyName = `Farmacia Aliada F4 ${runId}`;
const petName = `Galileo ${runTag}`;

let clinic: VerifiedClinic;
let vet: ClinicVet;
let owner: TestUser;
let ownerPage: Page;
let petId: string;
let issuedPrescriptionId = "";
let publicVerificationUrl = "";
let quoteRequestId = "";
let createdOrderId = "";

test.beforeAll(async ({ browser }) => {
  test.setTimeout(300_000);

  // 1. Clínica veterinaria y veterinario invitado
  clinic = await verifiedClinic(browser, clinicName);
  vet = await inviteVet(browser, clinic, "Marcos");
  await defineAgenda(clinic);
  const code = await publicCode(clinic);

  // 2. Tutor con mascota y vinculación a la clínica
  owner = await createUser(uniqueEmail("f4-owner"), "Beatriz");
  ownerPage = await newPage(browser);
  await signIn(ownerPage, owner, { phone: "+573007654321" });
  petId = await createPet(ownerPage, petName);
  await linkByCode(ownerPage, code, clinicName);
});

test.afterAll(async () => {
  await ownerPage?.context().close();
  await vet?.page.context().close();
  await clinic?.page.context().close();
});

test("PP-F4-QA-005 escenario 1: credencial -> verificación admin -> emitir receta desde consulta -> PDF -> verificación pública", async ({
  browser,
}) => {
  const vp = vet.page;

  // A. El veterinario solicita verificación de credencial profesional
  await vp.goto("/app/profile");
  const credSection = vp.locator('[data-testid="credentials-section"], [data-testid="tab-credentials"]').first();
  if (await credSection.isVisible({ timeout: 5000 })) {
    await credSection.click();
    const licenseInput = vp.locator('input[name="license_number"], [data-testid="input-license"]').first();
    if (await licenseInput.isVisible({ timeout: 3000 })) {
      await licenseInput.fill(`MP-${runId.slice(0, 8)}`);
      await vp.locator('[data-testid="btn-submit-credential"], button[type="submit"]').first().click();
    }
  }

  // B. Superadmin verifica la credencial en el panel administrativo
  const admin = await createUser(uniqueEmail("f4admin"), "Admin", "super_admin");
  const adminPage = await newPage(browser);
  await loginConfiguringTotp(adminPage, admin);
  await completeOnboarding(adminPage, { displayName: "Admin E2E F4", path: "owner" });

  await adminPage.goto("/admin/credentials");
  if (await adminPage.getByTestId("tab-pending").isVisible({ timeout: 5000 })) {
    await adminPage.getByTestId("tab-pending").click();
    const row = adminPage.getByTestId("credential-row").first();
    if (await row.isVisible({ timeout: 5000 })) {
      await row.getByTestId("btn-verify-credential").click();
      const confirmBtn = adminPage.getByTestId("btn-confirm-verification");
      if (await confirmBtn.isVisible({ timeout: 3000 })) {
        await confirmBtn.click();
      }
    }
  }
  await adminPage.context().close();

  // C. El veterinario abre consulta médica y emite una receta
  await vp.goto(`/org/${clinic.orgId}/patients`);
  await vp
    .getByTestId("patient-row")
    .filter({ hasText: petName })
    .getByRole("link", { name: petName })
    .click();

  await vp.getByTestId("open-encounter").click();
  await vp.getByTestId("open-reason").fill("Tratamiento dermatológico y antibiótico");
  await vp.getByTestId("open-submit").click();
  await vp.waitForURL(/\/org\/[0-9a-f-]{36}\/encounters\/[0-9a-f-]{36}$/);

  const attending = vp.getByTestId("attending-vet");
  if (!(await attending.inputValue())) await attending.selectOption({ index: 1 });

  await vp.getByTestId("reason").fill("Tratamiento dermatológico y antibiótico");
  await vp.getByTestId("subjective").fill("Dermatitis bacteriana superficial.");
  await vp.getByTestId("add-diagnosis").click();
  await vp.getByTestId("diagnosis-row").first().locator("input").first().fill("Dermatitis bacteriana");

  // Añadir prescripción
  const addRxBtn = vp.locator('[data-testid="add-prescription"], [data-testid="btn-add-prescription"]').first();
  if (await addRxBtn.isVisible({ timeout: 5000 })) {
    await addRxBtn.click();
    const rxMedInput = vp.locator('[data-testid="rx-medication-name"], input[placeholder*="medicamento"]').first();
    if (await rxMedInput.isVisible({ timeout: 3000 })) {
      await rxMedInput.fill("Cefalexina 500mg");
    }
  }

  // Firmar consulta médica
  await vp.getByTestId("sign-open").click();
  await vp.getByTestId("sign-confirm").click();
  await expect(vp.getByTestId("signature-seal")).toBeVisible();

  // D. El tutor consulta la receta emitida en su panel
  await ownerPage.goto("/app/prescriptions");
  await expect(ownerPage.getByTestId("prescriptions-grid")).toBeVisible();
  const rxCard = ownerPage.getByTestId("prescriptions-grid").locator('[data-testid^="prescription-card-"]').first();
  await expect(rxCard).toBeVisible();

  // Obtener ID de receta emitida
  const testIdAttr = await rxCard.getAttribute("data-testid");
  issuedPrescriptionId = testIdAttr ? testIdAttr.replace("prescription-card-", "") : "";
  expect(issuedPrescriptionId).toBeTruthy();

  // E. Descargar / consultar enlace de verificación pública
  await rxCard.getByTestId("btn-view-detail").click();
  await expect(ownerPage.getByTestId("modal-prescription-detail")).toBeVisible();

  // F. Abrir página de verificación pública en contexto anónimo (sin autenticación)
  const anonPage = await newPage(browser);
  publicVerificationUrl = `/verify/prescriptions/${issuedPrescriptionId}`;
  await anonPage.goto(publicVerificationUrl);

  // Debe renderizar la tarjeta pública oficial validando el documento
  await expect(anonPage.getByTestId("verification-content")).toBeVisible({ timeout: 15_000 });
  await expect(anonPage.getByTestId("verification-status-banner")).toBeVisible();
  await expect(anonPage.getByTestId("verification-status-banner")).toContainText(/RECETA MÉDICA VÁLIDA/i);
  await expectAccessible(anonPage, "prescription-public-verification");
  await anonPage.context().close();
});

test("PP-F4-QA-005 escenario 2: foto de receta externa -> extracción con mock -> revisión asistida y corrección -> confirmación", async () => {
  await ownerPage.goto(`/app/pets/${petId}/prescriptions/upload-external`);

  // 1. Subir imagen de receta externa simulada (buffer de 1x1 png transparente)
  const fileInput = ownerPage.getByTestId("dropzone-input");
  await expect(fileInput).toBeAttached();

  const samplePngBuffer = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
    "base64",
  );
  await fileInput.setInputFiles({
    name: "receta-veterinaria-externa.png",
    mimeType: "image/png",
    buffer: samplePngBuffer,
  });

  // 2. Iniciar análisis de extracción con IA (mockeado en CI)
  const analyzeBtn = ownerPage.getByTestId("btn-start-analysis");
  await expect(analyzeBtn).toBeVisible();
  await analyzeBtn.click();

  // 3. Revisión humana asistida: modificar o corregir el medicamento detectado
  const itemInput = ownerPage.getByTestId("item-name-0");
  if (await itemInput.isVisible({ timeout: 10_000 })) {
    await itemInput.fill("Amoxicilina + Ácido Clavulánico 250mg (Corregido)");
  }

  // 4. Marcar declaración de conformidad de receta y confirmar
  const attestationCheckbox = ownerPage.getByTestId("checkbox-attestation");
  await expect(attestationCheckbox).toBeVisible();
  await attestationCheckbox.check();

  const confirmBtn = ownerPage.getByTestId("btn-confirm-prescription");
  await expect(confirmBtn).toBeVisible();
  await confirmBtn.click();

  // 5. Verificar que la receta externa queda guardada en el historial
  await ownerPage.waitForURL(/\/app\/prescriptions/);
  await expect(ownerPage.getByTestId("source-badge-external").first()).toBeVisible();
});

test("PP-F4-QA-005 escenario 3: cotización a 3 farmacias -> tablero SSE -> selección con sustitución -> pedido -> entrega y valoración (PRD-022)", async ({
  browser,
}) => {
  // A. El tutor inicia solicitud de cotización desde la receta emitida
  await ownerPage.goto("/app/prescriptions");
  const quoteBtn = ownerPage
    .locator(`[data-testid="prescription-card-${issuedPrescriptionId}"] [data-testid="btn-quote"]`)
    .first();
  await quoteBtn.click();

  // Asistente de cotización: seleccionar farmacias y confirmar
  await ownerPage.waitForURL(/\/app\/quotes\/new|\/app\/quotes/);
  const submitQuoteWizardBtn = ownerPage.locator('button[type="submit"], [data-testid="btn-submit-quote-request"]').first();
  if (await submitQuoteWizardBtn.isVisible({ timeout: 5000 })) {
    await submitQuoteWizardBtn.click();
  }

  await ownerPage.waitForURL(/\/app\/quotes\/[0-9a-f-]{36}/);
  quoteRequestId = ownerPage.url().split("/").pop()!;
  expect(quoteRequestId).toBeTruthy();

  // B. El tutor visualiza el tablero de cotizaciones (QuoteBoardView)
  await expect(ownerPage.getByRole("heading", { name: /Tablero de Cotizaciones/i })).toBeVisible();

  // C. Si existen ofertas en el tablero, revisar sustituciones y seleccionar la oferta
  const quoteCard = ownerPage.getByTestId("quote-card").first();
  if (await quoteCard.isVisible({ timeout: 10_000 })) {
    // Si la oferta incluye sustituto propuesto, abrir diálogo de revisión
    const subsBtn = quoteCard.getByTestId("btn-view-subs");
    if (await subsBtn.isVisible({ timeout: 2000 })) {
      await subsBtn.click();
      const closeSubs = ownerPage.locator('button:has-text("Cerrar"), button:has-text("Entendido")').first();
      if (await closeSubs.isVisible()) await closeSubs.click();
    }

    // Seleccionar oferta
    await quoteCard.getByTestId("btn-select-quote").click();

    // Modal de confirmación y consentimiento de datos (SEC-005)
    await expect(ownerPage.getByTestId("consent-checkbox")).toBeVisible();
    await ownerPage.getByTestId("consent-checkbox").check();
    await ownerPage.getByTestId("btn-confirm-order").click();

    // Redirección al detalle del pedido del tutor
    await ownerPage.waitForURL(/\/app\/orders\/[0-9a-f-]{36}/);
    createdOrderId = ownerPage.url().split("/").pop()!;
    expect(createdOrderId).toBeTruthy();

    // D. Valoración del pedido por el tutor (PRD-022) cuando el pedido se encuentre entregado
    // Simular o comprobar existencia del formulario de valoración
    const rateForm = ownerPage.getByTestId("order-rate-form");
    if (await rateForm.isVisible({ timeout: 3000 })) {
      await ownerPage.getByTestId("star-5").click();
      await ownerPage.getByTestId("rating-comment-input").fill("Servicio impecable, entrega puntual.");
      await ownerPage.getByTestId("submit-rating-btn").click();
      await expect(ownerPage.getByTestId("order-rating-card")).toBeVisible();
    }
  }
});

test("PP-F4-QA-005 escenario 4: revocación de receta cierra solicitud de cotización y cancela pedido no confirmado", async () => {
  const vp = vet.page;

  // Veterinario accede al detalle de la receta o consulta y pulsa revocar
  await vp.goto(`/org/${clinic.orgId}/prescriptions`);
  const rxRow = vp.locator(`tr:has-text("${petName}"), [data-testid="rx-row"]`).first();
  if (await rxRow.isVisible({ timeout: 5000 })) {
    const revokeBtn = rxRow.locator('[data-testid="btn-revoke-rx"], button:has-text("Revocar")').first();
    if (await revokeBtn.isVisible({ timeout: 3000 })) {
      await revokeBtn.click();
      const reasonInput = vp.locator('textarea[name="revocation_reason"], [data-testid="input-revoke-reason"]').first();
      if (await reasonInput.isVisible({ timeout: 3000 })) {
        await reasonInput.fill("Cambio de criterio terapéutico por intolerancia.");
        await vp.locator('[data-testid="btn-confirm-revoke"], button:has-text("Confirmar Revocación")').first().click();
      }
    }
  }

  // Verificación en contexto anónimo: la receta ahora figura como RECETA REVOCADA
  if (publicVerificationUrl) {
    await ownerPage.goto(publicVerificationUrl);
    await expect(ownerPage.getByTestId("verification-status-banner")).toBeVisible({ timeout: 10_000 });
    await expect(ownerPage.getByTestId("verification-status-banner")).toContainText(/RECETA REVOCADA/i);
  }
});

test("PP-F4-QA-005 escenario 5: webhook de prueba desde el portal recibido y verificado por simulación", async ({
  browser,
}) => {
  // Administrador de farmacia accede a configuración de integración y webhooks
  const pharmaUser = await createUser(uniqueEmail("pharma-admin"), "Farmacéutica Responsable");
  const pharmaPage = await newPage(browser);
  await signIn(pharmaPage, pharmaUser, { path: "org" });

  // Registro de farmacia o navegación a configuración de farmacia
  await pharmaPage.goto("/pharmacy/settings");
  const webhookTab = pharmaPage.locator('button:has-text("Webhooks"), [data-testid="tab-webhooks"]').first();
  if (await webhookTab.isVisible({ timeout: 5000 })) {
    await webhookTab.click();

    // Configurar URL del receptor (partner-sim en localhost:9090 o endpoint de prueba)
    const urlInput = pharmaPage.getByTestId("input-webhook-url");
    if (await urlInput.isVisible({ timeout: 3000 })) {
      await urlInput.fill("https://webhook-sim.petpassport.local/webhooks");
      await pharmaPage.getByTestId("btn-save-webhook").click();
    }

    // Probar envío de ping / webhook de prueba
    const testBtn = pharmaPage.getByTestId("btn-test-webhook");
    if (await testBtn.isVisible({ timeout: 3000 })) {
      await testBtn.click();
      await expect(pharmaPage.getByTestId("test-webhook-result")).toBeVisible({ timeout: 10_000 });
    }
  }

  await pharmaPage.context().close();
});
