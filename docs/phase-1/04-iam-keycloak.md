# F1 · 04 — Identidad y acceso (Keycloak)

Decisión marco: ADR-003 (Keycloak autentica; la app autoriza).

## 1. Realm como código

**[PP-F1-IAM-001] (MUST)** El realm `petpassport` se define en `infra/keycloak/realm.yaml` y se aplica con
`keycloak-config-cli` (idempotente) en dev, staging y prod. Prohibido configurar a mano en la consola;
cualquier cambio manual se considera *drift* y se revierte en el siguiente despliegue.

Ajustes de realm:

| Ajuste | Valor |
|---|---|
| `registrationAllowed` | `true` |
| `registrationEmailAsUsername` / `loginWithEmailAllowed` | `true` |
| `duplicateEmailsAllowed` | `false` |
| `verifyEmail` | `true` |
| `resetPasswordAllowed` | `true` |
| `rememberMe` | `false` |
| `sslRequired` | `external` (TLS termina en Traefik; `KC_PROXY_HEADERS=xforwarded`) |
| `bruteForceProtected` | `true` (ver IAM-005) |
| `internationalizationEnabled` | `true`, `supportedLocales: [es, en]`, `defaultLocale: es` |
| `eventsEnabled` / `adminEventsEnabled` | `true`, retención 30 días, tipos: LOGIN, LOGIN_ERROR, LOGOUT, REGISTER, UPDATE_PASSWORD, RESET_PASSWORD… |
| `defaultSignatureAlgorithm` | `RS256` (ES256 opcional) |

## 2. Clients

**[PP-F1-IAM-002] (MUST)**

| Client | Tipo | Flujos | Configuración clave |
|---|---|---|---|
| `petpassport-web` | público | Authorization Code + **PKCE S256** obligatorio | `redirectUris: [https://app.<dominio>/auth/callback, http://localhost:5173/auth/callback]`, `webOrigins: [+]` restringido a orígenes de la SPA, `postLogoutRedirectUris`, *implicit* y *direct grant* **deshabilitados**, `fullScopeAllowed: false`, default client scopes: `openid profile email roles petpassport-api-audience`. |
| `petpassport-api` | *bearer-only* (sin flujos habilitados) | — | Existe como **audiencia**. Scope `petpassport-api-audience` con *Audience mapper* `included.client.audience = petpassport-api`. |
| `petpassport-backend` | confidencial, `serviceAccountsEnabled: true` | Client credentials | Roles de `realm-management`: `view-users`, `manage-users` (solo lo necesario para IAM-011). Secreto en secret manager, rotación semestral. |

Mappers en el token de acceso: `sub`, `email`, `email_verified`, `name`, `given_name`, `family_name`,
`preferred_username`, `locale`, `realm_access.roles`, `aud` (incluye `petpassport-api`), `azp`, `sid`.
**No** se incluyen membresías de organización (ADR-003).

## 3. Roles

**[PP-F1-IAM-003] (MUST)**

| Rol de realm | Asignación | Uso |
|---|---|---|
| `pet_owner` | Automática: incluido en `default-roles-petpassport`. | Toda persona puede ser tutor. |
| `super_admin` | Manual por un operador con acceso a la consola (auditado); nunca por auto-registro. | Panel admin, acciones de verificación/suspensión. |

Roles por organización (`clinic_admin`, `veterinarian`, `receptionist`, `pharmacy_admin`, `pharmacist`) **no existen en Keycloak**; viven en `Organization.Members` (DOM-006).

## 4. Registro, verificación y contraseñas

**[PP-F1-IAM-004] (MUST)**

- Registro con email como username, verificación obligatoria (`VERIFY_EMAIL` como *required action* por defecto); enlace válido 12 h.
- Política de contraseñas: `length(12) and upperCase(1) and lowerCase(1) and digits(1) and notUsername and notEmail and passwordHistory(3) and hashAlgorithm(argon2)`; *password blacklist* con las 100k más comunes.
- Recuperación: enlace válido 30 min, de un solo uso; al cambiar contraseña se invalidan las demás sesiones (`UPDATE_PASSWORD` + logout de sesiones en el flujo).
- Campos de registro: solo email, contraseña, nombre y apellido. Teléfono, país y consentimiento se capturan en el onboarding de la app (PRD-003), no en Keycloak.
- Mensaje de "usuario existente" en registro es genérico (configuración `Login → Duplicate emails` + tema).

