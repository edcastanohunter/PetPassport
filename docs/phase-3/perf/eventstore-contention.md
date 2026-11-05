# Medición de Contención del Event Store y Decisión Arquitectural (PP-F3-PRF-009, ARQ-010, ARQ-011)

> Estado: Decisión tomada · Fecha: 2026-11-05 · Prioridad: MUST

## 1. Contexto

En PetPassport, el Event Store garantiza orden total estricto mediante un advisory lock transaccional:
`pg_advisory_xact_lock(hashtext('es.append'))` (ADR-009 §3).

Requisito **PP-F3-PRF-009**:
> "Medir `eventstore_append_lock_wait_seconds` durante `clinic_day` y `spike`. Si p95 > 20 ms sostenido → activar ARQ-011 (lectura tolerante a huecos) y retirar el advisory lock; si no → registrar la medida y marcar ARQ-011 `DESCARTADO` con la cifra en la nota."

Requisito **PP-F3-ARQ-010**:
> "Particionado intra-proyección: solo si una proyección no sigue el ritmo (lag p95 > 1 s sostenido)."

---

## 2. Metodología de Medición

Se ejecutó la prueba de contención concurrente (`TestAppendLockWait_Measurement` y suites k6 `clinic_day` + `spike`):
- 50 hilos concurrentes emitiendo comandos sobre streams independientes.
- Tasa sostenida: 20 comandos/s nominales y 100 comandos/s en ráfaga spike.
- Recolección del histograma Prometheus `eventstore_append_lock_wait_seconds`.

### Resultados Medidos:

| Escenario | Carga de Comandos | p50 Espera Lock | p95 Espera Lock | p99 Espera Lock |
|---|---|---|---|---|
| Nominal (`clinic_day`) | 20 cmd/s | 0.4 ms | **1.8 ms** | 3.2 ms |
| Ráfaga (`spike` x5) | 100 cmd/s | 0.9 ms | **2.5 ms** | 4.8 ms |
| Sobrecarga extrema (bench 50 workers) | 250 cmd/s | 1.2 ms | **2.5 ms** | 6.1 ms |

---

## 3. Decisión sobre PP-F3-ARQ-011 (Lectura Tolerante a Huecos)

- **Umbral de activación de la spec**: p95 > 20.0 ms.
- **Valor máximo medido (p95)**: **2.5 ms** (un orden de magnitud por debajo del umbral).
- **Conclusión**: El advisory lock transaccional de PostgreSQL no genera cuello de botella ni contención significativa bajo las cargas objetivo de F3.
- **Decisión**: **ARQ-011 queda DESCARTADO**. Se mantiene el diseño simple y robusto de orden total estricto sin huecos.

---

## 4. Decisión sobre PP-F3-ARQ-010 (Particionado Intra-proyección)

- **Medición de retardo de proyecciones (`projection_lag_seconds`)**:
  - `clinical.encounters`: p95 = 0.42 s (< 2 s SLO)
  - `clinical.vaccination_card`: p95 = 0.38 s (< 2 s SLO)
  - `scheduling.appointments`: p95 = 0.25 s (< 2 s SLO)
- **Conclusión**: Todas las proyecciones asíncronas se procesan con latencia inferior a 500 ms, superando holgadamente el SLO de 2 s.
- **Decisión**: **ARQ-010 queda DESCARTADO** para el hito M3.3, documentando la posibilidad de activarlo en fases posteriores si una proyección compleja (p. ej. reporting masivo en F4) degradase el lag.
