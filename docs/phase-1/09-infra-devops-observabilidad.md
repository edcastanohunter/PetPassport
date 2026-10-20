# F1 · 09 — Infraestructura, DevOps y observabilidad

Marco: ADR-007 (monorepo), ADR-008 (despliegue inicial).

## 1. Monorepo y tareas

**[PP-F1-INF-001] (MUST)**

```
petpassport/
  backend/   frontend/   infra/   docs/   scripts/   .github/
  Makefile   .editorconfig   .pre-commit-config.yaml   .tool-versions (go, node, pnpm)
```

`Makefile` (objetivos mínimos): `dev-up`, `dev-down`, `dev-reset` (borra volúmenes), `migrate`, `gen` (oapi-codegen, sqlc, openapi-typescript), `lint`, `test`, `test-integration`, `e2e`, `build`, `images`, `specs-status`, `specs-check`.

Pre-commit: `gofmt`/`goimports`, `golangci-lint` (rápido), `eslint --fix`, `prettier`, `gitleaks`, `spectral` sobre OpenAPI.

## 2. Entorno de desarrollo (Docker Compose)

**[PP-F1-INF-002] (MUST)** `infra/compose/docker-compose.yml` + `docker-compose.override.yml` (hot reload):

| Servicio | Imagen | Puertos (host) | Notas |
|---|---|---|---|
| `traefik` | `traefik:v3.x` | 80, 443, 8081 (dashboard dev) | TLS local con `mkcert`; hosts `app.pp.localhost`, `api.pp.localhost`, `auth.pp.localhost`. |
| `postgres` | `postgres:17` | 5432 | Extensiones `citext`, `pg_trgm`; init crea roles `pp_migrator`, `pp_app_writer`, `pp_app_reader`. |
| `keycloak` | `quay.io/keycloak/keycloak:26.x` | — (vía Traefik) | `start-dev` en dev; BD propia en el mismo Postgres (`keycloak` db). `KC_PROXY_HEADERS=xforwarded`, `KC_HOSTNAME=https://auth.pp.localhost`. |
| `keycloak-config` | `adorsys/keycloak-config-cli` | — | Aplica `infra/keycloak/realm.yaml` al arrancar; `IMPORT_VARSUBSTITUTION_ENABLED=true`. |
| `minio` | `minio/minio` | 9000, 9001 | Bucket `petpassport-media` creado por `mc` init. |
| `mailpit` | `axllent/mailpit` | 8025 | SMTP 1025. |
| `otel-collector` + `tempo` + `prometheus` + `grafana` + `loki` | perfil `observability` | 3000 | Opcional: `docker compose --profile observability up`. |
| `api` | build local (`air` hot reload) | — | Depende de `postgres`, `keycloak-config`. |
| `frontend` | `node:22` + Vite dev server | 5173 (también vía Traefik) | |
| `migrate` | build local | — | *One-shot* al levantar. |

Seeds de desarrollo (`make dev-seed`): usuarios `owner@dev.local`, `vet@dev.local`, `admin@dev.local` (super_admin), una clínica verificada y una pendiente, tres mascotas. Los seeds pasan por la API (no por SQL) para ejercitar los eventos.

## 3. CI (GitHub Actions)

**[PP-F1-INF-003] (MUST)** Workflows con filtros por ruta; todos bloqueantes para merge en `main`:

| Workflow | Jobs |
|---|---|
| `backend.yml` | `lint` (golangci-lint, depguard/arch-lint, go vet), `unit` (`go test -race -cover ./...`), `integration` (Postgres como *service container* + `testcontainers`), `generate-check` (`make gen` sin diff), `migrations` (up/down/up + `sqlc vet`), `security` (`govulncheck`, `gosec`, `semgrep`). |
| `frontend.yml` | `lint` (eslint, prettier, `vue-tsc`), `unit` (vitest con cobertura), `build`, `security` (`npm audit`, `osv-scanner`). |
| `api-contract.yml` | `spectral lint`, `oasdiff breaking` contra `main`. |
| `docs.yml` | `scripts/specs-status.sh --check`, `markdownlint`, enlaces rotos (`lychee`). |
| `images.yml` (en `main` y tags) | build multi-arch `api`, `frontend`; `trivy`; push a registro con `sha` y `semver`; SBOM `syft`; firma `cosign`. |
| `e2e.yml` (nightly y manual) | levanta compose completo, ejecuta Playwright, publica reporte. |
| `secrets.yml` | `gitleaks` sobre el historial del PR. |

Caché de módulos Go y `node_modules`; tiempo objetivo del pipeline de PR < 10 min.

## 4. Imágenes

**[PP-F1-INF-004] (MUST)**

- `api`: multi-stage `golang:1.25` → `gcr.io/distroless/static-debian12:nonroot`; binario estático (`CGO_ENABLED=0`), `-trimpath -ldflags "-s -w -X main.version=…"`; `USER nonroot`; `HEALTHCHECK` no (lo hace Traefik/compose vía `/readyz`).
- `frontend`: `node:22` build → `nginxinc/nginx-unprivileged:alpine` con `nginx.conf` que sirve `index.html` con `no-cache` y assets con `immutable`; `/config.json` montado por entorno.
- Etiquetas OCI (`org.opencontainers.image.*`), tamaño objetivo `api` < 40 MB.

