<script setup lang="ts">
import { computed, nextTick, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PublicLayout from './layouts/PublicLayout.vue'
import OwnerLayout from './layouts/OwnerLayout.vue'
import OrgLayout from './layouts/OrgLayout.vue'
import AdminLayout from './layouts/AdminLayout.vue'
import StatusBanner from '@/features/status/StatusBanner.vue'
import { useStatusStore } from '@/features/status/statusStore'

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
const statusStore = useStatusStore()

onMounted(() => {
  // Comprueba periódicamente status.json para el banner global de incidentes (FE-009, OPS-007)
  statusStore.startPolling(60000)
})

// Navegación SPA accesible (FE-010, WCAG 2.4.3): tras cambiar de ruta el foco va
// a <main> para que lectores de pantalla y teclado empiecen por el contenido.
router.afterEach((to, from) => {
  if (!from.matched.length || to.path === from.path) return
  void nextTick(() => document.getElementById('main')?.focus({ preventScroll: true }))
})

const layout = computed(() => layouts[(route.meta.layout as LayoutKey) ?? 'public'])
</script>

<template>
  <StatusBanner />
  <component :is="layout">
    <RouterView />
  </component>
</template>