## 5. Fuerza bruta y MFA

**[PP-F1-IAM-005] (MUST)**

- Brute force: `permanentLockout: false`, `failureFactor: 5`, `waitIncrementSeconds: 60`, `maxFailureWaitSeconds: 900`, `maxDeltaTimeSeconds: 43200`, `quickLoginCheckMilliSeconds: 1000`, `minimumQuickLoginWaitSeconds: 60`.
- MFA: flujo *browser* personalizado `browser-petpassport` con subflujo *Conditional OTP* usando la condición **"Condition - user role" = `super_admin`** → OTP **obligatoria** para administradores; para el resto, OTP opcional configurable desde "Mi cuenta".
- OTP TOTP, 6 dígitos, 30 s, 1 ventana de tolerancia.

## 6. Tiempos de vida

**[PP-F1-IAM-006] (MUST)**

| Parámetro | Valor |
|---|---|
| Access token | 5 min |
| Refresh token (SSO Session Idle) | 30 min, rotación de refresh habilitada (`revokeRefreshToken: true`, `refreshTokenMaxReuse: 0`) |
| SSO Session Max | 10 h |
| Client session idle/max | hereda |
| Offline tokens | deshabilitados en F1 |
| Login timeout / action tokens | 30 min / 12 h verificación, 30 min reset |

## 7. Validación de tokens en el backend

**[PP-F1-IAM-007] (MUST)** `platform/auth` con `github.com/coreos/go-oidc/v3`:

1. Descubrimiento `/.well-known/openid-configuration` al arranque; JWKS con caché y *refresh* ante `kid` desconocido (máx. 1 vez/min).
2. Validación: firma (`RS256`/`ES256` únicamente; rechazar `none`/HS*), `iss == PP_OIDC_ISSUER`, `aud ∋ petpassport-api`, `exp`/`nbf`/`iat` con *skew* 60 s, `typ == Bearer`, `azp == petpassport-web` (o lista permitida), `email_verified == true` para rutas de negocio (las rutas de onboarding aceptan no verificado solo para mostrar el aviso).
3. Token solo por cabecera `Authorization: Bearer`; nunca por query string ni cookie.
4. Ante fallo: `401` con `WWW-Authenticate: Bearer error="invalid_token"`, sin detalle del motivo en el cuerpo; motivo en logs (sin el token).

**[PP-F1-IAM-008] (MUST)** `Principal` y membresías:

```go
type Principal struct {
    UserID      uuid.UUID
    Email       string
    EmailVerified bool
    GlobalRoles []string               // de realm_access.roles
    Memberships []Membership           // {OrgID, OrgType, Role, OrgStatus}
    SessionID   string                 // sid
}
```

- Membresías cargadas desde `read_partnernetwork.org_members` (join con `organizations`), en caché en memoria LRU por `UserID` con TTL 60 s e invalidación por eventos `MemberRemoved/MemberRoleChanged/OrganizationSuspended/...` (DOM-011). En F3 la caché pasa a Redis.
- `UserAccount.Status == suspended` → `403 ACCOUNT_SUSPENDED` aunque el token sea válido (el token puede sobrevivir unos minutos a la suspensión en Keycloak).

**[PP-F1-IAM-009] (MUST)** Aprovisionamiento JIT: el middleware llama a `identity.EnsureUserAccount(claims)` en cada request autenticado; la operación es un *upsert* barato con *throttle* de 5 min por usuario para `last_login_at`. Si el usuario no completó onboarding, las rutas de negocio responden `403 ONBOARDING_REQUIRED` y la SPA redirige.

## 8. Modelo de autorización

**[PP-F1-IAM-010] (MUST)** Paquete `platform/authz`, *deny by default*, políticas explícitas y testeadas una a una.

```go
func Can(p Principal, action Action, res Resource) Decision // {Allow bool, Reason string}
```

Tabla de políticas F1 (G = tutor de la mascota; L = org con `CareLink` activo y org `active`; A = `super_admin`):

