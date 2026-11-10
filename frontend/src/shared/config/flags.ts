import { ref, computed } from 'vue'

// Store reactive flags map
const flagsState = ref<Record<string, boolean>>({
  f5_billing_portal: true,
  f5_canary_routing: false,
  f5_sms_notifications: false,
  f5_service_status_banner: true,
})

/**
 * Initializes or updates feature flags (PP-F5-OPS-010).
 */
export function setFeatureFlags(flags: Record<string, boolean>) {
  flagsState.value = { ...flagsState.value, ...flags }
}

/**
 * Checks synchronously whether a feature flag is enabled.
 */
export function isFeatureEnabled(key: string, defaultValue = false): boolean {
  return flagsState.value[key] ?? defaultValue
}

/**
 * Vue composable for reactive feature flag evaluation.
 */
export function useFeatureFlag(key: string, defaultValue = false) {
  const isEnabled = computed(() => isFeatureEnabled(key, defaultValue))
  return { isEnabled }
}

/**
 * Resets flags (useful for tests).
 */
export function resetFeatureFlags() {
  flagsState.value = {
    f5_billing_portal: true,
    f5_canary_routing: false,
    f5_sms_notifications: false,
    f5_service_status_banner: true,
  }
}
