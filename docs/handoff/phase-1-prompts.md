# Prompts de ejecución — Fase 1 (Fundación y MVP Core)

Cada bloque es un prompt autocontenido para una sesión de agente. Antes de cada uno, carga
`00-agent-operating-instructions.md` como system prompt. Sustituye `<...>` por tus valores.

---

## M1.0 — Setup

```text
Hito: F1 M1.0 Setup. Rama: feat/f1-m1.0-setup desde main.

Lee: /CLAUDE.md, docs/ROADMAP.md, ADR-001, ADR-002, ADR-003, ADR-005, ADR-007, ADR-008,
docs/phase-1/00, 02 (§1–2), 03, 04 (§1–3), 06 (§1–2, §8), 07 (§1), 08 (§2, §7, §9), 09, 10.

IDs del hito (todos deben quedar IMPLEMENTADO o con nota):
DOM-001, DOM-002, ARQ-001, ARQ-002, ARQ-011, IAM-001, IAM-002, IAM-003, API-002, API-009,
DAT-001, DAT-002, DAT-008, FE-001, SEC-002, SEC-006, SEC-008, INF-001, INF-002, INF-003, INF-004, INF-008,
QA-001, QA-006, QA-007, QA-008 (prefijo PP-F1-).

Entregables:
1. Monorepo según ADR-007 y docs/phase-1/09 §1: backend/ (módulo Go 1.25+, layout de docs/phase-1/03 §2 con
   paquetes vacíos pero compilables para identity, partnernetwork, petregistry y platform/*), frontend/ (Vue 3 +
   TS + Vite + Pinia + Router + PrimeVue + Tailwind según 07 §1, con layouts vacíos y página pública), infra/
   (compose, keycloak, traefik), scripts/, Makefile con los objetivos de 09 §1, .pre-commit-config.yaml, .tool-versions.
2. Docker Compose de desarrollo completo (09 §2) con traefik, postgres 17 (extensiones citext, pg_trgm; roles
   pp_migrator/pp_app_writer/pp_app_reader), keycloak 26 + keycloak-config-cli aplicando infra/keycloak/realm.yaml
   (realm petpassport, clients petpassport-web/petpassport-api/petpassport-backend, roles pet_owner y super_admin,
   ajustes de 04 §1–3), minio, mailpit, api (air), frontend (vite), migrate one-shot. `make dev-up` debe levantar todo
   y `make dev-seed` crear los usuarios de 09 §2 por la API de Keycloak.
3. backend/api/openapi.yaml inicial (info, servers, securitySchemes, esquema Problem, rutas /healthz y /readyz
   internas y /api/v1/healthz pública) + generación con oapi-codegen (strict server) y openapi-typescript en `make gen`.
4. Migraciones goose: esquemas es, identity, read_petregistry, read_partnernetwork, platform vacíos con roles y grants
   (DDL completo llega en hitos siguientes salvo platform.schema_migrations). CI: up/down/up.
5. Configuración 12-factor (03 §8) con validación al arranque; arranque y apagado ordenado; /healthz, /readyz, /metrics;
   slog JSON; OTel y Prometheus cableados (sin dashboards aún).
6. CI (09 §3): backend.yml, frontend.yml, api-contract.yml, docs.yml (ejecuta scripts/specs-status.sh --check), images.yml,
   secrets.yml. Lint de arquitectura (depguard o go-arch-lint) con las reglas de ADR-001. golangci-lint, govulncheck,
   gosec, semgrep, trivy, gitleaks. Imágenes multi-stage distroless no root.
7. Traefik dev con TLS local (mkcert), routers app./api./auth., middlewares de cabeceras, buffering, rate limit.
8. Testkit mínimo: helper testcontainers Postgres y servidor JWKS de prueba (docs/phase-1/10 §3), aunque aún no haya
   handlers que los usen; `make test-integration` ejecuta un test que arranca Postgres y aplica migraciones.
9. Documentos: README raíz con "cómo arrancar en 10 minutos"; `docs/phase-1/10` §7 convenciones aplicadas en plantillas.

No implementes aún: agregados, event store, handlers de negocio, pantallas. Deja TODOs enlazados a IDs.

DoD: `make dev-up` + `make dev-seed` funcionan desde cero; CI verde en el PR; login en Keycloak con owner@dev.local
devuelve token con aud petpassport-api y rol pet_owner (verifícalo con curl y adjunta la salida); tracking actualizado.
```

