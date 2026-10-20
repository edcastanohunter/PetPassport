# F1 · 06 — Datos y persistencia (PostgreSQL 17)

## 1. Esquemas y roles de base de datos

**[PP-F1-DAT-001] (MUST)**

| Esquema | Propietario lógico | Contenido |
|---|---|---|
| `es` | platform/eventstore | `streams`, `events`, `outbox` (+ `snapshots` en F2). |
| `identity` | Identity & Access | `user_accounts`, `consent_versions`. |
| `read_petregistry` | Pet Registry | `pets`, `pet_care_links`, `microchip_reservations`, `pet_transfers`. |
| `read_partnernetwork` | Partner Network | `organizations`, `org_members`, `invitations`, `public_code_reservations`. |
| `platform` | platform | `idempotency_keys`, `audit_log`, `projection_checkpoints` (F2), `schema_migrations` (goose). |

Roles:

| Rol | Permisos | Usado por |
|---|---|---|
| `pp_migrator` | `CREATE` en la BD; propietario de esquemas y tablas. | `cmd/migrate` (CI/CD), nunca la app en runtime. |
| `pp_app_writer` | `SELECT, INSERT, UPDATE, DELETE` en `read_*`, `identity`, `platform`; **solo `SELECT, INSERT`** en `es.events`/`es.outbox` (+ `UPDATE` en `es.streams` y en `es.outbox.published_at`). | `api`, `worker`. |
| `pp_app_reader` | `SELECT` en `read_*`, `identity`, `platform.audit_log`. | Reportes, BI (F5), réplica de lectura. |

Conexión con `sslmode=verify-full` en staging/prod. `statement_timeout = 5s` para la app; `lock_timeout = 2s`.

## 2. Migraciones

**[PP-F1-DAT-002] (MUST)**

- Herramienta: `goose` (SQL puro, `-- +goose Up/Down`), carpeta `backend/migrations/` con prefijo temporal `20261020120000_create_es.sql`.
- Reglas: toda migración tiene `Down` funcional o justificación `-- irreversible: <motivo>`; sin `DROP COLUMN` en la misma versión que deja de usar la columna (expand/contract); índices grandes con `CREATE INDEX CONCURRENTLY` fuera de transacción (`-- +goose NO TRANSACTION`).
- CI: `migrate up` sobre PostgreSQL limpio + `migrate down` de la última + `up` de nuevo; `sqlc vet` contra el esquema resultante.
- Despliegue: migración **antes** del *rollout* de la app; la app N debe funcionar con esquema N y N+1 (compatibilidad hacia delante en una versión).

## 3. Event Store

**[PP-F1-DAT-003] (MUST)**

```sql
CREATE SCHEMA es;

CREATE TABLE es.streams (
  stream_id    uuid PRIMARY KEY,
  stream_type  text NOT NULL,
  version      bigint NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX streams_type_idx ON es.streams (stream_type);

CREATE TABLE es.events (
  global_position bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id        uuid NOT NULL UNIQUE,
  stream_id       uuid NOT NULL REFERENCES es.streams(stream_id),
  stream_type     text NOT NULL,
  stream_version  bigint NOT NULL,
  event_type      text NOT NULL,
  schema_version  smallint NOT NULL DEFAULT 1,
  payload         jsonb NOT NULL,
  metadata        jsonb NOT NULL,
  occurred_at     timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (stream_id, stream_version)
);
CREATE INDEX events_stream_type_pos_idx ON es.events (stream_type, global_position);
CREATE INDEX events_type_pos_idx        ON es.events (event_type, global_position);
CREATE INDEX events_metadata_corr_idx   ON es.events ((metadata->>'correlation_id'));

-- Append-only: ni UPDATE ni DELETE, ni siquiera por el owner por accidente
CREATE OR REPLACE FUNCTION es.forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'es.events is append-only'; END $$;
CREATE TRIGGER events_append_only BEFORE UPDATE OR DELETE ON es.events
  FOR EACH ROW EXECUTE FUNCTION es.forbid_mutation();

CREATE TABLE es.outbox (
  global_position bigint PRIMARY KEY REFERENCES es.events(global_position),
  event_id        uuid NOT NULL,
  event_type      text NOT NULL,
  enqueued_at     timestamptz NOT NULL DEFAULT now(),
  published_at    timestamptz,
  attempts        int NOT NULL DEFAULT 0,
  last_error      text
);
CREATE INDEX outbox_pending_idx ON es.outbox (global_position) WHERE published_at IS NULL;
```

Retención: indefinida (es la fuente de verdad). Particionado por rango de `global_position` se evalúa en F3 si > 50 M eventos.

## 4. Identity

**[PP-F1-DAT-004] (MUST)**

