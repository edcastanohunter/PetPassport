import { onScopeDispose, ref, shallowRef } from 'vue'
import { FRESHNESS_VISIBLE_FROM_SECONDS, freshnessOf } from '@/shared/api/freshness'

/** Lo que devuelve una llamada de openapi-fetch (data + response). */
export interface DerivedResult<T> {
  data?: T
  response: Response
}

export interface DerivedViewOptions {
  /** Polling corto tras un comando (FE-014: 3 × 2 s). */
  pollAttempts?: number
  pollIntervalMs?: number
}

// useDerivedView (PP-F2-FE-014): carga una vista derivada (timeline, resumen,
// peso, estadísticas, bandeja) y expone su retraso de proyección leído de
// X-PP-Freshness. Tras un comando que la afecta (p. ej. firmar), `afterCommand`
// recarga hasta 3 veces cada 2 s, o hasta que la vista está al día.
export function useDerivedView<T>(
  load: () => Promise<DerivedResult<T>>,
  opts: DerivedViewOptions = {},
) {
  const attempts = opts.pollAttempts ?? 3
  const interval = opts.pollIntervalMs ?? 2000

  const data = shallowRef<T | undefined>()
  const lagSeconds = ref<number | null>(null)
  const loading = ref(false)
  const error = shallowRef<unknown>(null)
  let timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false

  async function reload() {
    loading.value = true
    error.value = null
    try {
      const res = await load()
      data.value = res.data
      lagSeconds.value = freshnessOf(res.response)
    } catch (e) {
      error.value = e
    } finally {
      loading.value = false
    }
  }

  function stale() {
    return lagSeconds.value !== null && lagSeconds.value >= FRESHNESS_VISIBLE_FROM_SECONDS
  }

  /** Recarga ya y repite cada `interval` mientras la vista siga retrasada. */
  async function afterCommand() {
    clearTimeout(timer)
    for (let i = 0; i < attempts && !disposed; i++) {
      if (i > 0) await new Promise<void>((resolve) => (timer = setTimeout(resolve, interval)))
      if (disposed) return
      await reload()
      if (!stale() && error.value === null) return
    }
  }

  onScopeDispose(() => {
    disposed = true
    clearTimeout(timer)
  })

  return { data, lagSeconds, loading, error, reload, afterCommand }
}
