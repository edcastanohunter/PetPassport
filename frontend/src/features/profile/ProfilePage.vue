<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Me, type UpdateMeRequest } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { runtimeConfig } from '@/shared/config/runtime'
import { COUNTRIES } from '@/shared/i18n/countries'
import FieldError from '@/shared/ui/FieldError.vue'

// Perfil (FE-005, PRD-004): PATCH /me con semántica merge-patch (null vacía).
// Email de solo lectura con enlace a la consola de cuenta de Keycloak.
const { t, locale } = useI18n()
const session = useSessionStore()

const saving = ref(false)
const saved = ref(false)
const fieldErrors = ref<Record<string, string>>({})
const generalError = ref<string | null>(null)

const form = reactive({
  displayName: '',
  locale: 'es',
  country: '',
  phone: '',
  address: {
    enabled: false,
    country: '',
    region: '',
    city: '',
    line1: '',
    line2: '',
    postal_code: '',
  },
  contact: { enabled: false, name: '', phone: '', relationship: '' },
})

const accountUrl = computed(() => `${runtimeConfig().oidc.issuer}/account`)
const securityUrl = computed(
  () => `${runtimeConfig().oidc.issuer}/account/account-security/signing-in`,
)

function fill(me: Me) {
  form.displayName = me.display_name
  form.locale = me.locale
  form.country = me.country ?? ''
  form.phone = me.phone ?? ''
  form.address.enabled = !!me.address
  form.address.country = me.address?.country ?? ''
  form.address.region = me.address?.region ?? ''
  form.address.city = me.address?.city ?? ''
  form.address.line1 = me.address?.line1 ?? ''
  form.address.line2 = me.address?.line2 ?? ''
  form.address.postal_code = me.address?.postal_code ?? ''
  form.contact.enabled = !!me.emergency_contact
  form.contact.name = me.emergency_contact?.name ?? ''
  form.contact.phone = me.emergency_contact?.phone ?? ''
  form.contact.relationship = me.emergency_contact?.relationship ?? ''
}

onMounted(async () => {
  const me = session.me ?? (await session.loadMe())
  if (me) fill(me)
})

function orUndefined(s: string) {
  const v = s.trim()
  return v === '' ? undefined : v
}

function buildPatch(me: Me): UpdateMeRequest {
  const patch: UpdateMeRequest = {}
  if (form.displayName.trim() !== me.display_name) patch.display_name = form.displayName.trim()
  if (form.locale !== me.locale) patch.locale = form.locale as 'es' | 'en'
  if ((form.country || null) !== (me.country ?? null))
    patch.country = form.country ? form.country : null
  if ((form.phone.trim() || null) !== (me.phone ?? null))
    patch.phone = form.phone.trim() ? form.phone.trim() : null
  if (form.address.enabled) {
    patch.address = {
      country: form.address.country,
      city: form.address.city.trim(),
      line1: form.address.line1.trim(),
      region: orUndefined(form.address.region),
      line2: orUndefined(form.address.line2),
      postal_code: orUndefined(form.address.postal_code),
    }
  } else if (me.address) {
    patch.address = null
  }
  if (form.contact.enabled) {
    patch.emergency_contact = {
      name: form.contact.name.trim(),
      phone: form.contact.phone.trim(),
      relationship: orUndefined(form.contact.relationship),
    }
  } else if (me.emergency_contact) {
    patch.emergency_contact = null
  }
  return patch
}

