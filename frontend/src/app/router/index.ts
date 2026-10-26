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
import { ADMIN_ROLES, CLINIC_ROLES } from '@/shared/validation/schemas'
import { authGuard } from './guards'

// Tabla de rutas de docs/phase-1/07 §4: mascotas con vínculos y transferencia
// (FE-006/FE-008), organizaciones y pacientes (FE-007/FE-008, M1.3); admin
// (M1.4) se añade con su hito.
const orgMeta = {
  layout: 'org',
  requiresAuth: true,
  requiresOnboarding: true,
  requiresMembership: true,
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
