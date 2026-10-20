# F2 · 02 — Modelo de dominio (DDD)

## 1. Lenguaje ubicuo (ampliación)

**[PP-F2-DOM-001] (MUST)**

| Término (ES) | Código | Definición |
|---|---|---|
| Consulta / atención | `Encounter` | Unidad de atención clínica a una mascota por una organización. Raíz de agregado. |
| Borrador / firmada | `draft` / `signed` | Estados del encuentro. Firmar congela el contenido. |
| Adenda | `Addendum` | Texto añadido a un encuentro firmado, firmado a su vez. |
| Constantes vitales | `Vitals` | Peso, temperatura, FC, FR, condición corporal… |
| SOAP | `Subjective/Objective/Assessment/Plan` | Estructura de documentación clínica. |
| Diagnóstico | `Diagnosis` | Texto + código opcional + certeza. |
| Ítem de medicación | `MedicationItem` | Medicamento estructurado dentro del plan (base de `Prescription` en F4). |
| Control recomendado | `FollowUpRecommendation` | Fecha y motivo de revisión sugerida. |
| Historial clínico | `MedicalTimeline` | Read model cronológico por mascota. |
| Carnet | `VaccinationCard` | Agregado por mascota con dosis de vacunas y preventivos. |
| Dosis / entrada | `CardEntry` | Vacuna o preventivo aplicado o declarado. |
| Protocolo | `VaccinationProtocol` | Esquema de dosis por especie/país/vacuna (datos de referencia). |
| Próxima dosis | `NextDue` | Fecha propuesta por protocolo o ajustada por la clínica. |
| Declarada / registrada / verificada | `declared` / `recorded` / `verified` | Origen y verificación de una entrada. |
| Cita | `Appointment` | Reserva de atención en franja. Raíz de agregado. |
| Agenda del profesional | `ProfessionalAgenda` | Disponibilidad de un profesional en una organización. Raíz de agregado. |
| Franja | `Slot` | Intervalo `[start, end)` en la zona horaria de la organización. |
| Check-in / no-show | `checked_in` / `no_show` | Estados de cita. |
| Solicitud de vínculo | `CareLinkRequest` | Petición de la clínica al tutor. Entidad dentro de `Pet`. |
| Enlace compartido | `RecordShare` | Token de lectura pública del carnet o historial. |
| Recordatorio | `Reminder` | Notificación programada derivada de una regla. |

## 2. Mapa de contextos F2

**[PP-F2-DOM-002] (MUST)**

```
┌───────────────┐      PetAccess (puerto)      ┌──────────────────────┐
│ Pet Registry  │◄─────────────────────────────│ Clinical Records     │
│ Pet           │      eventos CareLink*       │ Encounter (ES)       │
│  +CareLink    │─────────────────────────────►│ VaccinationCard (ES) │
│  +CareLinkReq │                              │ RecordShare (CRUD)   │
└──────┬────────┘                              │ Catálogos (CRUD)     │
       │ PetAccess                             └─────────┬────────────┘
       ▼                                                 │ EncounterSigned, FollowUpRecommended,
┌───────────────┐   AppointmentCheckedIn (evento)        │ VaccinationEntryRecorded…
│ Scheduling    │────────────────────────────────────────┤
│ Appointment   │◄─── EncounterSigned (completa cita) ───┤
│ Agenda (ES)   │                                        ▼
└──────┬────────┘                              ┌──────────────────────┐
       │ AppointmentConfirmed/Cancelled…       │ Notifications        │
       └──────────────────────────────────────►│ process manager      │
                                               │ scheduled/deliveries │
                                               └──────────────────────┘
Partner Network ──► Membership (puerto) ──► todos      Identity ──► Principal ──► todos
```

| Relación | Mecanismo |
|---|---|
| Pet Registry → Clinical / Scheduling | Puerto `PetAccess{CanOrgAccess(petID, orgID) (AccessLevel, error); GuardianOf(petID)}` sobre `read_petregistry` (incluye regla de custodia post-revocación consultando `read_clinical.encounter_custody`). |
| Scheduling → Clinical | `AppointmentCheckedIn` habilita `OpenEncounterFromAppointment`; `EncounterSigned{appointment_id}` → process manager completa la cita. |
| Clinical → Notifications | Eventos consumidos vía outbox por reglas (07-notificaciones). |
| Clinical ↔ Catálogos | Datos de referencia (CRUD) consultados por servicio de dominio `NextDueCalculator`. |

