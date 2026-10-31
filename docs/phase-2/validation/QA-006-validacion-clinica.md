# QA-006 — Validación clínica con veterinarios del piloto (M2.2)

Estado: **PENDIENTE** hasta cerrar la Fase 2 (decisión del responsable del producto).

Mientras no haya aprobación, las semillas de `backend/seeds/ref/` se cargan con `approved: false` y
**no se activan** (`ref.*.active = false`) salvo `PP_SEED_UNAPPROVED=1` (rechazado en `PP_ENV` staging/prod).

## Checklist para el veterinario revisor

1. Revisar `vaccines.yaml`: nombre, especies, enfermedades, tipo y vías.
2. Revisar `protocols_CO.yaml`: serie inicial (edad mínima, dosis, intervalos), refuerzo (meses) y
   validez legal (meses) por especie y vacuna.
3. Revisar `preventive_CO.yaml`: intervalos de desparasitación por edad/especie.
4. Anotar correcciones en el PR; cada cambio se refleja en el YAML.
5. Registrar nombre, matrícula, fecha y versión (commit) en `backend/seeds/ref/APPROVALS.md` y poner
   `approved: true` en los ítems aprobados.

## Criterios de cierre

- `APPROVALS.md` sin filas `PENDIENTE`.
- `go run ./cmd/migrate seed-ref` activa el catálogo sin `PP_SEED_UNAPPROVED`.
- Casos de `domain/nextdue_test.go` conformes con el protocolo aprobado.
