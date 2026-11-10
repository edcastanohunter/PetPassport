# Runbook de operación: staging (PP-F1-INF-006)

Entorno: una VM Linux con Podman **rootless** y systemd, y servicios gestionados (PostgreSQL, object storage,
SMTP), detrás de Cloudflare (ADR-006, ADR-008, ADR-026). Cada servicio es una unidad Quadlet
(`infra/quadlet/deploy/`) del systemd de usuario de la cuenta de servicio `petpassport`: `deploy.sh` las
renderiza e instala en `~petpassport/.config/containers/systemd/petpassport/`. Se opera como ese usuario con
`systemctl --user` y `journalctl --user` (`pp-api.service`, `pp-keycloak.service`, …). Root solo interviene en
la preparación del host (`host-setup.sh`, §1). El dominio es un parámetro (`PP_DOMAIN`). Los hosts públicos
son `app.`, `api.` y `auth.` bajo ese dominio.

> Convención: los comandos de §3–§14 se ejecutan como `petpassport` (`sudo -iu petpassport`). `host-setup.sh`
> deja `XDG_RUNTIME_DIR` en su perfil, que es lo que necesitan `systemctl --user` y `journalctl --user`.

> **Ensayo obligatorio**: alguien distinto de quien escribió este runbook ejecuta una vez §3, §4 y
> §9 antes de M1.5 y anota el resultado al final del documento.

Contenido: §1 VM y host-setup · §2 Secretos · §3 Despliegue · §4 Rollback · §5 Rotación · §6 WAF ·
§7 Alertas · §8 Observabilidad · §9 Backups · §10 Keycloak · §11 Incidentes · §12 Contacto ·
§13 Correo · §14 Operación con systemd.

## 1. Preparación de la VM (una vez)

1. VM con 2–4 vCPU y 8 GB. Acceso SSH solo por clave más MFA del proveedor, sin contraseñas
   (`PasswordAuthentication no`) y con registro de sesiones del proveedor activo.
2. SO con **Podman ≥ 5.4** y systemd (p. ej. Debian 13 o RHEL/Alma 10; en Ubuntu, comprobar la versión del
   paquete de la LTS elegida: la 24.04 trae 4.9 y no sirve). `host-setup.sh` y `deploy.sh` rechazan versiones
   anteriores. Paquetes: `podman`, `passt` (red rootless), `netavark`, `aardvark-dns`, `nftables`, `curl`,
   `jq`, `openssl`, `util-linux` (`flock`), `git`. No hace falta Docker ni ningún proveedor de Compose.
   ```bash
   podman version --format '{{.Client.Version}}'      # ≥ 5.4
   podman info --format '{{.Host.NetworkBackend}}'    # netavark
   ```
3. Clonar el repo en `/opt/petpassport` (`git pull` del tag a desplegar). Las unidades montan ficheros de ahí
   (`infra/traefik/staging`, `infra/keycloak`, `infra/observability`). Tras el paso 4, dárselo a `petpassport`
   (`chown -R petpassport: /opt/petpassport`): con SELinux, el reetiquetado `:z` lo exige.
4. Como root, una vez: `/opt/petpassport/infra/deploy/host-setup.sh staging`. Es lo único que necesita root
   y es idempotente:
   - crea el usuario de servicio `petpassport` (UID normal, sin contraseña: journald solo da journal propio a
     UIDs ≥ 1000, y sin él ni `journalctl --user` ni Alloy verían los logs), con rangos subuid/subgid y
     **linger** (su systemd de usuario arranca con la VM aunque nadie haya iniciado sesión);
   - `net.ipv4.ip_unprivileged_port_start=443` en `/etc/sysctl.d/90-petpassport.conf`: el 443 lo abre
     `pp-traefik.socket` como usuario sin privilegios;
   - journal persistente (`/var/log/journal`);
   - la tabla nftables `pp-origin` (`infra/deploy/host/pp-origin.nft`) con los rangos vigentes de Cloudflare,
     en `/etc/pp-origin/`, cargada por `pp-origin-firewall.service` (unidad de sistema);
   - `/etc/petpassport/` y `/var/lib/petpassport/staging/` con dueño `petpassport` (700).
5. Firewall en dos capas:
   - **Proveedor** (security group): entrada solo SSH desde la VPN del equipo y 443 desde los rangos de
     `infra/waf/cloudflare-ips.txt`; el resto, denegado.
   - **Host**: la tabla `pp-origin` del paso 4. Filtra en `input`: el socket del 443 vive en el propio host
     (activación por socket, sin DNAT), así que el tráfico pasa por esa cadena. Acepta loopback. Si Cloudflare
     publica rangos nuevos, `deploy.sh` avisa y hay que volver a ejecutar `host-setup.sh staging` como root.
     Comprobar: `nft list table inet pp-origin` (contadores de `drop`).
6. Certificado de origen de Cloudflare (SSL/TLS → Origin Server, 15 años, `*.${PP_DOMAIN}`):
   `/var/lib/petpassport/staging/certs/origin.pem` y `origin-key.pem` (dueño `petpassport`, `600`).
7. Acceso de `petpassport` a ghcr.io con un token de solo lectura de paquetes, en un fichero persistente:
   `podman login --authfile ~/.config/containers/auth.json ghcr.io` (Podman lo consulta si no existe el de
   `$XDG_RUNTIME_DIR`, que se borra al reiniciar).

## 2. Secretos y configuración (INF-005, SEC-006)

- Fuente de verdad: el secret manager del proveedor (o SOPS+age). En la VM se genera
  `/etc/petpassport/staging.env` a partir de `infra/deploy/staging.env.example`:
  `install -m 600 -o petpassport -g petpassport /dev/null /etc/petpassport/staging.env` y se rellenan los valores.
  `deploy.sh` se niega a usarlo si los permisos no son 600/400.
- Prerrequisitos humanos: dominio y zona en Cloudflare; Postgres gestionado con las bases
  `petpassport` y `keycloak` y los roles de `infra/postgres/init/` (creados por el DBA con contraseñas
  del secret manager); SMTP con SPF/DKIM/DMARC; bucket con versionado.
- **Usuario sintético del smoke**: registrarse en `https://app.${PP_DOMAIN}` con `SMOKE_USER`,
  completar el onboarding y no configurar OTP. Solo tiene el rol `pet_owner` y no tiene datos reales.
- **Primer super_admin**: crear el usuario, asignarle el rol `super_admin` en la consola de Keycloak
  (desde la VPN) y configurar la OTP en el primer login (es obligatoria para ese rol).

## 3. Despliegue

```bash
sudo -iu petpassport
cd /opt/petpassport && git fetch --tags && git checkout <tag-o-sha>
infra/deploy/deploy.sh staging sha-<sha-largo>       # se niega a correr como root
```

Pasos del script:

0. Comprueba lo que deja `host-setup.sh`: linger, el 443 abrible sin privilegios y `pp-origin-firewall` activo.
1. Renderiza las unidades de `infra/quadlet/deploy/` para la versión: imagen, rutas y rangos de Cloudflare.
   Genera un `EnvironmentFile` por servicio en `/var/lib/petpassport/staging/env/` (600), porque las
   unidades no llevan secretos. Valida todo con el generador de Quadlet (`-user -dryrun`) y hace
   `podman pull` de las imágenes.
