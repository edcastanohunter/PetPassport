# F3 · 02 — Modelo de dominio (DDD)

## 1. Lenguaje ubicuo (ampliación)

**[PP-F3-DOM-001] (MUST)**

| Término | Código | Definición |
|---|---|---|
| Documento de búsqueda | `SearchDocument` | Representación desnormalizada indexada en OpenSearch (`PetDoc`, `ClinicalDoc`, `MedicationDoc`, `OrgDoc`). |
| Alcance de acceso | `AccessScope` | `{guardian_id, org_ids_linked[], org_ids_custody[]}` embebido en documentos. |
| Frescura | `Freshness` | Retraso entre el último evento y el último indexado/proyectado. |
| Catálogo de medicamentos | `MedicationCatalog` | Datos de referencia de productos farmacéuticos veterinarios. |
| Medicamento | `Medication` | Entrada del catálogo con `catalog_id` estable. |
| Sugerencia de catálogo | `MedicationSuggestion` | Propuesta de alta por un veterinario, pendiente de revisión. |
| Cohorte | `Cohort` | Consulta parametrizada que devuelve pacientes de una organización. |
| Directorio | `Directory` | Listado público opt-in de organizaciones. |
| Paciente provisional | `ProvisionalPet` | Mascota creada por una clínica sin tutor registrado; se reclama. |
| Reclamación | `PetClaim` | Proceso por el que un tutor toma la tutela de un paciente provisional. |
| Lease | `Lease` | Arrendamiento temporal de una proyección/runner por una réplica de worker. |
| Suscripción push | `PushSubscription` | Endpoint Web Push de un dispositivo de un usuario. |

## 2. Mapa de contextos F3

**[PP-F3-DOM-002] (MUST)**

```
Pet Registry ─┐                         ┌──────────────────────────┐
Clinical ─────┼── eventos (catch-up) ──►│ Search & Reporting        │──► OpenSearch (pets, clinical, medications, organizations)
Scheduling ───┤                         │ indexers, query service,  │
Partner Net. ─┘                         │ cohorts, platform stats   │◄── Principal (filtros de acceso)
                                        └──────────┬───────────────┘
Clinical (MedicationItem v2) ──── Catalog (puerto) ─┤
                                        ┌──────────▼───────────────┐
                                        │ Medication Catalog        │ (CRUD + import + sugerencias)
                                        └──────────────────────────┘
Notifications ── + canal push ── PushSubscription (CRUD)
Pet Registry ── + ProvisionalPet / PetClaim
Partner Network ── + DirectoryListing
```

- *Search & Reporting* es **downstream de todos** (Conformist sobre eventos publicados); no emite eventos de dominio; expone consultas.
- *Medication Catalog* es un subdominio de soporte con datos de referencia; *Clinical* lo consume por puerto `Catalog.Medication(id)`.

## 3. Search & Reporting

### 3.1 Documentos

**[PP-F3-DOM-003] (MUST)**

```jsonc
// PetDoc (índice pets) — id = pet_id
{ "pet_id": "…", "name": "Luna", "name_suggest": ["Luna"], "species": "dog", "breed": "Beagle", "sex": "female",
  "birth_date": "2021-05-10", "microchip": "941000012345678", "microchip_suffix": "5678", "status": "active",
  "guardian_display_name": "Ana G.",            // nombre, no contacto
  "provisional": false,
  "access": { "guardian_id": "…", "org_ids_linked": ["…"], "org_ids_custody": ["…"] },
  "last_encounter_at": "…", "updated_at": "…", "version": 12 }

// ClinicalDoc (índice clinical) — id = encounter_id | card_entry_id
{ "doc_kind": "encounter" | "addendum" | "card_entry",
  "pet_id": "…", "pet_name": "Luna", "species": "dog", "organization_id": "…", "organization_name": "Clínica Sur",
  "professional_id": "…", "occurred_at": "…", "encounter_type": "consultation",
  "diagnoses": [{ "text": "Otitis externa", "code": "…", "certainty": "confirmed" }],
  "text": "…anamnesis + hallazgos + plan + recomendaciones…",     // campo de texto libre concatenado
  "medications": [{ "name": "Prednisolona", "catalog_id": "…" }],
  "procedures": ["Limpieza ótica"], "vaccine_product": null,
  "locale": "es",
  "access": { "guardian_id": "…", "org_ids_linked": ["…"], "org_ids_custody": ["org-que-firmó"] },
  "position": 123456 }                                             // global_position para idempotencia

// MedicationDoc (índice medications) — id = catalog_id
{ "catalog_id": "…", "name": "…", "name_suggest": […], "active_ingredients": ["…"], "form": "…", "concentration": "…",
  "presentation": "…", "manufacturer": "…", "species": ["dog","cat"], "routes": ["oral"], "prescription_required": true,
  "controlled": false, "status": "active", "synonyms": [], "atcvet_code": "…", "usage_count": 42 }

// OrgDoc (índice organizations) — id = organization_id
{ "organization_id": "…", "type": "clinic", "display_name": "…", "city": "…", "region": "…", "country": "CO",
  "geo": { "lat": 6.2, "lon": -75.5 }, "services": ["…"], "listed": true, "status": "active" }
```

