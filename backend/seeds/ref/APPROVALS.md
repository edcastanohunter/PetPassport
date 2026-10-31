# Aprobaciones veterinarias de los catálogos de referencia (PP-F2-DAT-009, QA-006)

Los valores de `vaccines.yaml`, `protocols_CO.yaml` y `preventive_CO.yaml` son un punto de
partida, **no verdad clínica**. Mientras un veterinario no firme aquí, los tres archivos llevan
`approved: false` y el cargador deja los registros con `active = false` (salvo en `dev`, donde
`PP_SEED_UNAPPROVED=1` los activa para desarrollo).

Para aprobar: el veterinario revisa los tres archivos, el equipo cambia `approved: true` en el mismo
PR que añade su firma a la tabla, y el siguiente `migrate up` los activa.

| Archivo | Versión revisada (commit) | Veterinario/a | Nº de matrícula | Fecha | Estado |
|---|---|---|---|---|---|
| `vaccines.yaml` | — | — | — | — | **PENDIENTE** |
| `protocols_CO.yaml` | — | — | — | — | **PENDIENTE** |
| `preventive_CO.yaml` | — | — | — | — | **PENDIENTE** |

Estado global: **PENDIENTE**. Decisión del responsable del producto: se mantiene pendiente hasta
cerrar la Fase 2; entre tanto no se activan en staging ni en producción.
