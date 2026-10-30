<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Encounter } from '@/shared/api/client'
import AttachmentsPanel from './AttachmentsPanel.vue'
import PdfButton from './PdfButton.vue'
import { BODY_SYSTEMS } from './encounterDraft'

// Vista de solo lectura de una consulta firmada (PP-F2-FE-004, FE-009): sello
// de firma (firmante, fecha, hash abreviado + copiar), SOAP completo, adjuntos
// visibles, adendas numeradas con sus adjuntos, PDF y verificación pública.
// La comparten la clínica y el tutor; el servidor ya filtra lo que el tutor
// no debe ver (adjuntos ocultos).
const props = defineProps<{ encounter: Encounter; organizationName?: string }>()
const { t, te, d } = useI18n()

const e = computed(() => props.encounter)
const bodyAttachments = computed(() =>
  e.value.attachments.filter((a) => a.addendum_seq === undefined && !a.pending_addendum),
)
const hash = computed(() => e.value.signature?.content_hash ?? '')
const shortHash = computed(() =>
  hash.value ? `${hash.value.slice(0, 8)}…${hash.value.slice(-8)}` : '',
)
const verifyHref = computed(() => `/verify/encounters/${e.value.id}?hash=${hash.value}`)
const copied = ref(false)

async function copyHash() {
  try {
    await navigator.clipboard.writeText(hash.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    copied.value = false
  }
}

const vitals = computed(() =>
  Object.entries(e.value.vitals ?? {}).filter(([, v]) => v !== undefined && v !== null),
)
const findings = computed(() =>
  BODY_SYSTEMS.flatMap((s) => {
    const f = e.value.objective?.systems?.[s]
    return f ? [{ system: s, ...f }] : []
  }),
)

function label(prefix: string, key: string) {
  const k = `${prefix}.${key}`
  return te(k) ? t(k) : key
}

function frequency(m: NonNullable<Encounter['plan']['medications']>[number]) {
  if (m.frequency.once) return t('encounters.plan.once')
  return t(`encounters.plan.every_${m.frequency.unit ?? 'hours'}`, { n: m.frequency.every ?? 0 })
}
</script>

<template>
  <article class="space-y-6" data-testid="encounter-detail">
    <header class="space-y-1">
      <p class="text-sm text-surface-500">
        {{ label('encounters.type', e.type) }} · {{ d(e.started_at, 'long') }}
        <template v-if="organizationName"> · {{ organizationName }}</template>
      </p>
      <h1 class="text-2xl font-bold">{{ e.reason }}</h1>
      <p v-if="e.attending_vet" class="text-sm">
        {{ t('encounters.attendingVet') }}: {{ e.attending_vet.display_name }}
      </p>
    </header>

    <section
      v-if="e.signature"
      class="rounded-xl border border-emerald-300 bg-emerald-50 p-4 text-sm dark:border-emerald-800 dark:bg-emerald-950"
      data-testid="signature-seal"
    >
      <p class="font-semibold">
        <span class="pi pi-verified mr-1" aria-hidden="true" />
        {{
          t('encounters.seal.signedBy', {
            name: e.signature.signed_by.display_name,
            date: d(e.signature.signed_at, 'long'),
          })
        }}
      </p>
      <p class="mt-1 flex flex-wrap items-center gap-2">
        <span>{{ t('encounters.seal.hash') }}:</span>
        <code class="font-mono" :title="hash">{{ shortHash }}</code>
        <button type="button" class="underline" data-testid="copy-hash" @click="copyHash">
          {{ copied ? t('orgs.copied') : t('orgs.copy') }}
        </button>
        <RouterLink :to="verifyHref" class="underline">{{
          t('encounters.seal.verify')
        }}</RouterLink>
      </p>
      <div class="mt-3"><PdfButton :encounter-id="e.id" /></div>
    </section>

    <section v-if="vitals.length" aria-labelledby="ro-vitals">
      <h2 id="ro-vitals" class="mb-2 font-semibold">{{ t('encounters.sections.vitals') }}</h2>
      <dl class="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <div v-for="[k, v] in vitals" :key="k">
          <dt class="text-xs text-surface-500">{{ label('encounters.vitals', k) }}</dt>
          <dd>{{ k === 'mucosa' ? label('encounters.mucosa', String(v)) : v }}</dd>
        </div>
      </dl>
    </section>

    <section v-if="e.subjective" aria-labelledby="ro-s">
      <h2 id="ro-s" class="mb-2 font-semibold">{{ t('encounters.sections.subjective') }}</h2>
      <p class="whitespace-pre-line">{{ e.subjective }}</p>
    </section>

    <section v-if="e.objective?.narrative || findings.length" aria-labelledby="ro-o">
      <h2 id="ro-o" class="mb-2 font-semibold">{{ t('encounters.sections.objective') }}</h2>
      <p v-if="e.objective?.narrative" class="whitespace-pre-line">{{ e.objective.narrative }}</p>
      <ul v-if="findings.length" class="mt-2 space-y-1 text-sm">
        <li v-for="f in findings" :key="f.system">
          <strong>{{ label('encounters.systems', f.system) }}</strong
          >: {{ label('encounters.finding', f.status)
          }}<template v-if="f.note"> — {{ f.note }}</template>
        </li>
      </ul>
    </section>

    <section v-if="e.diagnoses.length" aria-labelledby="ro-a">
      <h2 id="ro-a" class="mb-2 font-semibold">{{ t('encounters.sections.assessment') }}</h2>
      <ul class="list-disc pl-5">
        <li v-for="(dx, i) in e.diagnoses" :key="i">
          {{ dx.text }}
          <span class="text-sm text-surface-500"
            >({{ label('encounters.certainty', dx.certainty) }})</span
          >
        </li>
      </ul>
    </section>

    <section
      v-if="e.plan.medications?.length || e.plan.procedures?.length || e.plan.recommendations"
      aria-labelledby="ro-p"
    >
      <h2 id="ro-p" class="mb-2 font-semibold">{{ t('encounters.sections.plan') }}</h2>
      <table v-if="e.plan.medications?.length" class="w-full text-left text-sm">
        <thead>
          <tr class="text-surface-500">
            <th scope="col">{{ t('encounters.plan.medication') }}</th>
            <th scope="col">{{ t('encounters.plan.dose') }}</th>
            <th scope="col">{{ t('encounters.plan.route') }}</th>
            <th scope="col">{{ t('encounters.plan.frequency') }}</th>
            <th scope="col">{{ t('encounters.plan.duration') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(m, i) in e.plan.medications" :key="i">
            <td :data-label="t('encounters.plan.medication')">
              {{ m.medication_name
              }}<template v-if="m.presentation"> ({{ m.presentation }})</template>
              <p v-if="m.instructions" class="text-surface-500">{{ m.instructions }}</p>
            </td>
            <td :data-label="t('encounters.plan.dose')">
              {{ m.dose }} {{ label('encounters.doseUnit', m.dose_unit) }}
            </td>
            <td :data-label="t('encounters.plan.route')">
              {{ label('encounters.route', m.route) }}
            </td>
            <td :data-label="t('encounters.plan.frequency')">{{ frequency(m) }}</td>
            <td :data-label="t('encounters.plan.duration')">
              {{ m.duration_days ? t('encounters.plan.days', { n: m.duration_days }) : '—' }}
            </td>
          </tr>
        </tbody>
      </table>
      <ul v-if="e.plan.procedures?.length" class="mt-2 list-disc pl-5 text-sm">
        <li v-for="(p, i) in e.plan.procedures" :key="i">
          {{ p.name }}<template v-if="p.notes"> — {{ p.notes }}</template>
        </li>
      </ul>
      <p v-if="e.plan.recommendations" class="mt-2 whitespace-pre-line">
        {{ e.plan.recommendations }}
      </p>
    </section>

    <section v-if="e.follow_up" aria-labelledby="ro-f">
      <h2 id="ro-f" class="mb-2 font-semibold">{{ t('encounters.sections.followUp') }}</h2>
      <p>{{ d(e.follow_up.due_date + 'T12:00:00', 'short') }} — {{ e.follow_up.reason }}</p>
    </section>

    <section aria-labelledby="ro-att">
      <h2 id="ro-att" class="mb-2 font-semibold">{{ t('encounters.sections.attachments') }}</h2>
      <AttachmentsPanel :encounter-id="e.id" :attachments="bodyAttachments" />
    </section>

    <section v-if="e.addenda.length" aria-labelledby="ro-add" data-testid="addenda">
      <h2 id="ro-add" class="mb-2 font-semibold">{{ t('encounters.addenda.title') }}</h2>
      <ol class="space-y-4">
        <li
          v-for="a in e.addenda"
          :key="a.seq"
          class="rounded-lg border border-surface-200 p-3 dark:border-surface-800"
        >
          <p class="text-sm text-surface-500">
            {{ t('encounters.addenda.item', { n: a.seq }) }} ·
            {{ label('encounters.addendumReason', a.reason) }} · {{ a.signed_by.display_name }} ·
            {{ d(a.signed_at, 'long') }}
          </p>
          <p class="mt-1 whitespace-pre-line">{{ a.text }}</p>
          <AttachmentsPanel
            v-if="a.attachments.length"
            class="mt-2"
            :encounter-id="e.id"
            :attachments="a.attachments"
          />
        </li>
      </ol>
    </section>
    <slot name="after" />
  </article>
</template>