```sql
CREATE SCHEMA identity;

CREATE TABLE identity.consent_versions (
  kind        text NOT NULL CHECK (kind IN ('terms','privacy')),
  version     text NOT NULL,            -- '2026-10'
  url         text NOT NULL,
  effective_from date NOT NULL,
  PRIMARY KEY (kind, version)
);

CREATE TABLE identity.user_accounts (
  id                 uuid PRIMARY KEY,                 -- Keycloak sub
  email              citext NOT NULL UNIQUE,
  email_verified     boolean NOT NULL DEFAULT false,
  display_name       text NOT NULL DEFAULT '',
  locale             text NOT NULL DEFAULT 'es' CHECK (locale IN ('es','en')),
  country            char(2),
  phone              text,                            -- E.164
  address            jsonb,
  emergency_contact  jsonb,
  status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
  status_reason      text,
  global_roles       text[] NOT NULL DEFAULT '{}',
  terms_version      text, privacy_version text, consent_accepted_at timestamptz,
  onboarding_completed_at timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  last_login_at      timestamptz
);
CREATE INDEX user_accounts_display_name_trgm ON identity.user_accounts USING gin (display_name gin_trgm_ops);
```

Extensiones requeridas: `citext`, `pg_trgm`.

## 5. Read models — Pet Registry

**[PP-F1-DAT-005] (MUST)**

```sql
CREATE SCHEMA read_petregistry;

CREATE TABLE read_petregistry.pets (
  id              uuid PRIMARY KEY,
  version         bigint NOT NULL,
  guardian_id     uuid NOT NULL,
  guardian_display_name text NOT NULL DEFAULT '',      -- desnormalizado (puerto Identity)
  name            text NOT NULL,
  species         text NOT NULL,
  species_other   text,
  breed           text,
  sex             text NOT NULL,
  neutered        text NOT NULL,
  birth_date      date,
  birth_date_approximate boolean,
  markings        text,
  microchip       text UNIQUE,
  microchip_corrections smallint NOT NULL DEFAULT 0,
  photo_object_key text, photo_thumb_key text, photo_content_type text,
  status          text NOT NULL CHECK (status IN ('active','deceased')),
  deceased_at     date,
  deceased_marked_at timestamptz,
  pending_transfer jsonb,                               -- {to_email_hash, to_email_masked, token_hash, expires_at}
  active_links_count int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL
);
CREATE INDEX pets_guardian_idx ON read_petregistry.pets (guardian_id, status, created_at DESC);
CREATE INDEX pets_name_trgm ON read_petregistry.pets USING gin (name gin_trgm_ops);

CREATE TABLE read_petregistry.pet_care_links (
  pet_id          uuid NOT NULL REFERENCES read_petregistry.pets(id),
  organization_id uuid NOT NULL,
  org_display_name text NOT NULL DEFAULT '',
  org_city        text,
  status          text NOT NULL CHECK (status IN ('active','revoked')),
  org_suspended   boolean NOT NULL DEFAULT false,      -- reacción a OrganizationSuspended
  granted_by      uuid NOT NULL,
  granted_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  PRIMARY KEY (pet_id, organization_id)
);
CREATE INDEX care_links_org_active_idx ON read_petregistry.pet_care_links (organization_id, granted_at DESC)
  WHERE status = 'active';

-- Unicidad global transaccional (ADR-002)
CREATE TABLE read_petregistry.microchip_reservations (
  microchip   text PRIMARY KEY,
  pet_id      uuid NOT NULL,
  reserved_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE read_petregistry.pet_transfers (          -- para resolver token → pet sin recorrer eventos
  token_hash  bytea PRIMARY KEY,
  pet_id      uuid NOT NULL,
  to_email_hash bytea NOT NULL,
  expires_at  timestamptz NOT NULL,
  status      text NOT NULL CHECK (status IN ('pending','accepted','cancelled','expired'))
);
```

Patrón de reserva de microchip en `AssignMicrochip` (misma tx): `INSERT INTO microchip_reservations … ON CONFLICT DO NOTHING` → si 0 filas y `pet_id` distinto → `MICROCHIP_ALREADY_ASSIGNED` (antes del `Append`); si el agregado tenía otro microchip, se borra su reserva.

## 6. Read models — Partner Network

**[PP-F1-DAT-006] (MUST)**

