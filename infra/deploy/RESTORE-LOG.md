# Registro de pruebas de restauración (INF-009, DAT-009)

Procedimiento: infra/deploy/RUNBOOK.md §9.3 (A = PITR del proveedor, B = respaldo frío de
`es.events`). Una fila por prueba mensual; solo cuentan las de staging/prod.

| Fecha | Entorno | Tipo | Punto restaurado | Versión de migración | `count(es.events)` | Duración | Quién | Resultado |
|---|---|---|---|---|---|---|---|---|
| 2026-10-28 | dev (Podman, ensayo del procedimiento; no cuenta como mensual) | B | exportación `es-events/2026/2026-10-28T123436Z.jsonl.gz` (sha256 `5b8adc79…`) | 20261026000007 | 1200 = manifiesto (max pos 1200; 1200 streams) | 9 s (migrate + import) | agente (PR M1.5) | OK: verify-events coincide; fichero alterado → rechazado; segundo import → rechazado |
| | staging | A | | | | | | _pendiente: requiere PostgreSQL gestionado (prerrequisito humano)_ |
| | staging | B | | | | | | _pendiente: primera exportación semanal en staging_ |
