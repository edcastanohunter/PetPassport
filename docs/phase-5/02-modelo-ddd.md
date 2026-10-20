# F5 · 02 — Modelo de dominio (DDD)

## 1. Lenguaje ubicuo (ampliación)

**[PP-F5-DOM-001] (MUST)**

| Término | Código | Definición |
|---|---|---|
| Cuenta de facturación | `BillingAccount` | Titular fiscal y de pago; agrupa 1..n organizaciones. Raíz de agregado. |
| Suscripción | `Subscription` | Plan contratado por una cuenta con asientos/volumen, ciclo y estado. Raíz de agregado. |
| Plan | `Plan` | Oferta comercial versionada (precio, base de cobro, límites, capacidades). Datos de referencia. |
| Asiento | `Seat` | Unidad de cobro por profesional. |
| Cuota | `Quota` | Límite de volumen por periodo (p. ej. cotizaciones/mes). |
| Capacidad | `Feature` | Función activable por plan (API partner, cohortes, SMS). |
| Entitlements | `EntitlementSet` | Conjunto efectivo de límites y capacidades de una organización. |
| Cargo / Pago / Factura | `Charge` / `Payment` / `Invoice` | Intento de cobro / cobro confirmado / documento fiscal. |
| Dunning | `Dunning` | Proceso de reintentos y avisos tras un pago fallido. |
| Gracia / Restringida | `grace` / `restricted` | Estados de suscripción tras impago. |
| Medidor de uso | `UsageCounter` | Contador por organización, cuota y periodo, derivado de eventos. |
| Clave de sujeto | `SubjectKey` | DEK por persona para cifrar su PII en eventos. |
| Solicitud de derechos | `DataSubjectRequest` | Proceso de exportación, supresión o rectificación. |
| Ticket | `SupportTicket` | Conversación de soporte con SLA. |
| Impersonación | `ImpersonationSession` | Sesión de soporte autorizada por el usuario. |
| Canal | `Channel` | `email`, `push`, `sms`, `whatsapp`. |

## 2. Mapa de contextos F5

**[PP-F5-DOM-002] (MUST)**

```
Partner Network (Organization) ──► Billing ◄── Identity (Principal, billing_admin)
   ▲ Entitlements (puerto)           │  PaymentProvider · InvoicingProvider (puertos)
   │                                  ▼
Clinical · Scheduling · Pharmacy Orders · Partners  ── eventos ──► Billing.UsageCounters
Identity ──► Privacy (SubjectKeys, DSR) ──► todos los read models · Search · Storage · Keycloak
Identity ──► Support (tickets, impersonación) ──► Notifications
Notifications ── + canales sms/whatsapp ── MessagingProvider (puerto)
```

- *Billing* es **generic** pero con reglas propias (prorrateo, dunning): upstream de todos a través del puerto `Entitlements`; downstream de los eventos de negocio para medir uso.
- *Privacy* es transversal: no posee datos de negocio; orquesta cifrado y borrado en los demás contextos por puertos (`PIIEraser` implementado por cada contexto).
- *Support* es generic con integración ligera (tickets pueden ser un servicio externo; la impersonación es propia).

## 3. Billing

### 3.1 Agregado `BillingAccount`

**[PP-F5-DOM-003] (MUST)** Stream `bacct-<id>`.

```go
type BillingAccount struct {
    ID, Version
    OwnerUserID     UserID                 // billing_admin principal
    LegalEntity     LegalEntity            // {Kind company|person, LegalName, TaxID{Country, Value, Kind NIT|CC|...}, Address, Email (facturación), Phone?}
    Country         CountryCode; Currency Currency
    Organizations   map[OrganizationID]Membership // {JoinedAt, SeatPool shared|own}
    PaymentMethod   *PaymentMethodRef      // {Provider, Token, Kind card|pse|manual, Last4?, ExpiresAt?}
    Provider        PaymentProviderRef     // {Name, CustomerID}
    Status          active | closed
}
```

Comandos: `OpenBillingAccount{OwnerUserID, LegalEntity, Country}` (crea cliente en proveedor por reacción), `UpdateLegalEntity`, `AttachOrganization{OrgID, Accepted by org admin}`, `DetachOrganization`, `SetPaymentMethod{Ref}`, `CloseAccount` (sin suscripción activa). Invariantes: una org pertenece a lo sumo a una cuenta; `TaxID` válido por país; no cerrar con suscripción `active|grace|restricted`.

### 3.2 Agregado `Subscription`

**[PP-F5-DOM-004] (MUST)** Stream `sub-<id>`; una por cuenta y por tipo de plan (clínica / farmacia).

