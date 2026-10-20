# ADR-004: Modelo owner-centric — el pasaporte es del tutor; la clínica accede por CareLink

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Contexto

En los sistemas de gestión veterinaria tradicionales el paciente pertenece a la clínica (tenant). Eso impide
que la historia viaje con la mascota y crea duplicados cuando el tutor cambia de clínica. PetPassport vende
exactamente lo contrario.

## Decisión

1. `Pet` tiene **un tutor principal** (`GuardianID`, un `UserAccount`). El tutor es quien crea la mascota y
   quien puede transferir la tutela.
2. Las clínicas acceden al paciente a través de un **`CareLink`** (vínculo de atención) dentro del agregado `Pet`:
   `{OrganizationID, Status: active|revoked, GrantedBy, GrantedAt}`.
   - F1: el tutor concede el vínculo introduciendo el código público de la clínica; el tutor puede revocarlo.
   - F2: la clínica puede solicitar el vínculo y el tutor lo aprueba; registros clínicos creados por una clínica
     siguen siendo visibles para el tutor aunque el vínculo se revoque (la clínica deja de ver *nuevos* datos y
     conserva acceso de lectura a lo que ella misma registró, por obligación legal de custodia).
3. El `OrganizationID` no es un tenant de datos: no hay esquemas ni filas "por clínica". El aislamiento se
   aplica en la capa de autorización y en las consultas (`WHERE org_id IN (links activos)`).
4. Las organizaciones (clínicas y farmacias) sí son **unidades de administración**: tienen miembros, roles,
   verificación y, en F5, suscripción.

## Consecuencias

- Las consultas "pacientes de la clínica" son proyecciones de `CareLink`, no de propiedad.
- La autorización es siempre contextual: *actor × acción × recurso × vínculo*.
- Un tutor puede tener la misma mascota vinculada a varias clínicas (segunda opinión, urgencias).
- La transferencia de tutela mantiene la historia clínica intacta (misma `PetID`).

## Alternativas consideradas

- **Tenant = clínica con mascotas compartidas por copia**: descartado; rompe la promesa del producto.
- **Multi-tutor desde F1**: diferido a F2 como `SecondaryGuardian` (co-tutor con permisos limitados).
