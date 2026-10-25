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
import { authGuard } from './guards'

// Tabla de rutas de docs/phase-1/07 §4. Mascotas (M1.2, FE-006) sin vínculos
// ni transferencia (M1.3); organizaciones (M1.3) y admin (M1.4) se añaden con
// sus hitos.
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
