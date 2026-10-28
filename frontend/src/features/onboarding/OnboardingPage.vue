<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type LegalCurrent } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { returnToFrom } from '@/app/router/guards'
import { countries } from '@/shared/i18n/countries'
import FieldError from '@/shared/ui/FieldError.vue'

// Onboarding en 3 pasos (FE-005, PRD-003, PRD-020): consentimiento → datos
// básicos → elección de camino: "Soy tutor" va a /app y "Registrar
// organización" al asistente /org/new (M1.3, FE-007).
const { t, locale } = useI18n()
const COUNTRIES = computed(() => countries(locale.value))
const router = useRouter()
const route = useRoute()
const session = useSessionStore()

const step = ref<1 | 2 | 3>(1)
const legal = ref<LegalCurrent | null>(null)
const loadError = ref(false)
const submitting = ref(false)
const fieldErrors = ref<Record<string, string>>({})
const generalError = ref<string | null>(null)

const form = reactive({
  acceptTerms: false,
  acceptPrivacy: false,
  displayName: '',
  country: '',
  locale: locale.value === 'en' ? 'en' : 'es',
  phone: '',
})

onMounted(async () => {
  try {
    const { data } = await api().GET('/legal/current')
    legal.value = data ?? null
  } catch {
    loadError.value = true
  }
  const me = session.me ?? (await session.loadMe().catch(() => null))
  if (me) {
    form.displayName = me.display_name
    if (me.country) form.country = me.country
    if (me.locale) form.locale = me.locale
    if (me.phone) form.phone = me.phone
    if (me.consent && !me.consent_outdated) {
      form.acceptTerms = form.acceptPrivacy = true
    }
  }
})

const consentOk = computed(() => form.acceptTerms && form.acceptPrivacy && !!legal.value)
const basicsOk = computed(() => form.displayName.trim().length > 0 && form.country.length === 2)

function next() {
  if (step.value === 1 && consentOk.value) step.value = 2
  else if (step.value === 2 && basicsOk.value) step.value = 3
}
function back() {
  if (step.value > 1) step.value = (step.value - 1) as 1 | 2
}

