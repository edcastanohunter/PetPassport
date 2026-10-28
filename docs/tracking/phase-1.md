# Tracking — Fase 1: Fundación y MVP Core

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F1-PRD-001 | PRD | Registro con email y contraseña, verificación obligatoria | MUST | M1.1 | IMPLEMENTADO | | #7 | Registro en Keycloak (realm.yaml); verificación obligatoria; E2E Playwright pendiente (nightly, M1.5). |
| PP-F1-PRD-002 | PRD | Login, logout y recuperación de contraseña | MUST | M1.1 | IMPLEMENTADO | | #7 | Login/logout PKCE con oidc-client-ts; recuperación vía Keycloak. |
| PP-F1-PRD-003 | PRD | Onboarding: consentimiento, datos básicos, elección de camino | MUST | M1.1 | IMPLEMENTADO | | #7 | OnboardingPage 3 pasos; POST /me/onboarding. Opción «Registrar organización» lleva a /app hasta M1.3. |
| PP-F1-PRD-004 | PRD | Ver y editar perfil de tutor | MUST | M1.1 | IMPLEMENTADO | | #7 | ProfilePage + PATCH /me (merge-patch, null vacía). |
| PP-F1-PRD-005 | PRD | Registrar mascota | MUST | M1.2 | IMPLEMENTADO | | #8 | POST /pets con proyección síncrona (aparece inmediato); VOs DOM-004; PetFormPage.
| PP-F1-PRD-006 | PRD | Ver y editar perfil de mascota con control de versión | MUST | M1.2 | IMPLEMENTADO | | #8 | GET/PATCH /pets/{id} con ETag/If-Match → 409 CONFLICT_VERSION; fallecida bloquea edición (I3).
| PP-F1-PRD-007 | PRD | Microchip único con una corrección | MUST | M1.2 | IMPLEMENTADO | | #8 | PUT /pets/{id}/microchip: reserva transaccional DAT-005; MICROCHIP_ALREADY_ASSIGNED sin revelar; 1 corrección (I7).
| PP-F1-PRD-008 | PRD | Foto de mascota | SHOULD | M1.2 | IMPLEMENTADO | | #8 | PUT/DELETE /pets/{id}/photo: pipeline magic-bytes + límite 25 MP + 5 MB; salida JPEG q82; URLs prefirmadas 15 min. HEIC rechazado (sin libheif).
| PP-F1-PRD-009 | PRD | Marcar fallecida / deshacer 24 h | SHOULD | M1.2 | IMPLEMENTADO | | #8 | POST /pets/{id}/deceased (+ If-Match) y undo con ventana 24 h; sección «En memoria» en FE.
| PP-F1-PRD-010 | PRD | Transferencia de tutela con aceptación | SHOULD | M1.3 | IMPLEMENTADO | | #9 | POST/DELETE /pets/{id}/transfer, GET/POST /pet-transfers/{token}[/accept]; token 32 B, hash en read_petregistry.pet_transfers (no en eventos); email al destinatario y a ambos tutores (best-effort). FE: TransferPanel + /app/transfers/:token. |
| PP-F1-PRD-011 | PRD | Registro de organización → pendiente de verificación | MUST | M1.3 | IMPLEMENTADO | | #9 | POST /organizations (Idempotency-Key): pending_verification, PublicCode reservado (reintento ante colisión), fundador admin; tax_id duplicado → 422 ALREADY_REGISTERED. FE: asistente /org/new de 5 pasos. |
| PP-F1-PRD-012 | PRD | Editar perfil de organización (reinicio de verificación) | MUST | M1.3 | IMPLEMENTADO | | #9 | PATCH /organizations/{id} (If-Match): cambio de razón social/tax_id en activa → OrganizationVerificationReset. FE: /org/:id/settings con aviso. |
| PP-F1-PRD-013 | PRD | Invitar miembros y aceptar invitación | MUST | M1.3 | IMPLEMENTADO | | #9 | Invitaciones (API-007): token de un solo uso 7 días, re-invitar revoca (O4), reenviar rota el token (máx 3/24 h → 429), aceptar exige email coincidente y verificado. FE: pestaña invitaciones + /invitations/:token. |
| PP-F1-PRD-014 | PRD | Cambiar rol / retirar miembro (último admin protegido) | MUST | M1.3 | IMPLEMENTADO | | #9 | PATCH/DELETE /organizations/{id}/members/{user}: O1/O2; la caché de membresías se invalida al commit (pierde acceso en el siguiente request). |
| PP-F1-PRD-015 | PRD | Vincular mascota a clínica por código y revocar | MUST | M1.3 | IMPLEMENTADO | | #9 | GET /organizations/lookup?code, POST/DELETE /pets/{id}/care-links: solo clínicas activas (I5), máx 20 (I8), idempotente (200). FE: CareLinksPanel con previsualización. |
| PP-F1-PRD-016 | PRD | Lista de pacientes y ficha (clínica, solo lectura) | MUST | M1.3 | IMPLEMENTADO | | #9 | GET /organizations/{id}/patients (q ILIKE escapado + cursor) y /patients/{pet} con guardian_contact; org suspendida → 403 ORGANIZATION_SUSPENDED. FE: PatientsPage/PatientDetailPage. |
| PP-F1-PRD-017 | PRD | SuperAdmin: verificación, rechazo, suspensión de organizaciones | MUST | M1.4 | IMPLEMENTADO | | #10 | Cola de verificación FIFO + verificar/rechazar/suspender/reactivar con motivo (≥10; opcional en verificar) y audit_log en la misma tx; FE /admin/organizations. |
| PP-F1-PRD-018 | PRD | SuperAdmin: buscar y suspender usuarios | MUST | M1.4 | IMPLEMENTADO | | #10 | Búsqueda (email/nombre, ILIKE escapado, cursor) y suspender/reactivar con motivo: plataforma primero, luego Keycloak (503 si falla, reintento idempotente); auto-suspensión prohibida. |
| PP-F1-PRD-019 | PRD | i18n es/en, responsive, accesibilidad | SHOULD | M1.5 | IMPLEMENTADO | | #11 | Selector de idioma (pie) con locale de perfil; ui_locales a Keycloak; navegación móvil; E2E 5 flujos + axe AA en verde (dev). |
| PP-F1-PRD-020 | PRD | Páginas legales y consentimiento versionado | MUST | M1.1 | IMPLEMENTADO | | #7 | identity.consent_versions con semilla 2026-10-draft; GET /legal/current; modal de re-consentimiento. |
| PP-F1-DOM-001 | DOM | Glosario / lenguaje ubicuo aplicado en código | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-DOM-002 | DOM | Context map F1 como módulos aislados | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-DOM-003 | DOM | Agregado Pet: estado, comandos, invariantes I1–I8 | MUST | M1.2 | IMPLEMENTADO | | #8 | Agregado Pet: 11 comandos, invariantes I1–I8 con prueba nombrada por ID (harness QA-002). Transferencia completa en dominio; HTTP en M1.3.
| PP-F1-DOM-004 | DOM | Value Objects de Pet Registry | MUST | M1.2 | IMPLEMENTADO | | #8 | VOs petregistry/domain/vo.go; fixture compartido docs/phase-1/fixtures/validation-cases.json generado desde Go.
| PP-F1-DOM-005 | DOM | Eventos Pet Registry v1 | MUST | M1.2 | IMPLEMENTADO | | #8 | 11 eventos v1 con schema_version, PII en pii{} (transferencia); golden tests por evento.
| PP-F1-DOM-006 | DOM | Agregado Organization: estado, comandos, invariantes O1–O5 | MUST | M1.3 | IMPLEMENTADO | | #9 | Agregado Organization: 11 comandos, O1–O5 con prueba nombrada (TestOrg_O1..O5); cobertura domain 98 %. |
| PP-F1-DOM-007 | DOM | Value Objects de Partner Network | MUST | M1.3 | IMPLEMENTADO | | #9 | VOs: TaxID (CO NIT con DV DIAN + genérico), PublicCode Crockford, contacto/dirección; casos compartidos con FE en validation-cases.json (taxIdCO/taxIdGeneric). |
| PP-F1-DOM-008 | DOM | Eventos Partner Network v1 | MUST | M1.3 | IMPLEMENTADO | | #9 | 12 eventos v1 con golden tests; email de invitación solo en pii{}. |
| PP-F1-DOM-009 | DOM | Identity: UserAccount CRUD y ACL Keycloak | MUST | M1.1 | IMPLEMENTADO | | #7 | internal/identity/domain.UserAccount (CRUD, snapshot de token). |
| PP-F1-DOM-010 | DOM | Puertos entre contextos | MUST | M1.2 | IMPLEMENTADO | | #8 | GuardianLookup adaptador in-process sobre identity.app; OrganizationLookup definido (adaptador en M1.3).
| PP-F1-DOM-011 | DOM | Políticas/reacciones F1 (suspensión, emails) | SHOULD | M1.3 | IMPLEMENTADO | | #9 | OrganizationSuspended/Reactivated → pet_care_links.org_suspended (misma tx, sin eventos en Pet); cambios de miembros/estado invalidan caché; emails SMTP best-effort tras commit tras interfaz Mailer (invitación, transferencia, verificación/rechazo). |
| PP-F1-DOM-012 | DOM | Metadatos de eventos | MUST | M1.2 | IMPLEMENTADO | | #8 | eventstore.Metadata: actor, roles, correlation, causation, source, ip_hash, idempotency_key.
| PP-F1-ARQ-001 | ARQ | Layout de paquetes y reglas de dependencia en CI | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-ARQ-002 | ARQ | Hexagonal por módulo | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-ARQ-003 | ARQ | Command bus + Unit of Work | MUST | M1.2 | IMPLEMENTADO | | #8 | bus + db.UnitOfWork (tx por contexto) + CommandContext con If-Match; reintento único en GrantCareLink.
| PP-F1-ARQ-004 | ARQ | Event Store v0: Append/Load/ReadAll, concurrencia optimista | MUST | M1.2 | IMPLEMENTADO | | #8 | Store Append/Load/ReadAll con semántica 03 §4; UPDATE es.streams con expected → ConcurrencyError; métricas events_appended_total y eventstore_concurrency_conflicts_total.
| PP-F1-ARQ-005 | ARQ | Proyecciones síncronas e idempotentes | MUST | M1.2 | IMPLEMENTADO | | #8 | projection.Runner síncrono en la misma tx; PetsProjector idempotente con guarda de versión; prueba de rebuild dump-idéntico.
| PP-F1-ARQ-006 | ARQ | Outbox escrito (consumo en F2) | MUST | M1.2 | IMPLEMENTADO | | #8 | outbox.Enqueue en la misma tx del Append; gauge outbox_pending; contrato de consumo documentado para F2.
| PP-F1-ARQ-007 | ARQ | Registry de eventos, serialización y versionado | MUST | M1.2 | IMPLEMENTADO | | #8 | Registry tipo@versión (validación en arranque), interfaz Upcaster (sin implementaciones), golden tests.
| PP-F1-ARQ-008 | ARQ | Idempotencia de comandos | MUST | M1.2 | IMPLEMENTADO | | #8 | idempotency.Begin/Commit/Abort sobre platform.idempotency_keys; replay con Idempotent-Replayed; reuse 422; in-flight 409.
| PP-F1-ARQ-009 | ARQ | Lado de consulta sobre read models con filtros por principal | MUST | M1.2 | IMPLEMENTADO | | #8 | Queries sqlc sobre read_petregistry; listado siempre filtrado por guardian_id = actor; paginación por cursor opaco.
| PP-F1-ARQ-010 | ARQ | Modelo de errores y mapeo a problem+json | MUST | M1.1 | IMPLEMENTADO | | #7 | kernel.{DomainError,NotFoundError,ForbiddenError,ValidationError} → problem.From. |
| PP-F1-ARQ-011 | ARQ | Configuración 12-factor, arranque y apagado | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-ARQ-012 | ARQ | Object storage e imágenes | SHOULD | M1.2 | IMPLEMENTADO | | #8 | Puerto ObjectStorage + adaptador MinIO + pipeline imagen (storage/image). Salida JPEG q82 (sin encoder WebP pure-Go; decisión en PR).
| PP-F1-IAM-001 | IAM | Realm como código (keycloak-config-cli) | MUST | M1.0 | IMPLEMENTADO | | #6 | realm.yaml aplicado por keycloak-config-cli; política de contraseñas se completa en M1.1 (IAM-004). |
| PP-F1-IAM-002 | IAM | Clients web/api/backend | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-IAM-003 | IAM | Roles de realm y rol por defecto | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-IAM-004 | IAM | Registro, verificación y política de contraseñas | MUST | M1.1 | IMPLEMENTADO | | #7 | passwordHistory(3)+argon2 en realm.yaml; blacklist 100k pendiente de fichero (M1.5). |
| PP-F1-IAM-005 | IAM | Brute force y OTP obligatoria para super_admin | MUST | M1.4 | IMPLEMENTADO | | #10 | Flujo browser-petpassport: OTP obligatoria para super_admin (conditional-user-role), opcional si configurada; brute force activo. Verificado con KC 26.1.5 + config-cli 6.5.1. |
| PP-F1-IAM-006 | IAM | Tiempos de vida de tokens y sesiones | MUST | M1.1 | IMPLEMENTADO | | #7 | Tiempos de vida en realm.yaml desde M1.0; verificados en M1.1. |
| PP-F1-IAM-007 | IAM | Validación JWT en backend | MUST | M1.1 | IMPLEMENTADO | | #7 | platform/auth.Verifier (go-oidc): RS256/ES256, iss, aud, azp, typ, skew 60 s. |
| PP-F1-IAM-008 | IAM | Principal y membresías con caché | MUST | M1.3 | IMPLEMENTADO | | #9 | Principal con membresías desde read_partnernetwork (incluye display_name/estado); caché LRU 60 s (cota 10 000) invalidada por eventos; pruebas TTL/LRU/invalidación/concurrencia. |
| PP-F1-IAM-009 | IAM | Aprovisionamiento JIT de UserAccount | MUST | M1.1 | IMPLEMENTADO | | #7 | identity/app.Service.Resolve: upsert JIT, throttle 5 min en memoria. |
| PP-F1-IAM-010 | IAM | Modelo de autorización (paquete authz) | MUST | M1.2 | IMPLEMENTADO | | #8 | authz: políticas de pets (G/L/A) deny-by-default; métricas authz_decisions_total/authz_denied_total; sonda para cobertura de rutas (SEC-005 M1.3).
| PP-F1-IAM-011 | IAM | Service account para operaciones admin | SHOULD | M1.4 | IMPLEMENTADO | | #10 | keycloakadmin: client credentials con caché (renueva 60 s antes), reintento ante 401; SetEnabled, logout, send-verify-email. |
| PP-F1-IAM-012 | IAM | Tema de login, SMTP y localización | SHOULD | M1.5 | IMPLEMENTADO | | #11 | Tema petpassport login/email es/en; SMTP autenticado por env; SPF/DKIM/DMARC en Terraform Cloudflare. Pendiente ops: credenciales SMTP reales y apply DNS en staging (RUNBOOK §13). |
| PP-F1-IAM-013 | IAM | Cierre de sesión | MUST | M1.1 | IMPLEMENTADO | | #7 | signoutRedirect con id_token_hint; /auth/logout limpia estado. |
| PP-F1-API-001 | API | Convenciones REST | MUST | M1.1 | IMPLEMENTADO | | #7 | Convenciones aplicadas en /me*; validación de contrato en tests. |
| PP-F1-API-002 | API | OpenAPI 3.1 fuente + generación + oasdiff | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-API-003 | API | Endpoints /me, onboarding, consentimiento, legal | MUST | M1.1 | IMPLEMENTADO | | #7 | GET/PATCH /me, POST /me/onboarding, POST /me/consent, GET /legal/current. |
| PP-F1-API-004 | API | Endpoints de mascotas | MUST | M1.2 | IMPLEMENTADO | | #8 | Endpoints de mascotas en openapi.yaml (sin transfer/care-links); x-pp-action, Idempotency-Key, If-Match/ETag, Location; contrato validado en pruebas.
| PP-F1-API-005 | API | Endpoints de vínculos y pacientes | MUST | M1.3 | IMPLEMENTADO | | #9 | Lookup por código, care-links grant/revoke, pacientes y ficha (x-pp-action declarada; contrato OpenAPI + spectral). |
| PP-F1-API-006 | API | Endpoints de organizaciones y miembros | MUST | M1.3 | IMPLEMENTADO | | #9 | Organizaciones y miembros (OrganizationOrSummary: completa para miembros/super_admin, resumen para el resto). |
| PP-F1-API-007 | API | Endpoints de invitaciones | MUST | M1.3 | IMPLEMENTADO | | #9 | Invitaciones: crear/listar/revocar/reenviar, previsualizar y aceptar (devuelve Me con la membresía nueva). |
| PP-F1-API-008 | API | Endpoints de administración | MUST | M1.4 | IMPLEMENTADO | | #10 | /admin/organizations[/{id}/verify|reject|suspend|reactivate], /admin/users[/{id}/suspend|reactivate], /admin/audit-log; 403 salvo super_admin (suite authz). |
| PP-F1-API-009 | API | Salud y métricas | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-API-010 | API | Validación de payloads | MUST | M1.1 | IMPLEMENTADO | | #7 | nethttp-middleware (kin-openapi) por operación: 422 con errors[]; 415 por Content-Type. |
| PP-F1-API-011 | API | Catálogo de errores | MUST | M1.1 | IMPLEMENTADO | | #7 | Catálogo F1 en platform/problem; códigos de otros hitos se añaden al usarse. |
| PP-F1-DAT-001 | DAT | Esquemas y roles de BD | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-DAT-002 | DAT | Migraciones goose + CI | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-DAT-003 | DAT | DDL Event Store + trigger append-only | MUST | M1.2 | IMPLEMENTADO | | #8 | Migración 20261020000004: es.streams/es.events (trigger append-only)/es.outbox.
| PP-F1-DAT-004 | DAT | DDL identity | MUST | M1.1 | IMPLEMENTADO | | #7 | Migración 20261020000003. |
| PP-F1-DAT-005 | DAT | DDL read_petregistry + reservas | MUST | M1.2 | IMPLEMENTADO | | #8 | Migración 20261020000005: read_petregistry (pets, pet_care_links, microchip_reservations, pet_transfers).
| PP-F1-DAT-006 | DAT | DDL read_partnernetwork + reservas | MUST | M1.3 | IMPLEMENTADO | | #9 | Migración 000006: organizations, org_members, invitations (+ventana de reenvíos), public_code_reservations; tax_id único por índice; proyector idempotente (rebuild en test/integration/orgs_test.go). |
| PP-F1-DAT-007 | DAT | DDL platform (idempotencia, auditoría, checkpoints) | MUST | M1.1 | IMPLEMENTADO | | #7 | Migración 20261020000003 (idempotency_keys, audit_log, projection_checkpoints). |
| PP-F1-DAT-008 | DAT | Convenciones de datos | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-DAT-009 | DAT | Backups y retención | MUST | M1.5 | IMPLEMENTADO | | #11 | cmd/backup export/verify/import de es.events (JSONL gzip + manifest sha256), timer semanal, lifecycle de buckets, RUNBOOK §9. Ensayo dev en RESTORE-LOG; pendiente ops: PITR del proveedor y simulacro mensual en staging. |
| PP-F1-FE-001 | FE | Scaffold Vue 3 + stack | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-FE-002 | FE | Autenticación OIDC PKCE en SPA | MUST | M1.1 | IMPLEMENTADO | | #7 | shared/auth/userManager + features/auth/sessionStore. |
| PP-F1-FE-003 | FE | Cliente API tipado y manejo de errores | MUST | M1.1 | IMPLEMENTADO | | #7 | ApiError + onApiError; Idempotency-Key la aporta useCommand en M1.2. |
| PP-F1-FE-004 | FE | Rutas, guards y layouts | MUST | M1.1 | IMPLEMENTADO | | #7 | Rutas y authGuard; requiresMembership/requiresGlobalRole listos para M1.3/M1.4. |
| PP-F1-FE-005 | FE | Onboarding y perfil | MUST | M1.1 | IMPLEMENTADO | | #7 | OnboardingPage, ProfilePage, ConsentDialog; vee-validate/zod llegan con FE-011 (M1.2). |
| PP-F1-FE-006 | FE | Pantallas de mascotas | MUST | M1.2 | IMPLEMENTADO | | #8 | Lista/formulario/ficha (sin vínculos ni transferencia); If-Match en edición; test de store.
| PP-F1-FE-007 | FE | Pantallas de organización | MUST | M1.3 | IMPLEMENTADO | | #9 | Registro (wizard), resumen con código copiable/imprimible, miembros/invitaciones, ajustes, aceptar invitación; OrgLayout con navegación por rol/tipo; guards requiresOrgType/requiresOrgRole. |
| PP-F1-FE-008 | FE | Pacientes (clínica) y vínculos (tutor) | MUST | M1.3 | IMPLEMENTADO | | #9 | Vínculos y transferencia en la ficha del tutor; lista de pacientes (búsqueda, cursor, tel:) y ficha de solo lectura; aviso de org suspendida sin llamar a la API. |
| PP-F1-FE-009 | FE | Administración | MUST | M1.4 | IMPLEMENTADO | | #10 | Organizaciones (pestañas, detalle con fundador, modal de motivo), usuarios (roles, membresías, mascotas) y auditoría (filtros, cursor); rutas requiresGlobalRole super_admin. |
| PP-F1-FE-010 | FE | i18n, accesibilidad y responsive | SHOULD | M1.5 | IMPLEMENTADO | | #11 | i18n completa con test de paridad; FieldError aria-*; skip link y foco en #main; tablas responsive; contraste AA validado con axe en E2E. |
| PP-F1-FE-011 | FE | Validación zod espejo de VOs con fixture compartido | MUST | M1.2 | IMPLEMENTADO | | #8 | shared/validation/schemas.ts (zod) + validation-cases.spec.ts ejecutando el fixture de Go.
| PP-F1-FE-012 | FE | Observabilidad en cliente | COULD | M1.5 | PENDIENTE | | | Aplazado: el endpoint de eventos de cliente es F2 y Sentry sería dependencia nueva sin ADR. |
| PP-F1-SEC-001 | SEC | WAF gestionado y rate limiting en el borde | MUST | M1.4 | EN_PROGRESO | | #10 | Código listo: Terraform Cloudflare (OWASP+Managed log→block, rate limits, X-Origin-Auth) y Traefik de origen cerrado con todos los límites SEC-001. Pendiente: aprovisionar (dominio/cuenta) y pasar a block tras 1 semana. |
| PP-F1-SEC-002 | SEC | Traefik: routers, middlewares, timeouts | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-SEC-003 | SEC | Cabeceras de seguridad y CORS | MUST | M1.1 | IMPLEMENTADO | | #7 | CSP en nginx.conf y Traefik; CORS en API desde M1.0. |
| PP-F1-SEC-004 | SEC | Validación de entrada y subidas seguras | MUST | M1.2 | IMPLEMENTADO | | #8 | Validación OpenAPI + VOs; subidas: nombre/Content-Type del cliente ignorados, magic bytes, ≤ 5 MB, ≤ 25 MP, SVG/GIF/HEIC rechazados; regexp RE2.
| PP-F1-SEC-005 | SEC | Autorización deny-by-default con suite exhaustiva | MUST | M1.3 | IMPLEMENTADO | | #9 | cmd/api/authz_suite_test.go: (1) toda operación del contrato llama a authz.Can con su x-pp-action (sonda); (2) matriz operación × 9 personas (tutor/vinculado/no vinculado/suspendida/farmacia/ajeno/super_admin) → 404/403/permitido; platform/authz/suite_test.go: 100 % de acciones × personas. |
| PP-F1-SEC-006 | SEC | Gestión de secretos | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-SEC-007 | SEC | Logging sin PII, correlación y audit log | MUST | M1.1 | IMPLEMENTADO | | #7 | kernel.Email/Phone/Address/Contact con LogValue; actor_id en logs; audit_log sin PII. |
| PP-F1-SEC-008 | SEC | SAST, dependencias, imágenes, SBOM | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-SEC-009 | SEC | Privacidad: consentimiento, derechos, diseño crypto-shredding | MUST | M1.4 | IMPLEMENTADO | | #10 | docs/phase-1/security/privacy-runbook.md (SQL verificado); inventario PII con TestRegistry_PIIInventory; aviso de privacidad en el pie. Hallazgo F5: sujetos de pii{} por email sin UserID. |
| PP-F1-SEC-010 | SEC | Mínimo privilegio en BD, red y contenedores | MUST | M1.4 | IMPLEMENTADO | | #10 | Migración 000007 (writer: UPDATE en es.streams; sin UPDATE/DELETE en events/outbox/audit_log; CREATE revocado a PUBLIC); contenedores no root, read_only, cap_drop ALL; solo Traefik publica 443. |
| PP-F1-SEC-011 | SEC | Threat model F1 revisado | MUST | M1.4 | EN_PROGRESO | | #10 | Threat model STRIDE en docs/phase-1/security/threat-model.md; pendiente revisión de dos personas (checklist §6 y PR). |
| PP-F1-INF-001 | INF | Monorepo, Makefile, pre-commit | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-INF-002 | INF | Docker Compose de desarrollo con seeds | MUST | M1.0 | IMPLEMENTADO | | #6 | `infra/compose/compose.yaml` con `podman compose` (ADR-026); entorno completo ejecutado de punta a punta en Podman 6.1 (E2E de M1.5). |
| PP-F1-INF-003 | INF | CI GitHub Actions | MUST | M1.0 | IMPLEMENTADO | | #6 | Workflows creados; primera ejecución real ocurre en este PR. |
| PP-F1-INF-004 | INF | Imágenes Docker endurecidas | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-INF-005 | INF | Entornos y configuración | MUST | M1.4 | IMPLEMENTADO | | #10 | infra/deploy/staging.env.example (PP_* + plataforma), /config.json por entorno generado en despliegue, .env 600 desde secret manager. |
| PP-F1-INF-006 | INF | Despliegue en staging y runbook | MUST | M1.4 | EN_PROGRESO | | #10 | Unidades Quadlet (infra/quadlet, ADR-026) + deploy.sh (render, -dryrun, pull, migrate oneshot, reinicio solo de lo cambiado con Notify=healthy, smoke PKCE, rollback automático, anotación) + RUNBOOK §14. Ensayado en Podman 6.1/systemd (migrate fallido aborta, idempotencia, rollback). Pendiente: VM real, primer despliegue y ensayo por alguien distinto del autor. |
| PP-F1-INF-007 | INF | Observabilidad: logs, trazas, métricas, dashboards, alertas | MUST | M1.4 | IMPLEMENTADO | | #10 | Dashboards API overview/Event Store/Auth & Authz/Keycloak; 6 alertas con pruebas promtool; Loki+Alloy (journald, sin socket del motor); métricas commands/projection/idempotency añadidas. |
| PP-F1-INF-008 | INF | Health checks y apagado ordenado | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-INF-009 | INF | PostgreSQL y storage gestionados con backups | MUST | M1.5 | IMPLEMENTADO | | #11 | staging.env exige PG gestionado sslmode=verify-full (API, migrator, backup, Keycloak); MinIO/S3 versionado + ILM (30 d no actuales); alertas >80% en RUNBOOK §9.1. Pendiente ops: aprovisionar y registrar simulacro en RESTORE-LOG. |
| PP-F1-QA-001 | QA | Estrategia de pruebas | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-QA-002 | QA | Harness Given/When/Then de dominio | MUST | M1.2 | IMPLEMENTADO | | #8 | shared/testkit Scenario Given/When/Then; todas las invariantes I1–I8 con prueba nombrada.
| PP-F1-QA-003 | QA | Integración con testcontainers, concurrencia y rebuild | MUST | M1.2 | IMPLEMENTADO | | #8 | testcontainers: concurrencia del store (1 ganador por versión), microchip_reservations, rebuild idempotente, flujo HTTP con contrato.
| PP-F1-QA-004 | QA | Pruebas de contrato OpenAPI | MUST | M1.1 | IMPLEMENTADO | | #7 | testkit.Contract (openapi3filter) en pruebas HTTP e integración. |
| PP-F1-QA-005 | QA | E2E Playwright de flujos críticos | MUST | M1.5 | IMPLEMENTADO | | #11 | e2e/ Playwright: 5 flujos críticos con axe; workflow nightly e2e.yml; k6 50 VUs en tests/load (pendiente correr en staging). |
| PP-F1-QA-006 | QA | Umbrales y quality gates | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-QA-007 | QA | Convenciones de trabajo y revisión | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
| PP-F1-QA-008 | QA | Definition of Done | MUST | M1.0 | IMPLEMENTADO | | #6 |  |
