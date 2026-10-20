# F1 · 05 — API REST v1

Contrato fuente: `backend/api/openapi.yaml` (ADR-005). Este documento es la especificación legible; ante discrepancia, se corrige el YAML y se actualiza aquí en el mismo PR.

## 1. Convenciones

**[PP-F1-API-001] (MUST)**

| Aspecto | Regla |
|---|---|
| Base URL | `https://api.<dominio>/api/v1` |
| Formato | `application/json; charset=utf-8`. Fechas ISO-8601 UTC (`2026-10-20T14:03:11Z`); fechas civiles `YYYY-MM-DD`. |
| Nombres | `snake_case` en JSON; recursos en plural; acciones de dominio como sub-recursos `POST /pets/{id}/transfer`. |
| Autenticación | `Authorization: Bearer <access_token>` en todo salvo `/healthz`, `/readyz`, `/legal/*`. |
| Idempotencia | `Idempotency-Key: <uuid>` obligatoria en POST de creación (ARQ-008). |
| Concurrencia | Recursos devuelven `version` y `ETag: "<version>"`. Comandos aceptan `If-Match`; discrepancia → `409 CONFLICT_VERSION`. |
| Correlación | `X-Correlation-ID` entrante (UUID) o generado; siempre devuelto. `X-Request-ID` único por request. |
| Paginación | `?cursor=&limit=` (20 por defecto, máx 100). Respuesta `{data: [...], next_cursor: string|null}`. |
| Filtros/orden | Parámetros explícitos por endpoint; sin filtros genéricos. |
| Errores | `application/problem+json` (API-011). |
| Límites | Cuerpo JSON ≤ 256 KB; multipart ≤ 6 MB; strings con `maxLength` en el esquema; arrays con `maxItems`. |
| Campos desconocidos | Rechazados (`422 VALIDATION_FAILED`, `additionalProperties: false`). |
| Enumeraciones | Siempre strings cerrados en el esquema. |
| Rate limit | Cabeceras `RateLimit-*` emitidas por el gateway/WAF (F1); por app en F3. |

**[PP-F1-API-002] (MUST)** OpenAPI 3.1 es la fuente: `oapi-codegen` (strict server) y `openapi-typescript` generan tipos; `spectral` valida estilo; `oasdiff` bloquea *breaking changes* en CI. Toda ruta tiene `operationId`, `security`, ejemplos y `x-pp-action` (la `Action` de authz, usada por el test de cobertura de autorización).

## 2. Recursos y DTOs principales

```jsonc
// Pet
{
  "id": "uuid", "version": 3, "guardian_id": "uuid",
  "name": "Luna", "species": "dog", "species_other": null, "breed": "Beagle",
  "sex": "female", "neutered": "yes",
  "birth_date": { "date": "2021-05-10", "approximate": false }, "age": { "years": 5, "months": 5 },
  "markings": "tricolor", "microchip": "941000012345678",
  "photo": { "url": "https://…presigned…", "thumb_url": "https://…", "expires_at": "…" } | null,
  "status": "active", "deceased_at": null,
  "care_links": [ { "organization_id": "uuid", "display_name": "Clínica Sur", "city": "Medellín",
                    "status": "active", "granted_at": "…", "org_suspended": false } ],
  "pending_transfer": { "to_email_masked": "j***@mail.com", "expires_at": "…" } | null,
  "created_at": "…", "updated_at": "…"
}
// Organization
{
  "id": "uuid", "version": 2, "type": "clinic", "legal_name": "…", "display_name": "…",
  "tax_id": { "country": "CO", "value": "900123456-7" },
  "address": { "country": "CO", "region": "Antioquia", "city": "Medellín", "line1": "…", "line2": null, "postal_code": null },
  "contact": { "email": "…", "phone": "+57…", "website": null },
  "public_code": "7K3MQ2", "status": "active", "status_reason": null,
  "my_role": "clinic_admin", "created_at": "…", "updated_at": "…"
}
// OrgSummary (público por código)
{ "id": "uuid", "type": "clinic", "display_name": "…", "city": "…", "country": "CO", "status": "active" }
// Member
{ "user_id": "uuid", "email": "…", "display_name": "…", "role": "veterinarian", "status": "active", "joined_at": "…" }
// Invitation
{ "id": "uuid", "email": "…", "role": "…", "status": "pending", "expires_at": "…", "created_at": "…" }
// Me
{ "id": "uuid", "email": "…", "email_verified": true, "display_name": "…", "locale": "es", "country": "CO",
  "phone": "+57…", "address": {…}|null, "emergency_contact": {…}|null,
  "global_roles": ["pet_owner"], "memberships": [ { "organization_id": "…", "display_name": "…", "type": "clinic", "role": "clinic_admin", "org_status": "active" } ],
  "onboarding_required": false, "consent": { "terms_version": "2026-10", "privacy_version": "2026-10", "accepted_at": "…" },
  "consent_outdated": false, "status": "active" }
```