```go
type Subscription struct {
    ID, Version; BillingAccountID; Kind clinic|pharmacy
    PlanID PlanID; PlanVersion int
    Seats   *Seats                       // {Purchased int}  (clínica)
    Tier    *VolumeTier                   // (farmacia)
    Cycle   BillingCycle                  // monthly|annual
    Status  Status                        // trialing | active | grace | restricted | cancelled | expired
    TrialEndsAt *time.Time
    CurrentPeriod Period                  // {Start, End}
    PendingChange *PlanChange             // downgrade programado al fin de periodo
    Dunning *DunningState                 // {FailedAt, Attempts, NextAttemptAt, GraceEndsAt}
    Charges map[ChargeID]Charge           // {Amount, Tax, Total, Period, Status pending|paid|failed|refunded, PaymentRef?, InvoiceRef?}
    Coupon  *CouponApplied
    ManualBilling bool                    // Enterprise
    CancelAtPeriodEnd bool
}
```

| Comando | Precondiciones | Evento |
|---|---|---|
| `StartTrial{Account, Kind, PlanID}` | B1 sin suscripción del mismo `Kind`; plan con `trial_days > 0`. | `TrialStarted` |
| `Subscribe{PlanID, Seats|Tier, Cycle, Coupon?}` | B1; B2 método de pago presente (o `ManualBilling`); B3 asientos ≥ profesionales activos (puerto `OrgRoster`). | `Subscribed` + `ChargeScheduled` |
| `ChangePlan{PlanID, Seats|Tier, Cycle}` | active/grace; upgrade inmediato con prorrateo (`ChargeScheduled`), downgrade → `PendingChange` al fin de periodo con B3 en ese momento. | `PlanChangeApplied` / `PlanChangeScheduled` |
| `AdjustSeats{Delta}` | active; B3 al reducir. | `SeatsAdjusted` (+ `ChargeScheduled` si aumenta) |
| `RenewPeriod{}` | fin de periodo (scheduler); aplica `PendingChange`; genera cargo. | `PeriodRenewed` + `ChargeScheduled` |
| `RecordChargeResult{ChargeID, Result paid|failed, PaymentRef?, Error?}` | cargo `pending`. | `ChargePaid` / `ChargeFailed` |
| `EnterGrace{}` / `Restrict{}` / `Reactivate{}` | por dunning (reacciones) / pago exitoso. | `SubscriptionGraceStarted` / `SubscriptionRestricted` / `SubscriptionReactivated` |
| `Cancel{AtPeriodEnd bool, Reason}` | active/grace/restricted. | `SubscriptionCancellationScheduled` / `SubscriptionCancelled` |
| `ExpireTrial{}` | trialing y `now > TrialEndsAt`. | `TrialExpired` (→ `restricted` con gracia 7 d) |
| `ApplyAdminAdjustment{Kind extend_trial|credit|comp_plan, Reason}` | `super_admin`. | `AdminAdjustmentApplied` |
| `RecordManualPayment{ChargeID, Amount, Reference}` | `ManualBilling`. | `ChargePaid{manual}` |
| `Refund{ChargeID, Amount, Reason}` | cargo `paid`. | `ChargeRefunded` (→ nota crédito por reacción) |

Invariantes: **B1** una suscripción por cuenta y tipo; **B2** método de pago salvo manual; **B3** asientos ≥ profesionales activos; **B4** transiciones válidas (`trialing→active|expired`, `active→grace→restricted→cancelled`, `restricted→active` al pagar); **B5** prorrateo = días restantes/días del periodo × diferencia, redondeo a 2 decimales en la moneda; **B6** importes calculados por el agregado desde el plan versionado (nunca del cliente); **B7** un cargo por periodo (+ cargos de prorrateo); **B8** `Refund ≤ Total − refunded`; **B9** cancelación al fin de periodo mantiene entitlements hasta `Period.End`; **B10** `PlanVersion` fijado al suscribir; cambios de precio aplican en `RenewPeriod` si hay nueva versión publicada ≥ 30 días antes (RN-39).

### 3.3 Planes y Value Objects

**[PP-F5-DOM-005] (MUST)** `ref.plans{ID, Kind, Code starter|pro|enterprise|pharmacy_basic|pharmacy_pro, Version, Name, Pricing{Model flat|per_seat|tiered, Currency, Monthly, Annual, Tiers[]}, TrialDays, Seats{Min, Max?}, Quotas{quotes_per_month, sms_per_month, storage_gb,…}, Features[], Status draft|published|retired, PublishedAt, EffectiveFrom}`; versionado inmutable (nueva versión = nueva fila). VOs: `Money{Amount decimal(12,2), Currency}`, `TaxID` (reutiliza F1 con `Kind`), `BillingCycle`, `Period`, `Seats`, `VolumeTier{Code, UpTo}`, `CouponApplied{Code, Kind pct|amount, Value, Until}`.

### 3.4 Eventos Billing v1

