<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type Organization } from '@/shared/api/client'
import { useSessionStore } from '@/features/auth/sessionStore'
import { useCommand } from '@/shared/composables/useCommand'
import { countries } from '@/shared/i18n/countries'
import {
  organizationFormSchema,
  ORG_TYPES,
  type OrganizationForm,
} from '@/shared/validation/schemas'
import FieldError from '@/shared/ui/FieldError.vue'

// Registro de organización (FE-007, PRD-011): asistente tipo → datos legales
// y fiscales (validación por país) → dirección → contacto → revisión. Al
// crear muestra el PublicCode y el estado "pendiente de verificación".
const { t, locale } = useI18n()
const COUNTRIES = computed(() => countries(locale.value))
const router = useRouter()
const session = useSessionStore()
const { key: idempotencyKey, renew } = useCommand()

const steps = ['type', 'legal', 'address', 'contact', 'review'] as const
type Step = (typeof steps)[number]
const step = ref<Step>('type')
const stepIndex = computed(() => steps.indexOf(step.value))

const form = reactive<OrganizationForm>({
  type: 'clinic',
  legal_name: '',
  display_name: '',
  tax_country: session.me?.country ?? 'CO',
  tax_value: '',
  address: {
    country: session.me?.country ?? 'CO',
    region: '',
    city: '',
    line1: '',
    line2: '',
    postal_code: '',
  },
  contact: { email: session.me?.email ?? '', phone: '', website: '' },
})
const fieldErrors = ref<Record<string, string>>({})
const generalError = ref<string | null>(null)
const submitting = ref(false)
const created = ref<Organization | null>(null)

// Campos que valida cada paso (prefijos de la ruta del issue zod).
const stepFields: Record<Step, string[]> = {
  type: ['type'],
  legal: ['legal_name', 'display_name', 'tax_country', 'tax_value'],
  address: ['address'],
  contact: ['contact'],
  review: [],
}

function validate(only?: string[]): boolean {
  const parsed = organizationFormSchema().safeParse(form)
  const errs: Record<string, string> = {}
  if (!parsed.success) {
    for (const issue of parsed.error.issues) {
      const field = issue.path.join('.')
      if (only && !only.some((f) => field === f || field.startsWith(`${f}.`))) continue
      if (!errs[field]) errs[field] = 'INVALID'
    }
  }
  fieldErrors.value = errs
  return Object.keys(errs).length === 0
}

function next() {
  if (!validate(stepFields[step.value])) return
  step.value = steps[Math.min(stepIndex.value + 1, steps.length - 1)]
}

function back() {
  step.value = steps[Math.max(stepIndex.value - 1, 0)]
}

function optional(v?: string) {
  const s = v?.trim()
  return s ? s : undefined
}

async function submit() {
  if (!validate()) {
    const first = Object.keys(fieldErrors.value)[0] ?? ''
    step.value = steps.find((s) => stepFields[s].some((f) => first.startsWith(f))) ?? 'legal'
    return
  }
  submitting.value = true
  generalError.value = null
  try {
    const { data, error } = await api().POST('/organizations', {
      params: { header: { 'Idempotency-Key': idempotencyKey.value } },
      body: {
        type: form.type,
        legal_name: form.legal_name.trim(),
        display_name: form.display_name.trim(),
        tax_id: { country: form.tax_country.toUpperCase(), value: form.tax_value.trim() },
        address: {
          country: form.address.country.toUpperCase(),
          region: optional(form.address.region),
          city: form.address.city.trim(),
          line1: form.address.line1.trim(),
          line2: optional(form.address.line2),
          postal_code: optional(form.address.postal_code),
        },
        contact: {
          email: form.contact.email.trim(),
          phone: form.contact.phone.trim(),
          website: optional(form.contact.website) ?? null,
        },
      },
    })
    if (error) throw error
    created.value = data as Organization
    // La membresía de admin aparece en /me (contexto Org en la barra).
    await session.loadMe(true)
  } catch (e) {
    renew()
    if (e instanceof ApiError) {
      const errs = e.fieldErrors()
      if (errs['tax_id'] === 'ALREADY_REGISTERED') {
        fieldErrors.value = { tax_value: 'ALREADY_REGISTERED' }
        step.value = 'legal'
      } else if (Object.keys(errs).length) {
        fieldErrors.value = errs
      }
      generalError.value = t(`errors.${e.code}`, t('errors.GENERIC'))
    } else {
      generalError.value = t('errors.GENERIC')
    }
  } finally {
    submitting.value = false
  }
}

