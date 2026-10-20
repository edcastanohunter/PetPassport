# ADR-016: Catálogo de medicamentos como datos de referencia con identificadores estables; `MedicationItem` v2 enlaza al catálogo

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F3 (base para F4)

## Contexto

En F2 los ítems de medicación del plan son texto estructurado (`medication_name`, dosis, vía…) sin
identificador. F4 necesita emparejar recetas (estructuradas u OCR) con productos reales que las farmacias
puedan cotizar. Un catálogo con identificadores estables es el puente, y la búsqueda con autocompletado es
lo que hará que los veterinarios lo usen.

## Decisión

1. **`ref.medications`** (datos de referencia, CRUD auditado, no ES): `id` (UUIDv7 estable), `name`
   (comercial), `active_ingredients[] {name, strength, unit}`, `form` (tableta, suspensión, inyectable…),
   `concentration`, `presentation` (p. ej. "caja × 10 tabletas"), `route[]`, `species[]`, `manufacturer`,
   `atcvet_code?`, `regulatory {country, registry_number?}` (Colombia: registro ICA), `controlled bool`,
   `prescription_required bool`, `status active|discontinued`, `synonyms[]`, `source {kind, ref, imported_at}`.
2. **Carga inicial** desde fuentes públicas del país (importador `cmd/worker catalog-import --source ica`)
   con revisión manual por lotes; alta manual por SuperAdmin; propuesta de alta por veterinarios
   (`suggested`, revisada por admin) para no bloquear la consulta.
3. **`MedicationItem` v2** añade `catalog_id?` y `free_text bool`; upcaster `PlanSet v1 → v2` rellena
   `catalog_id = null, free_text = true`. La UI ofrece autocompletado desde OpenSearch (`medications`) y
   guarda el `catalog_id`; el texto libre sigue permitido (con marca).
4. Identidad de producto para F4: el `catalog_id` será la clave de emparejamiento con los catálogos de
   partners (tabla de correspondencia por partner en F4). Los cambios en el catálogo **no** reescriben
   eventos pasados.
5. Fuera de alcance: interacciones medicamentosas, dosis recomendadas por especie (se evalúa en F6 con
   contenido clínico curado).

## Consecuencias

- Se introduce el primer upcaster con cambio semántico real (ARQ-005 de F2 ejercitó uno trivial).
- La calidad del catálogo es un trabajo de contenido continuo: hay métricas de uso (`% ítems con
  catalog_id`) y un flujo de sugerencias.

## Alternativas consideradas

- Catálogo externo por API (proveedor comercial): no existe uno veterinario fiable para el mercado inicial;
  se reconsidera por país en F6.
- Posponer el catálogo a F4: F4 ya es la fase más larga; el catálogo y su búsqueda encajan en F3 por naturaleza.
