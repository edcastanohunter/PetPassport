# Pruebas de Carga k6 y SLOs (F3 M3.3, PP-F3-PRF-003)

Esta carpeta contiene la suite de pruebas de rendimiento y resiliencia para validar los SLOs de PetPassport según **PP-F3-PRF-001** y **PP-F3-PRF-003**.

## 1. Escenarios

| Escenario | Fichero | Distribución / Carga | Propósito |
|---|---|---|---|
| `owner_browse` | `scenarios/owner_browse.js` | 60% del tráfico nominal | Navegación del tutor: `/me`, mascotas, ficha, timeline y carnet de vacunas. |
| `clinic_day` | `scenarios/clinic_day.js` | 30% del tráfico nominal | Flujo clínico completo: slots de agenda, búsqueda de paciente, borrador, edición, firma y registro de vacuna. |
| `search_heavy` | `scenarios/search_heavy.js` | 10% del tráfico nominal | Búsquedas clínicas con facetas y autocompletado (`/suggest`) con términos veterinarios reales. |
| `spike` | `scenarios/spike.js` | Ráfaga x5 (500 req/s, 2 min) | Evaluación de resiliencia, circuit breaker y recuperación. |
| `soak` | `scenarios/soak.js` | 2 h a 100 req/s sostenidos | Detección de fugas de memoria, conexiones o degradación del Event Store. |

---

## 2. Umbrales y SLOs (PP-F3-PRF-001)

| SLI | Umbral k6 | SLO |
|---|---|---|
| Disponibilidad API | `http_req_failed` < 0.1% | 99,9% respuestas no-5xx |
| Latencia API lectura (GET no búsqueda) | `p(95)<250`, `p(99)<600` | p95 < 250 ms, p99 < 600 ms |
| Latencia API comandos | `p(95)<400` | p95 < 400 ms |
| Latencia búsqueda (`/search/*`) | `p(95)<300` | p95 < 300 ms |
| Latencia autocompletado (`/suggest/*`) | `p(95)<150` | p95 < 150 ms |
| Retraso de proyecciones asíncronas | `projection_lag` p(95)<2000 | p95 < 2 s |
| Retraso de indexación OpenSearch | `search_index_lag` p(95)<5000 | p95 < 5 s |

---

## 3. Ejecución

### Carga Nominal (5 min, 100 req/s):
```bash
K6_DOMAIN=staging.<dominio> \
K6_USERS="load1@staging:pass,load2@staging:pass" \
K6_PROMETHEUS=http://localhost:9091 \
k6 run backend/test/load/run_suite.js
```

### Escenario Spike (Ráfaga x5):
```bash
K6_DOMAIN=staging.<dominio> \
K6_USERS="..." \
K6_SCENARIO=spike \
k6 run backend/test/load/run_suite.js
```

### Escenario Soak (2 horas sostenidas):
```bash
K6_DOMAIN=staging.<dominio> \
K6_USERS="..." \
K6_SCENARIO=soak \
k6 run backend/test/load/run_suite.js
```

### Ejecución Automática (Nightly CI)
El workflow `.github/workflows/perf-nightly.yml` ejecuta la suite de carga contra el entorno de staging cada noche a las 02:00 UTC (o manual mediante `workflow_dispatch` y en PRs con etiqueta `perf`). Utiliza la cabecera `X-RateLimit-Allowlist: load-test-nightly` autorizada en el entorno.
