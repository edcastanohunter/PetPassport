# Threat model F1 (PP-F1-SEC-011)

- Estado: **borrador para revisión** (requiere revisión de dos personas, ver §6)
- Alcance: F1 completo en staging (M1.0–M1.4): SPA, WAF, gateway, API, Keycloak, datos y operación.
- Método: STRIDE por componente sobre el diagrama de §1. Cada control apunta a su ID de spec y a la
  evidencia en el repo; los riesgos residuales tienen dueño y fase.
- Marco: ADR-006 (defensa en profundidad), docs/phase-1/08-seguridad.md.

## 1. Diagrama y fronteras de confianza

```
                Internet (no confiable)
                        │  HTTPS (TLS 1.2+)
            ┌───────────▼────────────┐   TB1: borde
            │ Cloudflare WAF/CDN     │   OWASP CRS, rate limit, bot, DDoS,
            │ app. / api. / auth.    │   inyecta X-Origin-Auth
            └───────────┬────────────┘
                        │  HTTPS (Full strict, cert de origen)
  ┌─────────────────────▼──────────────────────────────────── VM staging ─┐
  │  TB2: origen   firewall: 443 solo desde rangos Cloudflare             │
  │  ┌──────────────┐                                                     │
  │  │ Traefik v3   │ exige X-Origin-Auth, headers, límites, allowlist    │
  │  └──┬─────┬──┬──┘                                                     │
  │     │     │  └──────────────┐                                         │
  │  ┌──▼──┐ ┌▼────────────┐ ┌──▼───────┐   TB3: aplicación               │
  │  │ SPA │ │ API Go      │ │ Keycloak │   JWT + authz.Can por request   │
  │  │nginx│ │ (pp_app_*)  │ │ 26       │                                 │
  │  └─────┘ └──┬────┬─────┘ └────┬─────┘                                 │
  │             │    │ Admin API (service account, red interna)           │
  │  Observabilidad (Prometheus, Loki, Grafana: solo 127.0.0.1 / SSH)     │
  └─────────────┼────┼──────────────────────────────────────────────────── ┘
       TB4:     │    │
  ┌─────────────▼┐ ┌─▼──────────────┐ ┌──────────────┐
  │ PostgreSQL   │ │ Object storage │ │ SMTP transac.│   servicios gestionados
  │ gestionado   │ │ (S3)           │ │              │   (allowlist de la VM)
  └──────────────┘ └────────────────┘ └──────────────┘
```

Activos: datos de tutores (email, teléfono, dirección, contacto de emergencia), datos de mascotas e
historia (event store), membresías y roles, credenciales (Keycloak), secretos de despliegue,
`audit_log`.

Actores de amenaza: anónimo en Internet, usuario autenticado malicioso (tutor o miembro de clínica),
miembro retirado/suspendido, super_admin comprometido, insider con acceso a la VM, dependencia
comprometida (cadena de suministro).

## 2. STRIDE por componente

Leyenda: **S**poofing · **T**ampering · **R**epudiation · **I**nformation disclosure · **D**enial of
service · **E**levation of privilege. "Residual" = riesgo aceptado en F1 con dueño/fase.

### 2.1 SPA (navegador)

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Phishing de login / suplantación de la app | Login solo en `auth.<dominio>` (Keycloak), PKCE S256, `redirect_uri` exacto | `infra/keycloak/realm.yaml` (petpassport-web) |
| T | XSS que manipula la UI o roba tokens | CSP sin `unsafe-inline` en scripts, sin `v-html`/`innerHTML`, Vue escapa por defecto | `infra/traefik/staging/routes.yml` (security-headers), SEC-003 |
| I | Robo de tokens desde storage | Tokens solo en memoria (`oidc-client-ts` sin `localStorage`) | `frontend/src/shared/auth/userManager.ts` |
| I | Open redirect tras login | `returnTo` solo rutas internas | `frontend/src/app/router/guards.ts` (`returnToFrom`) |
| E | Acceso a vistas admin manipulando el router | La UI no es la autoridad: cada request se autoriza en backend (`admin:*` → 403) | `cmd/api/authz_suite_test.go` |