### 3.2 Proyección de `access`

**[PP-F3-DOM-004] (MUST)** Reglas (por `pet_id`):
- `guardian_id` = tutor actual (`PetRegistered`, `PetGuardianshipTransferred`, `PetClaimed`).
- `org_ids_linked` = organizaciones con `CareLink` activo y org `active` (`CareLinkGranted`, `CareLinkRevoked`, `OrganizationSuspended/Reactivated` → recalcula para todas las mascotas de la org).
- `org_ids_custody` = organizaciones con ≥ 1 encuentro firmado (`EncounterSigned`); nunca decrece.
- Cambios de `access` se aplican a **todos** los documentos de la mascota (`pets` y `clinical`) con `update_by_query` por `pet_id` (ARQ-001).

### 3.3 Cohortes

**[PP-F3-DOM-009] (SHOULD)** Cohortes como consultas parametrizadas (no agregados), definidas en código con `{Key, Params schema, Source pg|search, Build(params, orgID) Query}`:

| Key | Fuente | Criterio |
|---|---|---|
| `vaccine_due` | PG `vaccination_due` + `pet_care_links` | `due_date ∈ [hoy, hoy+N]` o vencidas, por `product_name?`, vínculo activo con la org |
| `no_visit_since` | PG `pets` + `encounter_custody`/`encounters` | sin encuentro firmado con la org en > N meses |
| `diagnosis` | OpenSearch `clinical` | `diagnoses.text|code` ≈ X, `certainty = confirmed`, `occurred_at ≥ hoy−N`, `access.org_ids_linked ∋ org` |
| `species_age_weight` | PG `pets` + `weight_history` | filtros estructurados |

Resultado: `{pet_id, pet_name, species, guardian_display_name, guardian_phone? (solo con permiso y auditado), last_encounter_at, reason}`; exportación limitada (RN-20).

### 3.4 Estadísticas de plataforma

Vistas materializadas en PG refrescadas por el worker (DAT-008) + agregaciones OpenSearch para "top clínicas por actividad" y tendencias de búsqueda; sin PII.

## 4. Medication Catalog

**[PP-F3-DOM-005] (MUST)**

```go
type Medication struct {
    ID                 MedicationID       // UUIDv7 estable
    Name               string             // comercial, 1–160
    ActiveIngredients  []Ingredient       // {Name, Strength decimal?, Unit?}
    Form               Form               // tablet, capsule, oral_suspension, oral_solution, injectable, topical, ophthalmic, otic, spray, powder, other
    Concentration      *string
    Presentation       *string
    Routes             []Route            // enum F2
    Species            []Species
    Manufacturer       *string
    ATCvetCode         *string            // Q… (WHO ATCvet)
    Regulatory         *Regulatory        // {Country, RegistryNumber?, Authority "ICA"}
    Controlled, PrescriptionRequired bool
    DefaultDoseUnit    *DoseUnit; DefaultRoute *Route
    Synonyms           []string
    Status             Status             // active | discontinued | merged
    MergedInto         *MedicationID
    Source             Source             // {Kind manual|import|suggestion, Ref, ImportedAt}
}
```

Operaciones (CRUD auditado, `catalog:admin`): `Create`, `Update`, `Discontinue{reason}`, `Merge{into, reason}` (el origen pasa a `merged`; las consultas de `catalog_id` resuelven la redirección), `ImportBatch{source, rows}` → `{created, updated, skipped, errors[]}` con `batch_id`, `RevertBatch{batch_id}`.
Sugerencias: `MedicationSuggestion{ID, ProposedBy, OrganizationID, Name, Notes, PhotoKey?, Status pending|approved|rejected, ReviewedBy, Reason}`; `Approve` crea `Medication` con `Source.Kind = suggestion`.