2. Instala solo `pp-migrate` y ejecuta `systemctl --user start pp-migrate`.
3. Instala las unidades que cambiaron (`~/.config/containers/systemd/petpassport/` y, para el socket, el timer
   y el aviso de fallos, `~/.config/systemd/user/`) y hace `daemon-reload`. Activa `pp-traefik.socket` y el
   timer de respaldo. Reinicia `pp-traefik`/`pp-keycloak` y la observabilidad si cambiaron, y después ejecuta
   `pp-keycloak-config`.
4. Ejecuta `systemctl --user restart pp-api pp-frontend`. Con `Notify=healthy` no termina hasta que pasan su
   health check.
5. Smoke, anotación en Grafana y aviso al canal del equipo.

Volver a ejecutarlo con la misma versión no reinicia nada: solo se reinicia lo que cambió, y la última
línea del log lo enumera.

- Si `migrate` falla, el despliegue se aborta y la versión anterior sigue sirviendo. Revisar el log y
  corregir con una migración nueva; nunca se edita una migración aplicada.
- Si el smoke falla, el script hace rollback automático a la versión anterior y termina con error.
  Ver la salida (`SMOKE FAIL: …`) y §4.
- Estado: `/var/lib/petpassport/staging/current-version` y `history`.

## 4. Rollback

```bash
infra/deploy/deploy.sh staging "$(tail -2 /var/lib/petpassport/staging/history | head -1 | cut -d' ' -f2)"
```

Las migraciones son compatibles hacia atrás una versión (DAT-002), así que el rollback no ejecuta
`down`. Si la versión a la que se vuelve es dos o más versiones anterior, primero hay que comprobar en
las notas de las migraciones intermedias que no haya cambios incompatibles. Si el rollback tampoco
llega a healthy: `journalctl --user -u pp-api -n 200`, contrastar con la alerta activa (§7) y escalar (§12).

## 5. Rotación de secretos (cada 6 meses o ante incidente)

| Secreto | Procedimiento |
|---|---|
| `PP_BACKEND_CLIENT_SECRET` | Keycloak → clients → `petpassport-backend` → Credentials → Regenerate. Actualizar el secret manager y el `.env`. `deploy.sh staging <versión-actual>` (keycloak-config reaplica el valor y la API se reinicia). |
| Contraseñas de BD | Crear contraseña nueva en el proveedor (`ALTER ROLE … PASSWORD`), actualizar el `.env` y redeplegar la versión actual. |
| `PP_ORIGIN_AUTH_SECRET` | 1) `terraform apply` con el nuevo `TF_VAR_origin_auth_secret`. 2) Inmediatamente después, actualizar el `.env` y redeplegar la versión actual (`deploy.sh` regenera `env/traefik.env` y reinicia `pp-traefik`). Entre 1 y 2 el origen responde 404 (unos segundos): hacerlo en una ventana de bajo tráfico. |
| `PP_IP_HASH_SALT` | No rotar salvo incidente: cambia los hashes de IP del `audit_log` (se pierde la correlación histórica). |
| Admin de Keycloak | Admin nominal con OTP. El usuario bootstrap se elimina tras el primer arranque. |

## 6. WAF (Cloudflare)

- Código: `infra/waf/cloudflare/`. El estado contiene el secreto de origen, así que va en un backend
  remoto cifrado con acceso solo del equipo de plataforma.
  ```bash
  export CLOUDFLARE_API_TOKEN=… TF_VAR_origin_auth_secret=…
  terraform -chdir=infra/waf/cloudflare plan -var-file=staging.tfvars
  terraform -chdir=infra/waf/cloudflare apply -var-file=staging.tfvars
  ```
- **Log → block**: la primera semana `waf_mode = "log"`. Revisar Security → Events (falsos positivos
  en `POST /api/v1/*` y en el login), ajustar con excepciones si hace falta, y luego pasar a
  `waf_mode = "block"` y aplicar. Anotar la fecha en el PR o ticket.
- **Verificar** tras cada `apply`:
  - `curl -sI https://api.${PP_DOMAIN}/api/v1/healthz` → 200.
  - Smoke con `SMOKE_ORIGIN_IP` → el origen rechaza tráfico directo.
  - 11 POST seguidos al endpoint de token → 429. Comprobar si la respuesta del borde trae
    `Retry-After`; el origen (Traefik) sí lo envía.
- **Bloquear una IP**: Security → WAF → Tools → IP Access Rules (temporal), o una regla en
  `cloudflare_ruleset.custom` (permanente, vía PR).
- **Purgar caché** tras un despliegue con assets rotos: Caching → Purge Everything. El `index.html` ya
  se sirve con `no-cache`.

## 7. Alertas (INF-007)

Las alertas llegan al canal del equipo vía Alertmanager. Severidades: `page` (atender ya),
`ticket` (en horario) e `info`.

### 7.1 ApiHigh5xxRate
5xx > 1% durante 5 min. Revisar el dashboard *API overview* (qué rutas fallan) y los logs en Loki
(`{service="api"} | json | level="ERROR"`). Mirar si coincide con un despliegue (anotación azul):
si es así, rollback (§4). Si no, revisar las dependencias (`/readyz`, pool pgx, Keycloak).

### 7.2 ApiHighLatencyP95
p95 > 500 ms durante 10 min. Revisar *API overview* → "p95 por ruta" y "Esperas por pool vacío". Si
el pool está saturado, revisar consultas lentas (`log_min_duration_statement`) y `PP_DB_MAX_CONNS`.
Tempo da las trazas de las requests lentas.

### 7.3 ApiNotReady
`/readyz` lleva 2 min caído. Ejecutar `systemctl --user status pp-api` y `journalctl --user -u pp-api -n 200`. Normalmente es la BD o
Keycloak (JWKS). Si la API está en bucle de reinicios tras un despliegue, rollback (§4).

### 7.4 OutboxPendingGrowing
Desde F2 M2.0 el worker consume el outbox: un crecimiento sostenido indica un worker caído
(ver §7.8) o un stream bloqueado por un evento en reintento. Inspección:
`SELECT global_position, event_type, attempts, last_error, next_attempt_at FROM es.outbox
WHERE published_at IS NULL ORDER BY global_position LIMIT 20;`. Los eventos en
`dead_letter = true` no bloquean y no cuentan como pendientes.

### 7.5 CertificateExpiringSoon
Un certificado público caduca en menos de 14 días. Los del borde los renueva Cloudflare: si salta,
revisar el estado del certificado en SSL/TLS → Edge Certificates. El de origen lo vigila `deploy.sh`
(aviso en cada despliegue): regenerarlo (§1.4) y redeplegar.

### 7.6 ScrapeTargetDown
Prometheus no puede leer api, worker, keycloak o traefik. Comprobar la red `pp-obs`
(`podman network inspect pp-obs`) y la unidad afectada (`systemctl --user status pp-<servicio>`).

### 7.7 ProjectionFailed
Una proyección asíncrona agotó sus 5 reintentos y está en *fail-stop* (ADR-009 §4): su read model
deja de actualizarse (la UI muestra "Actualizando…"), las demás siguen. Diagnóstico:
`podman exec pp-worker /worker status` (columna `LAST_ERROR`) y Loki
`{service="petpassport-worker"} | json | projection="<nombre>"`. El error indica la posición y el
tipo de evento. Corregir el código (nuevo despliegue) o los datos del read model y reanudar:
`podman exec pp-worker /worker resume --projection <nombre>`. Nunca se salta un evento ni se edita
`es.events`. Si el read model quedó inconsistente: `rebuild` (§15).

