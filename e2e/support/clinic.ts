import { expect, type Browser, type Page } from "@playwright/test";
import { completeOnboarding, loginConfiguringTotp, signIn } from "./app";
import { runId, uniqueEmail } from "./env";
import { waitForLink } from "./mailpit";
import { createUser, type TestUser } from "./keycloak";

export async function newPage(browser: Browser): Promise<Page> {
  return (await browser.newContext()).newPage();
}

export interface VerifiedClinic {
  page: Page;
  orgId: string;
  founder: TestUser;
  name: string;
}

/**
 * Registra una clínica por la UI y la verifica con un super_admin (con OTP).
 * La fundadora queda como clinic_admin con la página abierta en la clínica.
 */
export async function verifiedClinic(
  browser: Browser,
  name: string,
): Promise<VerifiedClinic> {
  const founder = await createUser(uniqueEmail("f2founder"), "Fundadora");
  const page = await newPage(browser);
  await signIn(page, founder, { path: "org" });
  await page.locator('input[name="org-type"][value="clinic"]').check();
  await page.getByTestId("org-next").click();
  await page.locator("#org-legal").fill(`${name} S.A.S.`);
  await page.locator("#org-display").fill(name);
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
  await adminPage.getByTestId("org-row").filter({ hasText: name }).click();
  await adminPage.getByTestId("action-verify").click();
  await adminPage.getByTestId("confirm-reason").click();
  await expect(adminPage.getByTestId("reason-dialog")).toBeHidden();
  await adminPage.context().close();
  await page.reload();
  return { page, orgId, founder, name };
}

export interface ClinicVet {
  vet: TestUser;
  page: Page;
}

/** Invita a un veterinario, acepta la invitación en su propia sesión y lo devuelve. */
export async function inviteVet(
  browser: Browser,
  clinic: VerifiedClinic,
  name = "Carlos",
): Promise<ClinicVet> {
  const vet = await createUser(uniqueEmail("f2vet"), name);
  const { page, orgId } = clinic;
  await page.goto(`/org/${orgId}/members`);
  await page.getByTestId("tab-invitations").click();
  await page.locator("#invite-email").fill(vet.email);
  await page.locator("#invite-role").selectOption("veterinarian");
  await page.getByTestId("send-invitation").click();
  await expect(
    page.getByTestId("invitation-row").filter({ hasText: vet.email }),
  ).toBeVisible();
  const link = await waitForLink(
    vet.email,
    /https?:\/\/[^\s"'<>]+\/invitations\/[A-Za-z0-9_-]+/,
  );
  const vetPage = await newPage(browser);
  await signIn(vetPage, vet);
  await vetPage.goto(link);
  await vetPage.getByTestId("accept-invitation").click();
  await vetPage.waitForURL(new RegExp(`/org/${orgId}$`));
  return { vet, page: vetPage };
}

/** Código público de la clínica (tarjeta del resumen de la organización). */
export async function publicCode(clinic: VerifiedClinic): Promise<string> {
  await clinic.page.goto(`/org/${clinic.orgId}`);
  const card = clinic.page.getByTestId("public-code-card");
  return (await card.locator(".font-mono").innerText()).trim();
}

/** Agenda lun–vie 08:00–17:00 para el veterinario (única fila de la lista). */
export async function defineAgenda(clinic: VerifiedClinic): Promise<void> {
  const { page, orgId } = clinic;
  await page.goto(`/org/${orgId}/settings/agendas`);
  await page.getByTestId("agenda-list").locator("button").first().click();
  const form = page.getByTestId("agenda-form");
  await form.locator("input").first().fill("America/Bogota");
  await form.locator('button[type="submit"]').click();
  await expect(page.getByTestId("exceptions")).toBeVisible();
}

/** El tutor vincula su mascota con el código público de la clínica (PRD-015). */
export async function linkByCode(
  ownerPage: Page,
  code: string,
  clinicName: string,
): Promise<void> {
  const links = ownerPage.getByTestId("care-links");
  await links.getByTestId("link-clinic").click();
  await ownerPage.locator("#link-code").fill(code);
  await ownerPage.locator("#link-code").press("Enter");
  await expect(ownerPage.getByTestId("link-preview")).toContainText(clinicName);
  await ownerPage.getByTestId("confirm-link").click();
  await expect(links).toContainText(clinicName);
}