async function save() {
  const me = session.me
  if (!me || saving.value) return
  const patch = buildPatch(me)
  fieldErrors.value = {}
  generalError.value = null
  saved.value = false
  if (Object.keys(patch).length === 0) {
    saved.value = true
    return
  }
  saving.value = true
  try {
    const { data } = await api().PATCH('/me', { body: patch })
    if (data) {
      session.setMe(data)
      fill(data)
      locale.value = data.locale
    }
    saved.value = true
  } catch (e) {
    if (e instanceof ApiError) {
      fieldErrors.value = e.fieldErrors()
      if (e.code !== 'VALIDATION_FAILED')
        generalError.value = t(`errors.${e.code}`, t('errors.GENERIC'))
    } else {
      generalError.value = t('errors.GENERIC')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="mx-auto max-w-2xl space-y-8" aria-labelledby="profile-title">
    <h1 id="profile-title" class="text-2xl font-bold">{{ t('profile.title') }}</h1>

    <p
      v-if="generalError"
      role="alert"
      class="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-700"
    >
      {{ generalError }}
    </p>
    <p
      v-if="saved"
      role="status"
      class="rounded border border-green-300 bg-green-50 p-3 text-sm text-green-700"
    >
      {{ t('profile.saved') }}
    </p>

    <form v-if="session.me" class="space-y-6" @submit.prevent="save">
      <div>
        <label for="email" class="label">{{ t('profile.email') }}</label>
        <input id="email" :value="session.me.email" class="input" readonly />
        <a :href="accountUrl" class="text-sm underline" rel="noopener">{{
          t('profile.manageAccount')
        }}</a>
      </div>
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
        <FieldError :code="fieldErrors.display_name" />
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label for="country" class="label">{{ t('profile.country') }}</label>
          <select id="country" v-model="form.country" class="input">
            <option value="">—</option>
            <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
          <FieldError :code="fieldErrors.country" />
        </div>
        <div>
          <label for="locale" class="label">{{ t('profile.locale') }}</label>
          <select id="locale" v-model="form.locale" class="input">
            <option value="es">Español</option>
            <option value="en">English</option>
          </select>
        </div>
      </div>
      <div>
        <label for="phone" class="label">{{ t('profile.phone') }}</label>
        <input
          id="phone"
          v-model="form.phone"
          class="input"
          type="tel"
          inputmode="tel"
          placeholder="+57 300 123 4567"
          autocomplete="tel"
        />
        <FieldError :code="fieldErrors.phone" />
      </div>

      <fieldset class="space-y-3 rounded border border-surface-200 p-4 dark:border-surface-800">
        <legend class="px-1 font-semibold">
          <label class="flex items-center gap-2"
            ><input v-model="form.address.enabled" type="checkbox" />
            {{ t('profile.address') }}</label
          >
        </legend>
        <template v-if="form.address.enabled">
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label for="addr_country" class="label">{{ t('profile.country') }}</label>
              <select id="addr_country" v-model="form.address.country" class="input" required>
                <option value="" disabled>{{ t('common.select') }}</option>
                <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
              </select>
              <FieldError :code="fieldErrors['address.country']" />
            </div>
            <div>
              <label for="addr_region" class="label">{{ t('profile.region') }}</label>
              <input id="addr_region" v-model="form.address.region" class="input" maxlength="120" />
            </div>
            <div>
              <label for="addr_city" class="label">{{ t('profile.city') }}</label>
              <input
                id="addr_city"
                v-model="form.address.city"
                class="input"
                maxlength="120"
                required
              />
              <FieldError :code="fieldErrors['address.city']" />
            </div>
            <div>
              <label for="addr_postal" class="label">{{ t('profile.postalCode') }}</label>
              <input
                id="addr_postal"
                v-model="form.address.postal_code"
                class="input"
                maxlength="20"
              />
            </div>
          </div>
          <div>
            <label for="addr_line1" class="label">{{ t('profile.line1') }}</label>
            <input
              id="addr_line1"
              v-model="form.address.line1"
              class="input"
              maxlength="200"
              required
            />
            <FieldError :code="fieldErrors['address.line1']" />
          </div>
          <div>
            <label for="addr_line2" class="label">{{ t('profile.line2') }}</label>
            <input id="addr_line2" v-model="form.address.line2" class="input" maxlength="200" />
          </div>
        </template>
      </fieldset>

      <fieldset class="space-y-3 rounded border border-surface-200 p-4 dark:border-surface-800">
        <legend class="px-1 font-semibold">
          <label class="flex items-center gap-2"
            ><input v-model="form.contact.enabled" type="checkbox" />
            {{ t('profile.emergencyContact') }}</label
          >
        </legend>
        <template v-if="form.contact.enabled">
          <div>
            <label for="ec_name" class="label">{{ t('profile.contactName') }}</label>
            <input
              id="ec_name"
              v-model="form.contact.name"
              class="input"
              maxlength="120"
              required
            />
            <FieldError :code="fieldErrors['emergency_contact.name']" />
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label for="ec_phone" class="label">{{ t('profile.phone') }}</label>
              <input id="ec_phone" v-model="form.contact.phone" class="input" type="tel" required />
              <FieldError :code="fieldErrors['emergency_contact.phone']" />
            </div>
            <div>
              <label for="ec_rel" class="label">{{ t('profile.relationship') }}</label>
              <input id="ec_rel" v-model="form.contact.relationship" class="input" maxlength="60" />
            </div>
          </div>
        </template>
      </fieldset>

      <div class="flex justify-end">
        <button type="submit" class="btn-primary" :disabled="saving">{{ t('common.save') }}</button>
      </div>
    </form>
    <p v-else>{{ t('common.loading') }}</p>

    <section class="space-y-2" aria-labelledby="security-title">
      <h2 id="security-title" class="text-lg font-semibold">{{ t('profile.security') }}</h2>
      <p class="text-sm text-surface-500">{{ t('profile.securityHint') }}</p>
      <a :href="securityUrl" class="text-sm underline" rel="noopener">{{
        t('profile.configureOtp')
      }}</a>
    </section>
  </section>
</template>
