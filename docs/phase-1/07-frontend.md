# F1 · 07 — Frontend (Vue 3)

## 1. Stack y scaffold

**[PP-F1-FE-001] (MUST)**

| Pieza | Elección | Motivo |
|---|---|---|
| Framework | Vue 3.5 + `<script setup lang="ts">` + Vite 7 | Composition API, tipado estricto (`strict: true`). |
| Estado | Pinia (stores por feature) | |
| Router | Vue Router 4 con guards tipados | |
| UI | PrimeVue 4 (tema Aura) + Tailwind CSS 4 para layout/utilidades | DataTable, formularios y accesibilidad resueltos. |
| Formularios | vee-validate + zod | Esquemas reutilizables que espejan reglas de dominio. |
| Auth | `oidc-client-ts` | OIDC estándar, PKCE, renovación silenciosa, almacenamiento en memoria. |
| HTTP | `openapi-fetch` + tipos de `openapi-typescript` | Cliente tipado desde el contrato (ADR-005). |
| i18n | `vue-i18n` (es, en) | |
| Pruebas | Vitest + Testing Library (unit), Playwright (E2E) | |
| Calidad | ESLint (vue, ts, security), Prettier, `vue-tsc --noEmit` | |

Estructura por *features*:

```
frontend/src/
  app/            # main.ts, router, i18n, plugins, layouts (PublicLayout, OwnerLayout, OrgLayout, AdminLayout)
  shared/         # api client, auth, ui components, composables, utils, types generados (api.d.ts)
  features/
    auth/         # callback, session store, guards
    onboarding/
    profile/
    pets/         # list, detail, form, photo, microchip, deceased, transfer, care-links
    organizations/# register, settings, members, invitations, accept-invitation
    patients/     # clinic patient list & detail
    admin/        # organizations queue, users, audit log
    legal/
```

## 2. Autenticación en la SPA

**[PP-F1-FE-002] (MUST)**

- `UserManager` de `oidc-client-ts` con `response_type=code`, PKCE, `scope: openid profile email`, `userStore: InMemoryWebStorage`, `automaticSilentRenew: true` (refresh token), `monitorSession: false` (sin iframe de sesión; evita problemas con cookies de terceros).
- Rutas: `/auth/callback` (procesa `signinRedirectCallback`), `/auth/logout` (callback post-logout).
- `sessionStore` (Pinia): `user`, `accessToken` (getter), `isAuthenticated`, `me` (resultado de `GET /me`), `login(returnTo)`, `logout()`. Tras login se guarda `returnTo` en `sessionStorage` y se restaura.
- Al expirar la renovación (`silentRenewError`) se muestra aviso y se vuelve al login manteniendo `returnTo`.
- El token **nunca** se guarda en `localStorage`; tras recarga completa se exige nuevo *redirect* (Keycloak mantiene SSO, así que es transparente).

## 3. Cliente API y errores

**[PP-F1-FE-003] (MUST)**

- `shared/api/client.ts`: `createClient<paths>({ baseUrl })` con *middleware* que añade `Authorization`, `X-Correlation-ID` (UUID por sesión de pestaña + sufijo por request) e `Idempotency-Key` (generada por el *composable* `useCommand` y conservada en reintentos del mismo intento de usuario).
- Manejo de `problem+json`: `ApiError{code, status, detail, errors[]}`; mapeo a mensajes i18n por `code`; `errors[].field` se vuelca a vee-validate (`setFieldError`).
- `401` → forzar re-login conservando ruta. `403 ONBOARDING_REQUIRED` → `/onboarding`. `403 CONSENT_VERSION_OUTDATED` → modal de aceptación. `409 CONFLICT_VERSION` → diálogo "Recargar y reintentar". `429` → *toast* con `Retry-After`.
- Reintento automático solo en `GET` idempotentes (1 reintento ante red/503).

## 4. Rutas, guards y layouts

**[PP-F1-FE-004] (MUST)**

| Ruta | Layout | Guard |
|---|---|---|
| `/`, `/legal/terms`, `/legal/privacy` | Public | — |
| `/auth/callback`, `/auth/logout` | Public | — |
| `/onboarding` | Public (autenticado) | `requiresAuth` |
| `/app` (dashboard tutor), `/app/pets/new`, `/app/pets/:petId`, `/app/pets/:petId/edit`, `/app/profile` | Owner | `requiresAuth`, `requiresOnboarding` |
| `/app/transfers/:token` | Owner | `requiresAuth` |
| `/invitations/:token` | Public (autenticado) | `requiresAuth` |
| `/org/new` | Owner | `requiresAuth`, `requiresOnboarding` |
| `/org/:orgId` (resumen), `/org/:orgId/patients`, `/org/:orgId/patients/:petId`, `/org/:orgId/members`, `/org/:orgId/settings` | Org | `requiresMembership(orgId)`; `patients/*` además `orgType === clinic` y rol ∈ {clinic_admin, veterinarian, receptionist}; `members`/`settings` rol admin |
| `/admin`, `/admin/organizations`, `/admin/users`, `/admin/audit` | Admin | `requiresGlobalRole('super_admin')` |
| `/403`, `/404` | Public | — |

- Los guards leen `sessionStore.me` (memberships, roles); si `me` no está cargado, lo cargan antes de resolver. La UI nunca es la autoridad: el backend repite la decisión.
- Selector de contexto en la barra superior: "Mis mascotas" + una entrada por organización + "Administración" si aplica.

## 5. Pantallas

**[PP-F1-FE-005] (MUST)** Onboarding y perfil.