## 3. Endpoints

Columna **Authz** = `Action` (IAM-010). Códigos de éxito indicados; errores comunes en API-011.

### 3.1 Cuenta propia — **[PP-F1-API-003] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /me` | `me:read` | → `200 Me`. Provoca JIT. Acepta `email_verified=false`. |
| `POST /me/onboarding` | `onboarding:complete` | `{display_name, country, locale, phone?, accept_terms_version, accept_privacy_version}` → `200 Me`. `422 CONSENT_VERSION_OUTDATED` si no son las vigentes. |
| `PATCH /me` | `me:update` | `{display_name?, locale?, country?, phone?, address?, emergency_contact?}` → `200 Me`. |
| `POST /me/consent` | `me:update` | `{terms_version, privacy_version}` → `204`. |
| `GET /legal/current` | público | → `{terms_version, privacy_version, terms_url, privacy_url}`. |

### 3.2 Mascotas — **[PP-F1-API-004] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /pets?status=active\|deceased\|all&cursor&limit` | `pet:read` (propias) | → `200 {data: Pet[], next_cursor}`. |
| `POST /pets` *(Idempotency-Key)* | `pet:register` | `{name, species, species_other?, breed?, sex, neutered, birth_date?, markings?}` → `201 Pet` + `Location`. |
| `GET /pets/{pet_id}` | `pet:read` | → `200 Pet` (si clínica: incluye `guardian_contact` solo con `pet:guardian_contact:read`). |
| `PATCH /pets/{pet_id}` *(If-Match)* | `pet:update` | campos opcionales → `200 Pet`. `409 PET_DECEASED`. |
| `PUT /pets/{pet_id}/microchip` *(If-Match)* | `pet:microchip` | `{microchip}` → `200 Pet`. `409 MICROCHIP_ALREADY_ASSIGNED`, `409 MICROCHIP_CORRECTION_LIMIT`. |
| `PUT /pets/{pet_id}/photo` | `pet:photo` | `multipart/form-data; file` → `200 Pet`. `415 UNSUPPORTED_MEDIA_TYPE`, `413`. |
| `DELETE /pets/{pet_id}/photo` | `pet:photo` | → `200 Pet`. |
| `POST /pets/{pet_id}/deceased` *(If-Match)* | `pet:deceased` | `{deceased_at?}` → `200 Pet`. |
| `POST /pets/{pet_id}/deceased/undo` | `pet:deceased` | → `200 Pet`. `409 UNDO_WINDOW_EXPIRED`. |
| `POST /pets/{pet_id}/transfer` *(Idempotency-Key)* | `pet:transfer` | `{to_email}` → `202 Pet` (pending_transfer). `409 TRANSFER_ALREADY_PENDING`, `422 SAME_GUARDIAN`. |
| `DELETE /pets/{pet_id}/transfer` | `pet:transfer` | → `200 Pet`. |
| `POST /pet-transfers/{token}/accept` | autenticado; email del token == `principal.email` | → `200 Pet`. `404` si token inválido/caducado (sin distinguir). |
| `GET /pet-transfers/{token}` | autenticado | → `{pet_name, species, from_guardian_display_name, expires_at}` (previsualización). |

### 3.3 Vínculos de atención — **[PP-F1-API-005] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/lookup?code=7K3MQ2` | autenticado | → `200 OrgSummary`. `404` si no existe o no está `active`. |
| `POST /pets/{pet_id}/care-links` | `pet:carelink:grant` | `{organization_id}` → `201 Pet` (idempotente: `200` si ya activo). `422 ORGANIZATION_NOT_ELIGIBLE`, `409 CARE_LINK_LIMIT`. |
| `DELETE /pets/{pet_id}/care-links/{organization_id}` | `pet:carelink:revoke` | `{reason?}` opcional → `200 Pet`. |
| `GET /organizations/{org_id}/patients?q=&status=&cursor&limit` | `org:patients:list` | → `200 {data: PatientRow[], next_cursor}`; `PatientRow = {pet: PetSummary, guardian: {display_name, phone?}, linked_at}`. `403 ORGANIZATION_SUSPENDED`. |
| `GET /organizations/{org_id}/patients/{pet_id}` | `pet:read` + `pet:guardian_contact:read` (vía L) | → `200 Pet` con `guardian_contact`. |

