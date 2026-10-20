# ADR-013: OpenSearch como read model de búsqueda; indexación por suscripción catch-up; autorización denormalizada en los documentos

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F3

## Contexto

Las consultas de F1–F2 son por clave o por listas acotadas (PostgreSQL + `pg_trgm`). F3 necesita búsqueda
en texto libre con relevancia, resaltado, tolerancia a errores y facetas sobre historiales clínicos,
pacientes, catálogo de medicamentos y organizaciones. Además, cada búsqueda debe respetar la autorización
(tutor ve lo suyo; clínica ve lo vinculado y lo custodiado) sin filtrar en la aplicación miles de resultados.

## Decisión

1. **OpenSearch es un read model más**: se alimenta exclusivamente de `es.events` mediante una suscripción
   catch-up (ADR-009) con checkpoint propio (`search.<índice>`); nunca es fuente de verdad; se puede
   reconstruir con `rebuild`.
2. **Un índice por tipo de documento**, con alias de lectura y escritura (`clinical` → `clinical-v3`):
   `pets`, `clinical` (encuentros firmados, adendas, entradas de carnet), `medications`, `organizations`.
   Cambios de *mapping* se hacen creando `-vN+1`, reindexando desde eventos y cambiando el alias (sin downtime).
3. **Autorización denormalizada**: cada documento lleva `access: {guardian_id, org_ids_linked[], org_ids_custody[]}`.
   La API construye la consulta con un `filter` obligatorio derivado del `Principal` (`guardian_id = me` o
   `org_ids_linked ∋ org` / `org_ids_custody ∋ org` según el nivel). Los eventos `CareLinkGranted/Revoked`,
   `EncounterSigned` y `PetGuardianshipTransferred` actualizan `access` de los documentos afectados
   (`update_by_query` acotado por `pet_id`).
4. **Sin PII de contacto en el índice**: nombres de mascota, texto clínico y nombres de organización sí;
   emails, teléfonos y direcciones de tutores no (se resuelven desde PostgreSQL al presentar).
5. Idioma: analizador `spanish` + `icu_folding` + sinónimos veterinarios versionados; `edge_ngram` para
   autocompletado de medicamentos y pacientes; `english` para campos `locale = en`.
6. La búsqueda es **best-effort eventual** (segundos de retraso) y la UI lo indica (`X-PP-Freshness`).

## Consecuencias

- La lógica de autorización existe en dos lugares (políticas en Go y filtros en consulta) → prueba
  obligatoria de "fuga": para cada rol/nivel, una búsqueda sin filtro frente a con filtro debe diferir
  exactamente en los documentos no accesibles (SEC-F3).
- Reindexar `access` tras una revocación tarda segundos: durante ese intervalo la API aplica además un
  *post-filtro* por `pet_id` contra `PetAccess` sobre los resultados devueltos (cinturón y tirantes).
- OpenSearch es un componente operativo nuevo (gestionado en prod, contenedor en dev/staging).

## Alternativas consideradas

- **PostgreSQL full-text (`tsvector`) + `pg_trgm`**: suficiente para F1–F2; insuficiente para relevancia,
  sinónimos, facetas y autocompletado a escala; se mantiene para búsquedas simples ya existentes.
- **Filtrar por lista de `pet_id` accesibles pasada en la consulta** (`terms`): válido para tutores (pocas
  mascotas), inviable para clínicas con miles de pacientes; se usa solo como post-filtro.
- **Elasticsearch / Meilisearch / Typesense**: OpenSearch por licencia y por el stack pedido.
