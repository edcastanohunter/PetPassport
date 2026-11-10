<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, type PublicPrescriptionVerification } from '@/shared/api/client'

const route = useRoute()
const prescriptionId = route.params.id as string
const expectedHash = (route.query.h as string | undefined) ?? ''

const verification = ref<PublicPrescriptionVerification | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

async function verifyPrescription() {
  loading.value = true
  error.value = null
  try {
    const res = await api().GET('/verify/prescriptions/{prescriptionId}', {
      params: {
        path: { prescriptionId },
        query: expectedHash ? { h: expectedHash } : undefined,
      },
    })
    if (res.error) {
      error.value = 'No se encontró la receta o el identificador es inválido.'
      return
    }
    verification.value = res.data ?? null
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Error al verificar receta'
  } finally {
    loading.value = false
  }
}

const hashMatches = computed(() => {
  if (!expectedHash || !verification.value?.content_hash) return null
  return expectedHash.toLowerCase() === verification.value.content_hash.toLowerCase()
})

const statusConfig = computed(() => {
  const st = verification.value?.status
  switch (st) {
    case 'issued':
    case 'confirmed':
      return {
        label: 'RECETA MÉDICA VÁLIDA',
        sub: 'Documento original firmado electrónicamente por un profesional verificado.',
        class: 'bg-emerald-600 text-white',
        cardBorder: 'border-emerald-200',
        badge: 'bg-emerald-100 text-emerald-800',
        icon: '✓',
      }
    case 'revoked':
      return {
        label: 'RECETA REVOCADA',
        sub: 'Esta receta ha sido revocada por el emisor y carece de validez legal para dispensación.',
        class: 'bg-rose-600 text-white',
        cardBorder: 'border-rose-200',
        badge: 'bg-rose-100 text-rose-800',
        icon: '✕',
      }
    case 'expired':
      return {
        label: 'RECETA VENCIDA',
        sub: 'El periodo de vigencia legal para dispensación de esta receta ha concluido.',
        class: 'bg-orange-600 text-white',
        cardBorder: 'border-orange-200',
        badge: 'bg-orange-100 text-orange-800',
        icon: '⏱',
      }
    case 'exhausted':
      return {
        label: 'RECETA AGOTADA',
        sub: 'La cantidad autorizada y los refills permitidos han sido totalmente dispensados.',
        class: 'bg-slate-700 text-white',
        cardBorder: 'border-slate-300',
        badge: 'bg-slate-100 text-slate-800',
        icon: '–',
      }
    default:
      return {
        label: 'ESTADO NO DISPONIBLE',
        sub: '',
        class: 'bg-slate-600 text-white',
        cardBorder: 'border-slate-200',
        badge: 'bg-slate-100 text-slate-800',
        icon: '?',
      }
  }
})

onMounted(() => {
  void verifyPrescription()
})
</script>

