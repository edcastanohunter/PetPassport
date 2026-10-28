# Runbook de operación: staging (PP-F1-INF-006)

Entorno: una VM Ubuntu LTS con Docker y servicios gestionados (PostgreSQL, object storage, SMTP)
detrás de Cloudflare (ADR-006, ADR-008). El dominio es un parámetro (`PP_DOMAIN`). Los hosts
públicos son `app.`, `api.` y `auth.` bajo ese dominio.

> **Ensayo obligatorio**: alguien distinto de quien escribió este runbook ejecuta una vez §3, §4 y
> §9 antes de M1.5 y anota el resultado al final del documento.

Contenido: §1 VM · §2 Secretos · §3 Despliegue · §4 Rollback · §5 Rotación · §6 WAF ·
§7 Alertas · §8 Observabilidad · §9 Backups · §10 Keycloak · §11 Incidentes · §12 Contacto.

## 1. Preparación de la VM (una vez)

1. VM con 2–4 vCPU y 8 GB. Acceso SSH solo por clave más MFA del proveedor, sin contraseñas
   (`PasswordAuthentication no`) y con registro de sesiones del proveedor activo.
2. Paquetes: `docker-ce`, `docker-compose-plugin`, `curl`, `jq`, `openssl`, `util-linux` (`flock`), `git`, `ufw`.
3. Firewall: solo SSH desde la VPN del equipo y 443 desde Cloudflare.
   ```bash
   ufw default deny incoming && ufw default allow outgoing
   ufw allow from <ip-vpn>/32 to any port 22 proto tcp
   for r in $(grep -v '^#' infra/waf/cloudflare-ips.txt); do ufw allow from "$r" to any port 443 proto tcp; done
   ufw enable
   ```
   Docker publica puertos saltándose las reglas `INPUT` de ufw. Por eso se replica la restricción
   en la cadena `DOCKER-USER`:
   ```bash
   iptables -I DOCKER-USER -p tcp --dport 8443 -j DROP
   for r in $(grep -v '^#' infra/waf/cloudflare-ips.txt | grep -v ':'); do
     iptables -I DOCKER-USER -p tcp --dport 8443 -s "$r" -j RETURN; done
   netfilter-persistent save
   ```
   (Puerto 8443 = destino del 443 publicado tras el DNAT. Para IPv6, lo mismo con `ip6tables`.)
4. Certificado de origen de Cloudflare (SSL/TLS → Origin Server, 15 años, `*.${PP_DOMAIN}`):
   `/var/lib/petpassport/staging/certs/origin.pem` y `origin-key.pem` (dueño root, `600`).
5. Clonar el repo en `/opt/petpassport` (solo lectura para el despliegue; `git pull` del tag a desplegar).
6. `docker login ghcr.io` con un token de solo lectura de paquetes.

## 2. Secretos y configuración (INF-005, SEC-006)

