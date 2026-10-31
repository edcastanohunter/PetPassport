import { expect, test, type Page } from "@playwright/test";
import { createPet, expectAccessible, signIn } from "../support/app";
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
import { waitForLink } from "../support/mailpit";

// PP-F2-QA-004 escenarios 1, 2, 3 y 5. Comparten una clínica verificada con un
// veterinario y agenda, y un tutor con una mascota vinculada por código (PRD-015).
test.describe.configure({ mode: "serial" });

const clinicName = `Clínica F2b ${runId}`;
const petName = `Nube ${runTag}`;

let clinic: VerifiedClinic;
let vet: ClinicVet;
let owner: TestUser;
let ownerPage: Page;
let petId: string;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(240_000);
  clinic = await verifiedClinic(browser, clinicName);
  vet = await inviteVet(browser, clinic);
  await defineAgenda(clinic);
  const code = await publicCode(clinic);
  owner = await createUser(uniqueEmail("f2b-owner"), "Marta");
  ownerPage = await newPage(browser);
  await signIn(ownerPage, owner, { phone: "+573001234567" });
  petId = await createPet(ownerPage, petName);
  await linkByCode(ownerPage, code, clinicName);
});

test.afterAll(async () => {
  await ownerPage?.context().close();
  await vet?.page.context().close();
  await clinic?.page.context().close();
});

test("PP-F2-QA-004 escenario 1: cita solicitada, propuesta alternativa y aceptación", async () => {
  await ownerPage.goto("/app/appointments");
  await ownerPage.getByTestId("request-btn").click();
  const form = ownerPage.getByTestId("request-form");
  await form.getByTestId("req-pet").selectOption({ label: petName });
  await form.getByTestId("req-org").selectOption({ label: clinicName });
  await form.locator("input").first().fill("Control anual");
  await form.getByTestId("slot-picker").locator("ul button").first().click();
  await expectAccessible(ownerPage, "appointment-request");
  await form.locator('button[type="submit"]').click();
  await expect(ownerPage.getByTestId("appointment-item")).toHaveCount(1);

  // La clínica ve la solicitud en su bandeja y propone otra franja.
  await clinic.page.goto(`/org/${clinic.orgId}/agenda`);
  await clinic.page.getByTestId("inbox").locator("button").first().click();
  const detail = clinic.page.getByTestId("appointment-detail");
  await expect(detail).toBeVisible();
  await detail.getByTestId("appointment-actions").locator("button").nth(1).click();
  const propose = detail.getByTestId("appointment-form");
  await propose.getByTestId("slot-picker").locator("ul button").last().click();
  await propose.locator('button[type="submit"]').click();

  // El tutor ve la propuesta y la acepta.
  await ownerPage.reload();
  const proposal = ownerPage.getByTestId("proposal");
  await expect(proposal).toBeVisible();
  await proposal.locator("button").first().click();
  await expect(ownerPage.getByTestId("proposal")).toHaveCount(0);
  await expect(ownerPage.getByTestId("appointment-item")).toContainText(
    /confirmad/i,
  );
});

let encounterId = "";

test("PP-F2-QA-004 escenario 2: consulta firmada, enmendada y visible para el tutor", async () => {
  const vp = vet.page;
  await vp.goto(`/org/${clinic.orgId}/patients`);
  await vp
    .getByTestId("patient-row")
    .filter({ hasText: petName })
    .getByRole("link", { name: petName })
    .click();
  await vp.getByTestId("open-encounter").click();
  await vp.getByTestId("open-reason").fill("Control general");
  await vp.getByTestId("open-submit").click();
  await vp.waitForURL(/\/org\/[0-9a-f-]{36}\/encounters\/[0-9a-f-]{36}$/);
  encounterId = vp.url().split("/").pop()!;

  // Edición SOAP con autoguardado.
  const attending = vp.getByTestId("attending-vet");
  if (!(await attending.inputValue())) await attending.selectOption({ index: 1 });
  await vp.getByTestId("reason").fill("Control general");
  await vp.getByTestId("subjective").fill("Come y bebe con normalidad.");
  await vp.getByTestId("add-diagnosis").click();
  await vp.getByTestId("diagnosis-row").first().locator("input").first().fill("Paciente sano");
  await expect(vp.getByTestId("autosave-status")).toContainText(/guardad/i);

  // Firma.
  await vp.getByTestId("sign-open").click();
  await vp.getByTestId("sign-confirm").click();
  await expect(vp.getByTestId("signature-seal")).toBeVisible();

  // Adenda.
  await vp.getByTestId("addendum-open").click();
  await vp.getByTestId("addendum-text").fill("Resultados de laboratorio normales.");
  await vp.getByTestId("addendum-submit").click();
  await expect(vp.getByTestId("addenda")).toContainText("laboratorio normales");

  // El tutor ve la consulta en el historial, con la adenda.
  await ownerPage.goto(`/app/pets/${petId}/history`);
  const card = ownerPage.getByTestId("history-card").filter({ hasText: /Control general/ });
  await expect(card).toBeVisible();
  await card.click();
  await expect(ownerPage.getByTestId("addenda")).toContainText("laboratorio normales");
  await expectAccessible(ownerPage, "encounter-detail-guardian");
});