### 7.8 WorkerDown
Ningún worker tiene el lock de instancia: proyecciones asíncronas, outbox y comandos programados
parados (los comandos de la API siguen funcionando). `systemctl --user status pp-worker` y
`journalctl --user -u pp-worker -n 200`. Si el proceso corre pero espera el lock, hay una sesión
huérfana con él: `SELECT * FROM platform.worker_instances;` (ver `heartbeat_at`) y
`SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND ((classid::bigint << 32) | objid::bigint) = hashtext('worker.default');`
→ `SELECT pg_terminate_backend(<pid>);` solo si su `heartbeat_at` tiene más de 1 min.

### 7.9 AppendLockContention
p95 de espera del lock de orden total de `Append` > 20 ms durante 10 min: umbral de revisión de
ADR-009 §3 (pasar a lectura tolerante a huecos en F3). Revisar en *Worker & Projections* el
throughput de appends y en *API overview* qué comandos alargan su transacción (el lock se retiene
hasta el commit). Abrir issue con los datos; no es una caída.

### 7.10 ProjectionLagHigh
Una proyección asíncrona va más de 60 s por detrás del event store durante 5 min. Revisar en
*Worker & Projections* el lote y su duración (¿ráfaga o lote lento?) y `projection_status`. Una
ráfaga puntual se recupera sola; si no, `worker status` y `rebuild`/`resume` según §7.7, y revisar
las consultas del proyector.

### 7.11 OutboxStuck
El evento más antiguo del outbox lleva más de 5 min sin procesarse: los process managers
(notificaciones, citas, completar al firmar) están parados o atascados. Comprobar §7.8 (¿hay worker
con lock?) y los logs `{service="worker"} | json | level="ERROR"`. Un PM que falla reintenta con
backoff; el evento problemático aparece en `es.outbox` con `attempts` altos.

### 7.12 OutboxDeadLetter
Un evento agotó los reintentos de un process manager. Inspeccionar
`SELECT * FROM es.outbox WHERE dead_letter ORDER BY id DESC LIMIT 20;`, corregir la causa (datos o
código) y reencolar con `UPDATE es.outbox SET dead_letter = false, attempts = 0 WHERE id = <id>;`.
Nunca se edita `es.events`.

### 7.13 ReminderDelay
Los recordatorios se despachan con más de 60 s de retraso (p95). Revisar el runner
`notifications-dispatch` en los logs del worker y el dashboard *Notifications*. Pendientes vencidos:
`SELECT count(*), min(due_at) FROM notifications.scheduled WHERE status = 'pending' AND due_at < now();`.
Suele ser un worker sin lock (§7.8) o la base saturada.

### 7.14 NotificationDeadLetter
Más de 10 notificaciones agotaron los 5 reintentos (backoff 30 s, 2 min, 10 min, 30 min, 2 h). La
causa, sin PII, está en `last_error`:
`SELECT template, last_error, count(*) FROM notifications.deliveries WHERE dead_letter AND created_at > now() - interval '1 hour' GROUP BY 1, 2;`.
Causas habituales: proveedor SMTP caído o credenciales rotadas (rotación de secretos: actualizar
`PP_SMTP_PASSWORD` y reiniciar el worker), remitente sin SPF/DKIM rechazado, plantilla con variable
ausente. Tras corregir, reencolar:
`UPDATE notifications.deliveries SET dead_letter = false, status = 'queued', attempts = 0, next_attempt_at = now() WHERE dead_letter AND id = ANY(<ids>);`.

### 7.15 DocumentRenderFailures
Más de 5 PDF fallidos en 1 h. `SELECT kind, last_error, count(*) FROM read_clinical.document_jobs WHERE status = 'failed' GROUP BY 1, 2;`.
Comprobar que el binario `typst` y las fuentes están presentes y el storage accesible.

### 7.16 SearchIndexLagHigh
Retraso de indexación OpenSearch > 30 s durante 5 min. Revisar `search.index_queue` (`status = 'failed'` o intentos agotados) y el estado de OpenSearch (`GET /_cluster/health`). Si OpenSearch está saturado, verificar cola `default` en Asynq y lanzar reintento con `/admin/search/index-queue/retry`.

### 7.17 SearchDegraded
Más del 5% de las búsquedas están degradando al fallback de PostgreSQL. Verificar conectividad con OpenSearch, breaker abierto en logs de `api` y latencia de consultas en OpenSearch.

### 7.18 SearchPostfilterDrops
El post-filtro de autorización `PetAccess` está descartando documentos devueltos por OpenSearch de forma sostenida (> 10 min). Indica desfase en el índice `access` o desincronización de eventos de permisos (`CareLinkRevoked`). Lanzar tarea `search:access` o reindexación parcial.

### 7.19 RedisMemoryHigh
Memoria de Redis > 80% de `maxmemory`. Verificar claves con TTL faltante (`redis-cli --bigkeys`) y consumo de las familias `membership`, `petaccess` y `pp:rl:*`. Comprobar política `allkeys-lru`.

### 7.20 RedisDown
Instancia de Redis no responde. El sistema entra en fail-open para rate limiting y fail-miss para caché. Reiniciar el servicio Redis o verificar red privada con la instancia gestionada.

### 7.21 RateLimitFailOpen
Rate limiting operando en fail-open porque Redis no está accesible. Toda petición se permite temporalmente para no bloquear tráfico legítimo; priorizar restauración inmediata de Redis.

### 7.22 AsynqQueueBacklog
Cola `default` > 5.000 o `critical` > 500 tareas por > 5 min. Escalar réplicas de worker o revisar logs en busca de tareas lentas (p. ej. renderizado PDF o escaneo ClamAV).

### 7.23 AsynqArchived
Más de 10 tareas agotaron reintentos en 1 h. Inspeccionar la DLQ en Asynqmon, corregir la causa raíz (timeout de red, payload inválido) y reencolar desde la interfaz administrativa.

### 7.24 LeaseTakeoverStorm
Más de 5 cambios de titularidad de leases de worker en 5 min. Señal de inestabilidad, reinicios continuos de réplicas worker o problemas de latencia de red contra PostgreSQL. Revisar `journalctl --user -u pp-worker`.

### 7.25 SLOBurnRateHigh
Consumo acelerado del presupuesto de error del SLO de disponibilidad (14x en 1h o 6x en 6h). Activar protocolo de incidente: revisar API Overview en Grafana, verificar despliegues recientes y dependencias caídas.

### 7.26 ClamAVDefinitionsStale
Firmas de antivirus ClamAV desactualizadas (> 48 h). Comprobar el servicio `freshclam` y conectividad con los mirrors de ClamAV.

### 7.27 MappingDrift
Divergencia entre los esquemas de mapeo versionados en el repositorio (`backend/internal/search/mappings/*.json`) y el índice activo en OpenSearch. Planificar reindexación sin downtime con `cmd/worker search reindex`.

### 7.28 AIBudget80
El consumo del presupuesto mensual de IA ha superado el 80 %. Revisar volumen diario en dashboard `OCR & IA`, considerar ajuste de cuota en `admin/ai` o cambiar al modelo de respaldo si la proyección mensual supera el límite.

### 7.29 AIBudget100
Presupuesto mensual de IA alcanzado al 100 %. El corte duro bloquea nuevas llamadas automáticas con error `503 AI_BUDGET_EXCEEDED` degradando a captura manual. Para restablecer, el SuperAdmin debe ampliar el presupuesto mensual en `admin/ai` o esperar al reinicio de mes.

### 7.30 OCRFailureRateHigh
Tasa de fallo en el pipeline OCR superior al 10 % en 1 h. Verificar disponibilidad y latencia del proveedor de IA, conectividad de egress (`api.anthropic.com`), o fallos en el worker Tesseract de respaldo.

