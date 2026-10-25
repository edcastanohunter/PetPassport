import { ref } from 'vue'

// useCommand (FE-003/FE-006): genera una Idempotency-Key por intento del
// usuario. Se conserva en reintentos del MISMO intento y se renueva cuando el
// usuario cambia datos o reabre el formulario (ARQ-008).
export function useCommand() {
  const key = ref<string>(crypto.randomUUID())
  function renew() {
    key.value = crypto.randomUUID()
  }
  return { key, renew }
}