**[PP-F5-DOM-006] (MUST)** `BillingAccountOpened{…, pii{legal_entity}}`, `LegalEntityUpdated`, `OrganizationAttached/Detached`, `PaymentMethodSet{provider, kind, last4}`, `BillingAccountClosed`; `TrialStarted`, `Subscribed`, `PlanChangeApplied/Scheduled`, `SeatsAdjusted`, `PeriodRenewed`, `ChargeScheduled{charge_id, amount, tax, total, period, reason}`, `ChargePaid{payment_ref, manual?}`, `ChargeFailed{error_code}`, `ChargeRefunded`, `SubscriptionGraceStarted`, `SubscriptionRestricted`, `SubscriptionReactivated`, `SubscriptionCancellationScheduled`, `SubscriptionCancelled`, `TrialExpired`, `AdminAdjustmentApplied`, `CouponApplied`. Los datos fiscales van en `pii` (sujeto = `OwnerUserID`; para empresas, la razón social no es PII personal pero se trata igual por simplicidad).

### 3.5 Facturas, pagos y conciliación

**[PP-F5-DOM-007] (MUST)** `Invoice` (read model + entidad de proceso): `{ID, ChargeID, AccountID, Number (del proveedor fiscal), Provider, Status draft|issued|accepted|rejected|credited, CUFE?, PDFKey, XMLKey, IssuedAt, Totals, Lines}`; `Payment{ID, ChargeID, Provider, ProviderRef, Amount, Status, At}`; `CreditNote` análogo. Conciliación diaria: lista de cargos `paid` sin `Payment` confirmado por el proveedor y viceversa → `billing.discrepancies` con alerta; ningún `Restrict` se aplica si hay discrepancia abierta sobre esa cuenta.

### 3.6 Uso y entitlements

**[PP-F5-DOM-008] (MUST)** Proyección `billing.usage_counters{org_id, quota_key, period_start, used}` desde eventos (`QuoteRequested` → `quotes_per_month` para la farmacia **invitada** al responder (`QuoteReceived`) — se cuenta respuesta, no invitación; `MemberJoined/RoleChanged/Removed` → `seats_used`; `NotificationSent{channel: sms|whatsapp}` → `sms_per_month`; `AttachmentAdded` → `storage_bytes`). Read model `billing.entitlements{org_id, plan_code, plan_version, status, seats{limit, used}, quotas{…}, features[], computed_at}` recalculado por eventos de suscripción y de uso. `EntitlementSet` por defecto para orgs sin cuenta: `trialing` si dentro de los 30 días desde verificación, si no `restricted` (clínicas) / `pharmacy_basic` (farmacias).

## 4. Privacy

**[PP-F5-DOM-009] (MUST)**

- `SubjectKey{SubjectID (UserID), DEKEncrypted, KEKID, Status active|shredding|shredded, CreatedAt, ShredRequestedAt, ShreddedAt}` (tabla cifrada, no ES).
- `DataSubjectRequest` (proceso con tabla + eventos de auditoría): `{ID, SubjectID, Kind export|erasure|rectification, Status requested|verified|processing|ready|completed|cancelled|rejected, RequestedAt, VerifiedAt, GraceEndsAt (erasure: +7 d), CompletedAt, ArtifactKey?, Reason?, LegalHolds[]}`.
- Comandos: `RequestExport`, `RequestErasure`, `CancelErasure` (en gracia), `VerifyIdentity` (re-auth reciente ≤ 10 min), `ExecuteExport` (worker), `ExecuteErasure` (worker: shred + anonimización por contexto + Keycloak), `RejectRequest{Reason}` (admin, con base legal).
- `LegalHold`: razones que impiden borrar partes (recetas dispensadas 5 años, facturas 10 años según país): se conserva el dato anonimizado al sujeto (ids), no la PII.
- Puerto `PIIEraser` implementado por cada contexto: `EraseSubject(ctx, subjectID)` idempotente (read models, OpenSearch, storage de fotos/documentos del sujeto, caché, Keycloak).

## 5. Support

**[PP-F5-DOM-010] (MUST)**

- `SupportTicket{ID, RequesterID, OrganizationID?, Category billing|account|clinical|partner|bug|other, Subject, Status open|waiting_user|waiting_agent|resolved|closed, Priority (por plan), SLA{FirstResponseDue, ResolvedDue}, Messages[], Attachments[], Satisfaction?}` (CRUD + auditoría; puede delegarse a un helpdesk externo vía adaptador — decisión en ARQ-011).
- `ImpersonationSession{ID, AgentID, SubjectUserID, TicketID?, GrantedAt, ExpiresAt (≤ 30 min), Scope read|write, ClinicalAllowed bool, RevokedAt?, Actions[]}`; invariantes: consentimiento explícito del sujeto (token de un clic o desde ajustes), un agente a la vez, expira, revocable por el usuario, cada request auditado con `impersonator_id` en `Principal`.