### 3.4 Organizaciones y miembros — **[PP-F1-API-006] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /organizations` *(Idempotency-Key)* | `org:register` | `{type, legal_name, display_name, tax_id, address, contact}` → `201 Organization` (status `pending_verification`). |
| `GET /organizations/{org_id}` | `org:read` | → `200 Organization` (miembro) o `OrgSummary` (no miembro). |
| `PATCH /organizations/{org_id}` *(If-Match)* | `org:update` | campos → `200 Organization`. Si cambia `legal_name`/`tax_id` en `active` → vuelve a `pending_verification`. |
| `GET /organizations/{org_id}/members` | `org:read` (miembro) | → `200 {data: Member[]}`. |
| `PATCH /organizations/{org_id}/members/{user_id}` | `org:member:role` | `{role}` → `200 Member`. `409 LAST_ADMIN_CANNOT_BE_REMOVED`, `422 ROLE_NOT_ALLOWED_FOR_TYPE`. |
| `DELETE /organizations/{org_id}/members/{user_id}` | `org:member:remove` | → `204`. `409 LAST_ADMIN_CANNOT_BE_REMOVED`. |

### 3.5 Invitaciones — **[PP-F1-API-007] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /organizations/{org_id}/invitations` *(Idempotency-Key)* | `org:member:invite` | `{email, role}` → `201 Invitation`. Re-invitar al mismo email revoca la anterior. |
| `GET /organizations/{org_id}/invitations?status=` | `org:member:invite` | → `200 {data: Invitation[]}`. |
| `DELETE /organizations/{org_id}/invitations/{invitation_id}` | `org:member:invite` | → `204`. |
| `POST /organizations/{org_id}/invitations/{invitation_id}/resend` | `org:member:invite` | → `204` (máx 3 reenvíos / 24 h). |
| `GET /invitations/{token}` | autenticado | → `{organization: OrgSummary, role, email_masked, expires_at}`. |
| `POST /invitations/{token}/accept` | autenticado; `principal.email == invitación.email` y `email_verified` | → `200 Me`. `403 INVITATION_EMAIL_MISMATCH`, `404` si inválida/caducada. |

### 3.6 Administración — **[PP-F1-API-008] (MUST)**

Todas requieren `super_admin`; los comandos exigen `{reason}` (≥ 10 caracteres) y se auditan.

| Método y ruta | Cuerpo / respuesta |
|---|---|
| `GET /admin/organizations?status=pending_verification\|active\|suspended\|rejected&q=&cursor&limit` | → `200 {data: Organization[], next_cursor}` (incluye `founder`, `member_count`). |
| `POST /admin/organizations/{org_id}/verify` | `{reason?}` → `200 Organization`. |
| `POST /admin/organizations/{org_id}/reject` | `{reason}` → `200 Organization`. |
| `POST /admin/organizations/{org_id}/suspend` | `{reason}` → `200 Organization`. |
| `POST /admin/organizations/{org_id}/reactivate` | `{reason}` → `200 Organization`. |
| `GET /admin/users?q=&status=&cursor&limit` | → `200 {data: AdminUserRow[]}` (`id, email, display_name, status, global_roles, created_at, last_login_at, pet_count, memberships`). |
| `POST /admin/users/{user_id}/suspend` | `{reason}` → `200 AdminUserRow`. |
| `POST /admin/users/{user_id}/reactivate` | `{reason}` → `200 AdminUserRow`. |
| `GET /admin/audit-log?actor_id=&target_id=&from=&to=&cursor` | → `200 {data: AuditEntry[]}`. |

### 3.7 Salud y métricas — **[PP-F1-API-009] (MUST)**