```sql
CREATE SCHEMA read_partnernetwork;

CREATE TABLE read_partnernetwork.organizations (
  id            uuid PRIMARY KEY,
  version       bigint NOT NULL,
  type          text NOT NULL CHECK (type IN ('clinic','pharmacy')),
  legal_name    text NOT NULL,
  display_name  text NOT NULL,
  tax_country   char(2) NOT NULL,
  tax_value     text NOT NULL,
  address       jsonb NOT NULL,
  city          text NOT NULL,
  country       char(2) NOT NULL,
  contact_email citext NOT NULL,
  contact_phone text NOT NULL,
  website       text,
  public_code   text NOT NULL UNIQUE,
  status        text NOT NULL CHECK (status IN ('pending_verification','active','suspended','rejected')),
  status_reason text,
  founder_id    uuid NOT NULL,
  member_count  int NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL
);
CREATE UNIQUE INDEX organizations_tax_idx ON read_partnernetwork.organizations (tax_country, tax_value);
CREATE INDEX organizations_status_idx ON read_partnernetwork.organizations (status, created_at);
CREATE INDEX organizations_name_trgm ON read_partnernetwork.organizations USING gin (display_name gin_trgm_ops);

CREATE TABLE read_partnernetwork.org_members (
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  user_id         uuid NOT NULL,
  role            text NOT NULL,
  status          text NOT NULL CHECK (status IN ('active','removed')),
  joined_at       timestamptz NOT NULL,
  removed_at      timestamptz,
  PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX org_members_user_idx ON read_partnernetwork.org_members (user_id) WHERE status = 'active';

CREATE TABLE read_partnernetwork.invitations (
  id              uuid PRIMARY KEY,
  organization_id uuid NOT NULL REFERENCES read_partnernetwork.organizations(id),
  email           citext NOT NULL,
  role            text NOT NULL,
  token_hash      bytea NOT NULL UNIQUE,
  status          text NOT NULL CHECK (status IN ('pending','accepted','revoked','expired')),
  expires_at      timestamptz NOT NULL,
  resend_count    int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL
);
CREATE UNIQUE INDEX invitations_pending_email_idx ON read_partnernetwork.invitations (organization_id, email) WHERE status = 'pending';

CREATE TABLE read_partnernetwork.public_code_reservations (
  public_code text PRIMARY KEY,
  organization_id uuid NOT NULL
);
```

Nota: la unicidad del `tax_id` se aplica también por reserva (`organizations_tax_idx` único) y produce `422 VALIDATION_FAILED {field: tax_id, code: ALREADY_REGISTERED}` en `RegisterOrganization`.

## 7. Platform

**[PP-F1-DAT-007] (MUST)**

```sql
CREATE SCHEMA platform;

CREATE TABLE platform.idempotency_keys (
  actor_id      uuid NOT NULL,
  key           uuid NOT NULL,
  request_hash  bytea NOT NULL,
  status        text NOT NULL CHECK (status IN ('in_progress','completed')),
  response_status int,
  response_body jsonb,
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  PRIMARY KEY (actor_id, key)
);
CREATE INDEX idempotency_expires_idx ON platform.idempotency_keys (expires_at);

CREATE TABLE platform.audit_log (
  id            uuid PRIMARY KEY,
  occurred_at   timestamptz NOT NULL DEFAULT now(),
  actor_id      uuid,                 -- null = system
  actor_roles   text[] NOT NULL DEFAULT '{}',
  action        text NOT NULL,        -- 'admin.user.suspend', 'me.update', 'authz.deny'
  target_type   text, target_id uuid,
  reason        text,
  details       jsonb,                -- sin PII en claro; diffs con campos enmascarados
  correlation_id uuid,
  ip_hash       bytea
);
CREATE INDEX audit_log_target_idx ON platform.audit_log (target_type, target_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx  ON platform.audit_log (actor_id, occurred_at DESC);

CREATE TABLE platform.projection_checkpoints (   -- usado desde F2; creado en F1
  projection_name text PRIMARY KEY,
  last_position   bigint NOT NULL DEFAULT 0,
  updated_at      timestamptz NOT NULL DEFAULT now()
);
```

Limpieza: job diario (`cmd/api` con `--job idempotency-gc` o `pg_cron`) borra `idempotency_keys` expiradas. `audit_log` se retiene ≥ 2 años.

## 8. Convenciones

**[PP-F1-DAT-008] (MUST)**

- UUIDv7 en todas las PK (ADR-007); `timestamptz` siempre; `date` solo para fechas civiles; `text` en lugar de `varchar(n)` + `CHECK` de longitud cuando importe.
- Nombres `snake_case`; índices `<tabla>_<cols>_idx`; únicos `_uq`; triggers `<tabla>_<accion>`.
- `jsonb` solo para estructuras cerradas documentadas (dirección, contacto) o payloads de eventos; nunca para campos que se filtran.
- Sin FKs entre esquemas de contextos distintos (ADR-001). Dentro de un contexto, FKs permitidas en read models.
- Consultas con `sqlc`; prohibido SQL concatenado.

## 9. Backups y retención

**[PP-F1-DAT-009] (MUST)**

- Prod/staging: PostgreSQL gestionado con backups automáticos diarios + PITR 7 días (prod: 30 días).
- Prueba de restauración mensual en staging documentada (runbook INF-006).
- Object storage con versionado habilitado; fotos reemplazadas se retienen 30 días (PRD-008).
- Exportación completa de `es.events` a object storage semanal (formato JSONL comprimido) como respaldo frío independiente del proveedor.
