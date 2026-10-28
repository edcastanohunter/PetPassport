# PetPassport

Pasaporte digital de salud para mascotas: carnet de vacunas, historial clínico inmutable, citas y conexión
con clínicas y farmacias aliadas. SaaS público. Backend Go (monolito modular, CQRS-ES), frontend Vue 3,
Keycloak, PostgreSQL, OpenSearch y Redis.

- Especificaciones y roadmap: [`docs/`](docs/README.md) · decisiones: [`docs/adr/`](docs/adr/README.md)
- Estado de implementación: `scripts/specs-status.sh`
- Instrucciones para agentes y personas: [`CLAUDE.md`](CLAUDE.md) y [`docs/handoff/`](docs/handoff/README.md)

## Arrancar en 10 minutos

Requisitos: [Podman](https://podman.io) ≥ 5 con `podman compose` y un proveedor de Compose (`docker-compose` v2 o
`podman-compose`), [mkcert](https://github.com/FiloSottile/mkcert), Go 1.26 y Node 22 (solo para ejecutar pruebas
fuera de los contenedores). Motor y despliegue: ADR-026.

- **Linux**: el entorno publica 80/443, así que `podman` rootful (`make dev-up ENGINE="sudo podman"`) o
  rootless con `sysctl net.ipv4.ip_unprivileged_port_start=80`. Para las pruebas de integración:
  `systemctl --user enable --now podman.socket`.
- **Windows / macOS**: `podman machine init --rootful && podman machine start`. La máquina expone la API que usan
  `podman compose` y testcontainers.

```bash
cp .env.example .env          # valores de desarrollo
make dev-certs                # certificados TLS locales para *.pp.localhost
make dev-up                   # traefik, postgres, keycloak (+ realm como código), minio, mailpit, api, frontend
make dev-seed                 # owner@dev.local, vet@dev.local, admin@dev.local (super_admin)
```

| Servicio | URL |
|---|---|
| App | https://app.pp.localhost |
| API | https://api.pp.localhost/api/v1/healthz |
| Keycloak | https://auth.pp.localhost (consola admin: `admin`/`admin` en dev) |
| Mailpit | http://localhost:8025 |
| MinIO | http://localhost:9001 |
| Métricas / readiness de la API | http://localhost:9090/metrics · http://localhost:9090/readyz |
| Traefik dashboard | http://localhost:8081 |

Comprobar un token de Keycloak con la audiencia correcta (rol `pet_owner` por defecto):

```bash
curl -s -X POST https://auth.pp.localhost/realms/petpassport/protocol/openid-connect/token \
  -d grant_type=password -d client_id=petpassport-web \
  -d username=owner@dev.local -d password=Password-Dev-2026 | jq -r .access_token | cut -d. -f2 | base64 -d 2>/dev/null | jq .
```

> Nota: `direct grant` está deshabilitado en `petpassport-web` por diseño (PKCE únicamente); el comando anterior
> fallará a propósito. Para obtener un token de prueba usa el flujo del navegador desde la app (M1.1) o un token
> firmado por el testkit (`backend/test/testkit`).

## Verificación manual del entorno (no cubierta por CI)

La CI ejecuta lint, pruebas unitarias, la prueba de integración con PostgreSQL real (testcontainers), el contrato
OpenAPI y la construcción de imágenes. **No** levanta el entorno Compose completo en cada PR (Keycloak + `keycloak-config-cli`,
Traefik, MinIO, Mailpit; lo hace el E2E nocturno). Tras cada cambio en `infra/` conviene verificarlo con Podman:

```bash
cp .env.example .env
make dev-certs                              # una vez por máquina (mkcert)
make dev-up                                 # espera a que keycloak-config y migrate terminen (podman compose ps)
make dev-seed                               # usuarios semilla
curl -k https://api.pp.localhost/api/v1/healthz        # {"status":"ok"}
curl -s localhost:9090/readyz | jq                     # "db": {"status":"ok"}
open https://auth.pp.localhost/realms/petpassport/account   # cuenta de Keycloak (login con owner@dev.local)
```

Si algo falla, los puntos más sensibles son el tag de `adorsys/keycloak-config-cli` y el `healthcheck` de Keycloak
en `infra/compose/compose.yaml`. Reporta el resultado en el PR del hito correspondiente.

Flujo de identidad (M1.1) de punta a punta, en el navegador:

1. `https://app.pp.localhost` → **Entrar o crear cuenta** → en Keycloak, "Registrarse" (email, contraseña ≥ 12 con
   mayúscula, minúscula y dígito, nombre y apellido).
2. Verificar el email desde Mailpit (`http://localhost:8025`) → vuelve a la app y entra en `/onboarding`
   (consentimiento → datos básicos → camino) → `/app`.
3. `/app/profile`: editar teléfono o dirección y guardar; comprobar la fila en `platform.audit_log`
   (`podman compose -f infra/compose/compose.yaml exec postgres psql -U postgres petpassport -c "select action, details from platform.audit_log"`).
4. `curl -k https://api.pp.localhost/api/v1/me` sin token → `401` con `WWW-Authenticate`; con un token caducado
   (espera 5 min sin renovar) → `401`; `GET /api/v1/legal/current` → `200` sin token.

## Desarrollo

```bash
make gen               # regenera servidor Go y tipos TS desde backend/api/openapi.yaml
make lint              # golangci-lint + eslint/prettier
make test              # unitarias (Go con -race, Vitest)
make test-integration  # PostgreSQL real con testcontainers sobre la API de Podman (migraciones up/down/up)
make build             # binarios y frontend
make images            # imágenes OCI con podman build (distroless / nginx no privilegiado)
make specs-status      # avance de las specs por fase
```

Estructura (ADR-007):

```
backend/    módulo Go: cmd/{api,migrate,worker}, internal/{platform,shared,identity,partnernetwork,petregistry}, api/openapi.yaml, migrations/
frontend/   Vue 3 + TS + Vite + Pinia + PrimeVue + Tailwind
infra/      compose (dev), quadlet (staging/prod), deploy (deploy.sh, RUNBOOK), keycloak, traefik, postgres, observability, scripts
docs/       roadmap, ADRs, specs por fase, tracking, handoff
scripts/    specs-status.sh
```

## Contribuir

Trabajo por hito (`docs/handoff/phase-N-prompts.md`), un PR por hito, tracking actualizado en el mismo PR.
Convenciones en `docs/phase-1/10-calidad-y-pruebas.md` §7. Instala los hooks: `pre-commit install`.