| Acción | pet_owner (G) | clinic_admin | veterinarian | receptionist | pharmacy_* | super_admin |
|---|---|---|---|---|---|---|
| `me:read/update`, `onboarding:complete` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ |
| `pet:register` | ✔ | ✔ (como tutor) | ✔ | ✔ | ✔ | ✔ |
| `pet:read` | G | L | L | L | ✖ | ✔ |
| `pet:update`, `pet:microchip`, `pet:photo`, `pet:deceased`, `pet:transfer` | G | ✖ | ✖ | ✖ | ✖ | ✔ |
| `pet:carelink:grant/revoke` | G | ✖ | ✖ | ✖ | ✖ | ✔ |
| `pet:guardian_contact:read` | G | L | L | L | ✖ | ✔ |
| `org:register` | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ |
| `org:read` | pública mínima (nombre, ciudad, tipo) por `PublicCode`; completa para miembros | ✔ | ✔ | ✔ | ✔ (propia) | ✔ |
| `org:update` | ✖ | ✔ (propia) | ✖ | ✖ | `pharmacy_admin` propia | ✔ |
| `org:member:invite/role/remove` | ✖ | ✔ (propia) | ✖ | ✖ | `pharmacy_admin` propia | ✔ |
| `org:patients:list` | ✖ | ✔ (propia, clínica) | ✔ | ✔ | ✖ | ✔ |
| `admin:org:verify/reject/suspend/reactivate`, `admin:user:*` | ✖ | ✖ | ✖ | ✖ | ✖ | ✔ (con motivo) |

Reglas de implementación:

- Cada handler HTTP declara su `Action` y construye el `Resource` (p. ej. `PetResource{ID, GuardianID, ActiveLinkOrgIDs}` cargado del read model) **antes** de ejecutar el comando/consulta. Un handler sin llamada a `authz.Can` falla en un test de *coverage* de rutas (SEC-005).
- Las decisiones `Deny` se registran (`authz_denied_total{action}`) y en `audit_log` cuando el actor es `super_admin`.
- `super_admin` no es omnipotente sobre datos personales: `pet:guardian_contact:read` por admin queda auditado con motivo.

## 9. Operaciones administrativas vía service account

**[PP-F1-IAM-011] (SHOULD)** Cliente `identity/infra/keycloakadmin` (Admin REST API, token de `petpassport-backend` cacheado hasta 60 s antes de expirar):

| Operación | Endpoint |
|---|---|
| `SetEnabled(userID, bool)` | `PUT /admin/realms/petpassport/users/{id}` `{enabled}` |
| `LogoutAllSessions(userID)` | `POST /admin/realms/petpassport/users/{id}/logout` |
| `SendVerifyEmail(userID)` | `PUT /admin/realms/petpassport/users/{id}/send-verify-email` |
| `FindByEmail(email)` (solo admin) | `GET /admin/realms/petpassport/users?email=&exact=true` |

La service account **no** tiene permisos para crear usuarios ni roles (principio de mínimo privilegio).

## 10. Tema, correo y localización

**[PP-F1-IAM-012] (SHOULD)**

- Tema `petpassport` para *login* y *email* (basado en `keycloak.v2`): logo, colores, textos es/en, enlaces a términos/privacidad.
- SMTP configurado en el realm (`smtpServer` con credenciales por variable de entorno en `keycloak-config-cli`); remitente `no-reply@<dominio>`; SPF/DKIM/DMARC configurados antes de M1.5.
- Dev: Mailpit (`smtp:1025`, UI `:8025`).

## 11. Cierre de sesión

**[PP-F1-IAM-013] (MUST)**

- Front-channel: SPA llama `end_session_endpoint` con `id_token_hint` y `post_logout_redirect_uri`.
- La API no mantiene sesión; tras logout la SPA descarta tokens en memoria. Las acciones de suspensión ejecutan `LogoutAllSessions` (back-channel implícito en Keycloak).
- Back-channel logout URL en el client web **no** se configura en F1 (SPA sin servidor).

## 12. Secuencia de autenticación (SPA)

```
SPA ─► GET auth.<dominio>/realms/petpassport/protocol/openid-connect/auth
        ?client_id=petpassport-web&response_type=code&scope=openid profile email
        &code_challenge=…&code_challenge_method=S256&redirect_uri=…&state=…&nonce=…
Keycloak ─► login/registro/verificación ─► redirect /auth/callback?code=…
SPA ─► POST /token (code + code_verifier) ─► {access_token, id_token, refresh_token}
SPA ─► GET api/v1/me (Bearer) ─► api: verify → EnsureUserAccount → 200 {onboarding_required?}
```

Tokens **solo en memoria** (oidc-client-ts con `InMemoryWebStorage`); renovación silenciosa por refresh token rotado; `sessionStorage` únicamente para `state/nonce` durante el *redirect*.
