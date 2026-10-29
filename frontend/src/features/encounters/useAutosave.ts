import { onScopeDispose, ref, shallowRef } from 'vue'
import { isApiError, type ApiError } from '@/shared/api/client'

export type AutosaveStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict'

export interface AutosaveOptions {
  /** Firma del contenido a guardar; si no cambia, no se guarda. */
  signature: () => string
  /** Guarda el contenido actual (lanza ApiError en fallo). */
  save: () => Promise<void>
  /** Desactiva el autoguardado (consulta firmada, solo lectura). */
  enabled?: () => boolean
  intervalMs?: number
}

// useAutosave (PP-F2-FE-003, PRD-003): cada 5 s guarda si el contenido cambió
// desde el último guardado. Un 409 CONFLICT_VERSION detiene el ciclo y pasa a
// `conflict` (la UI ofrece ver diferencias y recargar); cualquier otro error
// queda en `error` y no se reintenta hasta que el contenido vuelva a cambiar,
// para no repetir una petición que el servidor ya rechazó.
export function useAutosave(opts: AutosaveOptions) {
  const status = ref<AutosaveStatus>('idle')
  const savedAt = ref<Date | null>(null)
  const error = shallowRef<ApiError | null>(null)
  let baseline = opts.signature()
  let failed: string | null = null
  let running: Promise<void> | null = null

  function markClean(sig = opts.signature()) {
    baseline = sig
    failed = null
    if (status.value === 'conflict' || status.value === 'error') status.value = 'idle'
    error.value = null
  }

  function dirty() {
    return opts.signature() !== baseline
  }

  async function run() {
    if (status.value === 'conflict') return
    if (opts.enabled && !opts.enabled()) return
    const sig = opts.signature()
    if (sig === baseline || sig === failed) return
    status.value = 'saving'
    try {
      await opts.save()
      baseline = sig
      failed = null
      error.value = null
      savedAt.value = new Date()
      status.value = 'saved'
    } catch (e) {
      error.value = isApiError(e) ? e : null
      if (isApiError(e) && e.code === 'CONFLICT_VERSION') {
        status.value = 'conflict'
      } else {
        failed = sig
        status.value = 'error'
      }
    }
  }

  /** Guarda ya (p. ej. antes de firmar); espera a un guardado en curso. */
  async function flush() {
    while (running) await running
    running = run()
    try {
      await running
    } finally {
      running = null
    }
  }

  const timer = setInterval(() => {
    if (!running) void flush()
  }, opts.intervalMs ?? 5000)
  onScopeDispose(() => clearInterval(timer))

  return { status, savedAt, error, flush, markClean, dirty }
}
