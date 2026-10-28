<script setup lang="ts">
import { computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PublicLayout from './layouts/PublicLayout.vue'
import OwnerLayout from './layouts/OwnerLayout.vue'
import OrgLayout from './layouts/OrgLayout.vue'
import AdminLayout from './layouts/AdminLayout.vue'

// El layout se elige por meta.layout de la ruta (docs/phase-1/07 §4).
const layouts = {
  public: PublicLayout,
  owner: OwnerLayout,
  org: OrgLayout,
  admin: AdminLayout,
} as const
type LayoutKey = keyof typeof layouts

const route = useRoute()
const router = useRouter()

// Navegación SPA accesible (FE-010, WCAG 2.4.3): tras cambiar de ruta el foco va
// a <main> para que lectores de pantalla y teclado empiecen por el contenido.
router.afterEach((to, from) => {
  if (!from.matched.length || to.path === from.path) return
  void nextTick(() => document.getElementById('main')?.focus({ preventScroll: true }))
})

const layout = computed(() => layouts[(route.meta.layout as LayoutKey) ?? 'public'])
</script>

<template>
  <component :is="layout">
    <RouterView />
  </component>
</template>