---

## M1.1 — Identity

```text
Hito: F1 M1.1 Identity. Rama: feat/f1-m1.1-identity desde main.

Lee: ADR-003, ADR-005; docs/phase-1/01 (E1, E2), 02 §5, 03 §7, 04 (completo), 05 (§1, §3.1, §4, §5), 06 §4 y §7,
07 (§2–5), 08 (§3, §8), 10.

IDs: PRD-001, PRD-002, PRD-003, PRD-004, PRD-020, DOM-009, ARQ-010, IAM-004, IAM-006, IAM-007, IAM-009, IAM-013,
API-001, API-003, API-010, API-011, DAT-004, DAT-007, FE-002, FE-003, FE-004, FE-005, SEC-003, SEC-007, QA-004.

Entregables:
1. Keycloak: política de contraseñas, verificación de email, recuperación, tiempos de vida (04 §4, §6), flujo browser
   con OTP condicional preparado (la obligatoriedad para super_admin se activa en M1.4), tema mínimo opcional.
2. platform/auth: verificador OIDC (go-oidc), validación según 04 §7, Principal (sin membresías aún: slice vacío),
   middleware; respuestas 401 según spec.
3. identity: UserAccount CRUD (DDL DAT-004 y platform DAT-007), EnsureUserAccount JIT con throttle, onboarding,
   consentimiento versionado (identity.consent_versions con semilla), UpdateProfile; audit_log para cambios.
4. platform/problem con el catálogo de errores de API-011 (los códigos de otros hitos pueden quedar registrados sin uso).
5. Endpoints API-003 y /legal/current en openapi.yaml con validación de payloads (API-010) y pruebas de contrato (QA-004).
6. Frontend: oidc-client-ts (07 §2), cliente API tipado con manejo de problem+json (07 §3), rutas/guards/layouts
   (07 §4), pantallas de onboarding y perfil (07 §5 FE-005), páginas legales públicas.
7. CORS y cabeceras de seguridad (08 §4) en Traefik y API; logging sin PII con slog.LogValuer en Email/Phone.
8. Pruebas: integración HTTP con JWKS de prueba; E2E mínimo Playwright: registro → verificación por Mailpit →
   onboarding → /me (puede quedar en nightly).

DoD: flujo PRD-001..004 y 020 demostrable en dev; cobertura app ≥ 80 % en identity; tracking actualizado.
```

---

## M1.2 — Pets (Event Store v0)