### 7.31 OCRRefusalsSpike
Más de 5 respuestas de rechazo (refusal) del modelo en 1 h. Inspeccionar si hay un ataque de prompt injection o imágenes con contenido no apto en la cola de OCR. Revisar `read_prescriptions.extraction_jobs` para muestras afectadas.

### 7.32 OCRLatencyHigh
El percentil 95 de duración de las etapas de extracción OCR supera los 60 s. Comprobar concurrencia del worker asynq `ocr`, timeouts del proveedor de IA y cola de procesamiento.

### 7.33 WebhookFailuresHigh
Más del 20 % de entregas de webhooks hacia un partner específico están fallando en los últimos 15 min. Comprobar estado del endpoint del partner, circuit breaker, y verificar si requiere suspensión preventiva tras 7 días de fallos continuos.

### 7.34 PartnerCircuitOpen
Circuit breaker abierto hacia el host de un partner debido a 5 fallos consecutivos. El tráfico saliente hacia ese partner se encuentra pausado temporalmente (5 min) para evitar saturación.

### 7.35 EgressDenied
Intento de conexión saliente hacia una IP privada, link-local, loopback o host no incluido en allowlist bloqueado por anti-SSRF (`platform/egress`). Auditar inmediatamente los registros de auditoría y la URL de webhook o adaptador registrada por el partner.

### 7.36 QuoteFanoutStuck
Invitaciones de cotización atascadas en estado `pending` por más de 10 min. Verificar la cola asynq `critical` de webhooks y el despachador de fanout en el worker.

### 7.37 PrescriptionPDFFailures
Más de 5 errores en generación o firmado de PDFs de recetas en 1 h. Revisar disponibilidad de MinIO/S3, binario Typst y plantilla `prescription.typ`.

### 7.38 PartnerAPI5xx
Tasa de errores 5xx en la Partner API `/partner/v1` superior al 1 % en 5 min. Revisar `journalctl --user -u pp-api`, logs de Traefik para `partners.<dominio>` y estado de la base de datos PostgreSQL.

### 7.39 SandboxDown
El endpoint de healthcheck del entorno sandbox (`partners-sandbox.<dominio>`) no responde durante más de 2 min. Revisar contenedor `pp-sandbox-api` o máquina virtual aislada de sandbox.

## 8. Observabilidad

Grafana, Prometheus y Alertmanager solo escuchan en `127.0.0.1`. Se accede con un túnel:
`ssh -L 3000:127.0.0.1:3000 <vm>` y después `http://localhost:3000` (usuario admin con
`GRAFANA_ADMIN_PASSWORD`; en staging no hay acceso anónimo). Los dashboards están en la carpeta
*PetPassport*: API overview, Event Store, Worker & Projections, Auth & Authz y Keycloak. `deploy.sh` usa el
`GRAFANA_TOKEN` (service account con rol Editor) solo para las anotaciones de despliegue.

## 9. Backups y restauración (INF-009, DAT-009)

### 9.1 Configuración del proveedor (una vez, verificar en cada revisión trimestral)

| Recurso | Ajuste obligatorio | Cómo se comprueba |
|---|---|---|
| PostgreSQL 17 gestionado | `ssl` obligatorio; backups automáticos diarios; PITR 7 días (prod: 30) | consola del proveedor → Backups; `SHOW ssl` = `on` |
| Bases | `petpassport` y `keycloak` en la misma instancia (ambas entran en el backup) | `\l` |
| Alertas del proveedor | almacenamiento > 80 %, CPU > 80 % 15 min, backup fallido → canal del equipo | regla visible en la consola |
| Rol de respaldo | `petpassport_backup` LOGIN, miembro de `pp_app_reader` (solo SELECT) | `\du petpassport_backup` |
| Object storage | buckets `petpassport-media-<env>` y `petpassport-backups-<env>`, privados, versionados | `infra/storage/apply.sh` (abajo) |

Rol de respaldo (con el usuario administrador del proveedor):

```sql
CREATE ROLE petpassport_backup LOGIN PASSWORD '<secreto>';
GRANT pp_app_reader TO petpassport_backup;
GRANT CONNECT ON DATABASE petpassport TO petpassport_backup;
```

Versionado y ciclo de vida (idempotente; fotos antiguas 30 días, exportaciones 26 semanas):

```bash
mc alias set pp https://<endpoint-s3> <access-key-admin> <secret-admin>
infra/storage/apply.sh pp petpassport-media-staging petpassport-backups-staging
```

La credencial `PP_BACKUP_STORAGE_*` solo tiene `PutObject`/`GetObject` sobre el bucket de
respaldos (no puede borrar versiones); la de la API no tiene acceso a ese bucket.

### 9.2 Respaldo frío semanal de `es.events`

`backup export-events` (imagen del backend, unidad Quadlet `pp-events-export`) exporta `es.events` completo desde un snapshot
REPEATABLE READ a `es-events/AAAA/AAAA-MM-DDTHHMMSSZ.jsonl.gz` + `.manifest.json` (recuento,
rango de posiciones, sha256). Es independiente del proveedor de PostgreSQL.

`deploy.sh` instala y activa `pp-events-export.timer` en cada despliegue, con la misma versión de la imagen
que la API.

```bash
systemctl --user list-timers pp-events-export.timer   # próxima ejecución (domingo 03:00 UTC)
systemctl --user start pp-events-export.service       # ejecución manual
journalctl --user -u pp-events-export -n 50           # resultado ("es.events uploaded")
```

Si falla, `OnFailure=` lanza `pp-notify-failure@pp-events-export.service`, que avisa por `TEAM_WEBHOOK_URL`. La exportación contiene PII de los
eventos (`pii{}`): el bucket es privado y tras restaurarla se reaplican las supresiones de
privacidad (abajo).

### 9.3 Prueba mensual de restauración

Dos partes; ambas se anotan en `infra/deploy/RESTORE-LOG.md`.

**A. PITR del proveedor** (la restauración real ante incidente):

1. Restaurar en una instancia **temporal** a un punto de hace 24 h (consola/CLI del proveedor).
2. `podman run --rm -e PP_DB_URL=<url-temporal-migrador> --entrypoint /migrate <imagen-backend> version` → la
   última migración desplegada.
3. `SELECT count(*), max(global_position) FROM es.events;` y `\l` (existe `keycloak`).
4. Destruir la instancia temporal.

**B. Respaldo frío** (independiente del proveedor):

1. Descargar la última exportación y su manifiesto:
   `mc cp --recursive pp/petpassport-backups-staging/es-events/<AAAA>/<fichero>* ./`
2. `backup verify-events <fichero>.jsonl.gz` → coincide con el manifiesto (sale ≠ 0 si no).
3. En una PostgreSQL temporal vacía: `/migrate up` y después, con el rol migrador,
   `backup import-events <fichero>.jsonl.gz` (solo INSERT; se niega si `es.events` tiene datos).
4. `SELECT count(*), max(global_position) FROM es.events;` = `events`/`last_position` del manifiesto.

Tras una restauración real, antes de abrir tráfico, se reaplican las supresiones de privacidad
(docs/phase-1/security/privacy-runbook.md §5).

## 10. Keycloak

- **Reinicio**: `systemctl --user restart pp-keycloak`, que vuelve cuando Keycloak está sano. Las sesiones viven
  en la BD, así que los usuarios no tienen que volver a entrar. Después, si se tocó el realm:
  `systemctl --user start pp-keycloak-config`.