## 5. Entornos y configuración

**[PP-F1-INF-005] (MUST)**

| Entorno | Propósito | Datos | Acceso |
|---|---|---|---|
| `dev` | local | sintéticos | — |
| `staging` | integración, QA, piloto cerrado | sintéticos + piloto (datos reales mínimos con consentimiento) | público tras WAF, Keycloak con registro abierto |
| `prod` | — (activación en M1.5 / F2) | reales | público |

- Config por variables `PP_*` (ARQ-011) y `/config.json` del front; sin ramas por entorno: misma imagen promovida.
- Secretos vía secret manager → `.env` generado en la VM en despliegue, permisos `600`, nunca commiteado.
- DNS: `app.`, `api.`, `auth.` apuntan al WAF; el origen por nombre interno.

## 6. Despliegue en staging y runbook

**[PP-F1-INF-006] (MUST)**

- VM (Ubuntu LTS, 2–4 vCPU, 8 GB) con Docker; `infra/deploy/deploy.sh <env> <version>`:
  1. `docker compose pull` de la versión,
  2. `migrate up` (contenedor one-shot; aborta el despliegue si falla),
  3. `docker compose up -d api frontend` (reemplazo con `--wait` y `readyz`),
  4. *smoke test* (`/readyz`, login de usuario sintético, `GET /me`),
  5. anotación en Grafana y mensaje en el canal del equipo.
- Rollback: `deploy.sh <env> <version-anterior>` (las migraciones son compatibles hacia atrás una versión, DAT-002).
- Runbook (`infra/deploy/RUNBOOK.md`): despliegue, rollback, rotación de secretos, restauración de backup, reinicio de Keycloak, purga de WAF, contacto on-call. Se ensaya una vez por alguien distinto a su autor antes de M1.5.

## 7. Observabilidad

**[PP-F1-INF-007] (MUST)**

- **Logs**: `slog` JSON a stdout → recolector (Loki en dev/staging; servicio gestionado en prod). Campos obligatorios (SEC-007).
- **Trazas**: OpenTelemetry SDK Go (`otelhttp`, `otelpgx`), propagación W3C `traceparent`; exportación OTLP al collector; muestreo 100% en staging, 10% en prod (+100% de errores). El front envía `traceparent` en requests (opcional, FE-012).
- **Métricas** (Prometheus, `/metrics` interno):
  - HTTP: `http_server_requests_total{route,method,status}`, `http_server_request_duration_seconds` (histograma), *in-flight*.
  - Dominio: `commands_total{command,result}`, `command_duration_seconds`, `events_appended_total{type}`, `eventstore_concurrency_conflicts_total`, `projection_duration_seconds{projector}`, `outbox_pending`, `authz_denied_total{action}`, `idempotency_replays_total`.
  - Infra: pool pgx (`pgxpool` stats), Go runtime, JWKS refresh.
- **Dashboards Grafana** (provisionados en `infra/observability/grafana/`): "API overview" (RED), "Event Store", "Auth & Authz", "Keycloak" (métricas nativas `KC_METRICS_ENABLED=true`).
- **Alertas** (staging, mínimo): error rate 5xx > 1% 5 min, p95 > 500 ms 10 min, `readyz` caído 2 min, `outbox_pending` creciendo > 1 h (señal de que falta el consumidor en F2; en F1 informativa), certificados < 14 días.

## 8. Health checks y apagado

**[PP-F1-INF-008] (MUST)** Ver ARQ-011: `/healthz` (liveness), `/readyz` (readiness con dependencias), apagado ordenado con drenado. Traefik usa `/readyz` como *health check* del servicio; compose `healthcheck` con `start_period: 20s`.

## 9. Datos gestionados y backups

**[PP-F1-INF-009] (MUST)**

- Prod y staging: PostgreSQL gestionado (versión 17), `ssl` obligatorio, backups automáticos + PITR (DAT-009), alertas de almacenamiento > 80%.
- Object storage gestionado con versionado y política de ciclo de vida (30 días para versiones antiguas de fotos).
- Keycloak usa la misma instancia gestionada en BD separada (`keycloak`); se incluye en el backup.
- Prueba de restauración mensual (DAT-009) con resultado registrado en `infra/deploy/RESTORE-LOG.md`.

## 10. Costes orientativos F1 (staging + prod pequeño)

| Partida | Rango mensual |
|---|---|
| 2 VMs (staging, prod) | 40–80 USD |
| PostgreSQL gestionado ×2 (pequeño) | 30–100 USD |
| Object storage + egress | < 10 USD |
| WAF/CDN (plan Pro o equivalente) | 20–40 USD |
| SMTP transaccional | 0–20 USD |
| Observabilidad gestionada (opcional) | 0–50 USD |