<template>
  <div class="min-h-screen bg-slate-50 px-4 py-8 sm:py-12">
    <div class="mx-auto max-w-2xl">
      <!-- Brand Logo / Header -->
      <div class="mb-6 text-center">
        <div
          class="inline-flex items-center gap-2 text-emerald-700 font-extrabold text-xl tracking-tight"
        >
          <span>🐾</span>
          <span>PetPassport</span>
        </div>
        <p class="mt-1 text-xs text-slate-500 uppercase tracking-widest font-semibold">
          Servicio de Verificación Pública de Recetas
        </p>
      </div>

      <!-- Loading / Error states -->
      <div
        v-if="loading"
        class="rounded-2xl border border-slate-200 bg-white p-12 text-center shadow-sm"
      >
        <div class="text-sm text-slate-500">Consultando libro mayor de eventos inmutable...</div>
      </div>

      <div
        v-else-if="error"
        class="rounded-2xl border border-rose-200 bg-white p-8 text-center shadow-sm"
      >
        <div
          class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-rose-100 text-rose-600 text-xl font-bold"
        >
          ✕
        </div>
        <h2 class="mt-4 text-lg font-bold text-slate-900">No se pudo verificar la receta</h2>
        <p class="mt-2 text-sm text-slate-600">{{ error }}</p>
      </div>

      <div v-else-if="verification" class="space-y-6" data-testid="verification-content">
        <!-- Big Status Card (FE-012) -->
        <div :class="[statusConfig.class, 'rounded-2xl p-6 text-center shadow-lg transition-all']" data-testid="verification-status-banner">
          <div class="text-3xl font-black tracking-wide">{{ statusConfig.label }}</div>
          <p class="mt-2 text-sm opacity-90">{{ statusConfig.sub }}</p>
        </div>

        <!-- Hash Verification Badge -->
        <div
          v-if="hashMatches !== null"
          :class="[
            hashMatches
              ? 'border-emerald-200 bg-emerald-50 text-emerald-900'
              : 'border-rose-200 bg-rose-50 text-rose-900',
            'flex items-center gap-3 rounded-xl border p-4 text-xs font-medium',
          ]"
        >
          <span class="text-base">{{ hashMatches ? '🛡️' : '⚠️' }}</span>
          <div>
            <strong
              >{{
                hashMatches
                  ? 'Firma criptográfica verificada'
                  : 'Discrepancia en el hash de verificación'
              }}:</strong
            >
            {{
              hashMatches
                ? 'El contenido coincide exactamente con el sello de emisión original SHA-256 (RFC 8785).'
                : 'El hash proporcionado en el enlace no coincide con el registro original.'
            }}
          </div>
        </div>

        <!-- Main Info Card -->
        <div class="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm space-y-6 text-sm">
          <!-- Doctor & Organization -->
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 border-b border-slate-100 pb-5">
            <div>
              <div class="text-xs uppercase tracking-wider text-slate-400 font-bold">
                Médico Prescriptor
              </div>
              <div class="mt-1 text-base font-bold text-slate-900">
                {{ verification.issuer_name }}
              </div>
              <div v-if="verification.vet_registry" class="mt-0.5 text-xs text-slate-600">
                Matrícula:
                <span class="font-semibold">{{ verification.vet_registry }}</span> ({{
                  verification.country
                }})
              </div>
            </div>

            <div>
              <div class="text-xs uppercase tracking-wider text-slate-400 font-bold">
                Clínica / Emisor
              </div>
              <div class="mt-1 text-base font-bold text-slate-900">
                {{ verification.issuer_org_name || 'Clínica Veterinaria' }}
              </div>
              <div v-if="verification.issued_at" class="mt-0.5 text-xs text-slate-600">
                Fecha de emisión: {{ verification.issued_at.slice(0, 10) }}
              </div>
            </div>
          </div>

          <!-- Patient (No Tutor PII per FE-012 / PRD-004) -->
          <div class="border-b border-slate-100 pb-5">
            <div class="text-xs uppercase tracking-wider text-slate-400 font-bold">Paciente</div>
            <div class="mt-1 flex items-center gap-3">
              <span class="text-lg font-bold text-slate-900">{{
                verification.pet_name || 'Paciente'
              }}</span>
              <span
                v-if="verification.pet_species"
                class="rounded bg-slate-100 px-2 py-0.5 text-xs font-semibold capitalize text-slate-600"
              >
                {{ verification.pet_species }}
              </span>
            </div>
            <p class="mt-1 text-[11px] text-slate-400">
              * Por confidencialidad médica y protección de datos, los datos de contacto del tutor
              no se muestran públicamente.
            </p>
          </div>

          <!-- Items Prescribed -->
          <div class="border-b border-slate-100 pb-5">
            <div class="text-xs uppercase tracking-wider text-slate-400 font-bold mb-3">
              Medicamentos Prescritos
            </div>
            <div class="space-y-3">
              <div
                v-for="(it, itIdx) in verification.items"
                :key="it.item_id || itIdx"
                class="rounded-xl border border-slate-200 bg-slate-50 p-4"
              >
                <div class="flex items-center justify-between font-bold text-slate-900">
                  <span>{{ it.name }} {{ it.presentation ? `(${it.presentation})` : '' }}</span>
                  <span
                    v-if="it.controlled"
                    class="rounded-full bg-rose-100 px-2 py-0.5 text-[10px] font-bold text-rose-800"
                  >
                    CONTROL ESPECIAL
                  </span>
                </div>
                <div class="mt-1 text-xs text-slate-600">
                  Dosis: {{ it.dose.value }} {{ it.dose.unit }} · {{ it.frequency }}
                  <span v-if="it.duration_days"> por {{ it.duration_days }} días</span>
                </div>
                <div class="mt-1 text-xs text-slate-500">
                  Cantidad autorizada:
                  <strong>{{ it.quantity.value }} {{ it.quantity.unit }}</strong> · Sustitución por
                  genérico: {{ it.substitution_allowed ? 'Permitida' : 'No permitida' }}
                </div>
                <div
                  v-if="it.instructions"
                  class="mt-2 text-xs italic text-slate-600 bg-white rounded p-2 border border-slate-200"
                >
                  "{{ it.instructions }}"
                </div>
              </div>
            </div>
          </div>

          <!-- Refills & Dispensations -->
          <div>
            <div class="flex items-center justify-between text-xs mb-3">
              <span class="font-bold uppercase tracking-wider text-slate-400"
                >Control de Dispensaciones</span
              >
              <span class="font-semibold text-slate-700">
                Refills usados: {{ verification.refills_used }} / {{ verification.refills_allowed }}
              </span>
            </div>

            <div class="rounded-lg bg-slate-50 p-3 text-xs text-slate-600">
              Dispensaciones registradas: <strong>{{ verification.dispensations_count }}</strong>
            </div>
          </div>

          <!-- Hash & Technical integrity stamp -->
          <div
            class="rounded-xl border border-slate-200 bg-slate-50 p-4 text-[11px] font-mono text-slate-500"
          >
            <div><strong>ContentHash SHA-256:</strong></div>
            <div class="break-all text-slate-700 select-all">{{ verification.content_hash }}</div>
          </div>
        </div>

        <!-- Dispenser Notice Box -->
        <div class="rounded-xl border border-sky-200 bg-sky-50 p-4 text-xs text-sky-900">
          <div class="font-bold">Aviso para Farmacias y Dispensadores:</div>
          <p class="mt-1">
            Verifique la vigencia y el estado de la receta antes de la dispensación. Al entregar
            medicamentos bajo esta receta electrónica, registre la dispensación correspondiente en
            la red PetPassport para mantener actualizado el saldo de refills e ítems restantes.
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
