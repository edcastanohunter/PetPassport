import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type Pet, type PetListItem, type PetPage } from '@/shared/api/client'

// petStore (FE-006): lista y ficha de mascotas del tutor. La autoridad es el
// backend; este store solo guarda vistas proyectadas.
export const usePetStore = defineStore('pets', () => {
  const items = ref<PetListItem[]>([])
  const nextCursor = ref<string | null>(null)
  const status = ref<'active' | 'deceased' | 'all'>('all')
  const loading = ref(false)
  const current = ref<Pet | null>(null)

  async function load(s: 'active' | 'deceased' | 'all' = status.value) {
    loading.value = true
    status.value = s
    try {
      const { data, error } = await api().GET('/pets', {
        params: { query: { status: s, limit: 50 } },
      })
      if (error) throw error
      const page = (data ?? { data: [], next_cursor: null }) as PetPage
      items.value = page.data
      nextCursor.value = page.next_cursor
    } finally {
      loading.value = false
    }
  }

  async function loadPet(id: string) {
    const { data, error } = await api().GET('/pets/{petId}', { params: { path: { petId: id } } })
    if (error) throw error
    current.value = data ?? null
    return current.value
  }

  return { items, nextCursor, status, loading, current, load, loadPet }
})
