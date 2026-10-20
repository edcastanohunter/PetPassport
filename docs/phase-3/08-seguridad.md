# F3 · 08 — Seguridad

Controles de F1/F2 vigentes. F3 añade dos almacenes nuevos (OpenSearch, Redis), páginas públicas (directorio), push y escaneo antivirus.

## 1. Fuga de autorización en búsqueda

**[PP-F3-SEC-001] (MUST)**

- `accessFilter` es la única función que construye el filtro (ARQ-002); está prohibido construir consultas a OpenSearch fuera de `internal/search/app`. Lint: ningún otro paquete importa el cliente de OpenSearch.
- **Test de fuga**: con el dataset sintético, para cada combinación (rol × nivel `guardian|linked|custody|none` × org suspendida sí/no) se ejecutan 50 consultas de términos frecuentes con y sin filtro; el conjunto con filtro debe ser exactamente el subconjunto autorizado calculado desde PostgreSQL (`PetAccess`). 0 discrepancias. Gate de CI (integración) y nightly en staging.
- Post-filtro `PetAccess` siempre activo; `search_postfilter_dropped_total` > 0 durante 10 min → alerta (índice atrasado o bug).
- `super_admin` busca solo con motivo (`X-PP-Reason`), auditado, y nunca desde la UI normal (ruta `/admin/search`).

## 2. OpenSearch

**[PP-F3-SEC-002] (MUST)**

- Sin endpoint público; acceso solo desde `api`/`worker` por red privada; TLS; usuario con permisos mínimos (`indices:data/*` y gestión de alias sobre `pets*`, `clinical*`, `medications*`, `organizations*`; sin `cluster:admin` salvo el job de reindex con credencial separada).
- Sin PII de contacto en documentos (RN-17); auditoría de mapping en CI (`dynamic: strict` y lista de campos permitidos; test que falla si aparece `email`, `phone`, `address`).
- *Dashboards* de OpenSearch solo en dev/staging detrás de autenticación; en prod deshabilitado.
- Snapshots cifrados en object storage.

## 3. Redis

**[PP-F3-SEC-003] (MUST)** `requirepass`/ACL por proceso (usuario `api` sin acceso a la base de colas; `worker` a ambas), TLS en prod, red privada, sin `CONFIG`/`FLUSHALL` para usuarios de aplicación (ACL), sin PII de contacto en caché (ARQ-006), payloads de asynq con ids (nunca contenido clínico ni direcciones), sesiones de enlace con TTL ≤ 15 min.

## 4. Abuso y límites

**[PP-F3-SEC-004] (MUST)** Rate limiting por principal/org/IP/token (ARQ-007) con *fail-open* medido; detección de *scraping* de búsqueda (usuario con > 2.000 consultas/día o > 500 pacientes distintos consultados/día) → alerta y limitación temporal (`search` 10/min) hasta revisión; `pet-lookup` y directorio con respuestas de tiempo constante y resultados mínimos; exportaciones de cohortes limitadas y auditadas (RN-20).

## 5. Antivirus y cuarentena

**[PP-F3-SEC-005] (SHOULD)** ClamAV (ARQ-014): adjuntos no descargables hasta `clean`; `infected` → cuarentena, objeto inaccesible, aviso a la organización, alerta a SuperAdmin, registro en `audit_log`; definiciones actualizadas a diario; prueba con EICAR en CI; adjuntos subidos antes de F3 se escanean en lote (job `media:scan-backfill`).

## 6. Accesos anómalos

**[PP-F3-SEC-006] (MUST)** Job horario `anomaly-detector` sobre `audit_log` (`clinical.read`): umbrales por rol (vet > 200 fichas distintas/día, recepcionista > 150, cualquier usuario > 50 fichas de custodia/día), accesos fuera de horario de la agenda de la clínica (> 20 entre 00:00–05:00), ráfagas de exportación. Genera `security_alerts` (tabla) + email a SuperAdmin; SuperAdmin puede suspender al usuario (F1) desde la alerta. Falsos positivos ajustables por organización (clínicas 24 h).

## 7. Push

**[PP-F3-SEC-007] (SHOULD)** Claves VAPID por entorno en secret manager, rotación anual (resuscripción transparente); payload mínimo sin datos clínicos ni de contacto; endpoints validados (`https`, dominios de servicios push conocidos); suscripciones ligadas al usuario y revocables; `410/404` expiran la suscripción.

## 8. Directorio público

**[PP-F3-SEC-008] (SHOULD)** Opt-in explícito; datos expuestos: nombre comercial, ciudad, barrio/zona, servicios, teléfono solo si `show_phone`; **nunca** dirección exacta, miembros, pacientes ni identificación fiscal; `geo` redondeado a 3 decimales (≈ 100 m) en la respuesta pública; rate limit `public`; `noindex` configurable por la organización (por defecto indexable para que las clínicas ganen visibilidad).

## 9. Threat model F3

**[PP-F3-SEC-009] (MUST)** Actualización STRIDE con OpenSearch, Redis, asynq, directorio, push, ClamAV, CDN, réplicas múltiples (p. ej. *split-brain* de leases, *cache poisoning* por invalidación perdida → TTLs cortos como red de seguridad, *replay* de tareas asynq → idempotencia). Revisado antes de M3.4.
