# F4 · 02 — Modelo de dominio (DDD)

## 1. Lenguaje ubicuo (ampliación)

**[PP-F4-DOM-001] (MUST)**

| Término | Código | Definición |
|---|---|---|
| Receta | `Prescription` | Documento dispensable con ítems, validez y dispensaciones. Raíz de agregado. |
| Receta emitida / externa | `issued` / `external` | Origen: firmada por veterinario en PetPassport / subida por el tutor. |
| Ítem de receta | `PrescriptionItem` | Producto (catálogo o texto), posología, cantidad a dispensar, sustitución permitida. |
| Credencial profesional | `ProfessionalCredential` | Registro profesional del veterinario, verificado por SuperAdmin. |
| Extracción | `Extraction` | Resultado estructurado del análisis de la imagen de una receta externa, con confianzas. |
| Confirmación | `ExtractionConfirmed` | Acto del tutor que fija los ítems de una receta externa. |
| Validación farmacéutica | `PharmacistValidation` | Juicio de un farmacéutico sobre una receta externa. |
| Dispensación | `Dispensation` | Entrega de cantidades de ítems por una farmacia contra la receta. |
| Solicitud de cotización | `QuoteRequest` | Saga que invita a farmacias a cotizar ítems de una receta. Raíz de agregado. |
| Cotización | `Quote` | Respuesta de una farmacia: precios, disponibilidad, sustituciones, envío, validez. Entidad en la saga. |
| Sustitución | `Substitution` | Producto equivalente propuesto por la farmacia para un ítem. |
| Pedido | `Order` | Aceptación de una cotización; ciclo de preparación y entrega. Raíz de agregado. |
| Partner | `Partner` | Organización `pharmacy` con integración activa (`PartnerIntegration`). |
| Nivel de integración | `IntegrationMode` | `portal` \| `api` \| `adapter`. |
| Mapeo de producto | `ProductMapping` | Correspondencia `catalog_id` ↔ SKU del partner. |
| Regla regulatoria | `RegulatoryRule` | Parámetros legales por país (validez, controladas, refills). |

## 2. Mapa de contextos F4

**[PP-F4-DOM-002] (MUST)**

```
Clinical (Encounter.Plan) ──items──► ┌────────────────────┐     ┌──────────────────────────┐
Partner Network (Credential) ──────► │ Prescriptions      │────►│ Pharmacy Orders           │
Medication Catalog ─────────────────►│ Prescription (ES)  │◄────│ QuoteRequest (ES, saga)   │
OCR (DocumentExtractor, puerto) ────►│                    │ disp│ Order (ES)                │
                                     └────────────────────┘     └───────────┬──────────────┘
                                                                            │ PartnerGateway (puerto)
                                                                ┌───────────▼──────────────┐
Partner Network (Organization pharmacy) ───────────────────────►│ Partner Integration       │
Identity (client credentials, puerto) ─────────────────────────►│ PartnerIntegration (CRUD) │
                                                                │ adapters · webhooks · API │
                                                                └──────────────────────────┘
Notifications ◄── eventos de los tres ── Search (índice prescriptions, COULD)
```

| Relación | Mecanismo |
|---|---|
| Clinical → Prescriptions | Puerto `EncounterPlanReader{SignedPlanItems(encounterID)}`; el encuentro no cambia. |
| Partner Network → Prescriptions | Puerto `CredentialCheck{VerifiedCredential(userID, country)}`. |
| Prescriptions ↔ Pharmacy Orders | `QuoteRequest` lee ítems por puerto `PrescriptionReader`; `OrderDelivered{dispensation}` → PM → `Prescription.RecordDispensation`. |
| Pharmacy Orders → Partner Integration | Puerto `PartnerGateway`; respuestas vuelven como comandos (`RecordQuote`, `UpdateOrderStatus`). |
| OCR → Prescriptions | Puerto `DocumentExtractor`; resultado como comando `RecordExtraction`. |

