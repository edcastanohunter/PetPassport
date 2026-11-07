import { createRouter, createWebHistory } from 'vue-router'
import { ADMIN_ROLES, CLINIC_ROLES } from '@/shared/validation/schemas'
import { authGuard } from './guards'

// Code splitting por ruta y feature (PP-F3-FE-010) para cumplir el presupuesto de bundle
// (JS inicial <= 180 KB gz para /app y <= 250 KB gz para espacio clínico)
const HomePage = () => import('@/features/legal/HomePage.vue')
const LegalPage = () => import('@/features/legal/LegalPage.vue')
const NotFoundPage = () => import('@/features/legal/NotFoundPage.vue')
const ForbiddenPage = () => import('@/features/legal/ForbiddenPage.vue')
const CallbackPage = () => import('@/features/auth/CallbackPage.vue')
const LogoutPage = () => import('@/features/auth/LogoutPage.vue')
const OnboardingPage = () => import('@/features/onboarding/OnboardingPage.vue')
const ProfilePage = () => import('@/features/profile/ProfilePage.vue')
const DashboardPage = () => import('@/features/dashboard/DashboardPage.vue')
const PetsPage = () => import('@/features/pets/PetsPage.vue')
const PetFormPage = () => import('@/features/pets/PetFormPage.vue')
const PetDetailPage = () => import('@/features/pets/PetDetailPage.vue')
const AcceptTransferPage = () => import('@/features/pets/AcceptTransferPage.vue')
const OrgRegisterPage = () => import('@/features/orgs/OrgRegisterPage.vue')
const OrgSummaryPage = () => import('@/features/orgs/OrgSummaryPage.vue')
const OrgMembersPage = () => import('@/features/orgs/OrgMembersPage.vue')
const OrgSettingsPage = () => import('@/features/orgs/OrgSettingsPage.vue')
const AcceptInvitationPage = () => import('@/features/orgs/AcceptInvitationPage.vue')
const PatientsPage = () => import('@/features/orgs/PatientsPage.vue')
const PatientDetailPage = () => import('@/features/orgs/PatientDetailPage.vue')
const EncountersPage = () => import('@/features/encounters/EncountersPage.vue')
const EncounterEditorPage = () => import('@/features/encounters/EncounterEditorPage.vue')
const PetHistoryPage = () => import('@/features/records/PetHistoryPage.vue')
const PetEncounterPage = () => import('@/features/records/PetEncounterPage.vue')
const VerifyEncounterPage = () => import('@/features/records/VerifyEncounterPage.vue')
const GuardianCardPage = () => import('@/features/card/GuardianCardPage.vue')
const SharedRecordPage = () => import('@/features/card/SharedRecordPage.vue')
const VerifyCardPage = () => import('@/features/card/VerifyCardPage.vue')
const OrgAgendaPage = () => import('@/features/scheduling/OrgAgendaPage.vue')
const AgendaSettingsPage = () => import('@/features/scheduling/AgendaSettingsPage.vue')
const GuardianAppointmentsPage = () => import('@/features/scheduling/GuardianAppointmentsPage.vue')
const InboxPage = () => import('@/features/notifications/InboxPage.vue')
const NotificationSettingsPage = () =>
  import('@/features/notifications/NotificationSettingsPage.vue')
const UnsubscribePage = () => import('@/features/notifications/UnsubscribePage.vue')
const AdminOrganizationsPage = () => import('@/features/admin/AdminOrganizationsPage.vue')
const AdminUsersPage = () => import('@/features/admin/AdminUsersPage.vue')
const AdminAuditPage = () => import('@/features/admin/AdminAuditPage.vue')
const AdminMedicationsPage = () => import('@/features/admin/AdminMedicationsPage.vue')
const OrgMedicationSuggestionsPage = () =>
  import('@/features/orgs/OrgMedicationSuggestionsPage.vue')
const OrgSearchPage = () => import('@/features/search/OrgSearchPage.vue')
const OwnerSearchPage = () => import('@/features/search/OwnerSearchPage.vue')
const DirectoryPage = () => import('@/features/orgs/DirectoryPage.vue')
const OrgCohortsPage = () => import('@/features/search/OrgCohortsPage.vue')
const AdminPlatformPage = () => import('@/features/admin/AdminPlatformPage.vue')
const PetClaimPage = () => import('@/features/pets/PetClaimPage.vue')
const CredentialsPage = () => import('@/features/profile/CredentialsPage.vue')
const AdminCredentialsPage = () => import('@/features/admin/AdminCredentialsPage.vue')
const OrgPrescriptionsPage = () => import('@/features/prescriptions/views/OrgPrescriptionsPage.vue')
const VerifyPrescriptionPage = () =>
  import('@/features/prescriptions/views/VerifyPrescriptionPage.vue')

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
        path: '/directory',
        name: 'directory',
        component: DirectoryPage,
        meta: { layout: 'public' },
      },
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
        path: '/app/search',
        name: 'owner-search',
        component: OwnerSearchPage,
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
        path: '/app/appointments',
        name: 'appointments',
        component: GuardianAppointmentsPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/inbox',
        name: 'inbox',
        component: InboxPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/settings/notifications',
        name: 'notification-settings',
        component: NotificationSettingsPage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/profile',
        name: 'profile',
        component: ProfilePage,
        meta: { layout: 'owner', requiresAuth: true, requiresOnboarding: true },
      },
      {
        path: '/app/profile/credentials',
        name: 'profile-credentials',
        component: CredentialsPage,
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
        path: '/org/:orgId/search',
        name: 'org-search',
        component: OrgSearchPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
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
        path: '/org/:orgId/prescriptions',
        name: 'org-prescriptions',
        component: OrgPrescriptionsPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/agenda',
        name: 'org-agenda',
        component: OrgAgendaPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/settings/agendas',
        name: 'org-agendas',
        component: AgendaSettingsPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: ADMIN_ROLES },
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
      {
        path: '/org/:orgId/medication-suggestions',
        name: 'org-medication-suggestions',
        component: OrgMedicationSuggestionsPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
      },
      {
        path: '/org/:orgId/cohorts',
        name: 'org-cohorts',
        component: OrgCohortsPage,
        meta: { ...orgMeta, requiresOrgType: 'clinic', requiresOrgRole: CLINIC_ROLES },
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
        path: '/admin/catalogs/medications',
        name: 'admin-medications',
        component: AdminMedicationsPage,
        meta: adminMeta,
      },
      {
        path: '/admin/platform',
        name: 'admin-platform',
        component: AdminPlatformPage,
        meta: adminMeta,
      },
      {
        path: '/admin/credentials',
        name: 'admin-credentials',
        component: AdminCredentialsPage,
        meta: adminMeta,
      },
      {
        path: '/unsubscribe/:token',
        name: 'unsubscribe',
        component: UnsubscribePage,
        meta: { layout: 'public' },
      },
      {
        path: '/pet-claims/:token',
        name: 'pet-claim',
        component: PetClaimPage,
        meta: { layout: 'public' },
      },
      {
        path: '/verify/encounters/:encounterId',
        name: 'verify-encounter',
        component: VerifyEncounterPage,
        meta: { layout: 'public' },
      },
      {
        path: '/verify/prescriptions/:id',
        name: 'verify-prescription',
        component: VerifyPrescriptionPage,
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
