# Fase 1 — Fundación y MVP Core (Onboarding & Identity)

> Estado: v1.0 en revisión · Fecha: 2026-10-20
> Tracking: [`docs/tracking/phase-1.md`](../tracking/phase-1.md)

## 1. Objetivo

Poner en producción (staging público + piloto cerrado) la base sobre la que se construye todo lo demás:

- Identidad y roles reales (Keycloak), onboarding de tutores y organizaciones.
- Perfiles de tutor y mascota, organizaciones (clínicas/farmacias) con miembros, vínculo mascota–clínica.
- **Event Store v0** y el esqueleto CQRS (comandos, eventos, proyecciones síncronas, read models) en Go.
- Frontend Vue con los tres espacios: Tutor, Organización (clínica/farmacia), SuperAdmin.
- Borde seguro (WAF + gateway), CI/CD, observabilidad base y mecanismo de tracking de specs.

Al terminar F1, F2 solo añade contextos y el pipeline asíncrono: **no** reescribe nada.

## 2. Documentos de la fase

| # | Documento | Área |
|---|---|---|
| 01 | [Producto: personas, historias, criterios de aceptación](01-producto.md) | PRD |
| 02 | [Modelo DDD: contextos, agregados, VOs, eventos](02-modelo-ddd.md) | DOM |
| 03 | [Arquitectura: monolito modular, CQRS-ES v0, flujos](03-arquitectura.md) | ARQ |
| 04 | [IAM: Keycloak como código, tokens, autorización](04-iam-keycloak.md) | IAM |
| 05 | [API REST v1](05-api.md) | API |
| 06 | [Datos y persistencia (PostgreSQL)](06-datos-persistencia.md) | DAT |
| 07 | [Frontend Vue](07-frontend.md) | FE |
| 08 | [Seguridad](08-seguridad.md) | SEC |
| 09 | [Infraestructura, DevOps y observabilidad](09-infra-devops-observabilidad.md) | INF |
| 10 | [Calidad y pruebas](10-calidad-y-pruebas.md) | QA |

## 3. Alcance

### Dentro (IN)

- Registro/login/recuperación con email verificado; onboarding con aceptación de términos versionada.
- Perfil de tutor; registro y gestión de mascotas (perfil, microchip único, foto, fallecimiento, transferencia de tutela).
- Registro de organización (clínica o farmacia) → verificación por SuperAdmin → activa; perfil; miembros por invitación con roles; código público.
- Vínculo mascota–clínica concedido por el tutor; lista de pacientes y ficha (solo lectura) en la clínica.
- SuperAdmin: cola de verificación, suspender/reactivar organizaciones, buscar/suspender usuarios.
- Plataforma: Event Store v0, outbox escrito, proyecciones síncronas, idempotencia, problem+json, OpenAPI, i18n es/en.
- Infra: compose dev completo, CI, imágenes, staging con WAF, observabilidad base, backups.

### Fuera (OUT) — y dónde cae

| Fuera de F1 | Fase |
|---|---|
| Historia clínica, vacunas, citas, recordatorios | F2 |
| Solicitud de vínculo iniciada por la clínica; co-tutores | F2 |
| Proyecciones asíncronas, snapshots, rebuild | F2 |
| Búsqueda full-text, Redis, rate limit distribuido | F3 |
| Recetas, OCR, farmacias (más allá de registrar la organización) | F4 |
| Planes/pagos, exportación/eliminación automatizada de datos | F5 |
| App móvil nativa, login social (Google/Apple) | F6 / evaluar en F2 |

## 4. Hitos

| Hito | Nombre | Resultado observable |
|---|---|---|
| M1.0 | Setup | Monorepo, compose dev levanta todo, CI verde en PR vacío, Keycloak como código, esqueleto Go/Vue, tracking operativo. |
| M1.1 | Identity | Registro → verificación → login → `/me` devuelve el `UserAccount` aprovisionado; onboarding y perfil en UI. |
| M1.2 | Pets | Event Store v0 en uso: CRUD de mascota por eventos, proyecciones síncronas, foto, microchip único, UI tutor. |
| M1.3 | Organizations & Links | Registro de organización, verificación admin, invitaciones, roles, vínculo por código, lista de pacientes. |
| M1.4 | Admin & Hardening | Panel SuperAdmin; WAF/gateway en staging; autorización auditada; threat model revisado; escaneos en CI. |
| M1.5 | Release MVP | Staging público con piloto cerrado (≥ 2 clínicas, ≥ 20 tutores); criterios de salida cumplidos. |

## 5. Criterios de salida de la fase

1. Todos los `MUST` de `tracking/phase-1.md` en `VERIFICADO`.
2. E2E de Playwright verde para los cinco flujos críticos: registro tutor, crear mascota, registrar clínica + verificación, invitar miembro, vincular mascota y verla como clínica.
3. Proyecciones reconstruibles desde `es.events` en entorno de prueba (ensayo de F2, aunque sea manual).
4. Sin vulnerabilidades críticas/altas abiertas (SAST, dependencias, imagen).
5. p95 de API < 300 ms en staging con 50 usuarios concurrentes (prueba ligera con k6).
6. Runbook de despliegue y rollback probado una vez por otra persona distinta a quien lo escribió.

## 6. Equipo sugerido y responsabilidades

| Rol | Dedicación | Dueño de |
|---|---|---|
| Backend senior (arquitecto) | 100% | ARQ, DOM, DAT, Event Store v0, revisiones |
| Backend | 100% | IAM backend, API, pruebas de integración |
| Frontend | 100% | FE, E2E |
| Full-stack / DevOps | 50–100% | INF, SEC borde, Keycloak como código |
| PM / QA | 50% | PRD, tracking, verificación en staging |

## 7. Dependencias externas

- Dominio y DNS en el proveedor de WAF; cuenta cloud con PostgreSQL gestionado y object storage.
- Proveedor SMTP transaccional (staging puede usar Mailpit).
- Textos legales (términos y privacidad) revisados antes de M1.5.

## 8. Riesgos específicos de F1

| Riesgo | Mitigación |
|---|---|
| Event Store v0 mal diseñado contamina F2 | Spec detallada (ARQ-004..007), revisión por pares, pruebas de concurrencia desde M1.2. |
| Keycloak configurado a mano, no reproducible | Config como código obligatoria desde M1.0 (IAM-001). |
| Alcance de UI crece | Pantallas listadas y cerradas en FE; cualquier extra es `COULD`. |
| Unicidad de microchip con ES | Tabla de reserva transaccional (DAT-005), no validación por lectura eventual. |