Residual: extensiones del navegador con acceso a la página (fuera de control); CSP report-only no
implementado (F2, dueño: frontend).

### 2.2 Borde: Cloudflare WAF

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Bypass del WAF atacando la IP de origen | Firewall de la VM solo admite rangos de Cloudflare; Traefik exige `X-Origin-Auth` (404 sin ella) | `infra/deploy/RUNBOOK.md` §1, `routes.yml`, smoke paso 4 |
| T | Inyección (SQLi, XSS, path traversal) | OWASP CRS PL1 + Cloudflare Managed (log 1 semana → block) | `infra/waf/cloudflare/main.tf` |
| D | Fuerza bruta de login / enumeración de códigos | Rate limit borde: login/token 10/min/IP, lookup 20/min/IP; respaldo en origen para todos los límites de SEC-001 | `main.tf` (`rate_limits`), `routes.yml` (`rl-*`) |
| D | DDoS volumétrico | Mitigación DDoS del proveedor | ADR-006 |
| E | Acceso a consola de Keycloak / realm master | Bloqueo en borde fuera de `team_ip_ranges` + allowlist en Traefik | `main.tf` (custom), `routes.yml` (`keycloak-admin`) |

Residual: el plan Pro limita las reglas de rate limit en borde a 2; los límites de API se aplican
en el origen (Traefik) por `CF-Connecting-IP`. El secreto `X-Origin-Auth` vive en el estado de
Terraform (backend remoto cifrado, RUNBOOK §6). Dueño: plataforma.

### 2.3 Gateway: Traefik (origen)

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Falsificar IP de cliente con `X-Forwarded-For` | `forwardedHeaders.trustedIPs` = rangos Cloudflare; allowlist con `depth: 1`; rate limit por `CF-Connecting-IP` | `docker-compose.staging.yml`, prueba manual en PR (spoof → 403) |
| T | Request smuggling / cuerpos enormes | `buffering` 6 MiB, timeouts de entrypoint y forwarding | `routes.yml`, SEC-002 |
| I | Fuga por dashboard o logs de acceso | Dashboard deshabilitado; access log JSON con cabeceras en `drop` (salvo User-Agent, CF-Ray) | `docker-compose.staging.yml` |
| I | Exposición de rutas internas de Keycloak (`/metrics`, `/health`) | Solo `/realms/petpassport/*` y `/resources/*` se enrutan | `routes.yml` (404 verificado) |
| D | Agotamiento por conexiones lentas | `inFlightReq` 100 por IP, `idleTimeout` 180 s | `routes.yml` |

### 2.4 API (Go)

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Token falsificado / de otro cliente | JWKS, `iss`, `aud=petpassport-api`, `azp` permitido, `alg` fijo | `internal/platform/auth`, IAM-006 |
| T | Modificar historia clínica | Event store append-only (trigger) + rol writer sin UPDATE/DELETE en `es.events` | migraciones 000004 y 000007, `TestLeastPrivilege_WriterGrants` |
| T | Doble ejecución / replay de comandos | `Idempotency-Key` + `If-Match` con versión | ARQ-008 |
| R | Acción admin sin rastro | Toda acción admin exige motivo (≥ 10) y escribe `audit_log` **en la misma transacción** que el cambio | `partnernetwork/app/admin.go`, `identity/app/admin.go`, `TestAdmin_AuditFailureAbortsCommand` |
| I | IDOR sobre mascotas/pacientes | `authz.Can` por recurso; no visible → 404 | suite de authz (quality gate) |
| I | Enumeración de usuarios por búsqueda admin | `/admin/users` solo super_admin; ILIKE con escape de comodines | `TestAdmin_VerificationQueueSuspensionAndAudit` |
| I | PII en logs | Política SEC-007: emails enmascarados, sin cuerpos ni tokens | `slog.LogValuer` en VOs |
| D | Consultas costosas | Paginación por cursor con límite, índices trgm/(created_at,id) | migración 000007 |
| E | Escalada por membresía obsoleta | Membresías por request, caché 60 s con invalidación | IAM-008 |
| E | Usuario suspendido conserva sesión | Suspensión corta acceso en plataforma (Resolve → `ACCOUNT_SUSPENDED`) y deshabilita + cierra sesiones en Keycloak; si Keycloak falla → 503, reintento idempotente | `identity/app/admin.go`, `TestAdmin_KeycloakFailure_SuspendedInPlatformAnd503` |
| E | super_admin se auto-suspende o se bloquea la plataforma | Auto-suspensión prohibida | `TestAdmin_CannotSuspendSelf` |

