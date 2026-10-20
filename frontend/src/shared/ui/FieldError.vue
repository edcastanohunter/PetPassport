<script setup lang="ts">
import { useI18n } from 'vue-i18n'
// Mensaje de error por campo a partir del código de `errors[]` (FE-003).
defineProps<{ code?: string }>()
const { t } = useI18n()
</script>

<template>
  <p v-if="code" class="mt-1 text-sm text-red-600" role="alert">
    {{ t(`fieldErrors.${code}`, t('fieldErrors.INVALID')) }}
  </p>
</template>