async function submit(target?: string) {
  if (!legal.value || submitting.value) return
  submitting.value = true
  fieldErrors.value = {}
  generalError.value = null
  try {
    const { data } = await api().POST('/me/onboarding', {
      body: {
        display_name: form.displayName.trim(),
        country: form.country,
        locale: form.locale as 'es' | 'en',
        phone: form.phone.trim() || undefined,
        accept_terms_version: legal.value.terms_version,
        accept_privacy_version: legal.value.privacy_version,
      },
    })
    if (data) {
      session.setMe(data)
    }
    await router.replace(target ?? returnToFrom(route))
  } catch (e) {
    if (e instanceof ApiError) {
      fieldErrors.value = e.fieldErrors()
      if (e.code === 'CONSENT_VERSION_OUTDATED') {
        generalError.value = t('errors.CONSENT_VERSION_OUTDATED')
        step.value = 1
        const { data } = await api().GET('/legal/current')
        legal.value = data ?? legal.value
      } else if (e.code !== 'VALIDATION_FAILED') {
        generalError.value = t(`errors.${e.code}`, t('errors.GENERIC'))
      } else if (Object.keys(fieldErrors.value).length > 0) {
        step.value = 2
      }
    } else {
      generalError.value = t('errors.GENERIC')
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section class="mx-auto max-w-xl space-y-6" aria-labelledby="onboarding-title">
    <header class="space-y-1">
      <h1 id="onboarding-title" class="text-2xl font-bold">{{ t('onboarding.title') }}</h1>
      <p class="text-sm text-surface-500">{{ t('onboarding.step', { n: step, total: 3 }) }}</p>
    </header>

    <p
      v-if="generalError"
      role="alert"
      class="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700"
    >
      {{ generalError }}
    </p>

    <!-- Paso 1: consentimiento -->
    <form v-if="step === 1" class="space-y-4" data-testid="step-consent" @submit.prevent="next">
      <p v-if="loadError" role="alert" class="text-red-600">{{ t('errors.GENERIC') }}</p>
      <template v-else-if="legal">
        <label class="flex items-start gap-2">
          <input
            v-model="form.acceptTerms"
            type="checkbox"
            class="mt-1"
            data-testid="accept-terms"
          />
          <span>
            <i18n-t keypath="onboarding.acceptTerms" tag="span">
              <template #link>
                <RouterLink to="/legal/terms" target="_blank" class="underline">{{
                  t('legal.terms')
                }}</RouterLink>
              </template>
            </i18n-t>
            <span class="block text-xs text-surface-500"
              >{{ t('legal.version') }} {{ legal.terms_version }}</span
            >
          </span>
        </label>
        <label class="flex items-start gap-2">
          <input
            v-model="form.acceptPrivacy"
            type="checkbox"
            class="mt-1"
            data-testid="accept-privacy"
          />
          <span>
            <i18n-t keypath="onboarding.acceptPrivacy" tag="span">
              <template #link>
                <RouterLink to="/legal/privacy" target="_blank" class="underline">{{
                  t('legal.privacy')
                }}</RouterLink>
              </template>
            </i18n-t>
            <span class="block text-xs text-surface-500"
              >{{ t('legal.version') }} {{ legal.privacy_version }}</span
            >
          </span>
        </label>
      </template>
      <p v-else>{{ t('common.loading') }}</p>
      <div class="flex justify-end">
        <button type="submit" class="btn-primary" :disabled="!consentOk" data-testid="next">
          {{ t('common.next') }}
        </button>
      </div>
    </form>

    <!-- Paso 2: datos básicos -->
    <form v-else-if="step === 2" class="space-y-4" data-testid="step-basics" @submit.prevent="next">
      <div>
        <label for="display_name" class="label">{{ t('profile.displayName') }}</label>
        <input
          id="display_name"
          v-model="form.displayName"
          class="input"
          maxlength="120"
          required
          autocomplete="name"
        />
        <FieldError field="display_name" :code="fieldErrors.display_name" />
      </div>
      <div>
        <label for="country" class="label">{{ t('profile.country') }}</label>
        <select id="country" v-model="form.country" class="input" required autocomplete="country">
          <option value="" disabled>{{ t('common.select') }}</option>
          <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
        </select>
        <FieldError field="country" :code="fieldErrors.country" />
      </div>
      <div>
        <label for="locale" class="label">{{ t('profile.locale') }}</label>
        <select id="locale" v-model="form.locale" class="input">
          <option value="es" lang="es">Español</option>
          <option value="en" lang="en">English</option>
        </select>
      </div>
      <div>
        <label for="phone" class="label"
          >{{ t('profile.phone') }} ({{ t('common.optional') }})</label
        >
        <input
          id="phone"
          v-model="form.phone"
          class="input"
          type="tel"
          inputmode="tel"
          placeholder="+57 300 123 4567"
          autocomplete="tel"
        />
        <FieldError field="phone" :code="fieldErrors.phone" />
      </div>
      <div class="flex justify-between">
        <button type="button" class="btn-secondary" @click="back">{{ t('common.back') }}</button>
        <button type="submit" class="btn-primary" :disabled="!basicsOk" data-testid="next">
          {{ t('common.next') }}
        </button>
      </div>
    </form>

    <!-- Paso 3: elección de camino -->
    <div v-else class="space-y-4" data-testid="step-path">
      <p>{{ t('onboarding.choosePath') }}</p>
      <div class="grid gap-3 sm:grid-cols-2">
        <button
          type="button"
          class="card"
          :disabled="submitting"
          data-testid="path-owner"
          @click="submit()"
        >
          <span class="block font-semibold">{{ t('onboarding.pathOwner') }}</span>
          <span class="block text-sm text-surface-500">{{ t('onboarding.pathOwnerHint') }}</span>
        </button>
        <button
          type="button"
          class="card"
          :disabled="submitting"
          data-testid="path-org"
          @click="submit('/org/new')"
        >
          <span class="block font-semibold">{{ t('onboarding.pathOrg') }}</span>
          <span class="block text-sm text-surface-500">{{ t('onboarding.pathOrgHint') }}</span>
        </button>
      </div>
      <div class="flex justify-between">
        <button type="button" class="btn-secondary" :disabled="submitting" @click="back">
          {{ t('common.back') }}
        </button>
        <button type="button" class="underline text-sm" :disabled="submitting" @click="submit()">
          {{ t('onboarding.skip') }}
        </button>
      </div>
    </div>
  </section>
</template>