```text
Hito: F1 M1.2 Pets. Rama: feat/f1-m1.2-pets desde main. Este hito es el cimiento CQRS-ES: máxima atención.

Lee: ADR-001, ADR-002, ADR-004, ADR-007; docs/phase-1/01 (E3), 02 (§2–3, §6, §8–9), 03 (completo), 05 (§3.2), 06 (§3, §5),
07 (§5 FE-006, §7), 08 (§5, §6 SEC-004), 10 (§2–3).

IDs: PRD-005, PRD-006, PRD-007, PRD-008, PRD-009, DOM-003, DOM-004, DOM-005, DOM-010, DOM-012, ARQ-003, ARQ-004,
ARQ-005, ARQ-006, ARQ-007, ARQ-008, ARQ-009, ARQ-012, IAM-010, API-004, DAT-003, DAT-005, FE-006, FE-011, SEC-004,
QA-002, QA-003.

Entregables, en este orden:
1. platform/eventstore: DDL DAT-003 (es.streams, es.events con trigger append-only, es.outbox), Store con Append/Load/
   ReadAll y semántica exacta de 03 §4 (UPDATE es.streams con expected version → ErrConcurrency), Registry de tipos
   con schema_version, metadatos DOM-012, serialización, interfaz Upcaster (sin implementaciones).
2. platform/bus + db.UnitOfWork + projection.Runner síncrono + outbox.Enqueue + idempotency (ARQ-008, tabla DAT-007).
3. platform/authz: Can(principal, action, resource) deny-by-default con la tabla de IAM-010 (filas de pets);
   instrumentación para el test de cobertura de rutas (SEC-005 se completa en M1.3, pero el mecanismo nace aquí).
4. petregistry/domain: agregado Pet (estado, comandos, eventos v1, invariantes I1–I8 con una prueba por invariante
   usando el harness Given/When/Then de testkit — créalo: 10 §2), Value Objects DOM-004, golden tests de eventos.
5. petregistry/infra: repositorio sobre eventstore, proyector read_petregistry.pets (+ microchip_reservations en la misma
   tx, pet_transfers), queries sqlc, adaptador GuardianLookup sobre identity. Prueba de rebuild (proyectar dos veces).
6. petregistry/http: endpoints API-004 (sin transferencia: PRD-010 es M1.3; sin care-links), If-Match/ETag, Idempotency-Key,
   Location, foto con pipeline de imagen (ARQ-012, SEC-004) sobre MinIO con URLs prefirmadas.
7. Frontend: pantallas de mascotas FE-006 (sin vínculos ni transferencia), validación zod espejo de VOs con fixture
   docs/phase-1/fixtures/validation-cases.json generado desde Go (FE-011).
8. Pruebas de concurrencia del Event Store (QA-003): N goroutines sobre el mismo stream → una gana por versión; y sobre
   microchip_reservations.

Decisiones que no debes reinterpretar: proyecciones síncronas en la misma tx (ADR-002), unicidad por reserva (DAT-005),
404 para no visibles (03 §7).

DoD: PRD-005..009 demostrables; todas las invariantes I1–I8 con prueba nombrada; cobertura domain ≥ 90 %; test de
concurrencia verde; `events_appended_total` y `eventstore_concurrency_conflicts_total` expuestas; tracking actualizado.
```

---

## M1.3 — Organizations & Links

```text
Hito: F1 M1.3 Organizations & Links. Rama: feat/f1-m1.3-orgs desde main.

Lee: ADR-003, ADR-004; docs/phase-1/01 (E3 PRD-010, E4, E5), 02 (§4, §6–7), 04 §8 (IAM-008), 05 (§3.2 transferencia,
§3.3, §3.4, §3.5), 06 §6, 07 §5 (FE-007, FE-008), 08 §6.

IDs: PRD-010, PRD-011, PRD-012, PRD-013, PRD-014, PRD-015, PRD-016, DOM-006, DOM-007, DOM-008, DOM-011, IAM-008,
API-005, API-006, API-007, DAT-006, FE-007, FE-008, SEC-005.

Entregables:
1. partnernetwork/domain: agregado Organization (comandos, eventos v1, invariantes O1–O5 con prueba por invariante),
   VOs DOM-007 (TaxID con validador CO NIT + genérico, PublicCode Crockford, InvitationToken hash), reserva de public_code.
2. Proyectores read_partnernetwork (organizations, org_members, invitations) y queries; adaptador UserLookup.
3. Principal con membresías (IAM-008): carga desde read model, caché LRU 60 s, invalidación por eventos (DOM-011).
4. Pet: GrantCareLink/RevokeCareLink (I5, I8) con puerto OrganizationLookup; transferencia de tutela (PRD-010) con
   tokens y read_petregistry.pet_transfers; reacción OrganizationSuspended → org_suspended en pet_care_links.
5. Endpoints API-005, API-006, API-007; lista de pacientes de clínica con búsqueda simple y cursor.
6. Suite de autorización exhaustiva (SEC-005): acciones × roles × relación (tutor / vinculado / no vinculado / org
   suspendida) con códigos esperados; test que falla si un handler no llama a authz.Can.
7. Emails síncronos best-effort (DOM-011) por SMTP a Mailpit: invitación, transferencia, verificación de organización.
   (Se migrarán al process manager en F2; mantenlos en una interfaz Mailer aislada.)
8. Frontend FE-007 y FE-008 (registro de organización, resumen con código, miembros/invitaciones, ajustes, aceptar
   invitación; vínculos en el detalle de mascota; pacientes y ficha en clínica).

DoD: flujos PRD-010..016 demostrables con dos usuarios; suite de authz al 100 % de acciones existentes; invariantes O1–O5
probadas; tracking actualizado.
```

