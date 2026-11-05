/**
 * Real User Monitoring (RUM) para Web Vitals (PP-F3-FE-010, PP-F3-PRF-007).
 * Captura LCP, INP, CLS y TTFB y los envía a /api/v1/client-events con muestreo del 20% en piloto.
 */

export interface MetricEvent {
  name: 'LCP' | 'INP' | 'CLS' | 'TTFB'
  value: number // ms (o score para CLS)
  route: string
  rating: 'good' | 'needs-improvement' | 'poor'
}

let rumInitialized = false
const queue: MetricEvent[] = []
let flushTimer: ReturnType<typeof setTimeout> | null = null

export function initRUM(options: { sampleRate?: number; endpoint?: string } = {}) {
  if (rumInitialized || typeof window === 'undefined' || !('PerformanceObserver' in window)) {
    return
  }

  const sampleRate = options.sampleRate ?? 0.2 // 20% muestreo en piloto (FE-010)
  // En desarrollo o staging con query param ?rum=1 forzamos 100%
  const urlParams = new URLSearchParams(window.location.search)
  const forceRUM = urlParams.get('rum') === '1'
  if (!forceRUM && Math.random() > sampleRate) {
    return
  }

  rumInitialized = true
  const endpoint = options.endpoint || '/api/v1/client-events'

  const currentRoute = () => window.location.pathname || '/'

  // 1. TTFB (Time to First Byte)
  try {
    const navEntries = performance.getEntriesByType('navigation') as PerformanceNavigationTiming[]
    if (navEntries.length > 0) {
      const ttfb = navEntries[0].responseStart
      if (ttfb > 0) {
        recordMetric({
          name: 'TTFB',
          value: Math.round(ttfb),
          route: currentRoute(),
          rating: ttfb < 800 ? 'good' : ttfb < 1800 ? 'needs-improvement' : 'poor',
        })
      }
    }
  } catch {}

  // 2. LCP (Largest Contentful Paint - SLO < 2.5 s)
  try {
    const lcpObserver = new PerformanceObserver((entryList) => {
      const entries = entryList.getEntries()
      const lastEntry = entries[entries.length - 1]
      if (lastEntry) {
        const value = Math.round(lastEntry.startTime)
        recordMetric({
          name: 'LCP',
          value,
          route: currentRoute(),
          rating: value < 2500 ? 'good' : value < 4000 ? 'needs-improvement' : 'poor',
        })
      }
    })
    lcpObserver.observe({ type: 'largest-contentful-paint', buffered: true })
  } catch {}

  // 3. CLS (Cumulative Layout Shift - Objetivo < 0.1)
  try {
    let clsValue = 0
    const clsObserver = new PerformanceObserver((entryList) => {
      for (const entry of entryList.getEntries() as (PerformanceEntry & {
        hadRecentInput?: boolean
        value?: number
      })[]) {
        if (!entry.hadRecentInput && typeof entry.value === 'number') {
          clsValue += entry.value
        }
      }
      recordMetric({
        name: 'CLS',
        value: parseFloat(clsValue.toFixed(4)),
        route: currentRoute(),
        rating: clsValue < 0.1 ? 'good' : clsValue < 0.25 ? 'needs-improvement' : 'poor',
      })
    })
    clsObserver.observe({ type: 'layout-shift', buffered: true })
  } catch {}

  // 4. INP (Interaction to Next Paint - SLO < 200 ms)
  try {
    const inpObserver = new PerformanceObserver((entryList) => {
      for (const entry of entryList.getEntries() as (PerformanceEntry & { duration: number })[]) {
        const duration = Math.round(entry.duration)
        recordMetric({
          name: 'INP',
          value: duration,
          route: currentRoute(),
          rating: duration < 200 ? 'good' : duration < 500 ? 'needs-improvement' : 'poor',
        })
      }
    })
    inpObserver.observe({
      type: 'event',
      buffered: true,
      durationThreshold: 40,
    } as PerformanceObserverInit)
  } catch {}

  // Envío al descargar la página o periódicamente
  window.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      flushQueue(endpoint)
    }
  })
}

export function recordMetric(event: MetricEvent) {
  queue.push(event)
  if (!flushTimer) {
    flushTimer = setTimeout(() => {
      flushTimer = null
      flushQueue('/api/v1/client-events')
    }, 5000)
  }
}

export function flushQueue(endpoint = '/api/v1/client-events') {
  if (queue.length === 0) return
  const batch = queue.splice(0, queue.length)
  const body = JSON.stringify({ events: batch })

  if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
    const blob = new Blob([body], { type: 'application/json' })
    navigator.sendBeacon(endpoint, blob)
  } else if (typeof fetch !== 'undefined') {
    fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
    }).catch(() => {})
  }
}