- Fuente de verdad: el secret manager del proveedor (o SOPS+age). En la VM se genera
  `/etc/petpassport/staging.env` a partir de `infra/deploy/staging.env.example`:
  `install -m 600 -o root -g root /dev/null /etc/petpassport/staging.env` y se rellenan los valores.
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
cd /opt/petpassport && git fetch --tags && git checkout <tag-o-sha>
sudo infra/deploy/deploy.sh staging sha-<sha-largo>
```

Pasos del script: pull → `migrate up` → Traefik, Keycloak y realm → observabilidad → `api`/`frontend`
con `--wait` → smoke → anotación en Grafana y aviso al canal del equipo. Volver a ejecutarlo con la
misma versión no cambia nada.

- Si `migrate` falla, el despliegue se aborta y la versión anterior sigue sirviendo. Revisar el log y
  corregir con una migración nueva; nunca se edita una migración aplicada.
- Si el smoke falla, el script hace rollback automático a la versión anterior y termina con error.
  Ver la salida (`SMOKE FAIL: …`) y §4.
- Estado: `/var/lib/petpassport/staging/current-version` y `history`.

## 4. Rollback

```bash
sudo infra/deploy/deploy.sh staging "$(tail -2 /var/lib/petpassport/staging/history | head -1 | cut -d' ' -f2)"
```

Las migraciones son compatibles hacia atrás una versión (DAT-002), así que el rollback no ejecuta
`down`. Si la versión a la que se vuelve es dos o más versiones anterior, primero hay que comprobar en
las notas de las migraciones intermedias que no haya cambios incompatibles. Si el rollback tampoco
llega a healthy: `docker compose … logs api`, contrastar con la alerta activa (§7) y escalar (§12).

## 5. Rotación de secretos (cada 6 meses o ante incidente)

| Secreto | Procedimiento |
|---|---|
| `PP_BACKEND_CLIENT_SECRET` | Keycloak → clients → `petpassport-backend` → Credentials → Regenerate. Actualizar el secret manager y el `.env`. `deploy.sh staging <versión-actual>` (keycloak-config reaplica el valor y la API se reinicia). |
| Contraseñas de BD | Crear contraseña nueva en el proveedor (`ALTER ROLE … PASSWORD`), actualizar el `.env` y redeplegar la versión actual. |
| `PP_ORIGIN_AUTH_SECRET` | 1) `terraform apply` con el nuevo `TF_VAR_origin_auth_secret`. 2) Inmediatamente después, actualizar el `.env` y ejecutar `docker compose … up -d traefik`. Entre 1 y 2 el origen responde 404 (unos segundos): hacerlo en una ventana de bajo tráfico. |
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
`/readyz` lleva 2 min caído. Ejecutar `docker compose … ps` y `logs api`. Normalmente es la BD o
Keycloak (JWKS). Si la API está en bucle de reinicios tras un despliegue, rollback (§4).

### 7.4 OutboxPendingGrowing
Informativa en F1: no hay consumidor del outbox hasta F2. Solo se actúa si el crecimiento es
anómalo (tamaño de la tabla).

### 7.5 CertificateExpiringSoon
Un certificado público caduca en menos de 14 días. Los del borde los renueva Cloudflare: si salta,
revisar el estado del certificado en SSL/TLS → Edge Certificates. El de origen lo vigila `deploy.sh`
(aviso en cada despliegue): regenerarlo (§1.4) y redeplegar.

### 7.6 ScrapeTargetDown
Prometheus no puede leer api, keycloak o traefik. Comprobar la red `default` del proyecto y el
contenedor afectado.

## 8. Observabilidad

Grafana, Prometheus y Alertmanager solo escuchan en `127.0.0.1`. Se accede con un túnel:
`ssh -L 3000:127.0.0.1:3000 <vm>` y después `http://localhost:3000` (usuario admin con
`GRAFANA_ADMIN_PASSWORD`; en staging no hay acceso anónimo). Los dashboards están en la carpeta
*PetPassport*: API overview, Event Store, Auth & Authz y Keycloak. `deploy.sh` usa el
`GRAFANA_TOKEN` (service account con rol Editor) solo para las anotaciones de despliegue.

## 9. Backups y restauración (INF-009, DAT-009)

- PostgreSQL gestionado: backups automáticos con PITR (incluye la base `keycloak`).
- **Prueba mensual**:
  1. Restaurar en una instancia temporal a un punto de hace 24 h.
  2. Conectar un `migrate status` (debe estar en la última versión).
  3. Contar los eventos: `SELECT count(*) FROM es.events`.
  4. Anotar el resultado en `infra/deploy/RESTORE-LOG.md`.
- Tras una restauración real, antes de abrir tráfico, se reaplican las supresiones de privacidad
  (docs/phase-1/security/privacy-runbook.md §5).

## 10. Keycloak

- **Reinicio**: `docker compose … restart keycloak`. Las sesiones viven en la BD, así que los usuarios
  no tienen que volver a entrar. Después: `docker compose … run --rm keycloak-config` si se tocó el
  realm.
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

---

### Registro de ensayos

| Fecha | Quién (≠ autor) | Secciones | Resultado / incidencias |
|---|---|---|---|
| | | | |