## 3. Clinical Records

### 3.1 Agregado `Encounter`

**[PP-F2-DOM-003] (MUST)** Stream `enc-<EncounterID>` · `stream_type = "Encounter"`.

```go
type Encounter struct {
    ID             EncounterID
    Version        int64
    PetID          PetID
    OrganizationID OrganizationID
    AppointmentID  *AppointmentID
    Type           EncounterType
    Status         EncounterStatus      // draft | signed | discarded
    StartedAt      time.Time
    Reason         Reason               // ≤ 300
    AttendingVetID *UserID
    Vitals         *Vitals
    Subjective     *Narrative           // ≤ 5000
    Objective      *Objective           // {Narrative, SystemsExam map[BodySystem]Finding}
    Diagnoses      []Diagnosis          // ≤ 20
    Plan           Plan                 // {Medications []MedicationItem ≤ 30, Procedures []Procedure ≤ 20, Recommendations Narrative}
    FollowUp       *FollowUpRecommendation
    Attachments    map[AttachmentID]Attachment // ≤ 10
    Signature      *Signature           // {SignedBy, SignedAt, StreamVersion, ContentHash}
    Addenda        []Addendum           // {Seq, Text, Reason, SignedBy, SignedAt}
    LastActivityAt time.Time
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `OpenEncounter{PetID, OrgID, Type, Reason, StartedAt, AttendingVetID?, AppointmentID?}` | E1 org tiene acceso a la mascota (puerto); E2 `StartedAt` ∈ [ahora−24h, ahora+1h]; si `AppointmentID`, la cita está `checked_in` y pertenece a la misma mascota/org. | `EncounterOpened` |
| `RecordVitals{Vitals}` | E3 status draft; rangos VO. | `VitalsRecorded` |
| `UpdateNarrative{Subjective?, Objective?}` | E3. | `EncounterNarrativeUpdated` (solo campos cambiados) |
| `SetDiagnoses{[]Diagnosis}` | E3; E4 actor `veterinarian`. | `DiagnosesSet` (reemplazo completo de la lista) |
| `SetPlan{Plan}` | E3; E4. | `PlanSet` |
| `RecommendFollowUp{DueDate, Reason}` / `ClearFollowUp` | E3; E4; `DueDate` > hoy y ≤ +365 d. | `FollowUpRecommended` / `FollowUpCleared` |
| `AssignAttendingVet{UserID}` | E3; el usuario es `veterinarian` activo de la org (puerto Membership). | `AttendingVetAssigned` |
| `AddAttachment{Attachment}` / `RemoveAttachment{ID}` | E3 (añadir también permitido en `signed` solo con adenda — no: tras firma, adjuntos solo vía `AddAddendum{attachments}`); ≤ 10. | `AttachmentAdded` / `AttachmentRemoved` |
| `SignEncounter{SignerID}` | E3; E5 `SignerID == AttendingVetID` y es `veterinarian`; E6 hay ≥ 1 diagnóstico **o** (`Reason` y `Objective`); E7 `StartedAt ≤ ahora`. | `EncounterSigned{stream_version, content_hash}` |
| `AddAddendum{Text, Reason, SignerID, Attachments?}` | status signed; E4 firmante `veterinarian` de la misma org; ≤ 20 adendas. | `EncounterAddendumAdded` |
| `DiscardEncounter{Reason}` | E3; nunca firmado; por `clinic_admin` o el vet asignado. | `EncounterDiscarded` |
| `SetTutorVisibility{AttachmentID, Visible, Reason?}` | cualquier estado; E4. | `AttachmentVisibilityChanged` |

Invariantes:
- **E1** Acceso de la organización a la mascota verificado por puerto al abrir (no se re-verifica en cada edición: el encuentro ya pertenece a la org).
- **E2** Ventana de `StartedAt`.
- **E3** Solo `draft` admite edición de contenido.
- **E4** Diagnósticos, plan, control, firma y adendas: solo `veterinarian` (authz + aserción en agregado vía `ActorRole` en metadatos).
- **E5** Firma por el profesional responsable.
- **E6** Contenido mínimo para firmar.
- **E7** No se firma en el futuro.
- **E8** `ContentHash` = SHA-256 del JSON canónico de los eventos del stream hasta `stream_version` (calculado por el repositorio, no por el agregado; se verifica en lectura).
- **E9** Tras `signed`: solo `AddAddendum`, `SetTutorVisibility`.

### 3.2 Value Objects

**[PP-F2-DOM-004] (MUST)**

| VO | Reglas |
|---|---|
| `EncounterType` | `consultation, follow_up, emergency, vaccination, surgery, procedure, other`. |
| `Vitals` | `WeightKg` 0.01–500 (2 dec); `TempC` 30–45; `HeartRateBpm` 20–400; `RespRateRpm` 5–200; `BodyCondition` 1–9; `Mucosa` enum; `CRTSeconds` 0.5–5; `HydrationPct` 0–15. Rangos por especie como *warnings* (no invariantes). |
| `Narrative` | ≤ 5.000, sin control chars, Markdown restringido (negrita, listas). |
| `BodySystem` / `Finding` | enum (`integumentary, musculoskeletal, cardiovascular, respiratory, digestive, genitourinary, nervous, eyes, ears, lymph, oral`) / `{Status normal|abnormal|not_examined, Note ≤ 500}`. |
| `Diagnosis` | `{Text 1–200, Code? {System, Value}, Certainty presumptive|confirmed|differential|ruled_out}`. |
| `MedicationItem` | `{MedicationName 1–120, Presentation?, Dose decimal>0, DoseUnit enum (mg, ml, ui, tablet, capsule, drop, mg_per_kg, ml_per_kg), Route enum (oral, sc, im, iv, topical, ophthalmic, otic, inhaled, rectal), Frequency {Every int, Unit hours|days} o `once`, DurationDays? 1–365, Instructions? ≤ 300}`. |
| `Procedure` | `{Name 1–120, Notes? ≤ 1000}`. |
| `FollowUpRecommendation` | `{DueDate civil, Reason ≤ 200}`. |
| `Attachment` | `{ID, ObjectKey, ContentType ∈ {jpeg,png,webp,pdf}, SizeBytes ≤ 10 MB, Description? ≤ 200, VisibleToGuardian bool}`. |
| `Signature` | `{SignedBy, SignedAt, StreamVersion, ContentHash [32]byte}`. |
| `Addendum` | `{Seq, Text ≤ 5000, Reason enum, SignedBy, SignedAt, Attachments ≤ 5}`. |

### 3.3 Eventos `Encounter` (v1)

**[PP-F2-DOM-005] (MUST)**

| Evento | Payload |
|---|---|
| `EncounterOpened` | `{encounter_id, pet_id, organization_id, appointment_id?, type, reason, started_at, attending_vet_id?}` |
| `VitalsRecorded` | `{encounter_id, vitals{…}}` |
| `EncounterNarrativeUpdated` | `{encounter_id, subjective?, objective?{narrative?, systems?}}` |
| `DiagnosesSet` | `{encounter_id, diagnoses[]}` |
| `PlanSet` | `{encounter_id, plan{medications[], procedures[], recommendations?}}` |
| `FollowUpRecommended` / `FollowUpCleared` | `{encounter_id, due_date, reason}` / `{encounter_id}` |
| `AttendingVetAssigned` | `{encounter_id, user_id}` |
| `AttachmentAdded` / `AttachmentRemoved` / `AttachmentVisibilityChanged` | `{encounter_id, attachment{…}}` / `{encounter_id, attachment_id}` / `{encounter_id, attachment_id, visible, reason?}` |
| `EncounterSigned` | `{encounter_id, signed_by, signed_at, stream_version, content_hash, pet_id, organization_id, appointment_id?}` (desnormaliza ids para consumidores) |
| `EncounterAddendumAdded` | `{encounter_id, seq, text, reason, signed_by, signed_at, attachments[]}` |
| `EncounterDiscarded` | `{encounter_id, reason, by}` |

Ningún evento de `Encounter` contiene PII del tutor; los textos clínicos son datos de salud del animal (ver 08-seguridad).

### 3.4 Agregado `VaccinationCard`

**[PP-F2-DOM-006] (MUST)** Stream `vax-<PetID>` · `stream_type = "VaccinationCard"` · se crea implícitamente con la primera entrada.

```go
type VaccinationCard struct {
    PetID    PetID
    Version  int64
    Species  Species                      // copiada al crear (puerto PetAccess); cambio de especie → evento de ajuste
    Entries  map[CardEntryID]CardEntry
}
type CardEntry struct {
    ID              CardEntryID
    Kind            EntryKind            // vaccine | deworming | ectoparasite
    ProductRef      ProductRef           // {CatalogID?, Name}
    AdministeredAt  civil.Date
    Lot             *Lot
    Manufacturer    *string
    ExpiryDate      *civil.Date
    Route, Site     *string
    DoseLabel       *string
    WeightKg        *decimal
    Source          EntrySource          // recorded | declared | imported
    Verification    VerificationStatus   // verified | unverified | disputed
    RecordedBy      *Actor               // {UserID, OrganizationID}
    EncounterID     *EncounterID
    NextDue         *NextDue             // {Date, Basis protocol|clinic|guardian}
    Status          EntryStatus          // active | voided
    VoidReason      *string
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `RecordEntry{…, Actor(org member)}` | V1 org con acceso a la mascota; V2 `AdministeredAt ≤ hoy` y ≥ nacimiento; V3 sin duplicado activo `(Kind, ProductRef, AdministeredAt)`; `NextDue` calculado por servicio o dado. | `VaccinationEntryRecorded` (kind vaccine) / `PreventiveEntryRecorded` |
| `DeclareEntry{…, Actor(guardian)}` | V2; V3; actor tutor; `Source = declared`, `Verification = unverified`. | `VaccinationEntryDeclared` |
| `VerifyEntry{EntryID, Actor(org)}` / `DisputeEntry{EntryID, Note}` | entrada `declared` activa; V1. | `VaccinationEntryVerified` / `VaccinationEntryDisputed` |
| `VoidEntry{EntryID, Reason}` | V4 entrada activa; V5 actor = misma org que la registró y ≤ 30 d, o `super_admin`, o tutor si `declared`. | `VaccinationEntryVoided` |
| `AdjustNextDue{EntryID, Date, Basis clinic}` | entrada activa; actor org con acceso. | `NextDueAdjusted` |
| `SyncSpecies{Species}` | si cambia en Pet (reacción). | `CardSpeciesSynced` |

Invariantes: **V1** acceso org; **V2** fecha válida; **V3** no duplicados activos; **V4** anulación solo de activas; **V5** ventana/actor de anulación; **V6** ≤ 500 entradas; **V7** `NextDue.Date > AdministeredAt`.

### 3.5 Value Objects carnet

**[PP-F2-DOM-007] (MUST)**

| VO | Reglas |
|---|---|
| `EntryKind` | `vaccine, deworming, ectoparasite`. |
| `ProductRef` | `{CatalogID?, Name 1–120}`; si `CatalogID` existe, `Name` se copia del catálogo. |
| `Lot` | 1–40, `[A-Z0-9-/.]`, upper. |
| `DoseLabel` | ≤ 40 (texto libre: "1.ª dosis", "refuerzo"). |
| `NextDue` | `{Date, Basis protocol|clinic|guardian}`. |
| `EntrySource` / `VerificationStatus` / `EntryStatus` | enums anteriores. |

### 3.6 Eventos `VaccinationCard` (v1)

**[PP-F2-DOM-008] (MUST)**

| Evento | Payload |
|---|---|
| `VaccinationEntryRecorded` | `{pet_id, entry_id, product{catalog_id?, name}, administered_at, lot?, manufacturer?, expiry_date?, route?, site?, dose_label?, weight_kg?, recorded_by{user_id, organization_id}, encounter_id?, next_due?{date, basis}, source}` |
| `PreventiveEntryRecorded` | igual + `kind` |
| `VaccinationEntryDeclared` | `{pet_id, entry_id, kind, product, administered_at, lot?, dose_label?, next_due?, declared_by, evidence_object_key?}` |
| `VaccinationEntryVerified` / `VaccinationEntryDisputed` | `{pet_id, entry_id, by{user_id, organization_id}, note?}` |
| `VaccinationEntryVoided` | `{pet_id, entry_id, reason, by}` |
| `NextDueAdjusted` | `{pet_id, entry_id, date, basis, by}` |
| `CardSpeciesSynced` | `{pet_id, species}` |

### 3.7 Catálogos y servicio `NextDueCalculator`

**[PP-F2-DOM-009] (MUST)** Datos de referencia CRUD (no ES), mantenidos por SuperAdmin (API-005), con auditoría:

- `VaccineCatalog{ID, Name, Species[], Diseases[], Type core|non_core, Routes[], Active}`.
- `VaccinationProtocol{ID, Country, Species, VaccineID, InitialSeries []{MinAgeWeeks, MinIntervalDays}, BoosterIntervalMonths, LegalValidityMonths?, Notes}`.
- `PreventiveProtocol{Country, Species, Kind, IntervalDays by weight/age bands}` (SHOULD).

Servicio de dominio `NextDueCalculator.Propose(card, newEntry, pet.BirthDate, protocol) NextDue`:
1. Cuenta dosis activas previas del mismo `VaccineID` (recorded/verified; declared cuentan con `Basis=guardian`).
2. Si faltan dosis de la serie inicial → siguiente = `max(AdministeredAt + MinIntervalDays, BirthDate + MinAgeWeeks(k+1))`.
3. Si serie completa → `AdministeredAt + BoosterIntervalMonths`.
4. Sin protocolo → `nil` (la clínica indica manualmente).
Pruebas por tabla con los protocolos CO.

### 3.8 `RecordShare` (CRUD)

**[PP-F2-DOM-016] (SHOULD)** `{ID, PetID, Scope vaccination_card|full_record, TokenHash, PinHash?, ExpiresAt, CreatedBy, RevokedAt?, AccessCount, LastAccessAt}`; invariantes: ≤ 10 activos por mascota; caducidad 1–90 días; solo el tutor crea/revoca. Auditado.

## 4. Scheduling

### 4.1 Agregado `Appointment`

**[PP-F2-DOM-010] (MUST)** Stream `appt-<AppointmentID>`.

```go
type Appointment struct {
    ID, Version; PetID; OrganizationID
    ProfessionalID  *UserID
    Type            AppointmentType       // consultation | follow_up | vaccination | surgery | procedure | other
    Origin          Origin                // guardian | clinic
    Status          AppointmentStatus     // requested | proposed | confirmed | checked_in | completed | cancelled | no_show | rejected
    Reason          Reason
    Preferences     []SlotPreference      // ≤ 3, solo en requested
    Slot            *Slot                 // {Start, End} en UTC + TZ de la org
    EncounterID     *EncounterID
    CancellationPolicyHours int           // copiado de la agenda/org al confirmar
    History         []Transition          // para UI; derivable
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `RequestAppointment{PetID, OrgID, Type, Reason, Preferences}` | A1 tutor con mascota y vínculo activo a org `active`; preferencias dentro de [antelación mín, máx]; A2 ≤ 3 solicitudes `requested` por mascota/org. | `AppointmentRequested` |
| `CreateAppointment{PetID, OrgID, ProfessionalID, Type, Reason, Slot}` (clínica) | A3 org con acceso; A4 profesional `veterinarian` activo; A5 slot dentro de disponibilidad (consulta) y **reserva** obtenida (DAT-004). | `AppointmentCreated` + `AppointmentConfirmed` |
| `ConfirmAppointment{ProfessionalID, Slot}` | status requested/proposed-accepted; A4; A5. | `AppointmentConfirmed` |
| `ProposeAlternative{ProfessionalID, Slot}` | status requested; A5 reserva **provisional** 48 h. | `AppointmentAlternativeProposed` |
| `AcceptProposal{}` (tutor) | status proposed; no caducada. | `AppointmentConfirmed` |
| `RejectRequest{Reason}` | status requested/proposed. | `AppointmentRejected` |
| `Reschedule{Slot, ProfessionalID?}` | status confirmed; A5 (libera reserva anterior). | `AppointmentRescheduled` |
| `Cancel{Reason, By guardian|clinic}` | status ∈ {requested, proposed, confirmed}; marca `late = true` si tutor y < policy hours. | `AppointmentCancelled{late}` |
| `CheckIn{}` | status confirmed; A6 hoy (TZ org) y ≥ Start − 2 h. | `AppointmentCheckedIn` |
| `MarkNoShow{}` | status confirmed y ahora > End. | `AppointmentNoShow` |
| `Complete{EncounterID?}` | status checked_in (o confirmed con EncounterID por reacción). | `AppointmentCompleted` |

Invariantes: **A1** vínculo; **A2** límite de solicitudes; **A3** acceso org; **A4** profesional válido; **A5** sin solapamiento (restricción de exclusión, ADR-011); **A6** ventana de check-in; **A7** `Slot.End > Slot.Start`, duración 5–480 min; **A8** transiciones solo las de la tabla.

### 4.2 Agregado `ProfessionalAgenda`

**[PP-F2-DOM-011] (MUST)** Stream `agenda-<OrgID>-<UserID>` (ID compuesto determinista).

```go
type ProfessionalAgenda struct {
    OrganizationID; ProfessionalID; Version
    TimeZone        string                       // IANA, de la organización
    WeeklyTemplate  map[Weekday][]TimeRange      // ≤ 6 rangos/día, sin solapar
    SlotMinutes     map[AppointmentType]int      // por defecto 30
    Exceptions      map[civil.Date]DayException  // {Closed bool, Ranges []TimeRange}
    LeadTimeMin     time.Duration                // antelación mínima para tutores (default 2h)
    LeadTimeMax     time.Duration                // default 60 días
    CancellationPolicyHours int                  // default 4
    Active          bool
}
```

Comandos: `DefineAgenda`, `UpdateWeeklyTemplate`, `SetSlotDurations`, `AddException`/`RemoveException`, `SetPolicies`, `DeactivateAgenda`. Eventos correspondientes `Agenda*`. Invariantes: rangos válidos y no solapados; `SlotMinutes` ∈ [5, 480]; profesional miembro activo `veterinarian` (puerto). Snapshots cada 100 eventos (ARQ-004).

**Disponibilidad** (consulta, no comando): `Availability(org, professional?, from, to, type)` = plantilla − excepciones − reservas activas, discretizada por `SlotMinutes[type]`.

### 4.3 Eventos Scheduling (v1)

**[PP-F2-DOM-012] (MUST)** `AppointmentRequested{appointment_id, pet_id, organization_id, type, reason, preferences[], requested_by}`, `AppointmentCreated{…, created_by}`, `AppointmentConfirmed{appointment_id, pet_id, organization_id, professional_id, slot{start,end}, tz, cancellation_policy_hours}`, `AppointmentAlternativeProposed{…, expires_at}`, `AppointmentRejected{reason}`, `AppointmentRescheduled{previous_slot, slot, professional_id}`, `AppointmentCancelled{reason, by, late}`, `AppointmentCheckedIn{at}`, `AppointmentNoShow{}`, `AppointmentCompleted{encounter_id?}`; `AgendaDefined`, `AgendaWeeklyTemplateUpdated`, `AgendaSlotDurationsSet`, `AgendaExceptionAdded`, `AgendaExceptionRemoved`, `AgendaPoliciesSet`, `AgendaDeactivated`.

## 5. Pet Registry (ampliación)

**[PP-F2-DOM-013] (MUST)** Nuevos elementos en `Pet`:

- `CareLinkRequests map[RequestID]CareLinkRequest{OrgID, RequestedBy, Message ≤ 300, ExpiresAt, Status pending|approved|declined|expired}`.
- Comandos: `RequestCareLink{OrgID, RequestedBy, Message}` (P1 org clínica activa; P2 sin vínculo activo ni solicitud pendiente con esa org) → `CareLinkRequested`; `ApproveCareLinkRequest{RequestID}` (tutor) → `CareLinkRequestApproved` + `CareLinkGranted{requested_by}`; `DeclineCareLinkRequest{RequestID, Reason?}` → `CareLinkRequestDeclined`; expiración por reacción programada (`CareLinkRequestExpired`, 14 d).
- Perfil ampliado (SHOULD): `Alerts []Alert{Kind allergy|chronic_condition|behavior|other, Text ≤ 200}` editable por tutor y por clínica vinculada → `PetAlertsUpdated`.
- (COULD, PRD-021) `SecondaryGuardians map[UserID]{InvitedAt, AcceptedAt}` con `SecondaryGuardianInvited/Accepted/Removed`.

Búsqueda de mascota para solicitud (consulta `LookupPetForRequest{microchip | guardian_email}`): devuelve `{pet_id, name, species, guardian_display_name_masked}`; sin resultados devuelve lista vacía siempre con el mismo tiempo de respuesta (padding) y cuenta para el rate limit.

## 6. Puertos F2

**[PP-F2-DOM-014] (MUST)**

| Puerto | Consumidor | Implementación |
|---|---|---|
| `PetAccess{CanOrgAccess(pet, org) AccessLevel{none, linked, custody}; GuardianOf(pet); PetSummary(pet)}` | Clinical, Scheduling | sobre `read_petregistry` + `read_clinical.encounter_custody`. |
| `Membership{RoleOf(user, org) (OrgRole, active)}` | Clinical, Scheduling | sobre `read_partnernetwork`. |
| `Catalog{Vaccine(id); Protocol(country, species, vaccine)}` | Clinical | tablas de referencia. |
| `SlotReservations{Reserve(tx, professional, slot, appointmentID) error; Release(tx, appointmentID)}` | Scheduling | tabla con exclusión (DAT-004). |
| `DocumentRenderer{EncounterPDF, CardPDF}` | Clinical | ARQ-010. |
| `Clock`, `IDGen`, `TokenGen` | todos | F1. |

## 7. Políticas y process managers

**[PP-F2-DOM-015] (MUST)**

| Disparador | Reacción | Dónde |
|---|---|---|
| `AppointmentCheckedIn` | Habilita "Iniciar consulta" (solo read model; no comando). | proyección síncrona |
| `EncounterOpened{appointment_id}` | — (la cita queda enlazada en read model). | proyección |
| `EncounterSigned{appointment_id}` | `Appointment.Complete{EncounterID}` | process manager `scheduling.complete-on-sign` (outbox) |
| `EncounterSigned` con `vitals.weight_kg` | `weight_history` | proyección async |
| `FollowUpRecommended` / `FollowUpCleared` / `EncounterSigned` | crea/cancela recordatorios T-3d, T-0 (solo si firmado) | Notifications |
| `VaccinationEntryRecorded/Declared{next_due}` / `NextDueAdjusted` / `VaccinationEntryVoided` | crea/actualiza/cancela recordatorios T-14d, T-0; proyección `vaccination_due` | Notifications + proyección async |
| `AppointmentConfirmed/Rescheduled/Cancelled` | recordatorios T-24h, T-2h; emails inmediatos | Notifications |
| `CareLinkRequested` | email + bandeja del tutor; programa expiración 14 d → comando `ExpireCareLinkRequest` | Notifications + scheduler de comandos (ARQ-009) |
| `AppointmentAlternativeProposed` | expiración 48 h → libera reserva provisional (`ExpireProposal`) | scheduler de comandos |
| `AppointmentConfirmed` y `End` pasado sin check-in (24 h) | `MarkNoShow` automático (configurable por org) | scheduler de comandos |
| `PetProfileUpdated{species}` | `VaccinationCard.SyncSpecies` | process manager |
| `OrganizationSuspended` | citas confirmadas futuras → aviso a tutores (no se cancelan automáticamente) | Notifications |

## 8. Decisiones de modelado explícitas

- `Encounter` no referencia a `VaccinationCard`: registrar una vacuna durante una consulta son **dos comandos** (uno por agregado) orquestados por la UI/API (`POST /encounters/{id}/vaccinations` ejecuta `RecordEntry` con `encounter_id` y, si el encuentro está en borrador, añade un `Procedure` "Vacunación: X"); no hay transacción que abarque ambos streams.
- `FollowUpRecommendation` vive en `Encounter` (es parte del plan firmado); la cita derivada es un `Appointment` independiente que lo referencia (`follow_up_of_encounter_id` en `RequestAppointment`, COULD).
- Las citas de tutor no bloquean agenda (ADR-011).
- Los catálogos son datos de referencia versionados por auditoría, no agregados ES: cambiar un protocolo **no** recalcula `next_due` ya emitidos (se respeta lo propuesto en su momento).
