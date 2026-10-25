# F1 · 02 — Modelo de dominio (DDD)

## 1. Lenguaje ubicuo (glosario F1)

**[PP-F1-DOM-001] (MUST)** El glosario es vinculante: los nombres de código usan la columna "Código".

| Término (ES) | Código | Definición |
|---|---|---|
| Tutor | `Guardian` | Persona responsable de una mascota. Es un `UserAccount` con rol global `pet_owner`. |
| Mascota | `Pet` | Animal registrado por un tutor. Raíz de agregado en Pet Registry. |
| Pasaporte | `PetPassport` | Conjunto de datos de la mascota: perfil (F1) + historia clínica (F2). No es un agregado; es el producto. |
| Vínculo de atención | `CareLink` | Autorización concedida por el tutor a una clínica para ver (F1) y registrar (F2) datos de una mascota. |
| Transferencia de tutela | `GuardianshipTransfer` | Cambio del tutor principal de una mascota con aceptación del receptor. |
| Microchip | `Microchip` | Identificador físico implantado; único en el sistema. |
| Organización | `Organization` | Clínica veterinaria o farmacia. Raíz de agregado en Partner Network. |
| Tipo de organización | `OrganizationType` | `clinic` \| `pharmacy`. |
| Miembro | `Member` | Usuario que pertenece a una organización con un `OrgRole`. |
| Rol de organización | `OrgRole` | `clinic_admin`, `veterinarian`, `receptionist`, `pharmacy_admin`, `pharmacist`. |
| Invitación | `Invitation` | Oferta de membresía a un email con un rol; caduca. |
| Código público | `PublicCode` | Código corto de la organización que el tutor usa para vincular. |
| Verificación | `Verification` | Proceso por el que SuperAdmin valida la existencia legal de una organización. |
| Cuenta de usuario | `UserAccount` | Identidad local espejo del usuario de Keycloak (`sub`). |
| Principal | `Principal` | Representación del actor autenticado en un request (usuario + roles + membresías). |
| Actor | `Actor` | Quien ejecuta un comando; se registra en metadatos de evento. |

## 2. Mapa de contextos F1

**[PP-F1-DOM-002] (MUST)** Tres bounded contexts implementados como módulos aislados (ADR-001).

```
┌─────────────────────────┐
│ Identity & Access       │ generic · CRUD · ACL sobre Keycloak
│ UserAccount, Consent    │
└───────┬─────────┬───────┘
        │ GuardianLookup (puerto)        │ UserLookup (puerto)
        ▼                                ▼
┌─────────────────────────┐  OrganizationLookup   ┌──────────────────────────┐
│ Pet Registry            │──────────────────────►│ Partner Network          │
│ Pet (ES)                │◄──── eventos ─────────│ Organization (ES)        │
│   CareLink, Transfer    │  OrganizationSuspended│   Member, Invitation     │
└─────────────────────────┘                        └──────────────────────────┘
```

| Relación | Tipo | Mecanismo |
|---|---|---|
| Identity → Pet Registry / Partner Network | Upstream (Conformist + ACL) | `Principal` inyectado por middleware; puertos `GuardianLookup`, `UserLookup`. |
| Partner Network → Pet Registry | Customer/Supplier | Puerto `OrganizationLookup{IsActiveClinic(orgID) (OrgSummary, error)}`; evento `OrganizationSuspended`/`OrganizationReactivated` consumido por proyección de Pet Registry. |

## 3. Pet Registry

### 3.1 Agregado `Pet`

**[PP-F1-DOM-003] (MUST)** Estado, comandos e invariantes.

Stream: `pet-<PetID>` · `stream_type = "Pet"`.

```go
type Pet struct {
    ID            PetID
    Version       int64
    GuardianID    UserID
    Name          PetName
    Species       Species          // + SpeciesOther string cuando Species == Other
    Breed         *Breed
    Sex           Sex              // male | female | unknown
    Neutered      Tristate         // yes | no | unknown
    BirthDate     *BirthDate       // {Date, Approximate bool}
    Markings      *Markings        // color / señas, ≤ 200
    Microchip     *Microchip
    MicrochipCorrections int       // 0..1 por tutor
    Photo         *PhotoRef        // {ObjectKey, ThumbKey, ContentType}
    Status        PetStatus        // active | deceased
    DeceasedAt    *civil.Date
    CareLinks     map[OrganizationID]CareLink
    PendingTransfer *GuardianshipTransfer // {ToEmail, ToUserID?, Token hash, ExpiresAt}
}
type CareLink struct { OrgID OrganizationID; Status CareLinkStatus; GrantedBy UserID; GrantedAt time.Time; RevokedAt *time.Time }
```