- **Consola**: `https://auth.${PP_DOMAIN}/admin/`, solo desde `TEAM_IP_RANGES` (WAF y Traefik).
- **Usuario bloqueado por brute force**: Users → el usuario → "Unlock" (solo tras verificar la
  identidad).
- **Revocar las sesiones de un usuario**: usar Administración → Usuarios → Suspender en la app, que
  hace también la parte de Keycloak y deja auditoría; o, solo en Keycloak, Users → Sessions → Logout.

## 11. Incidentes de seguridad (mínimo F1)

1. Contener: suspender las cuentas afectadas (§10), bloquear IPs en el WAF (§6) y rotar los
   secretos implicados (§5).
2. Preservar: exportar los logs del periodo (Loki → *Explore* → descargar, o `logcli` con el rango),
   las entradas de `audit_log` y los Security Events del WAF.
3. Comunicar a los afectados según la normativa (privacy-runbook y aviso de privacidad), y al equipo
   en el canal con un resumen y una línea de tiempo.
4. Revisar el threat model (docs/phase-1/security/threat-model.md) con lo aprendido.

## 12. Contacto on-call

| Rol | Persona | Canal |
|---|---|---|
| Plataforma (primario) | _pendiente de asignar (PM)_ | |
| Backend (secundario) | _pendiente de asignar (PM)_ | |
| Seguridad / privacidad | _pendiente de asignar (PM)_ | |

## 13. Correo transaccional (IAM-012, NOT-007)

Keycloak (verificación, recuperación de cuenta) y el worker (notificaciones de citas, carnet,
recordatorios, transferencias e invitaciones; el api solo las encola, ARQ-014) envían a través
del servidor de correo externo **Stalwart Mail Server** (`edcastdev.com`) como
`petpassport@edcastdev.com` por SMTP autenticado (STARTTLS puerto 587 o TLS puerto 465).
Tema de correo y de login: `infra/keycloak/themes/petpassport` (es/en; el idioma sigue al `ui_locales` de la SPA).

### 13.1 Parámetros de entorno del servidor SMTP (Stalwart)
Configurados en el archivo de entorno del despliegue (`/var/lib/petpassport/<env>/env/worker.env` y `keycloak.env`):
```ini
PP_SMTP_HOST=mail.edcastdev.com             # FQDN o IP del servidor Stalwart
PP_SMTP_PORT=587                            # 587 (STARTTLS) o 465 (TLS implícito)
PP_SMTP_USER=petpassport@edcastdev.com      # Cuenta o credencial SMTP de Stalwart
PP_SMTP_PASSWORD=<contraseña-secreta>       # Secreto de la cuenta en Stalwart
PP_SMTP_FROM=petpassport@edcastdev.com      # Remitente de Keycloak y del worker
PP_MAIL_ADDRESS="PetPassport, Bogotá, Colombia" # Dirección postal exigida fuera de dev (CAN-SPAM/GDPR)
PP_UNSUBSCRIBE_SECRET=<secreto-32+>         # Token HMAC para desuscripción en 1 clic
PP_EMAIL_WEBHOOK_SECRET=<secreto-32+>       # HMAC SHA256 para eventos/rebotes en /webhooks/email/stalwart
```

### 13.2 Estado de DNS y autenticación (edcastdev.com)
Los registros DNS del dominio `edcastdev.com` se encuentran configurados y activos:
- **SPF:** Registro TXT en `edcastdev.com` autorizando la IP/host del servidor Stalwart.
- **DKIM:** Clave pública DKIM publicada en el selector correspondiente (`<selector>._domainkey.edcastdev.com`).
- **DMARC:** Registro TXT en `_dmarc.edcastdev.com` (política `p=quarantine` o `p=reject`).

Comprobación rápida desde la VM o consola:
```bash
dig +short TXT edcastdev.com
dig +short TXT _dmarc.edcastdev.com
```

### 13.3 Rebotes y supresiones (Stalwart → PetPassport)
Stalwart puede emitir eventos de entrega fallida (hard bounce) o quejas (complaint) al endpoint:
`POST https://api.${PP_DOMAIN}/api/v1/webhooks/email/stalwart`
con la cabecera `X-PP-Signature: sha256=<hmac>` generada con `PP_EMAIL_WEBHOOK_SECRET`.
El backend registra automáticamente la dirección en `notifications.suppressions` para evitar reenvíos.

### 13.4 Prueba de envío
Antes de abrir el entorno a usuarios del piloto: registrar una cuenta de prueba en
`https://app.${PP_DOMAIN}` con la SPA en español y otra en inglés hacia buzones externos (Gmail,
Outlook). Confirmar que el correo llega a la bandeja de entrada (no spam) y que en "Mostrar original"
se verifique `spf=pass`, `dkim=pass` y `dmarc=pass`.

## 14. Operación con systemd (ADR-026)

Como `petpassport` (`sudo -iu petpassport`):

| Tarea | Comando |
|---|---|
| Estado de todo | `systemctl --user list-units 'pp-*'` · `podman ps` |
| Logs de un servicio | `journalctl --user -u pp-api -f` (también en Loki: `{service="api"}`) |
| Reiniciar un servicio | `systemctl --user restart pp-<servicio>` (espera al health check si tiene `Notify=healthy`) |
| Ver la unidad generada | `systemctl --user cat pp-api` · fuente: `~/.config/containers/systemd/petpassport/pp-api.container` |
| Validar las unidades instaladas | `QUADLET_UNIT_DIRS=~/.config/containers/systemd/petpassport /usr/libexec/podman/quadlet -user -dryrun` |
| Entorno de un servicio | `/var/lib/petpassport/<env>/env/<servicio>.env` (600; no copiar a tickets) |
| Health check a mano | `podman healthcheck run pp-api && echo sano` |
| Socket del 443 | `systemctl --user status pp-traefik.socket` (si cambia el `.socket`: `systemctl --user restart pp-traefik.socket`) |

Solo como root: `systemctl status pp-origin-firewall`, `nft list table inet pp-origin`,
`loginctl show-user petpassport -p Linger` (debe ser `yes`) y volver a ejecutar `host-setup.sh`.

- Las unidades instaladas son **generadas**: no se editan en la VM. Los cambios van por PR en
  `infra/quadlet/deploy/` y se aplican con `deploy.sh`; el siguiente despliegue sobrescribe cualquier edición local.
- Todas las unidades de larga duración tienen `Restart=always` y arrancan en el boot (`WantedBy=default.target`
  del systemd de usuario, que arranca gracias a linger). Tras un reinicio de la VM no hay que hacer nada;
  comprobar con la primera fila de la tabla.
- Reiniciar `pp-traefik` corta las conexiones nuevas durante unos segundos (en el ensayo fallaron 4 de 60
  peticiones durante el reinicio): el socket sigue abierto, pero no es un reinicio sin cortes.

---

### Registro de ensayos

| Fecha | Quién (≠ autor) | Secciones | Resultado / incidencias |
|---|---|---|---|
| | | | |

## 15. Worker y proyecciones asíncronas (F2 M2.0, ADR-009)

El worker (`pp-worker`, misma imagen que el api) ejecuta las proyecciones asíncronas, el consumidor
del outbox y el scheduler de comandos. Una réplica por grupo (lock de instancia). Los modos
administrativos se ejecutan en el contenedor en marcha y se coordinan con él por el checkpoint:

| Acción | Comando |
|---|---|
| Estado | `podman exec pp-worker /worker status` |
| Pausar / reanudar | `podman exec pp-worker /worker pause\|resume --projection <nombre>` |
| Rebuild de una proyección | `podman exec pp-worker /worker rebuild --projection <nombre>` |
| Rebuild de todas | `podman exec pp-worker /worker rebuild --all` |
| Rebuild parcial | `… rebuild --projection <nombre> --to-position <N>` |
| Borrar snapshots | `podman exec pp-worker /worker snapshots --drop` (sin pérdida) |
| Rebuild de primarias F1 (desastre) | `podman exec pp-worker /worker rebuild --sync` (ver §15.1) |
| Modo mantenimiento de la API | `podman exec pp-worker /worker maintenance on --reason "<motivo>"\|off\|status` |

Rebuild = pausa → borrado de las tablas de la proyección + checkpoint a 0 → replay por lotes de 500
→ reanuda; no toca `es.events`. Progreso en logs y en `projection_rebuild_progress`. Durante el
rebuild la API sirve la tabla parcial y las vistas muestran "Actualizando…".

`deploy.sh` detecta las migraciones con cabecera `-- rebuild: <nombre>` no aplicadas aún: detiene el
worker antes de migrar, lo arranca después y recuerda en el log el `rebuild` pendiente. El runbook
completo de F2 (tiempos esperados, dead-letter, restauración con replay) está en §15.3–§15.8 (INF-005).

### 15.1 Rebuild de las proyecciones primarias (`rebuild --sync`, solo desastre)

Para un read model primario de F1 corrupto o perdido (`read_petregistry.*`,
`read_partnernetwork.*`), con `es.events` intacto. Reconstruye **todas** las primarias a la vez,
en orden global (dependen entre sí: `org_reactions` actualiza `pet_care_links`).

1. Avisar en el canal: la API responderá 503 ("Servicio no disponible temporalmente") mientras dure.
2. `podman exec pp-worker /worker rebuild --sync`. El comando:
   - activa el modo mantenimiento (fila en `platform.maintenance`; cada api lo ve en ≤ 2 s) y espera 5 s;
   - en **una sola transacción** con el lock de orden total (ningún comando puede confirmar):
     guarda los datos operativos que no están en los eventos (hash de token y contadores de
     reenvío de invitaciones, `pet_transfers`, `reserved_at` de microchips), vacía las tablas,
     reproyecta todos los eventos y restaura esos datos;
   - confirma y desactiva el mantenimiento. Si algo falla, la transacción se revierte, los read
     models quedan como estaban y el mantenimiento se desactiva igualmente.
3. Comprobar: `podman exec pp-worker /worker maintenance status` → `off`; smoke de §5.

Si el proceso muere a mitad (OOM, reinicio del host), la transacción se revierte sola; el modo
mantenimiento puede quedar activo: `podman exec pp-worker /worker maintenance off`. Si el
mantenimiento ya estaba activo antes (activado a mano), `rebuild --sync` no lo desactiva.

Denormalizaciones (nombre del tutor, nombre y ciudad de la clínica en los vínculos) se resuelven
con el estado **actual** de Identity y Partner Network, como en cualquier replay de F1.

### 15.2 Ventana de la migración de M2.0 (`20261028000008_event_store_v1`)

La migración rellena `es.outbox.stream_id` y crea un índice sobre `es.events` dentro de una única
transacción: mientras dura, los comandos del api anterior **esperan** (no fallan). Medido con
`PP_MIGRATION_EVENTS` (test `TestMigration_EventStoreV1Backfill`): **5,1 s con 500 000 eventos**
(~1 s por 100 000). Con el timeout de petición de 30 s, el despliegue normal es seguro hasta
~2 M de eventos; por encima, desplegar en una ventana de bajo tráfico anunciada (algunos comandos
pueden agotar el timeout y el usuario reintenta). El modo mantenimiento no sirve aquí: lo crea esta
misma migración y el api anterior no lo conoce. Consultar el volumen: `SELECT count(*) FROM es.events;`.

### 15.3 Rebuild parcial o total de proyecciones asíncronas (INF-005)

Proyecciones asíncronas registradas: `clinical.*`, `scheduling.*`, `notifications.*` y
`partnernetwork.org_stats` (estadísticas mensuales de la clínica, PRD-026). `worker status` las lista.

- **Parcial**: `rebuild --projection <nombre>` (una) o `--to-position <N>` (hasta un punto, para
  diagnosticar). Las vistas de esa proyección muestran "Actualizando…" y el resto de la API no se ve afectado.
- **Total**: `rebuild --all`. Tiempos esperados: replay por lotes de 500 eventos; **orden de magnitud
  ~1–2 min por 100 000 eventos por proyección** (estimación; el valor real con >10 000 eventos de
  staging es evidencia pendiente de la salida de F2, `docs/phase-2/00` §5). Anotar aquí el tiempo medido
  tras cada ensayo.
- Seguimiento: logs del worker y `SELECT * FROM projection_rebuild_progress;`.
- Reanudar una proyección en `failed`: corregir la causa (log con el evento y el error), luego
  `podman exec pp-worker /worker resume --projection <nombre>`; si el estado quedó incoherente,
  `rebuild --projection <nombre>`.

### 15.4 Worker atascado

1. `worker status` y `SELECT instance_id, heartbeat_at FROM worker_instances ORDER BY heartbeat_at DESC;`
   Un `heartbeat_at` con más de 60 s indica worker caído o colgado (ver §7.8 WorkerDown).
2. Un lock huérfano (proceso muerto, sesión viva en Postgres) bloquea al nuevo worker:
   `SELECT pid, state, query_start FROM pg_stat_activity WHERE application_name LIKE 'pp-worker%';`
   y, si la sesión es de un worker que ya no existe, `SELECT pg_terminate_backend(<pid>);`.
3. `podman restart pp-worker`. Comprobar que `worker status` muestra los checkpoints avanzando.

### 15.5 Dead-letter (outbox y notificaciones)

- Outbox: `SELECT global_position, event_type, attempts, last_error FROM es.outbox WHERE dead_letter ORDER BY global_position DESC LIMIT 50;`
  (alerta §7.12). Corregir la causa y reintentar con `UPDATE es.outbox SET dead_letter=false, attempts=0, next_attempt_at=now() WHERE global_position = <n>;`
  (el outbox **no** es `es.events`: esta operación está permitida).
- Notificaciones: `SELECT id, template, last_error FROM notification_deliveries WHERE dead_letter;`
  (alerta §7.14). Reencolar solo si la causa fue del proveedor; si la dirección rebota, no reintentar.
- No registrar direcciones de correo en tickets: usar el `id` de la entrega.

### 15.6 Restauración con replay

Tras restaurar un respaldo (§9) a un punto anterior al fallo: `es.events` es la fuente de verdad, las
proyecciones se reconstruyen. Orden: (1) restaurar, (2) `worker rebuild --sync` (§15.1) para las
primarias, (3) `worker rebuild --all` para las asíncronas, (4) comprobar `worker status` sin lag y
smoke de §5. Los recordatorios y trabajos del scheduler se regeneran por replay; verificar que no se
duplican correos (la clave de idempotencia de cada entrega lo impide).

### 15.7 Rotación del secreto del proveedor de correo y verificación SPF/DKIM

1. Crear la nueva clave en el proveedor, actualizar el secreto del entorno (§13) y reiniciar
   `pp-api` y `pp-worker`.
2. Enviar un correo de prueba (registro de un usuario de staging) y confirmar la entrega en Mailpit/proveedor.
3. Revocar la clave anterior. Si hubo entregas fallidas durante la ventana, reencolarlas (§15.5).
4. SPF/DKIM/DMARC: `dig +short TXT <dominio>`, `dig +short TXT <selector>._domainkey.<dominio>` y
   revisar las cabeceras `Authentication-Results` de un correo real. Revisión trimestral junto a §9.1.