## 6. Puertos F5

**[PP-F5-DOM-011] (MUST)**

| Puerto | Consumidor | Implementaciones |
|---|---|---|
| `Entitlements{For(orgID)}` | todos los contextos de negocio | Billing read model + Redis; `FakeEntitlements` en tests |
| `OrgRoster{ActiveProfessionals(orgID) int}` | Billing | Partner Network read model |
| `PaymentProvider{CreateCustomer, CreateSetupIntent/Tokenize, Charge, Refund, ParseWebhook, GetPayment}` | Billing | `wompi`, `mercadopago`, `stripe`, `manual`, `fake` |
| `InvoicingProvider{Issue, CreditNote, Fetch}` | Billing | `dian_<proveedor>`, `pdf_local`, `fake` |
| `KMS{Encrypt/Decrypt DEK, RotateKEK}` | Privacy | cloud KMS; `local` para dev |
| `PIIEraser` | Privacy | uno por contexto + `search`, `storage`, `keycloak`, `cache` |
| `MessagingProvider{SendSMS, SendWhatsAppTemplate, ParseInbound}` | Notifications | `twilio`, `meta_cloud_api`, `fake` |
| `Helpdesk{CreateTicket, Sync}` | Support | `internal`, `external_<proveedor>` |
| `StatusPage{Publish}` | Ops | proveedor externo |

## 7. Process managers y políticas

**[PP-F5-DOM-012] (MUST)**

| Disparador | Reacción |
|---|---|
| `OrganizationVerified` (clínica) | `StartTrial` (si no hay cuenta → cuenta implícita con el admin como titular, datos fiscales pendientes). |
| `BillingAccountOpened`, `PaymentMethodSet` | `PaymentProvider.CreateCustomer` / token guardado (asynq `billing:provider`). |
| `ChargeScheduled` | `PaymentProvider.Charge` (asynq, idempotente por `charge_id`) → `RecordChargeResult`; `manual` → factura y espera. |
| `ChargePaid` | `InvoicingProvider.Issue` (asynq `billing:invoice`) → `Invoice`; email con factura; `Reactivate` si estaba `grace/restricted`. |
| `ChargeFailed` | dunning: programa reintentos (d+3, d+7, d+12) y emails; `EnterGrace` al primer fallo; `Restrict` a los 14 d si sigue fallido y sin discrepancia; `Cancel` a los 60 d. |
| `ChargeRefunded` | `InvoicingProvider.CreditNote`. |
| `PeriodRenewed` (scheduler diario por `Period.End`) | cargo del nuevo periodo. |
| `TrialEndsAt − 7d/−1d` | emails; `ExpireTrial` al vencer. |
| `Subscribed/PlanChange*/SeatsAdjusted/Restricted/Reactivated/Cancelled` + eventos de uso | recalcular `entitlements` + invalidar caché. |
| `MemberJoined{role: veterinarian}` con asientos llenos | el comando ya falla por `Entitlements`; además aviso al 100 % de asientos. |
| `UsageCounter ≥ 80 %` | email de aviso (una vez por periodo). |
| `DataSubjectRequest.verified(export)` | asynq `privacy:export` → ZIP → `ready` → email con enlace. |
| `DataSubjectRequest.grace_ended(erasure)` | asynq `privacy:erase` → shred + `PIIEraser` en cada contexto → Keycloak disable+delete → `completed` → email. |
| `ImpersonationGranted` | token de sesión de soporte; `ImpersonationExpired` por scheduler. |
| Mensajes entrantes `STOP` (SMS/WA) | opt-out del canal. |
| Incidente abierto/cerrado (ops) | `StatusPage.Publish` + banner en app (feature flag). |

## 8. Canal SMS/WhatsApp

**[PP-F5-DOM-013] (SHOULD)** `PhoneVerification{UserID, Phone, OTPHash, ExpiresAt, Attempts, VerifiedAt}`; `preferences.sms/whatsapp` por categoría; plantillas WhatsApp aprobadas (`messaging.templates{name, locale, provider_template_id, status}`); `deliveries.channel ∈ {email, push, sms, whatsapp}`; coste por mensaje registrado (`cost_cents`) y atribuido a la organización (recordatorios de cita) o al tutor (Plus) para cuotas.

## 9. Decisiones explícitas

- `Subscription` separada de `BillingAccount` para permitir cuenta con clínica y farmacia (dos suscripciones) y cadenas.
- Los importes y prorrateos se calculan en el agregado con el plan versionado: el proveedor de pagos solo cobra lo que se le indica.
- `Invoice` no es agregado: su ciclo lo gobierna el proveedor fiscal; se modela como proceso con estados y reintentos.
- Privacy no duplica datos: coordina a los contextos por `PIIEraser`; cada contexto es responsable de su propia anonimización (y se prueba por contexto).