const isoDay = (offsetDays: number) =>
  new Date(Date.now() + offsetDays * 86_400_000).toISOString().slice(0, 10);

test("PP-F2-QA-004 escenario 3: carnet con próxima dosis, vacuna declarada, verificación y enlace con PIN", async ({
  browser,
}) => {
  // La clínica registra una vacuna con próxima dosis.
  const vp = vet.page;
  await vp.goto(`/org/${clinic.orgId}/patients`);
  await vp
    .getByTestId("patient-row")
    .filter({ hasText: petName })
    .getByRole("link", { name: petName })
    .click();
  await vp.getByTestId("tab-card").click();
  const form = vp.getByTestId("card-form");
  await form.locator("input").first().fill("Triple canina");
  await form.locator('input[type="date"]').first().fill(isoDay(-1));
  await form.locator('input[type="date"]').nth(1).fill(isoDay(365));
  await form.locator('button[type="submit"]').click();
  await expect(
    vp.getByTestId("card-entry").filter({ hasText: "Triple canina" }),
  ).toBeVisible();

  // El tutor ve la próxima dosis y declara una vacuna antigua.
  await ownerPage.goto(`/app/pets/${petId}/card`);
  await expect(ownerPage.getByTestId("card-next-due")).toContainText(
    "Triple canina",
  );
  const declare = ownerPage.getByTestId("declare-form");
  await declare.locator("input").first().fill("Rabia (declarada)");
  await declare.locator('input[type="date"]').fill(isoDay(-400));
  await declare.locator('button[type="submit"]').click();
  await expect(
    ownerPage.getByTestId("card-entry").filter({ hasText: "Rabia (declarada)" }),
  ).toBeVisible();

  // La clínica verifica la entrada declarada.
  await vp.reload();
  await vp.getByTestId("tab-card").click();
  const declared = vp
    .getByTestId("card-entry")
    .filter({ hasText: "Rabia (declarada)" });
  await declared.getByRole("button", { name: "Verificar" }).click();
  await expect(declared.getByRole("button", { name: "Verificar" })).toHaveCount(
    0,
  );

  // El tutor comparte el carnet con PIN y la página pública lo pide.
  const shares = ownerPage.getByTestId("shares");
  await shares.locator("input").fill("4821");
  await shares.locator('form button[type="submit"]').click();
  const url = (await ownerPage.getByTestId("share-created").locator(".font-mono").innerText()).trim();
  expect(url).toMatch(/\/share\//);

  const publicPage = await newPage(browser);
  // La URL usa PP_PUBLIC_BASE_URL (localhost:5173 en dev); se abre en el origen del E2E.
  await publicPage.goto(new URL(url).pathname);
  await publicPage.locator("input").first().fill("4821");
  await publicPage.locator('button[type="submit"]').click();
  await expect(publicPage.getByTestId("shared-record")).toContainText(
    "Triple canina",
  );
  await expectAccessible(publicPage, "shared-record");
  await publicPage.context().close();
});


test("PP-F2-QA-004 escenario 5: correo de cita en Mailpit con baja por enlace", async ({
  browser,
}) => {
  // La confirmación del escenario 1 genera un correo al tutor (idioma del tutor, es).
  const unsubscribeUrl = await waitForLink(
    owner.email,
    /https?:\/\/[^\s"'<>]+\/unsubscribe\/[A-Za-z0-9_.-]+/,
    /cita/i,
  );
  const page = await newPage(browser);
  await page.goto(new URL(unsubscribeUrl).pathname);
  await expect(page.getByTestId("unsubscribed")).toBeVisible();
  await expectAccessible(page, "unsubscribe");
  await page.context().close();
});