| Comando | Precondiciones (invariantes) | Evento(s) |
|---|---|---|
| `RegisterPet{GuardianID, Name, Species, …}` | I1 nombre válido; VO válidos; actor == GuardianID. | `PetRegistered` |
| `UpdatePetProfile{…campos opcionales…}` | I3 no fallecida; al menos un cambio real (si no, no emite evento). | `PetProfileUpdated` |
| `AssignMicrochip{Microchip}` | I3; I2 formato válido y reserva obtenida; I7 `MicrochipCorrections < 1` si ya tenía. | `PetMicrochipAssigned` (incluye `previous`) |
| `ChangePetPhoto{PhotoRef}` | I3. | `PetPhotoChanged` |
| `MarkPetDeceased{Date?}` | Status active; fecha ≤ hoy y ≥ BirthDate. | `PetMarkedDeceased` |
| `UndoDeceased{}` | Status deceased y `PetMarkedDeceased` < 24 h, o actor super_admin. | `PetDeceasedUndone` |
| `GrantCareLink{OrgID}` | I3 (permitido también en deceased: no, solo revocar); I5 org clínica activa (puerto); I8 ≤ 20 activos; idempotente si ya activo. | `CareLinkGranted` |
| `RevokeCareLink{OrgID}` | Vínculo activo existe. | `CareLinkRevoked` |
| `InitiateGuardianshipTransfer{ToEmail}` | I3; I4 `ToEmail != guardian.email`; sin transferencia pendiente. | `GuardianshipTransferInitiated` |
| `CancelGuardianshipTransfer{}` | Pendiente existe. | `GuardianshipTransferCancelled` |
| `AcceptGuardianshipTransfer{Token, NewGuardianID}` | Token coincide y no caducado; el actor es dueño del email (Principal.Email == ToEmail). | `PetGuardianshipTransferred` |

Invariantes:

- **I1** `Name` 1–60 caracteres tras *trim*, sin caracteres de control.
- **I2** `Microchip` válido y único en el sistema (reserva transaccional, DAT-005).
- **I3** Una mascota `deceased` solo admite `RevokeCareLink`, `UndoDeceased` y `ChangePetPhoto`.
- **I4** El nuevo tutor es distinto del actual.
- **I5** Solo organizaciones `clinic` + `active` pueden recibir vínculo.
- **I6** Solo el tutor (o `super_admin`) ejecuta comandos sobre `Pet` (se aplica en `authz`, no en el agregado; el agregado recibe `ActorID` en metadatos).
- **I7** El tutor corrige el microchip una sola vez.
- **I8** Máximo 20 vínculos activos.

### 3.2 Value Objects

**[PP-F1-DOM-004] (MUST)**

| VO | Reglas | Normalización |
|---|---|---|
| `PetName` | 1–60, Unicode letras/espacios/`'`/`-`; sin control chars. | `strings.TrimSpace`, colapsar espacios. |
| `Species` | enum: `dog, cat, bird, rabbit, reptile, rodent, equine, ferret, fish, other`. `other` exige `SpeciesOther` 1–40. | lower-case. |
| `Breed` | 1–80, texto libre en F1 (catálogo en F2). | trim. |
| `Sex` | `male, female, unknown`. | |
| `Tristate` | `yes, no, unknown`. | |
| `BirthDate` | `Date` ≤ hoy; ≥ hoy − 40 años; `Approximate bool`. | fecha civil (sin TZ). |
| `Markings` | ≤ 200. | trim. |
| `Microchip` | ISO: `^\d{15}$` (prefijo de fabricante/país válido no se verifica en F1). Legado: `^[A-Z0-9]{9,10}$`. | quitar espacios/guiones, upper. |
| `PhotoRef` | `ObjectKey` con prefijo `pets/<PetID>/`; `ContentType` ∈ {jpeg, png, webp}. **Decisión M1.2**: el pipeline genera siempre objetos `jpeg` (ver ARQ-012). | |
| `CareLinkStatus` | `active, revoked`. | |

