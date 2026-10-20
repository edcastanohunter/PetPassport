<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>

<template>
  <section class="space-y-4">
    <h1 class="text-2xl font-bold">{{ t('notFound.title') }}</h1>
    <RouterLink to="/" class="underline">{{ t('notFound.back') }}</RouterLink>
  </section>
</template>