### 15.8 Retención de datos (DAT-008)

El bucle diario de mantenimiento del worker ejecuta `runRetention`: elimina las particiones de
auditoría de más de 2 años (`platform.drop_audit_partitions_before`), las entregas de notificación
enviadas o suprimidas de más de 180 días y los trabajos del scheduler despachados o cancelados de más de
30 días. **Nunca** toca `es.events`. Los plazos son una propuesta de ingeniería a confirmar con asesoría
legal (ver PR de M2.5). Los registros del job no incluyen datos personales.

## 16. Exportación de datos del usuario (SEC-006)

Solicitud de portabilidad (ACCESS/portabilidad, RGPD/Ley 1581): la atiende un operador autorizado.

1. Verificar la identidad de quien solicita desde su correo registrado y obtener su `user_id`.
2. `podman exec pp-worker /worker export --user <uuid> --out /tmp/export-<uuid>.json`
   (lectura en transacción de solo lectura; incluye cuenta, mascotas, vínculos, solicitudes de
   vínculo, historial clínico visible para el tutor, carnet sin claves de evidencia, citas y
   preferencias; **excluye** hashes de token y claves internas).
3. Entregar el archivo por un canal cifrado acordado con la persona (no por correo en claro) y borrarlo
   del servidor: `podman exec pp-worker rm /tmp/export-<uuid>.json`.
4. Registrar la solicitud (fecha, operador, `user_id`) en el libro de incidencias. Plazo máximo: 15 días hábiles.

## 17. Operación de plataforma, búsqueda y resiliencia F3 (PP-F3-INF-006)

Procedimientos operativos para la Fase 3: gestión de búsqueda en OpenSearch, resiliencia de Redis y colas asynq, coordinación distribuida de workers, escaneo antivirus ClamAV, Web Push (VAPID) y reglas CDN de borde.

### 17.1 Reindex sin downtime en OpenSearch (ARQ-003, DAT-001)

OpenSearch opera con aliases estables (`pets`, `clinical`, `medications`, `organizations`) apuntando a índices físicos versionados (`<alias>-v1`, `<alias>-v2`). La lectura se realiza contra `<alias>` y la indexación en segundo plano utiliza `<alias>-write`.

#### Paso a paso de reindexación sin parada:

1. **Inspección previa de estado y drift**:
   ```bash
   podman exec pp-worker /worker search status
   podman exec pp-worker /worker search mapping-check
   ```
   Si `mapping-check` reporta diferencias entre los JSON versionados en `backend/internal/search/mappings/*.json` y los mappings activos, se planifica la reindexación.

2. **Lanzar la reindexación con motivo auditado (SEC-006)**:
   ```bash
   podman exec pp-worker /worker search reindex --alias <alias> --reason "actualizacion schema mapping F3"
   ```
   El subcomando ejecuta atómicamente:
   - Crea el nuevo índice físico `<alias>-v(N+1)` con los mappings y analizadores del repositorio.
   - Añade el nuevo índice al alias de escritura `<alias>-write` para doble escritura temporal.
   - Sincroniza y reindexa los documentos desde PostgreSQL.
   - Realiza un cambio atómico de alias de lectura (`add <alias>-v(N+1)` y `remove <alias>-vN`) y retira el índice anterior de `<alias>-write`.
   - Registra el trabajo completado en `search.reindex_jobs`.

3. **Verificación post-conmutación**:
   - Comprobar que el alias apunta al nuevo índice: `podman exec pp-worker /worker search status`.
   - Verificar que no hay degradación en la API: cabecera `X-PP-Search-Mode` debe responder `normal` y la alerta `SearchDegraded` debe estar en estado `OK`.

4. **Rollback de emergencia del alias de lectura**:
   Si tras la conmutación se detectan anomalías o documentos incompletos, revertir inmediatamente el alias de lectura al índice físico anterior:
   ```bash
   curl -s -X POST "http://localhost:9200/_aliases" -H 'Content-Type: application/json' -d '{
     "actions": [
       {"remove": {"index": "<alias>-v(N+1)", "alias": "<alias>"}},
       {"add": {"index": "<alias>-vN", "alias": "<alias>"}}
     ]
   }'
   ```
   *Nota*: El índice anterior `<alias>-vN` se mantiene retenido durante al menos 24 horas antes de su eliminación física para garantizar la posibilidad de rollback instantáneo.

### 17.2 Incidente de OpenSearch caído (ARQ-004, SEC-002, QA-006)

#### Diagnóstico:
1. Alerta `SearchDegraded` disparada (> 5% de búsquedas degradadas) o `SearchIndexLagHigh`.
2. Comprobar salud del cluster local:
   ```bash
   curl -fsS http://localhost:9200/_cluster/health | jq .
   systemctl --user status pp-opensearch
   ```
3. **Verificación de degradación automática**:
   Confirmar que la API continúa respondiendo búsquedas de pacientes e historiales clínicos mediante el fallback a PostgreSQL (búsqueda por trigramas en `pets` y consultas ILIKE en `read_clinical.timeline_entries`). La API inyecta la cabecera `X-PP-Search-Mode: degraded` y `X-PP-Freshness`. La tasa de error 500 debe permanecer en 0%.

#### Restauración del servicio:
1. **Reinicio del servicio**:
   ```bash
   systemctl --user restart pp-opensearch
   ```
   El circuit breaker de `internal/search/infra` entrará en estado *half-open* a los 30 segundos; tras 3 consultas consecutivas exitosas contra OpenSearch, se cerrará y la API volverá automáticamente a `X-PP-Search-Mode: normal`.

2. **Decisión: Snapshot vs Rebuild completo**:
   - **Restaurar Snapshot (DAT-009)**: Si el almacenamiento de OpenSearch sufrió corrupción física pero el repositorio de snapshots está íntegro:
     ```bash
     # Listar snapshots disponibles
     curl -s -X GET "http://localhost:9200/_snapshot/petpassport_snapshots/_all" | jq .
     # Restaurar snapshot
     curl -s -X POST "http://localhost:9200/_snapshot/petpassport_snapshots/<snapshot_name>/_restore" -H 'Content-Type: application/json' -d '{
       "indices": "pets-*,clinical-*,medications-*,organizations-*",
       "include_global_state": false
     }'
     ```
   - **Rebuild desde PostgreSQL (fuente de verdad)**: Si no hay snapshot disponible o se prefiere reconstruir desde el histórico de eventos:
     ```bash
     podman exec pp-worker /worker search init
     podman exec pp-worker /worker search reindex --alias pets --reason "recuperacion de desastre post-caida"
     podman exec pp-worker /worker search reindex --alias clinical --reason "recuperacion de desastre post-caida"
     podman exec pp-worker /worker search reindex --alias medications --reason "recuperacion de desastre post-caida"
     ```

### 17.3 Incidente de Redis caído o vaciado (ARQ-005, ARQ-006, ARQ-007, QA-006)

Redis se utiliza exclusivamente como almacén efímero: DB 0 para caché compartida L2 y rate limiting distribuido; DB 1 para la cola de tareas asíncronas asynq. PostgreSQL es siempre la fuente durable de verdad.

