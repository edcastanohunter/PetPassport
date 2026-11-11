import { describe, it, expect, beforeEach } from 'vitest'
import { isFeatureEnabled, setFeatureFlags, resetFeatureFlags, useFeatureFlag } from './flags'

describe('Feature Flags (PP-F5-OPS-010)', () => {
  beforeEach(() => {
    resetFeatureFlags()
  })

  it('evaluates default configured flags correctly', () => {
    expect(isFeatureEnabled('f5_billing_portal')).toBe(true)
    expect(isFeatureEnabled('f5_canary_routing')).toBe(false)
    expect(isFeatureEnabled('non_existent_flag')).toBe(false)
    expect(isFeatureEnabled('non_existent_flag', true)).toBe(true)
  })

  it('updates flags dynamically via setFeatureFlags', () => {
    setFeatureFlags({ f5_canary_routing: true, custom_experiment: true })
    expect(isFeatureEnabled('f5_canary_routing')).toBe(true)
    expect(isFeatureEnabled('custom_experiment')).toBe(true)
  })

  it('useFeatureFlag returns a reactive computed boolean', () => {
    const { isEnabled } = useFeatureFlag('f5_sms_notifications')
    expect(isEnabled.value).toBe(false)

    setFeatureFlags({ f5_sms_notifications: true })
    expect(isEnabled.value).toBe(true)
  })
})
