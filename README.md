# PetPassport

Pasaporte digital de salud para mascotas: carnet de vacunas, historial clínico inmutable, citas y conexión
con clínicas y farmacias aliadas. SaaS público. Backend Go (monolito modular, CQRS-ES), frontend Vue 3,
Keycloak, PostgreSQL, OpenSearch y Redis.

- Especificaciones y roadmap: [`docs/`](docs/README.md) · decisiones: [`docs/adr/`](docs/adr/README.md)
- Estado de implementación: `scripts/specs-status.sh`
- Instrucciones para agentes y personas: [`CLAUDE.md`](CLAUDE.md) y [`docs/handoff/`](docs/handoff/README.md)

## Arrancar en 10 minutos

Requisitos: Docker + Compose v2, [mkcert](https://github.com/FiloSottile/mkcert), Go 1.26 y Node 22 (solo para
ejecutar pruebas fuera de Docker).

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

## Desarrollo

```bash
make gen               # regenera servidor Go y tipos TS desde backend/api/openapi.yaml
make lint              # golangci-lint + eslint/prettier
make test              # unitarias (Go con -race, Vitest)
make test-integration  # PostgreSQL real con testcontainers (migraciones up/down/up)
make build             # binarios y frontend
make images            # imágenes Docker (distroless / nginx no privilegiado)
make specs-status      # avance de las specs por fase
```

Estructura (ADR-007):

```
backend/    módulo Go: cmd/{api,migrate,worker}, internal/{platform,shared,identity,partnernetwork,petregistry}, api/openapi.yaml, migrations/
frontend/   Vue 3 + TS + Vite + Pinia + PrimeVue + Tailwind
infra/      compose, keycloak (realm.yaml), traefik, postgres, minio, observability, scripts
docs/       roadmap, ADRs, specs por fase, tracking, handoff
scripts/    specs-status.sh
```

## Contribuir

Trabajo por hito (`docs/handoff/phase-N-prompts.md`), un PR por hito, tracking actualizado en el mismo PR.
Convenciones en `docs/phase-1/10-calidad-y-pruebas.md` §7. Instala los hooks: `pre-commit install`.
