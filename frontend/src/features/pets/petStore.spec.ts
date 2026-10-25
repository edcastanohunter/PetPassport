import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const getMock = vi.fn()
vi.mock('@/shared/api/client', () => ({
  api: () => ({ GET: getMock }),
}))

import { usePetStore } from './petStore'

describe('petStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getMock.mockReset()
  })

  it('load guarda la página y el cursor', async () => {
    getMock.mockResolvedValue({
      data: {
        data: [
          {
            id: '0192f3a1-0000-7000-8000-000000000001',
            version: 1,
            name: 'Luna',
            species: 'dog',
            status: 'active',
            active_links: 0,
            created_at: '2026-10-20T10:00:00Z',
          },
        ],
        next_cursor: null,
      },
      error: undefined,
    })
    const store = usePetStore()
    await store.load('all')
    expect(getMock).toHaveBeenCalledWith('/pets', {
      params: { query: { status: 'all', limit: 50 } },
    })
    expect(store.items).toHaveLength(1)
    expect(store.items[0].name).toBe('Luna')
    expect(store.nextCursor).toBeNull()
  })

  it('loadPet guarda la ficha actual', async () => {
    getMock.mockResolvedValue({
      data: {
        id: '0192f3a1-0000-7000-8000-000000000002',
        version: 3,
        guardian_id: '0192f3a1-0000-7000-8000-0000000000aa',
        name: 'Rex',
        species: 'dog',
        sex: 'male',
        neutered: 'unknown',
        status: 'active',
        care_links: [],
        created_at: '2026-10-20T10:00:00Z',
        updated_at: '2026-10-20T10:00:00Z',
      },
      error: undefined,
    })
    const store = usePetStore()
    const pet = await store.loadPet('0192f3a1-0000-7000-8000-000000000002')
    expect(pet?.name).toBe('Rex')
    expect(store.current?.version).toBe(3)
  })
})
