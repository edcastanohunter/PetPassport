<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { WeightHistory } from '@/shared/api/client'
import { linear, nearestIndex, niceTicks } from './weightScale'

// Historial de peso (PP-F2-PRD-025, FE-005): una serie en el tiempo → línea
// de 2 px con marcadores ≥ 8 px y anillo de superficie, rejilla de líneas
// finas, valor directo en el último punto, cursor + tooltip (también con
// teclado: ←/→) y vista de tabla. Tendencia y alerta de variación > 10 % en
// 90 días (dato del servidor) con icono + texto, nunca solo color.
// Paleta: skill dataviz, serie 1 azul (#2a78d6 claro / #3987e5 oscuro),
// validada contra las superficies de la app (#ffffff / #030712).
const props = defineProps<{ history: WeightHistory }>()
const { t, d, n } = useI18n()

const root = ref<HTMLElement | null>(null)
const width = ref(640)
const HEIGHT = 220
const M = { top: 16, right: 64, bottom: 28, left: 44 }
let observer: ResizeObserver | undefined

onMounted(() => {
  if (!root.value || typeof ResizeObserver === 'undefined') return
  observer = new ResizeObserver(([entry]) => {
    if (entry) width.value = Math.max(280, Math.round(entry.contentRect.width))
  })
  observer.observe(root.value)
})
onBeforeUnmount(() => observer?.disconnect())

const points = computed(() =>
  [...props.history.data]
    .map((p) => ({ ...p, t: new Date(p.measured_at).getTime() }))
    .sort((a, b) => a.t - b.t),
)
const showTable = ref(false)
const active = ref<number | null>(null)

const geo = computed(() => {
  const pts = points.value
  const innerW = width.value - M.left - M.right
  const innerH = HEIGHT - M.top - M.bottom
  const kg = pts.map((p) => p.weight_kg)
  const yTicks = niceTicks(Math.min(...kg), Math.max(...kg))
  const y = linear(yTicks[0] ?? 0, yTicks[yTicks.length - 1] ?? 1, M.top + innerH, M.top)
  const t0 = pts[0]?.t ?? 0
  const t1 = pts[pts.length - 1]?.t ?? 1
  const x = pts.length > 1 ? linear(t0, t1, M.left, M.left + innerW) : () => M.left + innerW / 2
  const xy = pts.map((p) => ({ x: x(p.t), y: y(p.weight_kg) }))
  const path = xy.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join('')
  // Marcas de fecha: extremos y, si cabe, el punto medio del rango.
  const xTicks = pts.length > 1 ? [t0, ...(innerW > 360 ? [(t0 + t1) / 2] : []), t1] : [t0]
  return { innerW, innerH, yTicks, y, x, xy, path, xTicks }
})

const last = computed(() => points.value[points.value.length - 1])
const trend = computed(() => props.history.trend)
const deltaLabel = computed(() => {
  const tr = trend.value
  if (tr.delta_kg === undefined || tr.delta_pct === undefined) return null
  const sign = tr.delta_kg > 0 ? '+' : ''
  return `${sign}${n(tr.delta_kg, { maximumFractionDigits: 2 })} kg (${sign}${n(tr.delta_pct, { maximumFractionDigits: 1 })} %)`
})

function onPointer(ev: PointerEvent) {
  const svg = ev.currentTarget as SVGElement
  const rect = svg.getBoundingClientRect()
  const px = ((ev.clientX - rect.left) / rect.width) * width.value
  active.value = nearestIndex(
    geo.value.xy.map((p) => p.x),
    px,
  )
}
function onKey(ev: KeyboardEvent) {
  const max = points.value.length - 1
  if (max < 0) return
  if (ev.key === 'ArrowRight' || ev.key === 'ArrowLeft') {
    ev.preventDefault()
    const cur = active.value ?? (ev.key === 'ArrowRight' ? -1 : max + 1)
    active.value = Math.min(max, Math.max(0, cur + (ev.key === 'ArrowRight' ? 1 : -1)))
  } else if (ev.key === 'Escape') {
    active.value = null
  }
}