---

## M1.4 — Admin & Hardening

```text
Hito: F1 M1.4 Admin & Hardening. Rama: feat/f1-m1.4-admin-hardening desde main.
Prerrequisito humano: dominio, cuenta cloud y proveedor WAF decididos (docs/handoff/README.md §4).

Lee: ADR-006, ADR-008; docs/phase-1/01 (E6), 04 (§5, §9), 05 §3.6, 07 §5 (FE-009), 08 (completo), 09 (§5–7).

IDs: PRD-017, PRD-018, IAM-005, IAM-011, API-008, FE-009, SEC-001, SEC-009, SEC-010, SEC-011, INF-005, INF-006, INF-007.

Entregables:
1. Endpoints /admin (API-008) con motivo obligatorio y audit_log; cola de verificación; suspensión/reactivación de
   organizaciones y usuarios; service account Keycloak (IAM-011) para SetEnabled/LogoutAllSessions/SendVerifyEmail.
2. Keycloak: brute force y OTP obligatoria para super_admin (IAM-005).
3. Frontend admin FE-009 (organizaciones, usuarios, auditoría).
4. Staging: VM con compose, deploy.sh idempotente con migraciones, smoke y rollback (INF-006); WAF gestionado con reglas
   OWASP en modo log → bloqueo, rate limits de SEC-001, allowlist de origen; consola de Keycloak restringida.
5. Observabilidad (INF-007): dashboards API overview, Event Store, Auth & Authz, Keycloak; alertas mínimas; runbook inicial.
6. Mínimo privilegio y red (SEC-010); threat model F1 documentado en docs/phase-1/security/threat-model.md (SEC-011) y
   revisado por dos personas (deja la revisión como checklist en el PR).
7. Privacidad (SEC-009): runbook manual de exportación/eliminación; Registry marca PIISubjects por evento (ya existe
   desde M1.2 — verifica y documenta); aviso de privacidad enlazado.

DoD: staging público accesible solo vía WAF; smoke verde; admin puede verificar una organización desde la UI; tracking actualizado.
```

---

## M1.5 — Release MVP

```text
Hito: F1 M1.5 Release MVP. Rama: feat/f1-m1.5-release desde main.
Prerrequisitos humanos: textos legales revisados; proveedor SMTP; PostgreSQL y storage gestionados aprovisionados.

Lee: docs/phase-1/00 §5 (criterios de salida), 01 (E7), 04 §10, 06 §9, 07 §6 y §8, 09 §9, 10 (§5–6).

IDs: PRD-019, IAM-012, DAT-009, FE-010, FE-012, INF-009, QA-005.

Entregables:
1. i18n es/en completo, accesibilidad AA en formularios y navegación, responsive verificado (FE-010); tema de login y
   emails de Keycloak en es/en con SMTP real y SPF/DKIM/DMARC (IAM-012).
2. E2E Playwright de los cinco flujos críticos (QA-005) + axe en pantallas principales; workflow e2e.yml nightly.
3. Backups y retención (DAT-009, INF-009): PITR, exportación semanal de es.events a storage, prueba de restauración
   documentada en infra/deploy/RESTORE-LOG.md.
4. k6 ligero (50 VUs) con p95 < 300 ms en staging; informe en el PR.
5. Observabilidad en cliente (FE-012) si hay capacidad (COULD).
6. Ejecuta los criterios de salida de docs/phase-1/00 §5 uno a uno y adjunta evidencia; crea el tag v0.1.0 cuando
   el humano apruebe.

DoD: criterios de salida 1–6 con evidencia; `scripts/specs-status.sh --phase 1` muestra todos los MUST en IMPLEMENTADO
(VERIFICADO lo pone QA); tracking actualizado.
```
