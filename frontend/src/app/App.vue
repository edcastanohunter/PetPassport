<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
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
const layout = computed(() => layouts[(route.meta.layout as LayoutKey) ?? 'public'])
</script>

<template>
  <component :is="layout">
    <RouterView />
  </component>
</template>
