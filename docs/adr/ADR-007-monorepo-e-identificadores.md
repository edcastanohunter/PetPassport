# ADR-007: Monorepo; UUIDv7 como identificador universal

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Decisión

### Monorepo

```
petpassport/
  backend/        # módulo Go
  frontend/       # app Vue
  infra/          # docker compose, keycloak (config as code), traefik, scripts de despliegue
  docs/           # specs, ADRs, tracking
  scripts/        # utilidades (specs-status.sh, etc.)
  .github/        # workflows, plantillas
```

Un solo repositorio simplifica cambios de contrato API (backend + frontend en el mismo PR), CI y tracking.
Los workflows usan filtros de rutas para no ejecutar todo en cada cambio.

### Identificadores

- Todas las entidades y streams usan **UUIDv7** (ordenables por tiempo, buen comportamiento en índices B-tree).
  Generación en Go con `github.com/google/uuid` (`uuid.NewV7`).
- `UserAccount.ID` **es el `sub` de Keycloak** (UUIDv4 generado por Keycloak); es la única excepción y se documenta.
- Los eventos llevan `event_id` UUIDv7 y `global_position` BIGINT (orden total del store).
- Códigos públicos legibles (código de clínica) son `PublicCode`: 6 caracteres Crockford base32, sin vocales
  ambiguas, únicos por reserva.

## Consecuencias

- Las URLs exponen UUIDs; no son enumerables.
- Sin secuencias por tabla salvo `global_position`.

## Alternativas consideradas

- Repos separados por backend/frontend: descartado por fricción en cambios de contrato.
- ULID/KSUID: equivalentes; UUIDv7 es estándar (RFC 9562) y nativo en PostgreSQL 17 como tipo `uuid`.