function err(field: string) {
  return fieldErrors.value[field]
}
</script>

<template>
  <section class="mx-auto max-w-xl space-y-6">
    <h1 class="text-2xl font-bold">{{ t('orgs.register.title') }}</h1>

    <div
      v-if="created"
      class="space-y-4 rounded-xl border border-green-200 bg-green-50 p-6 dark:border-green-900 dark:bg-green-950"
      data-testid="org-created"
    >
      <p class="font-semibold">{{ t('orgs.register.created', { name: created.display_name }) }}</p>
      <p>
        {{ t('orgs.publicCode') }}:
        <span class="font-mono text-2xl font-bold tracking-widest">{{ created.public_code }}</span>
      </p>
      <p class="text-sm">{{ t('orgs.status.pending_verification_hint') }}</p>
      <button
        type="button"
        class="btn-primary"
        @click="router.push({ name: 'org-summary', params: { orgId: created.id } })"
      >
        {{ t('orgs.register.goToOrg') }}
      </button>
    </div>

    <template v-else>
      <ol class="flex flex-wrap gap-2 text-sm" :aria-label="t('orgs.register.steps')">
        <li
          v-for="(s, i) in steps"
          :key="s"
          class="rounded-full px-3 py-1"
          :class="
            i === stepIndex
              ? 'bg-surface-900 text-white dark:bg-surface-100 dark:text-surface-900'
              : 'bg-surface-100 text-surface-600 dark:bg-surface-800 dark:text-surface-300'
          "
          :aria-current="i === stepIndex ? 'step' : undefined"
        >
          {{ i + 1 }}. {{ t(`orgs.register.step.${s}`) }}
        </li>
      </ol>

      <p
        v-if="generalError"
        class="rounded-lg bg-red-50 p-4 text-red-700 dark:bg-red-950 dark:text-red-300"
        role="alert"
      >
        {{ generalError }}
      </p>

      <form class="space-y-5" novalidate @submit.prevent="step === 'review' ? submit() : next()">
        <fieldset v-if="step === 'type'" class="grid gap-3 sm:grid-cols-2">
          <legend class="mb-2 text-sm font-medium">{{ t('orgs.form.type') }}</legend>
          <label
            v-for="ty in ORG_TYPES"
            :key="ty"
            class="card flex cursor-pointer items-start gap-3"
            :class="form.type === ty ? 'ring-2 ring-green-600' : ''"
          >
            <input v-model="form.type" type="radio" name="org-type" :value="ty" class="mt-1" />
            <span>
              <span class="block font-semibold">{{ t(`orgs.type.${ty}`) }}</span>
              <span class="block text-sm text-surface-500">{{ t(`orgs.typeHint.${ty}`) }}</span>
            </span>
          </label>
        </fieldset>

        <template v-if="step === 'legal'">
          <div>
            <label class="block text-sm font-medium" for="org-legal">{{
              t('orgs.form.legalName')
            }}</label>
            <input id="org-legal" v-model="form.legal_name" maxlength="120" class="input" />
            <FieldError field="org-legal" :code="err('legal_name')" />
          </div>
          <div>
            <label class="block text-sm font-medium" for="org-display">{{
              t('orgs.form.displayName')
            }}</label>
            <input id="org-display" v-model="form.display_name" maxlength="80" class="input" />
            <FieldError field="org-display" :code="err('display_name')" />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="block text-sm font-medium" for="org-tax-country">{{
                t('orgs.form.taxCountry')
              }}</label>
              <select id="org-tax-country" v-model="form.tax_country" class="input">
                <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
              </select>
            </div>
            <div class="col-span-2">
              <label class="block text-sm font-medium" for="org-tax-value">{{
                t('orgs.form.taxValue')
              }}</label>
              <input
                id="org-tax-value"
                v-model="form.tax_value"
                maxlength="32"
                class="input font-mono"
                :placeholder="form.tax_country === 'CO' ? '900123456-8' : ''"
                aria-describedby="org-tax-hint"
              />
              <p id="org-tax-hint" class="mt-1 text-xs text-surface-500">
                {{
                  form.tax_country === 'CO'
                    ? t('orgs.form.taxHintCO')
                    : t('orgs.form.taxHintGeneric')
                }}
              </p>
              <FieldError field="org-tax-value" :code="err('tax_value')" />
            </div>
          </div>
        </template>

        <template v-if="step === 'address'">
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-sm font-medium" for="org-country">{{
                t('orgs.form.country')
              }}</label>
              <select id="org-country" v-model="form.address.country" class="input">
                <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
              </select>
            </div>
            <div>
              <label class="block text-sm font-medium" for="org-region">{{
                t('orgs.form.region')
              }}</label>
              <input id="org-region" v-model="form.address.region" maxlength="120" class="input" />
            </div>
          </div>
          <div>
            <label class="block text-sm font-medium" for="org-city">{{
              t('orgs.form.city')
            }}</label>
            <input id="org-city" v-model="form.address.city" maxlength="120" class="input" />
            <FieldError field="org-city" :code="err('address.city')" />
          </div>
          <div>
            <label class="block text-sm font-medium" for="org-line1">{{
              t('orgs.form.line1')
            }}</label>
            <input id="org-line1" v-model="form.address.line1" maxlength="200" class="input" />
            <FieldError field="org-line1" :code="err('address.line1')" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-sm font-medium" for="org-line2">{{
                t('orgs.form.line2')
              }}</label>
              <input id="org-line2" v-model="form.address.line2" maxlength="200" class="input" />
            </div>
            <div>
              <label class="block text-sm font-medium" for="org-postal">{{
                t('orgs.form.postalCode')
              }}</label>
              <input
                id="org-postal"
                v-model="form.address.postal_code"
                maxlength="20"
                class="input"
              />
            </div>
          </div>
        </template>

        <template v-if="step === 'contact'">
          <div>
            <label class="block text-sm font-medium" for="org-email">{{
              t('orgs.form.email')
            }}</label>
            <input id="org-email" v-model="form.contact.email" type="email" class="input" />
            <FieldError field="org-email" :code="err('contact.email')" />
          </div>
          <div>
            <label class="block text-sm font-medium" for="org-phone">{{
              t('orgs.form.phone')
            }}</label>
            <input
              id="org-phone"
              v-model="form.contact.phone"
              type="tel"
              placeholder="+573001234567"
              class="input"
            />
            <FieldError field="org-phone" :code="err('contact.phone')" />
          </div>
          <div>
            <label class="block text-sm font-medium" for="org-web">
              {{ t('orgs.form.website') }} ({{ t('common.optional') }})
            </label>
            <input
              id="org-web"
              v-model="form.contact.website"
              type="url"
              placeholder="https://"
              class="input"
            />
            <FieldError field="org-web" :code="err('contact.website')" />
          </div>
        </template>

        <dl
          v-if="step === 'review'"
          class="grid grid-cols-1 gap-3 rounded-xl border border-surface-200 p-4 text-sm sm:grid-cols-2 dark:border-surface-700"
          data-testid="org-review"
        >
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.type') }}</dt>
            <dd>{{ t(`orgs.type.${form.type}`) }}</dd>
          </div>
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.displayName') }}</dt>
            <dd>{{ form.display_name }}</dd>
          </div>
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.legalName') }}</dt>
            <dd>{{ form.legal_name }}</dd>
          </div>
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.taxValue') }}</dt>
            <dd class="font-mono">{{ form.tax_country }} {{ form.tax_value }}</dd>
          </div>
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.address') }}</dt>
            <dd>{{ form.address.line1 }}, {{ form.address.city }} ({{ form.address.country }})</dd>
          </div>
          <div>
            <dt class="text-surface-500">{{ t('orgs.form.contact') }}</dt>
            <dd>{{ form.contact.email }} · {{ form.contact.phone }}</dd>
          </div>
          <p class="sm:col-span-2 text-surface-500">{{ t('orgs.register.reviewHint') }}</p>
        </dl>

        <div class="flex justify-between">
          <button
            v-if="stepIndex > 0"
            type="button"
            class="btn-secondary"
            :disabled="submitting"
            @click="back"
          >
            {{ t('common.back') }}
          </button>
          <span v-else></span>
          <button type="submit" class="btn-primary" :disabled="submitting" data-testid="org-next">
            {{
              step === 'review'
                ? submitting
                  ? t('common.saving')
                  : t('orgs.register.submit')
                : t('common.next')
            }}
          </button>
        </div>
      </form>
    </template>
  </section>
</template>