Residual: el access token de un usuario suspendido sigue siendo criptográficamente válido hasta su
`exp` (5 min), pero la API lo rechaza por estado de cuenta en cada request. Dueño: backend.

### 2.5 Keycloak

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Fuerza bruta / credential stuffing | Brute force detection del realm + rate limit en borde y origen | `realm.yaml` (`bruteForceProtected`), SEC-001 |
| S | Robo de credenciales de super_admin | **OTP obligatoria para super_admin** (flujo `browser-petpassport`) | `realm.yaml` (IAM-005) |
| E | Abuso de la service account `petpassport-backend` | Solo `view-users` + `manage-users`; secreto rotado cada 6 meses; Admin API solo por red interna | `realm.yaml`, `keycloakadmin/client.go`, RUNBOOK §5 |
| E | Consola de administración expuesta | Allowlist IP/VPN en borde y origen; admin bootstrap reemplazado por admin nominal | RUNBOOK §10 |
| I | Enumeración de usuarios en registro/recuperación | Mensajes genéricos (IAM-004) | `realm.yaml` |

Residual: OTP opcional para el resto de roles (clinic_admin incluido) en F1; obligatoria para
clinic_admin a evaluar en F2. Dueño: IAM.

### 2.6 Datos: PostgreSQL gestionado, object storage, SMTP

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| I | Acceso directo a la BD | Allowlist de red a la VM, `sslmode=verify-full`, roles por función (migrator/writer/reader) | `staging.env.example`, DAT-001, SEC-010 |
| T | Migración maliciosa o errónea | Migraciones en PR revisado; `migrate` con rol `pp_migrator` separado; one-shot que aborta el despliegue | `deploy.sh` paso 2 |
| I | Fotos públicas por URL adivinable | `ObjectKey` generado, URLs firmadas de corta duración | ARQ-012 |
| R/I | Pérdida de datos | Backups + PITR del proveedor, restauración mensual registrada | RUNBOOK §9, DAT-009 |
| S | Emails falsificados desde nuestro dominio | SPF/DKIM/DMARC del proveedor SMTP | RUNBOOK §2 (prerrequisito humano) |

### 2.7 VM, despliegue y cadena de suministro

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| S | Acceso SSH no autorizado | Solo clave + MFA del proveedor; sin contraseñas | RUNBOOK §1, SEC-010 |
| T | Imagen manipulada | Imágenes por `sha`, firmadas con cosign, SBOM, trivy | `.github/workflows/images.yml` |
| T | Contenedor comprometido escala al host | No root, `read_only`, `no-new-privileges`, `cap_drop: ALL` | `docker-compose.staging.yml` |
| I | Secretos en repo o imagen | `.env` generado en la VM (600), gitleaks en CI, `deploy.sh` rechaza otros permisos | `deploy.sh`, `secrets.yml` |
| E | Alloy con socket de Docker | Montado en solo lectura; Alloy sin puertos publicados. **Residual**: el socket equivale a root en el host si Alloy se compromete | `docker-compose.observability.yml` |
| D | Despliegue roto deja el servicio caído | `--wait` + smoke + rollback automático a la versión anterior | `deploy.sh` |

### 2.8 Observabilidad