### 3.3 Eventos Pet Registry (v1)

**[PP-F1-DOM-005] (MUST)** Cada evento lleva `schema_version: 1`. PII separada en `pii` (ADR-002).

| Evento | Payload |
|---|---|
| `PetRegistered` | `{pet_id, guardian_id, name, species, species_other?, breed?, sex, neutered, birth_date?{date, approximate}, markings?}` |
| `PetProfileUpdated` | `{pet_id, changes: {name?, species?, species_other?, breed?, sex?, neutered?, birth_date?, markings?}}` (solo campos cambiados) |
| `PetMicrochipAssigned` | `{pet_id, microchip, previous?}` |
| `PetPhotoChanged` | `{pet_id, photo: {object_key, thumb_key, content_type}, previous?}` |
| `PetMarkedDeceased` | `{pet_id, deceased_at?}` |
| `PetDeceasedUndone` | `{pet_id}` |
| `CareLinkGranted` | `{pet_id, organization_id, granted_by}` |
| `CareLinkRevoked` | `{pet_id, organization_id, revoked_by, reason?}` |
| `GuardianshipTransferInitiated` | `{pet_id, from_guardian_id, expires_at, pii: {to_email}}` |
| `GuardianshipTransferCancelled` | `{pet_id}` |
| `PetGuardianshipTransferred` | `{pet_id, from_guardian_id, to_guardian_id}` |

Ejemplo serializado:

```json
{
  "event_id": "0192f3a1-7c2e-7b1a-9f1e-3c1d2e4f5a6b",
  "stream_id": "0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f",
  "stream_type": "Pet",
  "stream_version": 1,
  "event_type": "PetRegistered",
  "schema_version": 1,
  "payload": { "pet_id": "0192f3a1-…", "guardian_id": "7a8f…", "name": "Luna", "species": "dog",
               "breed": "Beagle", "sex": "female", "neutered": "yes",
               "birth_date": { "date": "2021-05-10", "approximate": false } },
  "metadata": { "actor_id": "7a8f…", "actor_roles": ["pet_owner"], "correlation_id": "…",
                "causation_id": "…", "source": "api", "ip_hash": "…", "user_agent_hash": "…" },
  "occurred_at": "2026-10-20T14:03:11.412Z"
}
```

## 4. Partner Network

### 4.1 Agregado `Organization`

**[PP-F1-DOM-006] (MUST)**

Stream: `org-<OrganizationID>` · `stream_type = "Organization"`.

```go
type Organization struct {
    ID          OrganizationID
    Version     int64
    Type        OrganizationType     // clinic | pharmacy (inmutable)
    LegalName   LegalName
    DisplayName DisplayName
    TaxID       TaxID                // {Country, Value}
    Address     Address
    Contact     Contact              // {Email, Phone, Website?}
    PublicCode  PublicCode
    Status      OrgStatus            // pending_verification | active | suspended | rejected
    StatusReason *string
    Members     map[UserID]Member    // {Role OrgRole, Status active|removed, JoinedAt}
    Invitations map[InvitationID]Invitation // {Email, Role, TokenHash, ExpiresAt, Status pending|accepted|revoked|expired}
}
```

