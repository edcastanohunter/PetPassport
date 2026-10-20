# F1 · 08 — Seguridad

Marco: ADR-006 (defensa en profundidad). Alcance: aplicación pública con datos personales de tutores y datos de salud animal.

## 1. Modelo de amenazas (resumen)

**[PP-F1-SEC-011] (MUST)** Threat model F1 documentado (STRIDE por componente) y revisado por al menos dos personas antes de M1.4. Resumen de amenazas prioritarias:

| Amenaza | Vector | Control principal |
|---|---|---|
| Acceso a mascotas/tutores ajenos | IDOR en `/pets/{id}`, `/organizations/{id}/patients` | authz por recurso + *not found if not visible* (SEC-005). |
| Robo de sesión | XSS en SPA, token en storage | tokens en memoria, CSP, sin `innerHTML` (SEC-003). |
| Enumeración de usuarios | registro, recuperación, invitaciones, transferencia por email | mensajes genéricos, tiempos constantes, rate limit (IAM-004, SEC-001). |
| Abuso de invitaciones/transferencias | tokens adivinables o reutilizables | 256 bits, hash en BD, un solo uso, caducidad. |
| Subida maliciosa | polyglot/zip bomb en foto | magic bytes, límite de píxeles, re-encode, sin ejecución (SEC-004). |
| Escalada por membresía obsoleta | token válido tras retirar miembro | membresías por request, caché 60 s con invalidación (IAM-008). |
| Manipulación de historia | UPDATE/DELETE en eventos | trigger append-only + rol sin permisos (DAT-003, SEC-010). |
| Fuga por logs | PII en logs/trazas | política de logging (SEC-007). |
| DoS | ráfagas, cuerpos grandes, regex | WAF, límites de cuerpo/tiempo, sin regex catastróficas (SEC-001/002). |
| Compromiso de secretos | secretos en repo/imagen | secret manager, escaneo (SEC-006/008). |

## 2. Borde: WAF

**[PP-F1-SEC-001] (MUST)**

- WAF gestionado delante de `app.`, `api.` y `auth.`: reglas gestionadas OWASP Core Rule Set en modo bloqueo (tras 1 semana en modo *log* en staging), reglas específicas del proveedor, *bot management* básico.
- Rate limiting en el borde: `POST /realms/*/login-actions/*` y `/protocol/openid-connect/token` 10/min/IP; `POST /api/v1/*` 60/min/IP; `GET /api/v1/*` 300/min/IP; `/api/v1/organizations/lookup` 20/min/IP; respuestas `429` con `Retry-After`.
- TLS 1.2+ (preferir 1.3), HSTS con `preload` tras validación, certificados gestionados.
- El origen (VM) solo acepta tráfico desde los rangos IP del WAF y valida una cabecera secreta de origen (`X-Origin-Auth`) en Traefik; conexiones directas a la IP del origen se rechazan.
- Consola de administración de Keycloak (`/admin/*`) y `master` realm: solo desde IPs/VPN del equipo (regla WAF + middleware Traefik).

## 3. Gateway: Traefik

**[PP-F1-SEC-002] (MUST)** Configuración declarativa (`infra/traefik/`):

- Routers: `app.` → frontend; `api.` → api (solo `/api/*` y `/api/v1/healthz`); `auth.` → Keycloak (`/realms/petpassport/*`, `/resources/*`; `/admin/*` restringido).
- Middlewares: `headers` (SEC-003), `buffering` (`maxRequestBodyBytes: 6 MiB`, `memRequestBodyBytes: 1 MiB`), `rateLimit` de respaldo (100 r/s por IP con burst 50), `inFlightReq` 100 por IP, `ipAllowList` para admin, `compress`.
- Timeouts de *entrypoint*: `readTimeout 30s`, `writeTimeout 60s`, `idleTimeout 180s`; *forwarding* timeouts a `api` 30 s.
- Dashboard de Traefik deshabilitado en prod; logs de acceso JSON sin cuerpos ni cabeceras de autorización.
- `X-Forwarded-*` solo de confianza desde el WAF/Traefik (`forwardedHeaders.trustedIPs`).

## 4. Cabeceras y CORS

**[PP-F1-SEC-003] (MUST)**

| Cabecera | Valor |
|---|---|
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains; preload` |
| `Content-Security-Policy` (SPA) | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://<storage-host>; connect-src 'self' https://api.<dominio> https://auth.<dominio>; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://auth.<dominio>; object-src 'none'; upgrade-insecure-requests` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Permissions-Policy` | `camera=(self), microphone=(), geolocation=()` (cámara para foto desde móvil) |
| `X-Frame-Options` | `DENY` (redundante con CSP) |
| `Cross-Origin-Opener-Policy` | `same-origin` |
| `Cache-Control` (API) | `no-store` |

CORS en la API: `Access-Control-Allow-Origin` solo `https://app.<dominio>` (y `http://localhost:5173` en dev); métodos `GET, POST, PUT, PATCH, DELETE`; cabeceras permitidas `Authorization, Content-Type, Idempotency-Key, If-Match, X-Correlation-ID`; expuestas `ETag, X-Correlation-ID, RateLimit-*, Retry-After`; `max-age 600`; sin credenciales (no cookies).

## 5. Validación de entrada y subidas

**[PP-F1-SEC-004] (MUST)**