## 3. Partner Network: credencial profesional

**[PP-F4-DOM-003] (MUST)** Entidad en `Organization`? No: la credencial es de la **persona**, no de la organización → se modela en Identity & Access como CRUD auditado (`identity.professional_credentials`): `{UserID, Country, Kind veterinarian, RegistryNumber, Issuer, IssuedAt, ExpiresAt?, EvidenceObjectKey, Status pending|verified|rejected|expired, ReviewedBy, Reason}`; invariantes: una `verified` por (usuario, país); número único por país; expirada → no emite. Puerto `CredentialCheck` expone `Verified(userID, country) (CredentialRef, bool)`.

## 4. Prescriptions

### 4.1 Agregado `Prescription`

**[PP-F4-DOM-004] (MUST)** Stream `rx-<PrescriptionID>`.

```go
type Prescription struct {
    ID, Version
    PetID          PetID
    GuardianID     UserID
    Source         Source                 // issued | external
    Status         Status                 // draft | issued | confirmed(external) | revoked | expired | exhausted
    // issued
    IssuerID       *UserID; IssuerOrgID *OrganizationID; CredentialRef *CredentialRef; EncounterID *EncounterID
    IssuedAt       *time.Time; Signature *Signature (hash, stream_version)
    // external
    Images         []ImageRef             // ≤ 5
    Extraction     *Extraction            // último resultado (ver 4.3)
    Confirmation   *Confirmation          // {ConfirmedAt, Diff}
    Validation     *PharmacistValidation  // {By, OrgID, Status validated|rejected, Note, At}
    ExternalMeta   *ExternalMeta          // {VetName?, ClinicName?, IssuedOn? (declarados)}
    // común
    Items          []PrescriptionItem     // ≤ 20
    Validity       Validity               // {ValidUntil, RefillsAllowed}
    RefillsUsed    int
    Dispensations  []Dispensation         // {OrderID, PharmacyOrgID, At, Lines[{ItemID, Qty}]}
    Country        CountryCode
    Notes          *string                // para la farmacia, ≤ 500
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `DraftIssuedPrescription{PetID, IssuerID, OrgID, EncounterID?, Items, Validity?, Notes?}` | R1 issuer `veterinarian` con credencial verificada en `Country`; R2 org con acceso a la mascota; R3 si `EncounterID`, encuentro firmado de la misma org/mascota; ítems válidos (R5). | `PrescriptionDrafted` |
| `UpdateDraft{Items?, Validity?, Notes?}` | status draft. | `PrescriptionDraftUpdated` |
| `IssuePrescription{SignerID}` | draft; R1; `SignerID == IssuerID`; R4 `Validity` dentro de reglas del país; ≥ 1 ítem. | `PrescriptionIssued{hash, …}` |
| `RevokePrescription{Reason, By}` | issued/confirmed; R6 sin dispensaciones (o `super_admin`). | `PrescriptionRevoked` |
| `UploadExternalPrescription{PetID, GuardianID, Images, Country}` | R2' actor es tutor de la mascota; ≤ 5 imágenes. | `ExternalPrescriptionUploaded` |
| `RecordExtraction{Extraction}` | source external; status draft. | `PrescriptionExtractionRecorded` |
| `RequestReupload{Reason}` | external draft (legibilidad baja). | `PrescriptionReuploadRequested` |
| `ConfirmExternalItems{Items, Validity, ExternalMeta}` | external draft; actor tutor; R5; R4; sin cotización abierta (puerto). | `ExternalPrescriptionConfirmed{diff}` |
| `ReopenExternal{}` | confirmed; sin cotización abierta ni dispensación. | `ExternalPrescriptionReopened` |
| `ValidateByPharmacist{By, OrgID, Status, Note}` | external confirmed; actor `pharmacist` de org partner activa. | `PrescriptionPharmacistValidated` |
| `RecordDispensation{OrderID, PharmacyOrgID, Lines}` | issued/confirmed y no expired; R7 cantidades ≤ pendientes; R8 refills. | `PrescriptionDispensed` (+ `PrescriptionExhausted` si procede) |
| `ExpirePrescription{}` | `now > ValidUntil` (scheduler). | `PrescriptionExpired` |

Invariantes: **R1** credencial verificada; **R2** acceso de la org / **R2'** tutor; **R3** encuentro coherente; **R4** validez `≤ max_validity_days(country)`, refills `≤ max_refills(country)`; **R5** ítems: `catalog_id` activo o `free_text` con nombre ≥ 3; cantidad > 0; posología válida; sin duplicados de `catalog_id`; **R6** revocación solo sin dispensación; **R7** no dispensar más de lo recetado (por refill); **R8** `RefillsUsed ≤ RefillsAllowed`; **R9** inmutabilidad tras `issued`/`confirmed` salvo dispensaciones, validación, revocación y expiración.

### 4.2 Value Objects

**[PP-F4-DOM-005] (MUST)**

| VO | Reglas |
|---|---|
| `PrescriptionItem` | `{ItemID, CatalogID?, FreeText?, NameSnapshot, Dose{Value, Unit}, Route, Frequency, DurationDays?, QuantityToDispense{Value, Unit pack|unit|ml|g}, SubstitutionAllowed bool, Instructions ≤ 300, Controlled bool (snapshot del catálogo), ExtractionRef? {ItemIndex, Confidence}}` |
| `Validity` | `{ValidUntil civil.Date, RefillsAllowed 0–5}`; defaults por país. |
| `Signature` | reutiliza F2 (`SignedBy, SignedAt, StreamVersion, ContentHash`). |
| `Dispensation` | `{OrderID, PharmacyOrgID, At, Lines[{ItemID, Qty}], Partial bool}`. |
| `ImageRef` | `{ObjectKey, ContentType, Page, Width, Height, Sha256}`. |
| `ExternalMeta` | `{VetName?, VetRegistry?, ClinicName?, IssuedOn?}` (texto declarado/extraído; no verificado). |
| `CredentialRef` | `{Country, RegistryNumber, VerifiedAt}` (snapshot). |

### 4.3 Extracción (entidad dentro de `Prescription` externa)

**[PP-F4-DOM-007] (MUST)**

```go
type Extraction struct {
    Version       int                   // intento
    ExtractorID   string                // "claude:claude-opus-5-5:prompt-v3"
    Legibility    float64               // 0–1
    Warnings      []string
    Header        ExtractedHeader       // {VetName?, Registry?, Clinic?, Date?, PetName?, Species?} con Confidence cada uno
    Items         []ExtractedItem       // {Index, RawText, Product{Text, Candidates[{CatalogID, Score}]}, Dose, Frequency, Duration, Quantity, Confidence por campo, BBox?}
    RawText       *string               // OCR clásico de apoyo
    CostUSD       float64; LatencyMs int
}
```

`Confirmation.Diff` registra por ítem: `accepted | edited{fields} | removed | added` → base de métricas (OCR-010).

### 4.4 Eventos `Prescription` v1

**[PP-F4-DOM-006] (MUST)** `PrescriptionDrafted{prescription_id, pet_id, guardian_id, source, issuer_id?, issuer_org_id?, encounter_id?, country, items[], validity, notes?}`, `PrescriptionDraftUpdated{changes}`, `PrescriptionIssued{issuer_id, credential{…}, issued_at, valid_until, refills_allowed, items[], stream_version, content_hash}`, `PrescriptionRevoked{reason, by}`, `ExternalPrescriptionUploaded{images[], country}`, `PrescriptionExtractionRecorded{extraction{…}}`, `PrescriptionReuploadRequested{reason}`, `ExternalPrescriptionConfirmed{items[], validity, external_meta, diff}`, `ExternalPrescriptionReopened{}`, `PrescriptionPharmacistValidated{by, org_id, status, note}`, `PrescriptionDispensed{order_id, pharmacy_org_id, lines[], partial, refills_used}`, `PrescriptionExhausted{}`, `PrescriptionExpired{}`. PII (nombre del tutor) no viaja en eventos; `external_meta` contiene nombres de terceros (vet/clínica) marcados `pii`.

## 5. Pharmacy Orders

### 5.1 Agregado `QuoteRequest` (saga)

**[PP-F4-DOM-008] (MUST)** Stream `qr-<id>`.

```go
type QuoteRequest struct {
    ID, Version; PrescriptionID; PetID; GuardianID; Country
    Items        []RequestedItem        // subconjunto de PrescriptionItem (no controlados), con Qty
    Mode         FulfillmentMode        // pickup | delivery
    DeliveryArea *Area                  // ciudad/zona (sin dirección exacta)
    Invited      map[OrganizationID]Invitation // {Status pending|quoted|declined|failed|expired, SentAt, Channel portal|api|adapter, Attempts}
    Quotes       map[QuoteID]Quote      // {OrgID, Lines[{ItemID, UnitPrice, Qty, Available full|partial|none, Substitution?}], DeliveryFee?, ETA?, PickupAddressRef?, ValidUntil, Notes, ReceivedAt}
    Status       Status                 // open | selected | closed
    SelectedQuoteID *QuoteID; OrderID *OrderID
    ExpiresAt    time.Time; ClosedReason *string
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `RequestQuotes{PrescriptionID, Items, Mode, Area, OrgIDs}` | Q1 actor tutor de la receta; Q2 receta vigente (`issued`/`confirmed`, no expired/exhausted/revoked); Q3 ítems ⊆ receta y no controlados (puerto `RegulatoryRules`); Q4 1–10 orgs `pharmacy` partner activas que soporten `Mode` en `Area`; Q5 ≤ 3 abiertas por receta, ≤ 10/día. | `QuoteRequested` |
| `MarkInvitationSent{OrgID, Channel}` / `MarkInvitationFailed{OrgID, Error}` | open. | `QuoteInvitationSent` / `QuoteInvitationFailed` |
| `RecordQuote{OrgID, Quote}` | open; org invitada; Q6 una cotización por org (reemplaza si `ReceivedAt` < `ExpiresAt`); Q7 sustituciones válidas (puerto `CatalogEquivalence`) y solo en ítems con `SubstitutionAllowed`; precios ≥ 0. | `QuoteReceived` |
| `DeclineQuote{OrgID, Reason}` | open; invitada. | `QuoteDeclined` |
| `SelectQuote{QuoteID, SubstitutionDecisions}` | open; Q8 cotización vigente (`ValidUntil`); decisiones para todas las sustituciones propuestas. | `QuoteSelected` |
| `CancelQuoteRequest{Reason}` | open; actor tutor. | `QuoteRequestCancelled` |
| `ExpireQuoteRequest{}` | open y `now > ExpiresAt` (scheduler); si hay cotizaciones, pasa a `open` con `expired_invitations` y sigue seleccionable 24 h más; si no, `closed`. | `QuoteRequestExpired` |
| `CloseQuoteRequest{Reason}` | tras `OrderPlaced`, revocación de receta, etc. | `QuoteRequestClosed` |

### 5.2 Agregado `Order`

**[PP-F4-DOM-009] (MUST)** Stream `ord-<id>`.

```go
type Order struct {
    ID, Version; QuoteRequestID; QuoteID; PrescriptionID; PetID; GuardianID; PharmacyOrgID
    Mode         FulfillmentMode; DeliveryAddress *Address (solo delivery); ContactPhone PhoneE164
    Lines        []OrderLine            // {ItemID, CatalogID|FreeText, Qty, UnitPrice, Substituted *CatalogID}
    Totals       Totals                 // {Subtotal, DeliveryFee, Total, Currency}
    Status       Status                 // placed | confirmed | preparing | ready | out_for_delivery | delivered | cancelled | rejected
    Timeline     []Transition
    Dispensation *DispensationRecord    // al entregar
    PartnerRef   *string                // id del pedido en el sistema del partner
}
```

Transiciones: `placed → confirmed|rejected`; `confirmed → preparing → ready (pickup) | out_for_delivery (delivery) → delivered`; `placed|confirmed|preparing → cancelled` (tutor hasta `preparing` inclusive solo si la farmacia no marcó "preparando" hace > 1 h; farmacia siempre con motivo). Invariantes: **O1** una cotización → un pedido; **O2** `Mode` coherente con la cotización; **O3** dirección obligatoria en delivery; **O4** transiciones de la tabla; **O5** `delivered` exige `DispensationRecord` con líneas ≤ `Lines`; **O6** cancelación por tutor limitada; **O7** `rejected` libera la `QuoteRequest` (reacción); **O8** totales recalculados en el agregado, no confiados al cliente.

### 5.3 Eventos Pharmacy Orders v1

**[PP-F4-DOM-010] (MUST)** `QuoteRequested{quote_request_id, prescription_id, pet_id, guardian_id, items[], mode, area, invited_org_ids[], expires_at}`, `QuoteInvitationSent/Failed`, `QuoteReceived{org_id, quote{…}}`, `QuoteDeclined`, `QuoteSelected{quote_id, substitution_decisions[]}`, `QuoteRequestCancelled/Expired/Closed`; `OrderPlaced{order_id, quote_request_id, quote_id, pharmacy_org_id, mode, lines[], totals, pii{delivery_address?, contact_phone}}`, `OrderConfirmed{partner_ref?, eta?}`, `OrderRejected{reason}`, `OrderPreparing`, `OrderReady`, `OrderOutForDelivery{eta?}`, `OrderDelivered{dispensation{lines[]}}`, `OrderCancelled{by, reason}`.

## 6. Partner Integration

**[PP-F4-DOM-011] (MUST)** CRUD auditado (no ES):

- `PartnerIntegration{OrganizationID (pharmacy), Mode portal|api|adapter, AdapterSlug?, Status active|suspended, KeycloakClientID?, WebhookEndpoint?{URL, SecretRefs[2], Events[], Active}, APIKeyLegacy?, Capabilities{pickup, delivery, substitutions}, ServiceAreas[], Hours, AutoDeclineOutsideArea bool, SLA{avg_response_minutes, quote_rate, orders_completed}, CreatedAt}`.
- `ProductMapping{OrganizationID, CatalogID, PartnerSKU, PartnerName, BasePrice?, Active}` — único por (org, catalog_id).
- `WebhookDelivery{ID, OrganizationID, Event, PayloadHash, Attempts, Status, LastError, NextAttemptAt}`.
- `PartnerCall{ID, OrganizationID, Direction in|out, Endpoint, Status, LatencyMs, PayloadDigest, CorrelationID, At}` (auditoría RN-31).

**[PP-F4-DOM-012] (MUST)** `ref.regulatory_rules{Country, DefaultValidityDays, MaxValidityDays, MaxRefills, ControlledOrderable bool (false), RequiresCredentialVerification bool, Notes}`; semilla `CO` (30 / 180 / 5 / false / true). SuperAdmin edita con auditoría.

## 7. Puertos F4

**[PP-F4-DOM-013] (MUST)**

| Puerto | Consumidor | Implementación |
|---|---|---|
| `CredentialCheck` | Prescriptions | sobre `identity.professional_credentials`. |
| `EncounterPlanReader` | Prescriptions | sobre `read_clinical.encounters` (solo firmados). |
| `RegulatoryRules{For(country)}` | Prescriptions, Pharmacy Orders | `ref.regulatory_rules` (caché). |
| `CatalogEquivalence{AreEquivalent(a, b) bool; IsControlled(id)}` | Pharmacy Orders, Prescriptions | sobre `ref.medications` (principio activo + concentración + forma). |
| `PrescriptionReader{Items, Status}` | Pharmacy Orders | read model. |
| `OpenQuoteRequests(prescriptionID)` | Prescriptions | read model de órdenes. |
| `DocumentExtractor{Extract(ctx, images, hints) (Extraction, error)}` | OCR → Prescriptions | adaptadores `claude`, `tesseract`, `none` (07). |
| `CatalogMatcher{Candidates(text, species) []Candidate}` | OCR | OpenSearch `medications` (F3). |
| `PartnerGateway{SendQuoteRequest, NotifyOrder, CancelOrder}` | Pharmacy Orders | `portal` (solo read model + notificación), `api` (webhook/pull), `adapter` (cliente HTTP del partner). |
| `PartnerDirectory{EligiblePharmacies(area, mode)}` | Pharmacy Orders | `partner_integrations` + directorio F3. |

## 8. Process managers y políticas

**[PP-F4-DOM-014] (MUST)**

| Disparador | Reacción |
|---|---|
| `PrescriptionIssued` | render PDF (F2 infra); notificación tutor; (COULD) índice `prescriptions`. |
| `ExternalPrescriptionUploaded` | encola `ocr:extract` (07). |
| extracción terminada | `RecordExtraction` o `RequestReupload`; notificación "lista para revisar". |
| `QuoteRequested` | por org invitada: `PartnerGateway.SendQuoteRequest` (asynq `partner:out`) → `MarkInvitationSent/Failed`; programa `ExpireQuoteRequest` y recordatorio a farmacias a mitad de plazo. |
| respuesta del partner (portal/API/adapter) | `RecordQuote` / `DeclineQuote`. |
| `QuoteReceived` | notificación tutor (agrupada 15 min); invalida caché del tablero. |
| `QuoteSelected` | `Order.Place` (comando) → `OrderPlaced` → `PartnerGateway.NotifyOrder` + `CloseQuoteRequest`. |
| `OrderRejected` / `OrderCancelled{by: pharmacy}` | `QuoteRequest.Reopen` (si cotizaciones vigentes) + notificación tutor. |
| `OrderDelivered` | `Prescription.RecordDispensation`; notificación tutor; SLA stats. |
| `PrescriptionRevoked` | cierra `QuoteRequest` abiertas; cancela pedidos `placed` (no confirmados) con motivo; notifica farmacia. |
| `PrescriptionDispensed` con sustitución y `source = issued` | notificación al veterinario emisor. |
| `OrganizationSuspended` (pharmacy) | invitaciones pendientes → `failed`; pedidos activos: aviso a tutores. |
| diario | `ExpirePrescription` para vencidas; limpieza de imágenes (OCR-009). |

**[PP-F4-DOM-015] (SHOULD)** SLA por partner: proyección `partner_sla` desde `QuoteInvitationSent`/`QuoteReceived`/`QuoteDeclined`/`Order*` (ventana móvil 90 días): tiempo medio y p50 de respuesta, tasa de respuesta, tasa de adjudicación, pedidos completados/rechazados, valoración media (PRD-022).

## 9. Decisiones explícitas

- `Quote` vive dentro de `QuoteRequest` (saga) y no es agregado: su ciclo de vida termina con la saga.
- `Order` es agregado propio porque sobrevive a la saga y tiene su ciclo con la farmacia.
- La dirección del tutor entra en el dominio solo en `OrderPlaced` (sub-objeto `pii`), nunca antes.
- Las credenciales profesionales son CRUD en Identity (PII, derecho de supresión) y no eventos.
- Los adaptadores de partners no emiten eventos de dominio: traducen a comandos.