Invariantes: `Name` único por `(Manufacturer, Form, Concentration)` normalizados (índice único parcial); un `merged` no puede ser destino de otro merge en cadena > 3; `Discontinue` no borra.

### 4.1 `MedicationItem` v2

**[PP-F3-DOM-006] (MUST)** `PlanSet` schema v2: cada `MedicationItem` añade `catalog_id *MedicationID`, `free_text bool`, `catalog_name_snapshot *string` (nombre en el momento, para auditoría). Upcaster `PlanSet v1→v2`: `catalog_id = null, free_text = true, catalog_name_snapshot = null`. El agregado valida en `SetPlan` que `catalog_id` exista y esté `active` (puerto `Catalog`), resolviendo `merged` al destino.

## 5. Pet Registry: paciente provisional

**[PP-F3-DOM-007] (SHOULD)** Nuevo flujo en `Pet`:

- `RegisterProvisionalPet{OrgID, CreatedBy, PetData, GuardianContact{Email?|Phone?}}` → `ProvisionalPetRegistered{…, pii{guardian_email?, guardian_phone?}}` + `CareLinkGranted{org, granted_by: org member, basis: provisional}`. Estado: `Guardian = nil`, `Provisional = true`, `ProvisionalBy = OrgID`.
- Invitación de reclamación (token 14 d, reenviable) → `PetClaimInvited`.
- `ClaimPet{Token, UserID}` (actor con email/teléfono verificado igual al de contacto) → `PetClaimed{guardian_id}`; `Provisional = false`; vínculo con la clínica se mantiene.
- `MergeProvisionalInto{TargetPetID}` (tutor, si ya tenía la mascota por microchip): eventos `ProvisionalPetMerged{into}` en el provisional (estado `merged`) y `CareLinkGranted` en el destino; los encuentros del provisional se re-vinculan por proyección (`clinical.merge-pet` process manager actualiza `pet_id` en read models y reindexa; los eventos clínicos originales conservan el `pet_id` antiguo + tabla `pet_aliases`).
- Solo la org creadora puede editar un provisional (P3); tras 90 días sin reclamar → `ProvisionalPetUnclaimed` (marca, sin pérdida de acceso).

## 6. Partner Network: directorio

**[PP-F3-DOM-010] (SHOULD)** `Organization` añade `DirectoryListing{Listed bool, Services []string ≤ 10, ShowPhone bool, Neighborhood?, Geo?}`; comandos `UpdateDirectoryListing` → `OrganizationDirectoryListingUpdated`. Solo orgs `active`; suspensión retira del directorio (proyección).

## 7. Notifications: push

**[PP-F3-DOM-011] (SHOULD)** `PushSubscription{ID, UserID, Endpoint, Keys{p256dh, auth}, UserAgentHash, CreatedAt, LastSeenAt, Status active|expired}` (CRUD). Canal `push` en reglas con `Mode` igual que email; preferencias por categoría y por canal (`email`, `push`).

## 8. Políticas F3

**[PP-F3-DOM-008] (MUST)**

| Disparador | Reacción |
|---|---|
| Cualquier evento de `Pet`, `Encounter` (firmado), `VaccinationCard`, `Organization` | indexer correspondiente (ARQ-001) |
| `CareLink*`, `OrganizationSuspended/Reactivated`, `EncounterSigned`, `PetGuardianshipTransferred`, `PetClaimed` | recálculo de `access` por mascota/organización |
| `MemberRemoved/RoleChanged`, `OrganizationSuspended`, `PetProfileUpdated`, `Appointment*`, `Agenda*`, catálogos | `cache-invalidator` publica claves (ARQ-006) |
| `Medication.Merge/Discontinue` | reindex del documento y de `usage_count` |
| Acceso clínico > umbral (SEC-006) | `anomaly-detector` crea alerta y notifica a SuperAdmin |
| `ProvisionalPetMerged` | `clinical.merge-pet` (read models + reindex) |

## 9. Decisiones explícitas

- Search no tiene agregados ni eventos propios: es puro read side.
- `usage_count` de medicamentos se calcula desde `PlanSet` (eventos) y se usa como *boost* de relevancia; no es dato de negocio.
- Los pacientes provisionales reutilizan `Pet` (mismo stream) para que reclamar no cambie `PetID` y la historia clínica quede intacta.
