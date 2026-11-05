# Evaluación de PgBouncer (PP-F3-PRF-005)

> Estado: Descartado temporalmente por capacidad · Fecha: 2026-11-05 · Prioridad: COULD

## 1. Criterio de Activación (Spec PRF-005)

La especificación define que PgBouncer se introduce **únicamente si** el producto de `réplicas × conexiones_por_pool` supera el **80% de `max_connections`** de la base de datos PostgreSQL.

### Medición Actual (Staging y Carga Nominal F3)
- `max_connections` en PostgreSQL: **100**
- Umbral del 80%: **80 conexiones concurrentes**
- Dimensionamiento de réplicas en F3 M3.3:
  - 2 réplicas `api` × 20 conexiones = 40
  - 2 réplicas `worker` × 10 conexiones = 20
  - Conexiones de mantenimiento, backups y monitorización: ~5
  - **Total de conexiones pico**: **65 conexiones**
  - **Porcentaje de uso**: **65%** (< 80%)

**Decisión**: No se activa PgBouncer en el hito M3.3. Se mantiene el pool nativo `pgxpool` en Go.

---

## 2. Análisis de Compatibilidad Técnica para Fases Posteriores

En caso de escalar a ≥ 4 réplicas `api` (80 conexiones + 20 worker = 100 conns), se evaluó el modo de operación de PgBouncer:

1. **Modo Transacción (`pool_mode = transaction`)**:
   - **Compatible con**:
     - `pg_advisory_xact_lock(hashtext('es.append'))`: el lock advisory dura estrictamente la duración de la transacción y se libera automáticamente en el `COMMIT`/`ROLLBACK`.
     - `SET LOCAL ...`: variables de entorno de transacción compatibles.
     - Consultas preparadas nominadas: requiere soporte de PgBouncer v1.21+ (`max_prepared_statements > 0`).
   - **Incompatible con**:
     - `LISTEN / NOTIFY`: no persiste a través de transacciones en modo transacción. En PetPassport, el worker que escucha notificaciones debe conectarse mediante un pool directo bypass o puerto sin pooler.
     - `SET` de sesión persistente: prohibido en la arquitectura (siempre se usa `SET LOCAL`).

---

## 3. Regla de Escalado
Si el escalado horizontal del HPA supera 3 réplicas `api` de forma sostenida (superando 80 conexiones hacia PostgreSQL), se desplegará PgBouncer como DaemonSet o sidecar en Kubernetes con `pool_mode = transaction` para `api`, reservando conexión directa exclusiva para el runner de notificaciones del `worker`.