| Ruta (puerto interno `PP_INTERNAL_ADDR`) | Respuesta |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` (proceso vivo). |
| `GET /readyz` | `200` si DB responde, JWKS cargado, storage accesible; `503` en caso contrario con detalle por dependencia. |
| `GET /metrics` | Prometheus. |
| `GET /debug/pprof/*` | solo `PP_ENV=dev`. |

En el puerto público solo se expone `GET /api/v1/healthz` (alias liviano) para el WAF.

## 4. Validación de payloads

**[PP-F1-API-010] (MUST)**

- Esquemas OpenAPI con `maxLength`, `pattern`, `enum`, `format`, `maxItems`, `additionalProperties: false`; el middleware generado rechaza antes de llegar al handler.
- Normalización previa al dominio: *trim*, NFC Unicode, lower-case en emails; rechazo de caracteres de control (`\x00-\x08\x0B\x0C\x0E-\x1F`).
- `Content-Type` estricto (`415` si no coincide); `Accept` ignorado salvo `problem+json`.
- Multipart: solo el campo `file`; tipo detectado por *magic bytes*, no por extensión ni cabecera.
- Los IDs de ruta se validan como UUID antes de cualquier consulta.
- Mensajes de validación no reflejan el valor recibido (evita *reflected content*).

## 5. Catálogo de errores

**[PP-F1-API-011] (MUST)** Formato:

```json
{ "type": "https://docs.petpassport.app/errors/MICROCHIP_ALREADY_ASSIGNED",
  "title": "Microchip ya asignado", "status": 409, "code": "MICROCHIP_ALREADY_ASSIGNED",
  "detail": "El microchip indicado ya está registrado.", "instance": "/api/v1/pets/…/microchip",
  "correlation_id": "…", "errors": [ { "field": "microchip", "code": "ALREADY_ASSIGNED" } ] }
```

| `code` | HTTP | Cuándo |
|---|---|---|
| `VALIDATION_FAILED` | 422 | Esquema/normalización; `errors[]` por campo. |
| `UNAUTHENTICATED` | 401 | Token ausente/inválido. |
| `EMAIL_NOT_VERIFIED` | 403 | Ruta de negocio con email sin verificar. |
| `ONBOARDING_REQUIRED` | 403 | Falta onboarding. |
| `CONSENT_VERSION_OUTDATED` | 422 / 403 | Versiones no vigentes / consentimiento pendiente. |
| `ACCOUNT_SUSPENDED` | 403 | Cuenta suspendida. |
| `FORBIDDEN` | 403 | Política deniega sobre recurso visible. |
| `NOT_FOUND` | 404 | No existe o no es visible para el actor. |
| `CONFLICT_VERSION` | 409 | `If-Match` no coincide. |
| `REQUEST_IN_PROGRESS` | 409 | Idempotency-Key en vuelo. |
| `IDEMPOTENCY_KEY_REUSED` | 422 | Misma clave, distinto cuerpo. |
| `PET_DECEASED` | 409 | Comando no permitido en fallecida. |
| `MICROCHIP_ALREADY_ASSIGNED` / `MICROCHIP_CORRECTION_LIMIT` / `MICROCHIP_INVALID` | 409 / 409 / 422 | |
| `UNDO_WINDOW_EXPIRED` | 409 | |
| `TRANSFER_ALREADY_PENDING` / `SAME_GUARDIAN` / `TRANSFER_TOKEN_INVALID` | 409 / 422 / 404 | |
| `ORGANIZATION_NOT_ELIGIBLE` | 422 | No es clínica activa. |
| `CARE_LINK_LIMIT` | 409 | > 20 activos. |
| `ORGANIZATION_SUSPENDED` | 403 | Miembro de org suspendida accede a pacientes. |
| `ROLE_NOT_ALLOWED_FOR_TYPE` | 422 | |
| `LAST_ADMIN_CANNOT_BE_REMOVED` | 409 | |
| `INVITATION_EMAIL_MISMATCH` | 403 | |
| `INVITATION_RESEND_LIMIT` | 429 | |
| `REASON_REQUIRED` | 422 | Acción admin sin motivo. |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | |
| `PAYLOAD_TOO_LARGE` | 413 | |
| `RATE_LIMITED` | 429 | Emitido por gateway/WAF (F1). |
| `INTERNAL` / `UNAVAILABLE` | 500 / 503 | Sin detalles; `correlation_id`. |

## 6. Ejemplo completo

```http
POST /api/v1/pets HTTP/1.1
Authorization: Bearer eyJ…
Idempotency-Key: 0192f3a2-1111-7aaa-bbbb-cccccccccccc
X-Correlation-ID: 0192f3a2-2222-7aaa-bbbb-dddddddddddd
Content-Type: application/json

{ "name": "Luna", "species": "dog", "breed": "Beagle", "sex": "female", "neutered": "yes",
  "birth_date": { "date": "2021-05-10", "approximate": false } }

HTTP/1.1 201 Created
Location: /api/v1/pets/0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f
ETag: "1"
X-Correlation-ID: 0192f3a2-2222-7aaa-bbbb-dddddddddddd
Content-Type: application/json

{ "id": "0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f", "version": 1, "name": "Luna", … }
```
