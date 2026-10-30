import { createRouter, createWebHistory } from 'vue-router'
import HomePage from '@/features/legal/HomePage.vue'
import LegalPage from '@/features/legal/LegalPage.vue'
import NotFoundPage from '@/features/legal/NotFoundPage.vue'
import ForbiddenPage from '@/features/legal/ForbiddenPage.vue'
import CallbackPage from '@/features/auth/CallbackPage.vue'
import LogoutPage from '@/features/auth/LogoutPage.vue'
import OnboardingPage from '@/features/onboarding/OnboardingPage.vue'
import ProfilePage from '@/features/profile/ProfilePage.vue'
import DashboardPage from '@/features/dashboard/DashboardPage.vue'
import PetsPage from '@/features/pets/PetsPage.vue'
import PetFormPage from '@/features/pets/PetFormPage.vue'
import PetDetailPage from '@/features/pets/PetDetailPage.vue'
import AcceptTransferPage from '@/features/pets/AcceptTransferPage.vue'
import OrgRegisterPage from '@/features/orgs/OrgRegisterPage.vue'
import OrgSummaryPage from '@/features/orgs/OrgSummaryPage.vue'
import OrgMembersPage from '@/features/orgs/OrgMembersPage.vue'
import OrgSettingsPage from '@/features/orgs/OrgSettingsPage.vue'
import AcceptInvitationPage from '@/features/orgs/AcceptInvitationPage.vue'
import PatientsPage from '@/features/orgs/PatientsPage.vue'
import PatientDetailPage from '@/features/orgs/PatientDetailPage.vue'
import EncountersPage from '@/features/encounters/EncountersPage.vue'
import EncounterEditorPage from '@/features/encounters/EncounterEditorPage.vue'
import PetHistoryPage from '@/features/records/PetHistoryPage.vue'
import PetEncounterPage from '@/features/records/PetEncounterPage.vue'
import VerifyEncounterPage from '@/features/records/VerifyEncounterPage.vue'
import GuardianCardPage from '@/features/card/GuardianCardPage.vue'
import SharedRecordPage from '@/features/card/SharedRecordPage.vue'
import VerifyCardPage from '@/features/card/VerifyCardPage.vue'
import AdminOrganizationsPage from '@/features/admin/AdminOrganizationsPage.vue'
import AdminUsersPage from '@/features/admin/AdminUsersPage.vue'
import AdminAuditPage from '@/features/admin/AdminAuditPage.vue'
import { ADMIN_ROLES, CLINIC_ROLES } from '@/shared/validation/schemas'
import { authGuard } from './guards'

// Tabla de rutas de docs/phase-1/07 §4: mascotas con vínculos y transferencia
// (FE-006/FE-008), organizaciones y pacientes (FE-007/FE-008, M1.3); admin
// (M1.4); consultas, ficha clínica e historial del tutor (F2 M2.1,
// FE-003/004/005/009) y verificación pública de consultas (API-013).
const orgMeta = {
  layout: 'org',
  requiresAuth: true,
  requiresOnboarding: true,
  requiresMembership: true,
} as const
const adminMeta = {
  layout: 'admin',
  requiresAuth: true,
  requiresGlobalRole: 'super_admin',
} as const

export function createAppRouter() {
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/', name: 'home', component: HomePage, meta: { layout: 'public' } },
      {
        path: '/legal/terms',
        name: 'legal-terms',
        component: LegalPage,
        props: { kind: 'terms' },
        meta: { layout: 'public' },
      },
      {
        path: '/legal/privacy',
        name: 'legal-privacy',
        component: LegalPage,
        props: { kind: 'privacy' },
        meta: { layout: 'public' },
      },
      {
        path: '/auth/callback',
        name: 'auth-callback',
        component: CallbackPage,
        meta: { layout: 'public' },
      },
      {
        path: '/auth/logout',
        name: 'auth-logout',
        component: LogoutPage,
        meta: { layout: 'public' },
      },
      {
        path: '/onboarding',
        name: 'onboarding',
        component: OnboardingPage,
        meta: { layout: 'public', requiresAuth: true },
      },
      {
        path: '/app',
        name: 'app',
        component: DashboardPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets',
        name: 'pets',
        component: PetsPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/new',
        name: 'pet-new',
        component: PetFormPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/:petId',
        name: 'pet-detail',
        component: PetDetailPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/:petId/edit',
        name: 'pet-edit',
        component: PetFormPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/:petId/history',
        name: 'pet-history',
        component: PetHistoryPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/:petId/history/:encounterId',
        name: 'pet-encounter',
        component: PetEncounterPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/pets/:petId/card',
        name: 'pet-card',
        component: GuardianCardPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/profile',
        name: 'profile',
        component: ProfilePage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/transfers/:token',
        name: 'transfer-accept',
        component: AcceptTransferPage,
        meta: { layout: 'owner', requiresAuth: true },
      },
      {
        path: '/invitations/:token',
        name: 'invitation-accept',
        component: AcceptInvitationPage,
        meta: { layout: 'public', requiresAuth: true },
      },
      {
        path: '/org/new',
        name: 'org-new',
        component: OrgRegisterPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      { path: '/org/:orgId', name: 'org-summary', component: OrgSummaryPage, meta: orgMeta },
      {
        path: '/org/:orgId/patients',
        name: 'org-patients',
        component: PatientsPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/patients/:petId',
        name: 'org-patient',
        component: PatientDetailPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/encounters',
        name: 'org-encounters',
        component: EncountersPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/encounters/:encounterId',
        name: 'org-encounter',
        component: EncounterEditorPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/members',
        name: 'org-members',
        component: OrgMembersPage,
        meta: { ...orgMeta, requiresOrgRole: ADMIN_ROLES },
      },
      {
        path: '/org/:orgId/settings',
        name: 'org-settings',
        component: OrgSettingsPage,
        meta: { ...orgMeta, requiresOrgRole: ADMIN_ROLES },
      },
      { path: '/admin', redirect: { name: 'admin-organizations' } },
      {
        path: '/admin/organizations',
        name: 'admin-organizations',
        component: AdminOrganizationsPage,
        meta: adminMeta,
      },
      { path: '/admin/users', name: 'admin-users', component: AdminUsersPage, meta: adminMeta },
      { path: '/admin/audit', name: 'admin-audit', component: AdminAuditPage, meta: adminMeta },
      {
        path: '/verify/encounters/:encounterId',
        name: 'verify-encounter',
        component: VerifyEncounterPage,
        meta: { layout: 'public' },
      },
      {
        path: '/share/:token',
        name: 'shared-record',
        component: SharedRecordPage,
        meta: { layout: 'public' },
      },
      {
        path: '/verify/cards/:petId',
        name: 'verify-card',
        component: VerifyCardPage,
        meta: { layout: 'public' },
      },
      { path: '/403', name: 'forbidden', component: ForbiddenPage, meta: { layout: 'public' } },
      {
        path: '/:pathMatch(.*)*',
        name: 'not-found',
        component: NotFoundPage,
        meta: { layout: 'public' },
      },
    ],
    scrollBehavior: () => ({ top: 0 }),
  })
  router.beforeEach(authGuard)
  return router
}