- Toda entrada pasa por el esquema OpenAPI (API-010) y luego por VOs de dominio; no existe *bypass* interno.
- Subidas: ver ARQ-012. Además: nombre de archivo ignorado (se genera `ObjectKey`), `Content-Type` del cliente ignorado, decodificación en *goroutine* con límite de memoria y timeout 10 s, SVG/GIF rechazados, HEIC convertido vía `libheif` o rechazado con mensaje (decisión en M1.2 según soporte de la imagen base).
- Prohibidas expresiones regulares con *backtracking* catastrófico; usar `regexp` de Go (RE2) exclusivamente.
- Enlaces en emails: solo a `PP_PUBLIC_BASE_URL`; nunca se construyen desde cabeceras `Host`.

## 6. Autorización

**[PP-F1-SEC-005] (MUST)**

- *Deny by default* (IAM-010). Test automático recorre todas las rutas del OpenAPI y verifica que cada handler invoca `authz.Can` con la `x-pp-action` declarada (instrumentación en test).
- Suite de pruebas de autorización por tabla: para cada `Action` × rol × relación (tutor/vinculado/no vinculado/suspendida) se asevera `200/404/403` esperado. Esta suite es *quality gate*.
- Recursos no visibles → `404`. Visibles pero sin permiso → `403`.
- Acciones de `super_admin` con motivo obligatorio y entrada en `audit_log`.

## 7. Secretos y configuración

**[PP-F1-SEC-006] (MUST)**

- Secretos solo en el secret manager del proveedor (o SOPS+age para la VM) inyectados como variables de entorno en despliegue; nunca en repo, imágenes ni logs. `gitleaks` en pre-commit y CI.
- Rotación: secreto de `petpassport-backend` y credenciales de BD cada 6 meses o ante incidente; procedimiento en runbook.
- Dev: `.env.example` con valores inocuos; `.env` ignorado.

## 8. Logging, correlación y auditoría

**[PP-F1-SEC-007] (MUST)**

- Logs JSON estructurados (`slog`) con `request_id`, `correlation_id`, `trace_id`, `actor_id`, `route`, `status`, `duration_ms`. **Prohibido**: tokens, cuerpos de request/response, emails en claro (usar `email_hash` o enmascarado), teléfonos, direcciones.
- Nivel `debug` nunca activo en prod. Redacción automática de campos sensibles vía `slog.LogValuer` en tipos VO (`Email`, `Phone` implementan `LogValue()` enmascarado).
- `platform.audit_log` para: acciones admin, cambios de perfil, decisiones `authz.deny` de admin, accesos de clínicas a datos de contacto de tutores (`pet:guardian_contact:read`), logins fallidos reiterados (desde eventos de Keycloak, F2).
- Retención: logs de aplicación 30 días; auditoría ≥ 2 años.

## 9. Cadena de suministro y análisis estático

**[PP-F1-SEC-008] (MUST)** En CI, bloqueantes:

| Herramienta | Alcance |
|---|---|
| `govulncheck` | vulnerabilidades alcanzables en Go. |
| `gosec` + `semgrep` (reglas Go/Vue/TS) | SAST. |
| `npm audit --audit-level=high` + `osv-scanner` | dependencias frontend. |
| `trivy image` | imágenes (CRITICAL/HIGH bloquean). |
| `gitleaks` | secretos. |
| Dependabot / Renovate | actualizaciones semanales agrupadas. |
| SBOM (`syft`) | publicado como artefacto por release. |

Versiones fijadas (`go.sum`, `package-lock.json`); imágenes base por *digest*.

## 10. Privacidad y datos personales

**[PP-F1-SEC-009] (MUST)**

- Base legal: consentimiento versionado en onboarding (PRD-020); aviso de privacidad explica compartición con clínicas vinculadas.
- Minimización: la clínica ve solo nombre y teléfono del tutor (no dirección ni email en F1); farmacias no ven tutores.
- Derechos: en F1, exportación y eliminación se atienden **manualmente** por SuperAdmin con runbook (plazo ≤ 15 días): exportación = JSON de `UserAccount` + eventos de sus mascotas; eliminación = suspensión + anonimización de `identity.user_accounts` + reemplazo de `pii{}` en eventos **no** permitido (append-only) → por eso:
- **Diseño crypto-shredding (implementación F5)**: por cada sujeto (`UserID`) una clave de datos (DEK) en `platform.subject_keys` cifrada con KEK del KMS; los campos `pii{}` de eventos se cifran con la DEK del sujeto al que pertenecen; borrar la DEK anonimiza irreversiblemente. **En F1** los esquemas de evento ya aíslan `pii{}` y el `Registry` marca qué eventos contienen PII y de qué sujeto (`PIISubjects(event) []UserID`), de modo que F5 no requiere re-versionar eventos.
- Datos de mascotas fallecidas o transferidas se conservan (historia clínica); la anonimización afecta al tutor, no al animal.
- No se usan cookies de seguimiento ni analítica de terceros en F1.

## 11. Mínimo privilegio y red

**[PP-F1-SEC-010] (MUST)**

- Roles de BD (DAT-001); `pp_app_writer` sin `UPDATE/DELETE` en `es.events`.
- Red: en compose/VM, solo Traefik expone puertos; PostgreSQL gestionado acepta conexiones únicamente desde la VM (allowlist/VPC); MinIO/Keycloak sin puertos públicos salvo a través de Traefik.
- Contenedores: usuario no root, `read-only` rootfs donde sea posible, `no-new-privileges`, *capabilities* mínimas.
- Acceso SSH a la VM por clave + MFA del proveedor; sin contraseñas; registro de sesiones.

## 12. Respuesta a incidentes (mínimo F1)

- Contacto de seguridad publicado (`/.well-known/security.txt`).
- Runbook: revocar sesiones (Keycloak), rotar secretos, bloquear IPs en WAF, exportar logs del periodo, comunicar a afectados según normativa.
