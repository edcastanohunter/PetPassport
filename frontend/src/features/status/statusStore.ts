import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { SystemStatusData, Incident, ScheduledMaintenance } from './statusTypes'

export const useStatusStore = defineStore('systemStatus', () => {
  const statusData = ref<SystemStatusData | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)
  const lastFetched = ref<number | null>(null)
  let pollTimer: ReturnType<typeof setInterval> | null = null

  const isOperational = computed(() => {
    if (!statusData.value) return true
    return statusData.value.status === 'operational'
  })

  const hasActiveIncident = computed(() => {
    if (!statusData.value) return false
    return statusData.value.active_incidents && statusData.value.active_incidents.length > 0
  })

  const hasActiveMaintenance = computed(() => {
    if (!statusData.value) return false
    return (
      statusData.value.scheduled_maintenances &&
      statusData.value.scheduled_maintenances.some((m) => m.status === 'in_progress')
    )
  })

  const activeIncident = computed<Incident | null>(() => {
    if (!statusData.value?.active_incidents?.length) return null
    return statusData.value.active_incidents[0]
  })

  const activeMaintenance = computed<ScheduledMaintenance | null>(() => {
    if (!statusData.value?.scheduled_maintenances?.length) return null
    return (
      statusData.value.scheduled_maintenances.find((m) => m.status === 'in_progress') ??
      statusData.value.scheduled_maintenances[0]
    )
  })

  const bannerSeverity = computed<'critical' | 'warning' | 'info' | null>(() => {
    if (activeIncident.value) {
      if (activeIncident.value.severity === 'S1') return 'critical'
      if (activeIncident.value.severity === 'S2' || activeIncident.value.severity === 'S3') {
        return 'warning'
      }
      return 'info'
    }
    if (hasActiveMaintenance.value) return 'info'
    if (!isOperational.value) return 'warning'
    return null
  })

  async function fetchStatus(url = '/status.json') {
    loading.value = true
    error.value = null
    try {
      const res = await fetch(url, { cache: 'no-cache' })
      if (!res.ok) {
        throw new Error(`Error al consultar status.json (${res.status})`)
      }
      const data = (await res.json()) as SystemStatusData
      statusData.value = data
      lastFetched.value = Date.now()
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Error desconocido al consultar estado'
    } finally {
      loading.value = false
    }
  }

  function startPolling(intervalMs = 60000, url = '/status.json') {
    stopPolling()
    void fetchStatus(url)
    pollTimer = setInterval(() => {
      void fetchStatus(url)
    }, intervalMs)
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function setMockStatus(data: SystemStatusData | null) {
    statusData.value = data
  }

  return {
    statusData,
    loading,
    error,
    lastFetched,
    isOperational,
    hasActiveIncident,
    hasActiveMaintenance,
    activeIncident,
    activeMaintenance,
    bannerSeverity,
    fetchStatus,
    startPolling,
    stopPolling,
    setMockStatus,
  }
})