- *Onboarding* (3 pasos): consentimiento (enlaces a términos/privacidad, checkbox por cada uno, versión visible) → datos básicos (nombre, país, idioma, teléfono opcional) → elección "Soy tutor" / "Registrar organización" (puede saltarse: va a `/app`).
- *Perfil*: formulario con los campos de `PATCH /me`; email de solo lectura con enlace a "Gestionar cuenta" (Keycloak account console); sección "Seguridad" con enlace a configurar OTP; selector de idioma aplica al instante.

**[PP-F1-FE-006] (MUST)** Mascotas.

- *Lista*: tarjetas con foto/miniatura, nombre, especie, edad, badges (vínculos activos, fallecida en sección "En memoria"); filtro activas/en memoria; CTA "Añadir mascota".
- *Formulario* (crear/editar): nombre, especie (select con "Otra" → campo), raza (texto), sexo, esterilizado, fecha de nacimiento con checkbox "aproximada" (*datepicker* limitado a ≤ hoy), señas; validación zod espejo de VOs (FE-011). En edición envía `If-Match`.
- *Detalle*: cabecera con foto (subir/cambiar/quitar con recorte cuadrado), datos, microchip (editar con aviso de "una sola corrección"), vínculos (lista con estado y botón revocar; CTA "Vincular con clínica" → modal de código con previsualización `OrgSummary`), transferencia (modal con email; estado pendiente con cancelar), acción "Marcar como fallecida" (confirmación, fecha opcional) y "Deshacer" si < 24 h.
- *Aceptar transferencia* (`/app/transfers/:token`): previsualización y botón aceptar; errores 404 como "enlace inválido o caducado".

**[PP-F1-FE-007] (MUST)** Organizaciones.

- *Registro*: wizard (tipo → datos legales y fiscales con validación por país → dirección → contacto → revisión); al crear muestra el `PublicCode` y el estado "Pendiente de verificación" con explicación.
- *Resumen* (`/org/:orgId`): estado (banner si pendiente/suspendida/rechazada con motivo), código público con botón copiar y tarjeta imprimible (QR en F2), accesos rápidos.
- *Miembros*: tabla (nombre, email, rol, desde), cambiar rol (select con roles válidos por tipo), retirar (confirmación; deshabilitado si último admin); pestaña invitaciones (crear, reenviar, revocar, estados, caducidad).
- *Ajustes*: edición del perfil; aviso de que cambiar razón social/identificación fiscal reinicia la verificación.
- *Aceptar invitación* (`/invitations/:token`): previsualización (organización, rol, email enmascarado); si el email de sesión no coincide, mensaje claro y botón "Cambiar de cuenta" (logout + login).

**[PP-F1-FE-008] (MUST)** Pacientes (clínica) y vínculos (tutor).

- *Lista de pacientes*: DataTable con búsqueda (`q`), paginación por cursor, columnas: foto, mascota, especie/raza, edad, tutor (nombre, teléfono con `tel:`), vinculado desde; estado vacío con instrucciones ("Comparte tu código `7K3MQ2` con tus clientes").
- *Ficha de paciente*: solo lectura; sección "Historia clínica" como *placeholder* "Disponible próximamente" (F2); datos de contacto del tutor visibles.
- Si la organización está suspendida, la sección muestra aviso y no consulta la API.

**[PP-F1-FE-009] (MUST)** Administración.

- *Organizaciones*: pestañas por estado; detalle lateral con datos legales, fundador, miembros; acciones verificar/rechazar/suspender/reactivar con modal de motivo (≥ 10 caracteres).
- *Usuarios*: búsqueda, tabla, acciones suspender/reactivar con motivo; indicador de roles globales y membresías.
- *Auditoría*: tabla filtrable por actor/objetivo/fechas.

## 6. i18n, accesibilidad y responsive

**[PP-F1-FE-010] (SHOULD)**

- Todos los textos en `locales/es.json`, `locales/en.json`; claves por feature; pluralización y fechas con `Intl`.
- Idioma: `me.locale` > navegador > `es`. El enlace a Keycloak añade `ui_locales`.
- Accesibilidad: foco visible, `aria-*` en componentes personalizados, contraste AA, navegación por teclado en modales y tablas, mensajes de error asociados a campos (`aria-describedby`), `lang` dinámico en `<html>`.
- Responsive: *mobile-first* desde 360 px; tablas se convierten en tarjetas < 768 px; áreas táctiles ≥ 44 px.

## 7. Validación en formularios

**[PP-F1-FE-011] (MUST)** `shared/validation/` exporta esquemas zod equivalentes a los VOs de F1 (`petName`, `microchip`, `birthDate`, `taxId(country)`, `phoneE164`, `email`, `publicCode`) con tests que ejecutan los mismos casos que las pruebas de dominio (fixture compartido `docs/phase-1/fixtures/validation-cases.json`, generado desde Go). La validación cliente mejora UX; la autoridad es el servidor.

## 8. Observabilidad en cliente

**[PP-F1-FE-012] (COULD)**

- Captura de errores no controlados y rechazos de promesas → endpoint propio `POST /api/v1/client-events` (F2) o proveedor (Sentry) con *scrubbing* de PII.
- Web Vitals (LCP, INP, CLS) enviados con muestreo 10%.

## 9. Build y despliegue

- `vite build` → estáticos servidos por Traefik (contenedor `nginx:alpine` o `caddy`) con `Cache-Control: immutable` para *hashed assets* y `no-cache` para `index.html`.
- Configuración en runtime vía `/config.json` (issuer, client id, API base) para usar la misma imagen en todos los entornos.
- CSP estricta (SEC-003) compatible con PrimeVue (sin `unsafe-inline` en scripts; estilos con *nonce* o `style-src 'self' 'unsafe-inline'` justificado y documentado).
