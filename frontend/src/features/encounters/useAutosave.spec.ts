import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { ApiError } from '@/shared/api/client'
import { useAutosave } from './useAutosave'

// PP-F2-FE-003: autoguardado cada 5 s solo si cambió; 409 detiene el ciclo;
// otros errores no se reintentan con el mismo contenido.
describe('useAutosave', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function setup(save: () => Promise<void>) {
    const content = ref('a')
    const scope = effectScope()
    const auto = scope.run(() => useAutosave({ signature: () => content.value, save }))!
    return { content, auto, scope }
  }

  it('guarda cada 5 s solo cuando el contenido cambió', async () => {
    const save = vi.fn().mockResolvedValue(undefined)
    const { content, auto, scope } = setup(save)
    await vi.advanceTimersByTimeAsync(5000)
    expect(save).not.toHaveBeenCalled()
    content.value = 'b'
    await vi.advanceTimersByTimeAsync(4999)
    expect(save).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(save).toHaveBeenCalledTimes(1)
    expect(auto.status.value).toBe('saved')
    expect(auto.savedAt.value).toBeInstanceOf(Date)
    await vi.advanceTimersByTimeAsync(10000)
    expect(save).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('409 CONFLICT_VERSION pasa a conflict y detiene el ciclo hasta recargar', async () => {
    const save = vi.fn().mockRejectedValue(new ApiError({ code: 'CONFLICT_VERSION' }, 409))
    const { content, auto, scope } = setup(save)
    content.value = 'b'
    await vi.advanceTimersByTimeAsync(5000)
    expect(auto.status.value).toBe('conflict')
    content.value = 'c'
    await vi.advanceTimersByTimeAsync(15000)
    expect(save).toHaveBeenCalledTimes(1)
    auto.markClean()
    expect(auto.status.value).toBe('idle')
    scope.stop()
  })

  it('un error no se repite con el mismo contenido; sí al cambiarlo', async () => {
    const save = vi
      .fn()
      .mockRejectedValueOnce(new ApiError({ code: 'VALIDATION_FAILED' }, 422))
      .mockResolvedValue(undefined)
    const { content, auto, scope } = setup(save)
    content.value = 'b'
    await vi.advanceTimersByTimeAsync(5000)
    expect(auto.status.value).toBe('error')
    expect(auto.error.value?.code).toBe('VALIDATION_FAILED')
    await vi.advanceTimersByTimeAsync(10000)
    expect(save).toHaveBeenCalledTimes(1)
    content.value = 'c'
    await vi.advanceTimersByTimeAsync(5000)
    expect(save).toHaveBeenCalledTimes(2)
    expect(auto.status.value).toBe('saved')
    scope.stop()
  })

  it('flush guarda de inmediato (antes de firmar)', async () => {
    const save = vi.fn().mockResolvedValue(undefined)
    const { content, auto, scope } = setup(save)
    content.value = 'b'
    await auto.flush()
    expect(save).toHaveBeenCalledTimes(1)
    expect(auto.dirty()).toBe(false)
    scope.stop()
  })
})
