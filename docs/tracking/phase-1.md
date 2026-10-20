# Tracking — Fase 1: Fundación y MVP Core

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F1-PRD-001 | PRD | Registro con email y contraseña, verificación obligatoria | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-PRD-002 | PRD | Login, logout y recuperación de contraseña | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-PRD-003 | PRD | Onboarding: consentimiento, datos básicos, elección de camino | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-PRD-004 | PRD | Ver y editar perfil de tutor | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-PRD-005 | PRD | Registrar mascota | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-PRD-006 | PRD | Ver y editar perfil de mascota con control de versión | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-PRD-007 | PRD | Microchip único con una corrección | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-PRD-008 | PRD | Foto de mascota | SHOULD | M1.2 | PENDIENTE | | | |
| PP-F1-PRD-009 | PRD | Marcar fallecida / deshacer 24 h | SHOULD | M1.2 | PENDIENTE | | | |
| PP-F1-PRD-010 | PRD | Transferencia de tutela con aceptación | SHOULD | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-011 | PRD | Registro de organización → pendiente de verificación | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-012 | PRD | Editar perfil de organización (reinicio de verificación) | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-013 | PRD | Invitar miembros y aceptar invitación | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-014 | PRD | Cambiar rol / retirar miembro (último admin protegido) | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-015 | PRD | Vincular mascota a clínica por código y revocar | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-016 | PRD | Lista de pacientes y ficha (clínica, solo lectura) | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-PRD-017 | PRD | SuperAdmin: verificación, rechazo, suspensión de organizaciones | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-PRD-018 | PRD | SuperAdmin: buscar y suspender usuarios | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-PRD-019 | PRD | i18n es/en, responsive, accesibilidad | SHOULD | M1.5 | PENDIENTE | | | |
| PP-F1-PRD-020 | PRD | Páginas legales y consentimiento versionado | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-DOM-001 | DOM | Glosario / lenguaje ubicuo aplicado en código | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-DOM-002 | DOM | Context map F1 como módulos aislados | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-DOM-003 | DOM | Agregado Pet: estado, comandos, invariantes I1–I8 | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DOM-004 | DOM | Value Objects de Pet Registry | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DOM-005 | DOM | Eventos Pet Registry v1 | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DOM-006 | DOM | Agregado Organization: estado, comandos, invariantes O1–O5 | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-DOM-007 | DOM | Value Objects de Partner Network | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-DOM-008 | DOM | Eventos Partner Network v1 | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-DOM-009 | DOM | Identity: UserAccount CRUD y ACL Keycloak | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-DOM-010 | DOM | Puertos entre contextos | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DOM-011 | DOM | Políticas/reacciones F1 (suspensión, emails) | SHOULD | M1.3 | PENDIENTE | | | |
| PP-F1-DOM-012 | DOM | Metadatos de eventos | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-001 | ARQ | Layout de paquetes y reglas de dependencia en CI | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-ARQ-002 | ARQ | Hexagonal por módulo | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-ARQ-003 | ARQ | Command bus + Unit of Work | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-004 | ARQ | Event Store v0: Append/Load/ReadAll, concurrencia optimista | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-005 | ARQ | Proyecciones síncronas e idempotentes | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-006 | ARQ | Outbox escrito (consumo en F2) | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-007 | ARQ | Registry de eventos, serialización y versionado | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-008 | ARQ | Idempotencia de comandos | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-009 | ARQ | Lado de consulta sobre read models con filtros por principal | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-ARQ-010 | ARQ | Modelo de errores y mapeo a problem+json | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-ARQ-011 | ARQ | Configuración 12-factor, arranque y apagado | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-ARQ-012 | ARQ | Object storage e imágenes | SHOULD | M1.2 | PENDIENTE | | | |
| PP-F1-IAM-001 | IAM | Realm como código (keycloak-config-cli) | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-IAM-002 | IAM | Clients web/api/backend | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-IAM-003 | IAM | Roles de realm y rol por defecto | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-IAM-004 | IAM | Registro, verificación y política de contraseñas | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-IAM-005 | IAM | Brute force y OTP obligatoria para super_admin | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-IAM-006 | IAM | Tiempos de vida de tokens y sesiones | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-IAM-007 | IAM | Validación JWT en backend | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-IAM-008 | IAM | Principal y membresías con caché | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-IAM-009 | IAM | Aprovisionamiento JIT de UserAccount | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-IAM-010 | IAM | Modelo de autorización (paquete authz) | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-IAM-011 | IAM | Service account para operaciones admin | SHOULD | M1.4 | PENDIENTE | | | |
| PP-F1-IAM-012 | IAM | Tema de login, SMTP y localización | SHOULD | M1.5 | PENDIENTE | | | |
| PP-F1-IAM-013 | IAM | Cierre de sesión | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-API-001 | API | Convenciones REST | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-API-002 | API | OpenAPI 3.1 fuente + generación + oasdiff | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-API-003 | API | Endpoints /me, onboarding, consentimiento, legal | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-API-004 | API | Endpoints de mascotas | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-API-005 | API | Endpoints de vínculos y pacientes | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-API-006 | API | Endpoints de organizaciones y miembros | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-API-007 | API | Endpoints de invitaciones | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-API-008 | API | Endpoints de administración | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-API-009 | API | Salud y métricas | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-API-010 | API | Validación de payloads | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-API-011 | API | Catálogo de errores | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-DAT-001 | DAT | Esquemas y roles de BD | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-DAT-002 | DAT | Migraciones goose + CI | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-DAT-003 | DAT | DDL Event Store + trigger append-only | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DAT-004 | DAT | DDL identity | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-DAT-005 | DAT | DDL read_petregistry + reservas | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-DAT-006 | DAT | DDL read_partnernetwork + reservas | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-DAT-007 | DAT | DDL platform (idempotencia, auditoría, checkpoints) | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-DAT-008 | DAT | Convenciones de datos | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-DAT-009 | DAT | Backups y retención | MUST | M1.5 | PENDIENTE | | | |
| PP-F1-FE-001 | FE | Scaffold Vue 3 + stack | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-FE-002 | FE | Autenticación OIDC PKCE en SPA | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-FE-003 | FE | Cliente API tipado y manejo de errores | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-FE-004 | FE | Rutas, guards y layouts | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-FE-005 | FE | Onboarding y perfil | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-FE-006 | FE | Pantallas de mascotas | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-FE-007 | FE | Pantallas de organización | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-FE-008 | FE | Pacientes (clínica) y vínculos (tutor) | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-FE-009 | FE | Administración | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-FE-010 | FE | i18n, accesibilidad y responsive | SHOULD | M1.5 | PENDIENTE | | | |
| PP-F1-FE-011 | FE | Validación zod espejo de VOs con fixture compartido | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-FE-012 | FE | Observabilidad en cliente | COULD | M1.5 | PENDIENTE | | | |
| PP-F1-SEC-001 | SEC | WAF gestionado y rate limiting en el borde | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-SEC-002 | SEC | Traefik: routers, middlewares, timeouts | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-SEC-003 | SEC | Cabeceras de seguridad y CORS | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-SEC-004 | SEC | Validación de entrada y subidas seguras | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-SEC-005 | SEC | Autorización deny-by-default con suite exhaustiva | MUST | M1.3 | PENDIENTE | | | |
| PP-F1-SEC-006 | SEC | Gestión de secretos | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-SEC-007 | SEC | Logging sin PII, correlación y audit log | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-SEC-008 | SEC | SAST, dependencias, imágenes, SBOM | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-SEC-009 | SEC | Privacidad: consentimiento, derechos, diseño crypto-shredding | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-SEC-010 | SEC | Mínimo privilegio en BD, red y contenedores | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-SEC-011 | SEC | Threat model F1 revisado | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-INF-001 | INF | Monorepo, Makefile, pre-commit | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-INF-002 | INF | Docker Compose de desarrollo con seeds | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-INF-003 | INF | CI GitHub Actions | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-INF-004 | INF | Imágenes Docker endurecidas | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-INF-005 | INF | Entornos y configuración | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-INF-006 | INF | Despliegue en staging y runbook | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-INF-007 | INF | Observabilidad: logs, trazas, métricas, dashboards, alertas | MUST | M1.4 | PENDIENTE | | | |
| PP-F1-INF-008 | INF | Health checks y apagado ordenado | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-INF-009 | INF | PostgreSQL y storage gestionados con backups | MUST | M1.5 | PENDIENTE | | | |
| PP-F1-QA-001 | QA | Estrategia de pruebas | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-QA-002 | QA | Harness Given/When/Then de dominio | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-QA-003 | QA | Integración con testcontainers, concurrencia y rebuild | MUST | M1.2 | PENDIENTE | | | |
| PP-F1-QA-004 | QA | Pruebas de contrato OpenAPI | MUST | M1.1 | PENDIENTE | | | |
| PP-F1-QA-005 | QA | E2E Playwright de flujos críticos | MUST | M1.5 | PENDIENTE | | | |
| PP-F1-QA-006 | QA | Umbrales y quality gates | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-QA-007 | QA | Convenciones de trabajo y revisión | MUST | M1.0 | PENDIENTE | | | |
| PP-F1-QA-008 | QA | Definition of Done | MUST | M1.0 | PENDIENTE | | | |
