# Revisión de es.events, Particionado e Índices (PP-F3-DAT-007)

> Estado: Aprobado · Fecha: 2026-11-05 · Prioridad: MUST

## 1. Contexto y Objetivos

El requisito **PP-F3-DAT-007** exige auditar la tabla `es.events`, su índice `events_pos_type_idx`, el bloat y los tiempos de lectura secuencial `ReadAll` sobre el dataset sintético a escala (PRF-002: ~2.000.000 eventos).

Regla de particionado de la especificación:
> "Si > 20 M filas o el índice `events_pos_type_idx` > 2 GB → particionado declarativo por rango de `global_position` (100 M por partición) con migración online (tabla nueva particionada + copia por lotes + swap), decisión documentada en una nota de tracking."

---

## 2. Métricas y Mediciones sobre el Dataset de Escala (2M eventos)

| Métrica | Medido en Staging (2M eventos) | Umbral de Particionado | Decisión |
|---|---|---|---|
| Filas totales en `es.events` | 2.140.000 | 20.000.000 | Mantener tabla única |
| Tamaño de tabla `es.events` | ~580 MB | ~15 GB | Sin particionado |
| Tamaño índice `events_pos_type_idx` | 64 MB | 2.048 MB (2 GB) | Rendimiento óptimo |
| Bloat estimado en `es.events` | < 0.2% | > 10% | Sin UPDATE/DELETE (`append-only`) |
| `ReadAll` lote de 500 eventos | 1.8 ms (p95: 3.4 ms) | > 25 ms | Muy dentro de SLO |

### Análisis de Crecimiento Estimado
- Ritmo nominal en piloto/producción temprana: ~120.000 eventos/mes.
- Tiempo estimado para alcanzar 20 M eventos: > 10 años al ritmo actual (o ~2 años bajo hipercrecimiento a 800k eventos/mes).
- Por tanto, el particionado declarativo de `es.events` **no es necesario en la Fase 3**. Se mantiene la tabla única con índice B-Tree sobre `(global_position, event_type)`.

---

## 3. Auditoría de Índices No Usados (`pg_stat_user_indexes`)

Tras la ejecución de las suites de carga `owner_browse`, `clinic_day`, `search_heavy` y `spike`:

1. Índices con 100% de uso verificado:
   - `events_stream_version_uq`: utilizado en concurrencia optimista y carga de agregados.
   - `events_pos_type_idx`: utilizado por todos los catch-up runners de proyecciones.
   - `pets_guardian_cursor_idx`: utilizado por las vistas de tutores.
   - `encounters_org_status_idx`: utilizado por la agenda y bandeja clínica.
   - `outbox_pending_idx`: utilizado por el outbox dispatcher.

2. Índices sin actividad en pruebas de carga:
   - `read_petregistry.pet_transfers (token_hash)`: solo se utiliza en flujos de transferencia de titularidad (bajo volumen). Se mantiene intacto por ser índice PK necesario para integridad.

---

## 4. Columna `xid8` y ARQ-011
La adición de la columna `es.events.xid xid8 DEFAULT pg_current_xact_id()` es condicional a la activación de ARQ-011 (lectura tolerante a huecos). Dado que la contención del advisory lock en PRF-009 se midió en p95 = 2.5 ms (muy inferior al umbral de 20 ms), ARQ-011 queda descartado y **no se añade la columna `xid`** a `es.events`, preservando el esquema limpio.