#### Comportamiento durante la caída:
- **Rate Limiting (ARQ-007)**: Entra inmediatamente en modo **fail-open** (`RateLimitFailOpen`). Las peticiones no se bloquean, garantizando la continuidad del servicio frente a picos o fallos de red hacia Redis.
- **Caché compartida L2 (ARQ-006)**: Entra en **fail-miss**. Las comprobaciones de membresía de organizaciones y accesos de mascota (`PetAccess`) se resuelven contra PostgreSQL y se conservan en la memoria L1 de cada réplica de la API.
- **Asynq / Colas (ARQ-008)**: Los eventos entrantes se persisten en `search.index_queue` y `es.outbox` en PostgreSQL. Los runners de asynq pausan temporalmente la extracción de tareas hasta que Redis se restablezca.

#### Recuperación:
1. Reiniciar Redis:
   ```bash
   systemctl --user restart pp-redis
   ```
2. Si se ejecutó un `FLUSHALL` (o pérdida de memoria):
   - El sistema se recupera de inmediato sin reiniciar la API ni los workers (verificado por el test de integración `TestRedisCacheFlushall`).
   - La caché L2 se repuebla automáticamente con el tráfico entrante.
   - Los productores de worker procesan las tareas acumuladas en `search.index_queue` sin pérdida de información.

### 17.4 Gestión y liberación de leases huérfanos de worker (ARQ-009, DAT-003)

Cada réplica de worker compite por leases individuales por runner (`projections`, `outbox`, `command-scheduler`, `clinical.*`, `notifications.*`) almacenados en `platform.worker_leases` con un TTL de 30 segundos renovado cada 10 segundos.

#### Diagnóstico:
1. Listar el estado de los leases activos:
   ```bash
   podman exec pp-worker /worker leases
   ```
   Salida esperada:
   ```text
   Runner              Holder              ExpiresIn      Renewals
   projections         pp-worker-1         21s            142
   outbox              pp-worker-2         18s            98
   command-scheduler   pp-worker-1         24s            110
   ```
2. Si una réplica murió violentamente (OOM, pánico de kernel) y la alerta `LeaseTakeoverStorm` o `OutboxStuck` se dispara:
   - El lease expirará por TTL en ≤ 30 segundos y otra réplica tomará el control automáticamente.
   - Si un lease queda bloqueado artificialmente o se requiere forzar su cesión inmediata:
     ```bash
     podman exec pp-worker /worker leases --release <nombre_del_runner>
     ```
     Ejemplo: `podman exec pp-worker /worker leases --release projections`.

### 17.5 DLQ (Dead-Letter Queue) de asynq (ARQ-008)

Tareas no ordenadas (indexación OpenSearch en bulk, renderizado de PDF Typst, escaneo ClamAV y despachos de notificaciones) gestionadas por colas `critical`, `default` y `low`.

#### Inspección y reintento:
1. **Acceso a Asynqmon**:
   Crear un túnel SSH hacia la máquina de staging:
   ```bash
   ssh -L 8088:127.0.0.1:8088 petpassport@<vm-staging>
   ```
   Abrir `http://localhost:8088` en el navegador para inspeccionar colas activas, latencias y la cola `archived` (DLQ).

2. **Diagnóstico de fallos en DLQ**:
   - Identificar el campo `error_message` y el tipo de tarea (`search:bulk`, `clinical:pdf`, `notifications:dispatch`).
   - Si el fallo se debió a indisponibilidad transitoria de un servicio dependiente (p. ej. OpenSearch caído durante una indexación masiva), presionar **Retry All** en Asynqmon o ejecutar desde la API administrativa:
     ```bash
     curl -s -X POST "http://localhost:9090/admin/search/index-queue/retry" -H "Authorization: Bearer $SUPERADMIN_TOKEN"
     ```
3. **Descarte definitivo de tareas**:
   Si la tarea falló por datos irreversibles o malformados, eliminar la tarea desde Asynqmon registrando obligatoriamente el `task_id` y el motivo en el registro de incidencias del equipo.

### 17.6 Cuarentena antivirus ClamAV y reescaneo (ARQ-014, SEC-005)

Todo adjunto subido a un encuentro clínico se analiza con ClamAV mediante el servicio `antivirus.Service`.

#### Protocolo ante archivo infectado:
1. **Aislamiento automático**:
   - El escáner detecta la firma de malware.
   - El archivo se traslada en el Object Storage desde su ubicación original hacia `quarantine/<key>` y se borra de la ruta accesible.
   - Los registros en `read_clinical.attachment_uploads` y `read_clinical.encounter_attachments` se actualizan a `scan_status = 'infected'`.
   - Se crea una alerta en `platform.security_alerts` (`alert_type = 'malware_detected'`).
2. **Consulta de alertas de seguridad**:
   ```sql
   SELECT id, alert_type, details, created_at
   FROM platform.security_alerts
   WHERE alert_type = 'malware_detected'
   ORDER BY created_at DESC;
   ```
3. **Actualización de firmas y reescaneo**:
   Si se sospecha de un falso positivo o firmas obsoletas (`ClamAVDefinitionsStale`):
   ```bash
   podman exec pp-clamav freshclam
   podman exec pp-clamav clamscan --version
   ```

### 17.7 Rotación de credenciales Web Push (VAPID) (DOM-011, ARQ-015, SEC-007)

Las notificaciones push para navegadores y PWA se autentican con el protocolo RFC 8292 mediante un par de claves elípticas VAPID (`p256dh`).

#### Procedimiento de rotación:
1. Generar nuevo par de claves criptográficas:
   ```bash
   # Utilizando el comando del worker o script interno
   podman exec pp-api /api vapid-generate-keys
   ```
2. Actualizar las credenciales en `/etc/petpassport/staging.env`:
   ```bash
   PP_VAPID_PUBLIC_KEY="<nueva_clave_publica_base64url>"
   PP_VAPID_PRIVATE_KEY="<nueva_clave_privada_base64url>"
   ```
3. Reiniciar los servicios de forma escalonada:
   ```bash
   systemctl --user restart pp-api pp-worker
   ```
4. **Comportamiento en clientes**:
   Al cargar la SPA, el cliente compara la clave pública devuelta por `GET /push/vapid-public-key` con la almacenada localmente. Si difiere, solicita automáticamente al `PushManager` del navegador una nueva suscripción y la registra mediante `POST /me/push-subscriptions` sin requerir acción manual del tutor.

### 17.8 Purga de CDN en Cloudflare (ARQ-016, INF-007)

Cloudflare opera como capa de borde y CDN delante del gateway Traefik.

#### Reglas de almacenamiento en caché vigentes:
- `/assets/*`, `/fonts/*` y hashes de compilación: `Cache-Control: public, max-age=31536000, immutable`.
- `/index.html`, `/manifest.webmanifest` y `/config.json`: `Cache-Control: no-cache, no-store, must-revalidate`.
- `/api/*`: `Cache-Control: no-store` con bypass total de caché de borde (Cloudflare Rule: Cache Level = Bypass).
- `/media/*`: Acceso restringido por URLs firmadas privadas.

#### Procedimiento de purga de caché:
1. **Purga selectiva en despliegue (automática)**:
   `deploy.sh` purga únicamente `index.html` tras actualizar la imagen del frontend:
   ```bash
   curl -s -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/purge_cache" \
     -H "Authorization: Bearer ${CF_API_TOKEN}" \
     -H "Content-Type: application/json" \
     -d "{\"files\": [\"https://app.${PP_DOMAIN}/index.html\", \"https://app.${PP_DOMAIN}/config.json\"]}"
   ```
2. **Purga completa de emergencia (en caso de incidente de activos web)**:
   ```bash
   curl -s -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/purge_cache" \
     -H "Authorization: Bearer ${CF_API_TOKEN}" \
     -H "Content-Type: application/json" \
     -d '{"purge_everything": true}'
   ```