const tip = computed(() => {
  const i = active.value
  if (i === null || i < 0) return null
  const p = points.value[i]
  const g = geo.value.xy[i]
  if (!p || !g) return null
  const left = Math.min(Math.max(g.x, 80), width.value - 80)
  return { p, g, left }
})

function kg(v: number) {
  return `${n(v, { maximumFractionDigits: 2 })} kg`
}
const ariaSummary = computed(() =>
  last.value
    ? t('records.weight.aria', {
        count: points.value.length,
        last: kg(last.value.weight_kg),
        date: d(last.value.measured_at, 'short'),
      })
    : '',
)
</script>

<template>
  <div ref="root" class="viz-root space-y-3" data-testid="weight-chart">
    <div class="flex flex-wrap items-center gap-3 text-sm">
      <p v-if="last" class="viz-text">
        <span class="text-2xl font-semibold tabular-nums">{{ kg(last.weight_kg) }}</span>
        <span class="viz-muted ml-2">{{ d(last.measured_at, 'short') }}</span>
      </p>
      <p v-if="deltaLabel" class="viz-secondary tabular-nums" data-testid="weight-trend">
        {{ t('records.weight.trend', { delta: deltaLabel, days: trend.window_days }) }}
      </p>
      <p
        v-if="trend.alert"
        class="viz-alert inline-flex items-center gap-1 rounded px-2 py-1"
        role="status"
        data-testid="weight-alert"
      >
        <span class="pi pi-exclamation-triangle viz-alert-icon" aria-hidden="true" />
        {{ t('records.weight.alert', { days: trend.window_days }) }}
      </p>
      <button
        type="button"
        class="ml-auto underline"
        data-testid="weight-table-toggle"
        @click="showTable = !showTable"
      >
        {{ showTable ? t('records.weight.showChart') : t('records.weight.showTable') }}
      </button>
    </div>

    <p v-if="points.length === 0" class="viz-muted text-sm">{{ t('records.weight.empty') }}</p>

    <div v-else-if="!showTable" class="relative">
      <svg
        :viewBox="`0 0 ${width} ${HEIGHT}`"
        :width="width"
        :height="HEIGHT"
        class="block max-w-full touch-none select-none outline-none"
        role="img"
        tabindex="0"
        :aria-label="ariaSummary"
        @pointermove="onPointer"
        @pointerdown="onPointer"
        @pointerleave="active = null"
        @keydown="onKey"
        @blur="active = null"
      >
        <g>
          <line
            v-for="v in geo.yTicks"
            :key="`g${v}`"
            :x1="M.left"
            :x2="M.left + geo.innerW"
            :y1="geo.y(v)"
            :y2="geo.y(v)"
            class="viz-grid"
          />
          <text
            v-for="v in geo.yTicks"
            :key="`l${v}`"
            :x="M.left - 8"
            :y="geo.y(v)"
            text-anchor="end"
            dominant-baseline="middle"
            class="viz-axis-label"
          >
            {{ n(v, { maximumFractionDigits: 1 }) }}
          </text>
          <line
            :x1="M.left"
            :x2="M.left + geo.innerW"
            :y1="M.top + geo.innerH"
            :y2="M.top + geo.innerH"
            class="viz-baseline"
          />
          <text
            v-for="(tk, i) in geo.xTicks"
            :key="`x${tk}`"
            :x="geo.x(tk)"
            :y="HEIGHT - 8"
            :text-anchor="
              geo.xTicks.length === 1
                ? 'middle'
                : i === 0
                  ? 'start'
                  : i === geo.xTicks.length - 1
                    ? 'end'
                    : 'middle'
            "
            class="viz-axis-label"
          >
            {{ d(new Date(tk), 'short') }}
          </text>
        </g>
        <line
          v-if="tip"
          :x1="tip.g.x"
          :x2="tip.g.x"
          :y1="M.top"
          :y2="M.top + geo.innerH"
          class="viz-crosshair"
        />
        <path v-if="points.length > 1" :d="geo.path" class="viz-line" />
        <circle
          v-for="(g, i) in geo.xy"
          :key="i"
          :cx="g.x"
          :cy="g.y"
          :r="active === i ? 5.5 : 4"
          class="viz-dot"
        />
        <text
          v-if="last && geo.xy.length"
          :x="geo.xy[geo.xy.length - 1]!.x + 10"
          :y="geo.xy[geo.xy.length - 1]!.y"
          dominant-baseline="middle"
          class="viz-end-label"
        >
          {{ kg(last.weight_kg) }}
        </text>
      </svg>
      <div
        v-if="tip"
        class="viz-tooltip pointer-events-none absolute top-0 -translate-x-1/2 rounded-md px-3 py-2 text-xs shadow"
        :style="{ left: `${(tip.left / width) * 100}%` }"
        role="status"
        data-testid="weight-tooltip"
      >
        <p class="flex items-center gap-2">
          <span class="viz-key" aria-hidden="true" />
          <strong class="viz-text text-sm tabular-nums">{{ kg(tip.p.weight_kg) }}</strong>
        </p>
        <p class="viz-secondary">
          {{ d(tip.p.measured_at, 'short') }} · {{ t(`records.weight.source.${tip.p.source}`) }}
        </p>
      </div>
    </div>

    <table v-else class="w-full text-left text-sm" data-testid="weight-table">
      <caption class="sr-only">
        {{
          t('records.weight.title')
        }}
      </caption>
      <thead>
        <tr class="viz-muted">
          <th scope="col">{{ t('records.weight.date') }}</th>
          <th scope="col" class="text-right">{{ t('records.weight.kg') }}</th>
          <th scope="col">{{ t('records.weight.origin') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(p, i) in [...points].reverse()" :key="i">
          <td :data-label="t('records.weight.date')">{{ d(p.measured_at, 'short') }}</td>
          <td :data-label="t('records.weight.kg')" class="text-right tabular-nums">
            {{ n(p.weight_kg, { maximumFractionDigits: 2 }) }}
          </td>
          <td :data-label="t('records.weight.origin')">
            {{ t(`records.weight.source.${p.source}`) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.viz-root {
  --surface-1: #ffffff;
  --text-primary: #0b0b0b;
  --text-secondary: #52514e;
  --text-muted: #898781;
  --grid: #e1e0d9;
  --baseline: #c3c2b7;
  --series-1: #2a78d6;
  --status-serious: #ec835a;
  --tooltip-bg: #ffffff;
  --tooltip-border: #e1e0d9;
}
@media (prefers-color-scheme: dark) {
  .viz-root {
    --surface-1: #030712;
    --text-primary: #f0efec;
    --text-secondary: #c3c2b7;
    --text-muted: #898781;
    --grid: #2c2c2a;
    --baseline: #383835;
    --series-1: #3987e5;
    --tooltip-bg: #1a1a19;
    --tooltip-border: #383835;
  }
}
.viz-text {
  color: var(--text-primary);
}
.viz-secondary {
  color: var(--text-secondary);
}
.viz-muted {
  color: var(--text-muted);
}
.viz-grid {
  stroke: var(--grid);
  stroke-width: 1;
  shape-rendering: crispEdges;
}
.viz-baseline {
  stroke: var(--baseline);
  stroke-width: 1;
  shape-rendering: crispEdges;
}
.viz-crosshair {
  stroke: var(--text-muted);
  stroke-width: 1;
}
.viz-axis-label {
  fill: var(--text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.viz-end-label {
  fill: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.viz-line {
  fill: none;
  stroke: var(--series-1);
  stroke-width: 2;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.viz-dot {
  fill: var(--series-1);
  stroke: var(--surface-1);
  stroke-width: 2;
}
svg:focus-visible {
  outline: 2px solid #1d4ed8;
  outline-offset: 2px;
}
.viz-tooltip {
  background: var(--tooltip-bg);
  border: 1px solid var(--tooltip-border);
  white-space: nowrap;
}
.viz-key {
  display: inline-block;
  width: 12px;
  height: 2px;
  background: var(--series-1);
  border-radius: 1px;
}
.viz-alert {
  color: var(--text-primary);
  border: 1px solid var(--status-serious);
}
.viz-alert-icon {
  color: var(--status-serious);
}
</style>
