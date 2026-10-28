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

## 13. Correo transaccional (IAM-012)

Keycloak (verificación, recuperación) y la API (invitaciones, transferencias) envían como
`no-reply@${PP_DOMAIN}` por el proveedor SMTP (STARTTLS, puerto 587). Tema de correo y de login:
`infra/keycloak/themes/petpassport` (es/en; el idioma sigue al `ui_locales` de la SPA).

1. **Proveedor**: verificar el dominio `${PP_DOMAIN}` y crear una credencial SMTP solo de envío.
   `PP_SMTP_HOST/PORT/USER/PASSWORD` van al env file (§2). `deploy.sh` las copia a los `EnvironmentFile`
   de `pp-api` y `pp-keycloak-config`, y este último las escribe en el realm.
2. **DNS como código**: copiar los valores del proveedor en `staging.tfvars`
   (`mail_spf_includes`, `mail_dkim_records`, `mail_dmarc_policy`, `mail_dmarc_rua`) y aplicar
   `infra/waf/cloudflare` (§6). Comprobar:
   ```bash
   dig +short TXT ${PP_DOMAIN}                 # "v=spf1 include:<proveedor> -all"
   dig +short TXT _dmarc.${PP_DOMAIN}          # "v=DMARC1; p=quarantine; adkim=s; aspf=s; ..."
   dig +short CNAME <selector>._domainkey.${PP_DOMAIN}
   ```
3. **Prueba de envío** (antes de abrir el piloto): registrar una cuenta de prueba en
   `https://app.${PP_DOMAIN}` con la SPA en español y otra en inglés hacia buzones de Gmail y
   Outlook. Debe llegar a la bandeja de entrada (no spam) con el tema PetPassport en el idioma
   elegido, y en "Mostrar original" verse `spf=pass`, `dkim=pass` y `dmarc=pass`. Anotar el
   resultado en el PR de M1.5.
4. **DMARC**: `quarantine` desde el inicio; pasar a `reject` tras 4 semanas de informes sin
   fuentes legítimas fallando.

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
completo de F2 (tiempos esperados, dead-letter, restauración con replay) es INF-005 (M2.5).

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
