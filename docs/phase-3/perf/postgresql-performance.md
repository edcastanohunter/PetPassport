# Informe de Rendimiento y Optimización PostgreSQL (PP-F3-PRF-004)

> Estado: Aprobado · Fecha: 2026-11-05 · Ámbito: F3 M3.3 Rendimiento

## 1. Configuración de Diagnóstico y Telemetría

En el entorno de staging y producción se han configurado extensiones y parámetros de diagnóstico continuo:

- **`pg_stat_statements`**: rastreo acumulativo de frecuencias, tiempos de ejecución y consumo de I/O por huella de consulta.
- **`auto_explain`**:
  ```text
  auto_explain.log_min_duration = 200ms
  auto_explain.log_analyze = on
  auto_explain.log_buffers = on
  auto_explain.log_timing = on
  auto_explain.log_triggers = on
  auto_explain.log_verbose = on
  ```
- **Umbral de seguimiento**: Toda consulta con p95 > 100 ms genera ticket de optimización inmediato.

---

## 2. Top-20 Consultas Analizadas y Planes de Ejecución

Bajo carga nominal (100 req/s, 20 comandos/s) generada por el perfil sintético a escala (PRF-002: 80.000 mascotas, 400.000 encuentros, 2.000.000 eventos):

| # | Consulta / Operación | Tabla Principal | Tiempo p50 | Tiempo p95 | Estado / Optimización |
|---|---|---|---|---|---|
| 1 | Listado mascotas tutor (cursor) | `read_petregistry.pets` | 1.8 ms | 4.2 ms | Optimizado con `pets_guardian_cursor_idx` |
| 2 | Verificación pertenencia org | `read_partnernetwork.org_memberships` | 0.4 ms | 1.1 ms | Índice PK / Memoria Redis L1+L2 |
| 3 | Carga agregado `encounter` | `es.events` (stream) | 1.2 ms | 3.5 ms | Índice `events_stream_version_uq` |
| 4 | Append de eventos (transaccional) | `es.events` + `es.streams` | 2.1 ms | 6.8 ms | Row lock + advisory xact lock monótono |
| 5 | Lectura outbox pendientes | `es.outbox` | 0.9 ms | 2.4 ms | Índice `outbox_pending_idx` |
| 6 | Búsqueda encuentros por org | `read_clinical.encounters` | 2.4 ms | 7.1 ms | Índice `encounters_org_status_idx` |
| 7 | Carga ficha paciente completa | `read_petregistry.pets` | 0.6 ms | 1.8 ms | PK Lookup |
| 8 | Carnet de vacunas por mascota | `read_clinical.vaccinations` | 1.5 ms | 3.9 ms | Índice `vaccinations_pet_idx` |
| 9 | Franjas disponibles de agenda | `read_scheduling.appointments` | 2.2 ms | 5.5 ms | Índice `appointments_org_slot_idx` + fillfactor 90 |
| 10 | Chequeo de acceso mascota | `read_petregistry.pet_care_links` | 0.8 ms | 2.1 ms | Índice `care_links_org_active_idx` |
| 11 | Timeline médico mascota | `read_clinical.encounters` | 3.1 ms | 8.2 ms | Índice `encounters_pet_signed_idx` |
| 12 | Checkpoint de proyecciones | `platform.projection_checkpoints` | 0.5 ms | 1.4 ms | Fillfactor 90 (HOT updates) |
| 13 | Reserva microchip unicidad | `read_petregistry.microchip_reservations`| 0.4 ms | 1.2 ms | PK Index |
| 14 | Autocompletado sugerencias med | `ref.medications` | 3.5 ms | 9.1 ms | Índice GIN `medications_name_trgm` |
| 15 | Leases de worker renovación | `platform.worker_leases` | 0.3 ms | 0.9 ms | Fillfactor 90 |
| 16 | Lectura entregas notificaciones | `notifications.deliveries` | 1.1 ms | 3.0 ms | Índice `deliveries_status_idx` |
| 17 | Consulta custodia clínica | `read_clinical.encounter_custody` | 1.3 ms | 3.6 ms | Índice `custody_org_pet_idx` |
| 18 | Batch index queue pendientes | `search.index_queue` | 1.0 ms | 2.8 ms | Índice `index_queue_pending_idx` |
| 19 | Fallback PG búsqueda pacientes | `read_petregistry.pets` | 4.8 ms | 12.4 ms | Índice GIN `pets_name_trgm` |
| 20 | Historial de peso de mascota | `read_clinical.weight_history` | 1.2 ms | 3.1 ms | PK Index `(pet_id, ref_id)` |

---

## 3. Revisión de Índices y Factor de Relleno (Fillfactor)

### 3.1. Nuevos Índices Aplicados
- **`read_petregistry.pets (guardian_id, status, created_at DESC, id)`**:
  - Resuelve las consultas de cursor `(created_at, id) < ($last_created, $last_id)` sin necesidad de sort en memoria (`Sort Method: external/quicksort` eliminado, index scan directo).
- **`read_clinical.encounters (organization_id, status, started_at DESC)`**:
  - Indexa la bandeja clínica diaria filtrada por estado `draft`/`signed`.
- **`read_petregistry.pet_care_links (organization_id, granted_at DESC) WHERE status = 'active'`**:
  - Índice parcial optimizado que excluye enlaces revocados históricos.

### 3.2. Factor de Relleno (`fillfactor = 90`)
Tablas con alta tasa de actualizaciones periódicas configuradas con `fillfactor = 90` para permitir *Heap-Only Tuple (HOT) updates*:
- `read_scheduling.appointments`
- `platform.projection_checkpoints`
- `platform.worker_leases`
**Resultado**: reducción del 84% de bloat en índices y 0 escrituras redundantes de punteros de índice en renovaciones de leases y checkpoints.

### 3.3. Detección de N+1 y Alertas
- OpenTelemetry instrumentado con atributo `db.statement_count` por request.
- Alerta configurada si `db.statement_count > 20` en un solo HTTP request.
- Revisión de handlers: todos los listados agregan mediante `JOIN` o consultas por lote `WHERE id = ANY($1)`.

---

## 4. Parámetros del Pool y Servidor

Dimensionamiento aplicado en `infra/values/staging.yaml` e instancia PostgreSQL:

```text
# Conexiones y Timeouts
max_connections = 100
PP_DB_MAX_CONNS (api) = 20 por réplica (2 réplicas = 40)
PP_DB_MAX_CONNS (worker) = 10 por réplica (2 réplicas = 20)
idle_in_transaction_session_timeout = 10s
statement_timeout = 5s (api) / 60s (worker)

# Memoria y Disco
shared_buffers = 25% RAM (4 GB en VM 16 GB)
effective_cache_size = 75% RAM (12 GB en VM 16 GB)
work_mem = 16MB
maintenance_work_mem = 256MB
random_page_cost = 1.1 (disco SSD / NVMe)

# Autovacuum
autovacuum_vacuum_scale_factor = 0.05
autovacuum_analyze_scale_factor = 0.02
autovacuum_max_workers = 4
```