| | Amenaza | Control | Evidencia |
|---|---|---|---|
| I | Grafana/Prometheus expuestos | Puertos solo en `127.0.0.1`; acceso por túnel SSH; anónimo deshabilitado en staging | `docker-compose.observability.yml`, RUNBOOK §8 |
| I | PII en logs centralizados | La app no emite PII (SEC-007); Traefik no registra cabeceras; retención 30 días | `loki.yaml`, `alloy.alloy` |
| R | Borrado de evidencias tras incidente | Logs exportables por periodo (RUNBOOK §11); `audit_log` en BD (≥ 2 años) | SEC-007 |

## 3. Amenazas prioritarias y estado

| # | Amenaza | Severidad | Estado F1 |
|---|---|---|---|
| 1 | IDOR sobre mascotas/pacientes | Alta | Mitigada: authz por recurso + suite (quality gate) |
| 2 | Compromiso de cuenta super_admin | Alta | Mitigada: OTP obligatoria, consola restringida, auditoría con motivo |
| 3 | Bypass del WAF hacia el origen | Alta | Mitigada: firewall + `X-Origin-Auth`; verificada por smoke |
| 4 | Fuerza bruta de login | Media | Mitigada: brute force KC + rate limit borde y origen |
| 5 | Manipulación de la historia | Alta | Mitigada: append-only + mínimo privilegio (bug de grants corregido en M1.4) |
| 6 | Fuga de PII por logs | Media | Mitigada: SEC-007; revisar muestras de Loki en la primera semana |
| 7 | Socket de Docker en Alloy | Media | Residual aceptado F1; F2: recolector sin socket o servicio gestionado |
| 8 | Derecho de supresión con eventos inmutables | Media | Parcial: runbook manual (anonimiza cuenta); crypto-shredding en F5 (ADR-023) |

## 4. Hallazgos durante la elaboración (M1.4)

- **Grants**: `pp_app_writer` no tenía `UPDATE` sobre `es.streams` (necesario para la versión del
  stream) y conservaba privilegios de más en `es.outbox`/`audit_log`. Corregido en la migración
  000007 y cubierto por `TestLeastPrivilege_WriterGrants`.
- **Métricas**: la etiqueta `status` de `http_server_requests_total` era texto; pasa a código
  numérico (las alertas por clase 5xx dependían de ello).
- **PII en eventos**: solo `MemberInvited` y `GuardianshipTransferInitiated` llevan `pii{}`, ambos con
  sujeto identificado por **email** (sin `UserID`). El diseño de crypto-shredding por `UserID` de
  SEC-009 no cubre esos sujetos: ver privacy-runbook §4 (decisión pendiente para F5, PR de spec aparte).

## 5. Revisión periódica

Se revisa al cerrar cada hito con superficie nueva (F2: notificaciones/Redis; F4: partners) y tras
cualquier incidente de seguridad.

## 6. Checklist de revisión (dos personas, SEC-011)

Cada revisor marca en el PR que ha comprobado:

- [ ] El diagrama de §1 coincide con `docker-compose.staging.yml` y `routes.yml` (no hay servicios publicados fuera de Traefik:443).
- [ ] Cada fila de §2 tiene control **y** evidencia verificable (test, fichero o paso del runbook).
- [ ] Las rutas `/admin/*` de la API responden 403 a todo rol salvo super_admin (suite de authz).
- [ ] Toda acción admin exige motivo y deja entrada en `audit_log` en la misma transacción.
- [ ] El origen rechaza tráfico sin `X-Origin-Auth` y la consola de Keycloak fuera de la allowlist.
- [ ] El realm exige OTP a super_admin y tiene brute force activo.
- [ ] No hay secretos en el repo (`staging.env.example` y `*.tfvars.example` solo con marcadores).
- [ ] Los riesgos residuales de §2/§3 tienen dueño y fase, y son aceptables para el piloto en staging.
- [ ] Los hallazgos de §4 están resueltos o tienen seguimiento.

| Revisor | Rol | Fecha | Resultado |
|---|---|---|---|
| _pendiente_ | | | |
| _pendiente_ | | | |
