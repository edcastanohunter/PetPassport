# Registro de pruebas de restauración (INF-009, DAT-009)

Procedimiento: infra/deploy/RUNBOOK.md §9.3 (A = PITR del proveedor, B = respaldo frío de
`es.events`). Una fila por prueba mensual; solo cuentan las de staging/prod.

| Fecha | Entorno | Tipo | Punto restaurado | Versión de migración | `count(es.events)` | Duración | Quién | Resultado |
|---|---|---|---|---|---|---|---|---|
| 2026-10-28 | dev (Podman, ensayo del procedimiento; no cuenta como mensual) | B | exportación `es-events/2026/2026-10-28T123436Z.jsonl.gz` (sha256 `5b8adc79…`) | 20261026000007 | 1200 = manifiesto (max pos 1200; 1200 streams) | 9 s (migrate + import) | agente (PR M1.5) | OK: verify-events coincide; fichero alterado → rechazado; segundo import → rechazado |
| 2026-11-10 | staging (K8s multi-AZ) | A | PITR RDS + rebuild proyecciones (ensayo completo M5.0) | 20261110000027 | 58.420 eventos | 24 min (RTO: 24 min, RPO: 2 min) | equipo ops + agente (M5.0) | OK: PITR + copia cross-region + rebuild --all completados; RPO 2 min (<= 5 min), RTO 24 min (<= 1 h) cumplidos (PP-F5-OPS-003, OPS-004, QA-004) |
| | staging | B | | | | | | _pendiente: primera exportación semanal en staging_ |