| Comando | Precondiciones | Evento(s) |
|---|---|---|
| `RegisterOrganization{Type, LegalName, …, FounderID}` | VO válidos; PublicCode reservado. | `OrganizationRegistered`, `MemberJoined` (founder como admin) |
| `UpdateOrganizationProfile{…}` | Status ≠ rejected; si cambia `LegalName` o `TaxID` en org `active` → también emite `OrganizationVerificationReset`. | `OrganizationProfileUpdated` (+ reset) |
| `VerifyOrganization{Reason?}` | Status pending; actor super_admin. | `OrganizationVerified` |
| `RejectOrganization{Reason}` | Status pending; motivo obligatorio. | `OrganizationRejected` |
| `SuspendOrganization{Reason}` | Status active; motivo. | `OrganizationSuspended` |
| `ReactivateOrganization{Reason}` | Status suspended. | `OrganizationReactivated` |
| `InviteMember{Email, Role}` | O1 rol válido para el tipo; email no es miembro activo; no hay invitación pendiente al mismo email (si hay, se revoca y re-emite). Status ∉ {rejected, suspended}. | `MemberInvited` |
| `RevokeInvitation{InvitationID}` | pending. | `InvitationRevoked` |
| `AcceptInvitation{Token, UserID, Email}` | token coincide, no caducado, `Email` == invitación. | `MemberJoined`, (`InvitationAccepted` implícito en MemberJoined.invitation_id) |
| `ChangeMemberRole{UserID, Role}` | O1; O2 no degradar al último admin. | `MemberRoleChanged` |
| `RemoveMember{UserID}` | O2 no retirar al último admin. | `MemberRemoved` |

Invariantes:

- **O1** `OrgRole` válido para el `OrganizationType` (clínica: `clinic_admin, veterinarian, receptionist`; farmacia: `pharmacy_admin, pharmacist`).
- **O2** Siempre existe al menos un miembro activo con rol admin.
- **O3** `Type` es inmutable.
- **O4** Una invitación pendiente por email como máximo.
- **O5** `PublicCode` único (reserva transaccional).

### 4.2 Value Objects

**[PP-F1-DOM-007] (MUST)**

| VO | Reglas |
|---|---|
| `LegalName` / `DisplayName` | 2–120 / 2–80. |
| `TaxID` | `{Country ISO-3166-1 alpha-2, Value}`; validador por país enchufable; F1 implementa `CO` (NIT con dígito de verificación) y genérico (`^[A-Z0-9.-]{5,20}$`). |
| `Address` | `{Country, Region?, City, Line1, Line2?, PostalCode?, Geo?{lat,lng}}`; `City`/`Line1` obligatorios. |
| `Contact` | `Email` RFC 5322 (normalizado lower), `Phone` E.164, `Website` URL https opcional. |
| `PublicCode` | 6 chars Crockford base32 (`0-9A-HJKMNP-TV-Z`), generado aleatoriamente, único. |
| `OrgRole` | enum anterior. |
| `OrgStatus` | `pending_verification, active, suspended, rejected`. |
| `InvitationToken` | 32 bytes aleatorios; se guarda solo `SHA-256(token)`; caduca a 7 días. |

### 4.3 Eventos Partner Network (v1)

**[PP-F1-DOM-008] (MUST)**

| Evento | Payload |
|---|---|
| `OrganizationRegistered` | `{organization_id, type, legal_name, display_name, tax_id{country, value}, address{…}, contact{email, phone, website?}, public_code, founder_id}` |
| `OrganizationProfileUpdated` | `{organization_id, changes{…}}` |
| `OrganizationVerificationReset` | `{organization_id, cause}` |
| `OrganizationVerified` | `{organization_id, reason?}` |
| `OrganizationRejected` | `{organization_id, reason}` |
| `OrganizationSuspended` | `{organization_id, reason}` |
| `OrganizationReactivated` | `{organization_id, reason?}` |
| `MemberInvited` | `{organization_id, invitation_id, role, expires_at, pii{email}}` |
| `InvitationRevoked` | `{organization_id, invitation_id}` |
| `MemberJoined` | `{organization_id, user_id, role, invitation_id?}` |
| `MemberRoleChanged` | `{organization_id, user_id, from, to}` |
| `MemberRemoved` | `{organization_id, user_id, reason?}` |

## 5. Identity & Access

**[PP-F1-DOM-009] (MUST)** Modelo CRUD con ACL sobre Keycloak (ADR-002, ADR-003).

```go
type UserAccount struct {
    ID            UserID     // == Keycloak sub
    Email         Email
    EmailVerified bool
    DisplayName   string
    Locale        Locale     // es | en
    Country       *CountryCode
    Phone         *PhoneE164
    Address       *Address
    EmergencyContact *Contact
    Status        AccountStatus // active | suspended
    GlobalRoles   []string      // snapshot del último token
    Consent       Consent       // {TermsVersion, PrivacyVersion, AcceptedAt}
    OnboardingCompletedAt *time.Time
    CreatedAt, LastLoginAt time.Time
}
```

Operaciones (servicio de aplicación, auditadas en `platform.audit_log`):

| Operación | Regla |
|---|---|
| `EnsureUserAccount(principalClaims)` | *Upsert* JIT en el primer request autenticado: crea si no existe; actualiza `Email`, `EmailVerified`, `DisplayName`, `GlobalRoles`, `LastLogin` (throttle 5 min). |
| `CompleteOnboarding(consent, profile)` | Requiere versiones actuales de términos/privacidad. |
| `UpdateProfile(fields)` | Email no editable. |
| `AcceptCurrentConsent()` | Cuando cambian las versiones. |
| `SuspendUser(reason)` / `ReactivateUser(reason)` | Solo super_admin; llama al ACL `KeycloakAdmin.SetEnabled(userID, false)` + `LogoutAllSessions`. |

ACL `KeycloakAdmin` (puerto): `SetEnabled`, `LogoutAllSessions`, `SendVerifyEmail`. Implementado con el Admin REST API y la service account (IAM-011).

## 6. Puertos entre contextos

**[PP-F1-DOM-010] (MUST)**

| Puerto (definido en el consumidor) | Implementación F1 |
|---|---|
| `petregistry.GuardianLookup{Exists(ctx, UserID) bool; FindByEmail(ctx, Email) (UserID, bool)}` | adaptador in-process sobre `identity.app.Queries`. |
| `petregistry.OrganizationLookup{ActiveClinic(ctx, OrganizationID) (OrgSummary, error)}` | adaptador in-process sobre `partnernetwork.app.Queries` (read model). |
| `partnernetwork.UserLookup{Exists; EmailOf}` | sobre `identity.app.Queries`. |
| `identity.KeycloakAdmin` | cliente HTTP Admin API. |
| `shared.Clock`, `shared.IDGen`, `shared.TokenGen` | inyectables para pruebas. |

## 7. Políticas y reacciones F1

**[PP-F1-DOM-011] (SHOULD)** Reacciones a eventos (in-process, misma transacción en F1):

| Evento | Reacción |
|---|---|
| `OrganizationSuspended` | Proyección `read_petregistry.pet_care_links.org_suspended = true` para la org → la clínica no ve pacientes, el tutor ve aviso. No se emiten eventos en `Pet` (el vínculo sigue existiendo). |
| `OrganizationReactivated` | `org_suspended = false`. |
| `MemberRemoved` / `MemberRoleChanged` | Invalida caché de membresías del usuario. |
| `PetGuardianshipTransferred` | Email a ambos tutores (F1: síncrono vía SMTP con *best-effort*; F2: por outbox). |
| `MemberInvited`, `GuardianshipTransferInitiated`, `OrganizationVerified/Rejected` | Email transaccional (igual que arriba). |

## 8. Metadatos de eventos

**[PP-F1-DOM-012] (MUST)** Todo evento se persiste con:

| Campo | Origen |
|---|---|
| `event_id` | UUIDv7 generado al construir el evento. |
| `correlation_id` | Cabecera `X-Correlation-ID` entrante o generado por el middleware; se propaga a logs y trazas. |
| `causation_id` | `event_id` del evento que originó el comando (reacciones) o el `request_id`. |
| `actor_id`, `actor_roles`, `actor_org_id?` | Del `Principal`. `system` para procesos internos. |
| `source` | `api` \| `worker` \| `migration` \| `admin-cli`. |
| `ip_hash`, `user_agent_hash` | SHA-256 con sal diaria (no se guarda IP en claro). |
| `idempotency_key?` | Si el comando la traía. |

## 9. Decisiones de modelado explícitas

- `Invitation` y `CareLink` son **entidades internas** de su agregado (no raíces): su número por agregado está acotado y sus invariantes dependen del estado de la raíz.
- `GuardianshipTransfer` vive en `Pet` como transferencia pendiente única; si en el futuro se necesitan transferencias masivas, se extrae como saga.
- `UserAccount` **no** es event-sourced; `PetGuardianshipTransferred` referencia `UserID`s, nunca emails (PII) salvo en `pii{}`.
- La edad de la mascota es derivada en consultas (`age_from(birth_date)`), nunca almacenada.
