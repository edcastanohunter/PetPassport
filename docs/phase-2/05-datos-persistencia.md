# F2 · 05 — Datos y persistencia

Convenciones de F1 (DAT-008) vigentes. Nuevas extensiones requeridas: `btree_gist`.

## 1. Event Store v1

**[PP-F2-DAT-001] (MUST)**

```sql
CREATE TABLE es.snapshots (
  stream_id       uuid PRIMARY KEY REFERENCES es.streams(stream_id),
  stream_version  bigint NOT NULL,
  schema_version  smallint NOT NULL,
  state           jsonb NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE es.outbox ADD COLUMN stream_id uuid, ADD COLUMN dead_letter boolean NOT NULL DEFAULT false;
UPDATE es.outbox o SET stream_id = e.stream_id FROM es.events e WHERE e.global_position = o.global_position; -- backfill
ALTER TABLE es.outbox ALTER COLUMN stream_id SET NOT NULL;
CREATE INDEX outbox_pending_stream_idx ON es.outbox (stream_id, global_position) WHERE published_at IS NULL;

ALTER TABLE platform.projection_checkpoints
  ADD COLUMN status text NOT NULL DEFAULT 'running' CHECK (status IN ('running','paused','failed','rebuilding')),
  ADD COLUMN last_error text,
  ADD COLUMN failed_at timestamptz,
  ADD COLUMN mode text NOT NULL DEFAULT 'async' CHECK (mode IN ('sync','async'));

-- Lectura eficiente por tipo para suscripciones filtradas
CREATE INDEX events_pos_type_idx ON es.events (global_position) INCLUDE (event_type, stream_id);
```

`pp_app_writer` recibe `INSERT/UPDATE/DELETE` en `es.snapshots` y `UPDATE` en `es.outbox` (`published_at, attempts, last_error, dead_letter`). `es.events` sigue siendo append-only.

## 2. Clinical — encuentros

**[PP-F2-DAT-002] (MUST)**

```sql
CREATE SCHEMA read_clinical;

CREATE TABLE read_clinical.encounters (
  id               uuid PRIMARY KEY,
  version          bigint NOT NULL,
  pet_id           uuid NOT NULL,
  organization_id  uuid NOT NULL,
  appointment_id   uuid,
  type             text NOT NULL,
  status           text NOT NULL CHECK (status IN ('draft','signed','discarded')),
  started_at       timestamptz NOT NULL,
  reason           text NOT NULL,
  attending_vet_id uuid,
  vitals           jsonb,
  subjective       text,
  objective        jsonb,            -- {narrative, systems}
  diagnoses        jsonb NOT NULL DEFAULT '[]',
  plan             jsonb NOT NULL DEFAULT '{}',
  follow_up        jsonb,            -- {due_date, reason}
  signature        jsonb,            -- {signed_by, signed_at, stream_version, content_hash}
  addenda          jsonb NOT NULL DEFAULT '[]',
  pdf_object_key   text, pdf_version int,
  last_activity_at timestamptz NOT NULL,
  created_at       timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX encounters_org_status_idx ON read_clinical.encounters (organization_id, status, started_at DESC);
CREATE INDEX encounters_pet_signed_idx ON read_clinical.encounters (pet_id, started_at DESC) WHERE status = 'signed';
CREATE INDEX encounters_vet_idx ON read_clinical.encounters (attending_vet_id, started_at DESC);
CREATE INDEX encounters_abandoned_idx ON read_clinical.encounters (organization_id, last_activity_at) WHERE status = 'draft';

CREATE TABLE read_clinical.encounter_attachments (
  id               uuid PRIMARY KEY,
  encounter_id     uuid NOT NULL REFERENCES read_clinical.encounters(id),
  object_key       text NOT NULL,
  content_type     text NOT NULL,
  size_bytes       int NOT NULL,
  description      text,
  visible_to_guardian boolean NOT NULL DEFAULT true,
  addendum_seq     int,
  created_at       timestamptz NOT NULL
);

-- Custodia: la org conserva lectura de lo que firmó
CREATE TABLE read_clinical.encounter_custody (
  organization_id uuid NOT NULL, pet_id uuid NOT NULL, encounter_id uuid NOT NULL, signed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, encounter_id)
);
CREATE INDEX custody_org_pet_idx ON read_clinical.encounter_custody (organization_id, pet_id);

CREATE TABLE read_clinical.medical_timeline (
  pet_id          uuid NOT NULL,
  position        bigint NOT NULL,          -- global_position del evento origen (orden estable)
  occurred_at     timestamptz NOT NULL,
  kind            text NOT NULL,
  ref_id          uuid NOT NULL,
  organization_id uuid,
  title           text NOT NULL,
  summary         text,
  visibility      text NOT NULL CHECK (visibility IN ('guardian','org','both')),
  PRIMARY KEY (pet_id, position)
);
CREATE INDEX timeline_pet_time_idx ON read_clinical.medical_timeline (pet_id, occurred_at DESC, position DESC);

CREATE TABLE read_clinical.weight_history (
  pet_id uuid NOT NULL, measured_at timestamptz NOT NULL, weight_kg numeric(6,2) NOT NULL,
  source text NOT NULL, ref_id uuid NOT NULL, organization_id uuid,
  PRIMARY KEY (pet_id, ref_id)
);
CREATE INDEX weight_pet_time_idx ON read_clinical.weight_history (pet_id, measured_at);

CREATE TABLE read_clinical.document_jobs (
  id uuid PRIMARY KEY, kind text NOT NULL, ref_id uuid NOT NULL, version int NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','rendering','ready','failed')),
  object_key text, attempts int NOT NULL DEFAULT 0, last_error text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, ref_id, version)
);
```

## 3. Clinical — carnet y catálogos

**[PP-F2-DAT-003] (MUST)**

```sql
CREATE TABLE read_clinical.card_entries (
  id               uuid PRIMARY KEY,
  pet_id           uuid NOT NULL,
  kind             text NOT NULL CHECK (kind IN ('vaccine','deworming','ectoparasite')),
  catalog_id       uuid, product_name text NOT NULL,
  administered_at  date NOT NULL,
  lot text, manufacturer text, expiry_date date, route text, site text, dose_label text, weight_kg numeric(6,2),
  source           text NOT NULL CHECK (source IN ('recorded','declared','imported')),
  verification     text NOT NULL CHECK (verification IN ('verified','unverified','disputed')),
  recorded_by_user uuid, recorded_by_org uuid,
  encounter_id     uuid,
  next_due_date    date, next_due_basis text,
  status           text NOT NULL CHECK (status IN ('active','voided')),
  void_reason      text,
  evidence_object_key text,
  created_at       timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX card_entries_pet_idx ON read_clinical.card_entries (pet_id, administered_at DESC);
CREATE UNIQUE INDEX card_entries_dup_uq ON read_clinical.card_entries (pet_id, kind, lower(product_name), administered_at) WHERE status = 'active';

CREATE TABLE read_clinical.vaccination_due (
  pet_id uuid NOT NULL, entry_id uuid NOT NULL, kind text NOT NULL, product_name text NOT NULL,
  due_date date NOT NULL, basis text NOT NULL,
  status text NOT NULL CHECK (status IN ('upcoming','overdue','satisfied','cancelled')),
  PRIMARY KEY (pet_id, entry_id)
);
CREATE INDEX vaccination_due_date_idx ON read_clinical.vaccination_due (due_date) WHERE status IN ('upcoming','overdue');

-- Catálogos (CRUD, auditados)
CREATE SCHEMA ref;
CREATE TABLE ref.vaccines (
  id uuid PRIMARY KEY, name text NOT NULL, species text[] NOT NULL, diseases text[] NOT NULL,
  type text NOT NULL CHECK (type IN ('core','non_core')), routes text[] NOT NULL,
  active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ref.vaccination_protocols (
  id uuid PRIMARY KEY, country char(2) NOT NULL, species text NOT NULL,
  vaccine_id uuid NOT NULL REFERENCES ref.vaccines(id),
  initial_series jsonb NOT NULL,          -- [{min_age_weeks, min_interval_days}]
  booster_interval_months int, legal_validity_months int, notes text,
  active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (country, species, vaccine_id)
);
CREATE TABLE ref.preventive_protocols (
  id uuid PRIMARY KEY, country char(2) NOT NULL, species text NOT NULL, kind text NOT NULL,
  interval_days int NOT NULL, notes text, active boolean NOT NULL DEFAULT true
);
CREATE TABLE ref.diagnosis_codes (system text NOT NULL, code text NOT NULL, label text NOT NULL, species text[], PRIMARY KEY (system, code));

CREATE TABLE read_clinical.record_shares (
  id uuid PRIMARY KEY, pet_id uuid NOT NULL, scope text NOT NULL CHECK (scope IN ('vaccination_card','full_record')),
  token_hash bytea NOT NULL UNIQUE, pin_hash text, expires_at timestamptz NOT NULL,
  created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz,
  access_count int NOT NULL DEFAULT 0, last_access_at timestamptz, failed_pin_attempts int NOT NULL DEFAULT 0
);
CREATE INDEX record_shares_pet_idx ON read_clinical.record_shares (pet_id) WHERE revoked_at IS NULL;
```

`ref.*` pertenece a Clinical Records; `pp_app_writer` tiene `SELECT`; escritura solo vía API admin con rol `pp_app_writer` también (CRUD auditado) — se separa a `pp_ref_writer` en F5 si procede.

## 4. Scheduling

**[PP-F2-DAT-004] (MUST)**

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE SCHEMA read_scheduling;

CREATE TABLE read_scheduling.appointments (
  id uuid PRIMARY KEY, version bigint NOT NULL,
  pet_id uuid NOT NULL, organization_id uuid NOT NULL, professional_id uuid,
  type text NOT NULL, origin text NOT NULL,
  status text NOT NULL CHECK (status IN ('requested','proposed','confirmed','checked_in','completed','cancelled','no_show','rejected')),
  reason text NOT NULL, preferences jsonb NOT NULL DEFAULT '[]',
  slot_start timestamptz, slot_end timestamptz, tz text,
  proposal_expires_at timestamptz, encounter_id uuid,
  cancellation jsonb, cancellation_policy_hours int,
  requested_by uuid, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX appointments_org_day_idx ON read_scheduling.appointments (organization_id, slot_start) WHERE status IN ('confirmed','checked_in');
CREATE INDEX appointments_org_inbox_idx ON read_scheduling.appointments (organization_id, created_at DESC) WHERE status IN ('requested','proposed');
CREATE INDEX appointments_pet_idx ON read_scheduling.appointments (pet_id, slot_start DESC);
CREATE INDEX appointments_guardian_idx ON read_scheduling.appointments (requested_by, created_at DESC);
CREATE INDEX appointments_noshow_idx ON read_scheduling.appointments (slot_end) WHERE status = 'confirmed';

-- Autoridad de no-solapamiento (ADR-011). Se escribe en la misma tx que el Append.
CREATE TABLE read_scheduling.slot_reservations (
  appointment_id  uuid PRIMARY KEY,
  professional_id uuid NOT NULL,
  during          tstzrange NOT NULL,
  kind            text NOT NULL CHECK (kind IN ('confirmed','provisional')),
  expires_at      timestamptz,            -- provisional
  EXCLUDE USING gist (professional_id WITH =, during WITH &&)
);

CREATE TABLE read_scheduling.agendas (
  organization_id uuid NOT NULL, professional_id uuid NOT NULL, version bigint NOT NULL,
  tz text NOT NULL, weekly_template jsonb NOT NULL, slot_minutes jsonb NOT NULL,
  exceptions jsonb NOT NULL DEFAULT '{}', lead_time_min_hours int NOT NULL, lead_time_max_days int NOT NULL,
  cancellation_policy_hours int NOT NULL, active boolean NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, professional_id)
);
```

`Availability` se calcula en Go a partir de `agendas` + `slot_reservations` (consulta por rango, `during && tstzrange(from, to)`), no en SQL, para reutilizar la lógica de discretización en pruebas.

## 5. Notifications

**[PP-F2-DAT-005] (MUST)**

```sql
CREATE SCHEMA notifications;

CREATE TABLE notifications.preferences (
  user_id uuid PRIMARY KEY,
  email   jsonb NOT NULL DEFAULT '{"appointments":true,"preventive_care":true,"clinical":true,"account":true,"security":true}',
  digest  jsonb NOT NULL DEFAULT '{"clinic_daily_agenda":false}',
  unsubscribe_token_hash bytea, updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notifications.scheduled (
  id           uuid PRIMARY KEY,
  dedup_key    text NOT NULL UNIQUE,          -- rule|subject|recipient|occurrence
  rule         text NOT NULL, category text NOT NULL,
  recipient_id uuid NOT NULL, subject_type text NOT NULL, subject_id uuid NOT NULL,
  due_at       timestamptz NOT NULL,
  payload      jsonb NOT NULL,
  status       text NOT NULL CHECK (status IN ('pending','dispatched','cancelled')),
  created_at   timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scheduled_due_idx ON notifications.scheduled (due_at) WHERE status = 'pending';
CREATE INDEX scheduled_subject_idx ON notifications.scheduled (subject_type, subject_id) WHERE status = 'pending';

CREATE TABLE notifications.deliveries (
  id            uuid PRIMARY KEY,
  dedup_key     text NOT NULL UNIQUE,
  channel       text NOT NULL CHECK (channel IN ('email')),
  category      text NOT NULL, template text NOT NULL, template_version int NOT NULL, locale text NOT NULL,
  recipient_id  uuid NOT NULL, recipient_address_hash bytea NOT NULL,
  payload       jsonb NOT NULL,
  status        text NOT NULL CHECK (status IN ('queued','sending','sent','failed','suppressed')),
  attempts      int NOT NULL DEFAULT 0, next_attempt_at timestamptz, last_error text,
  provider_message_id text, sent_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX deliveries_queue_idx ON notifications.deliveries (next_attempt_at) WHERE status IN ('queued','failed') ;
CREATE INDEX deliveries_recipient_idx ON notifications.deliveries (recipient_id, created_at DESC);

CREATE TABLE notifications.suppressions (   -- bounces/quejas (SHOULD)
  address_hash bytea PRIMARY KEY, reason text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
```

Retención: `deliveries` 180 días (payload se borra a los 30, se conservan metadatos); `scheduled` despachadas/canceladas 30 días.

## 6. Scheduler de comandos y worker

**[PP-F2-DAT-006] (MUST)**

```sql
CREATE SCHEMA scheduler;
CREATE TABLE scheduler.command_jobs (
  id uuid PRIMARY KEY, dedup_key text NOT NULL UNIQUE,
  run_at timestamptz NOT NULL, command_type text NOT NULL, command jsonb NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','running','done','failed','cancelled')),
  attempts int NOT NULL DEFAULT 0, last_error text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX command_jobs_due_idx ON scheduler.command_jobs (run_at) WHERE status = 'pending';

CREATE TABLE platform.worker_instances (
  group_name text PRIMARY KEY, instance_id text NOT NULL, hostname text, started_at timestamptz NOT NULL, heartbeat_at timestamptz NOT NULL
);
```

## 7. Pet Registry (ampliación)

**[PP-F2-DAT-007] (MUST)**

```sql
ALTER TABLE read_petregistry.pets ADD COLUMN alerts jsonb NOT NULL DEFAULT '[]';

CREATE TABLE read_petregistry.care_link_requests (
  id uuid PRIMARY KEY, pet_id uuid NOT NULL REFERENCES read_petregistry.pets(id), organization_id uuid NOT NULL,
  org_display_name text NOT NULL, requested_by uuid NOT NULL, message text,
  status text NOT NULL CHECK (status IN ('pending','approved','declined','expired')),
  expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL, resolved_at timestamptz
);
CREATE UNIQUE INDEX care_link_requests_pending_uq ON read_petregistry.care_link_requests (pet_id, organization_id) WHERE status = 'pending';
CREATE INDEX care_link_requests_guardian_idx ON read_petregistry.care_link_requests (pet_id, status, created_at DESC);

CREATE TABLE read_petregistry.guardian_inbox (
  id uuid PRIMARY KEY, guardian_id uuid NOT NULL, kind text NOT NULL, ref_id uuid NOT NULL,
  pet_id uuid, organization_id uuid, title text NOT NULL, body text, status text NOT NULL,
  created_at timestamptz NOT NULL, resolved_at timestamptz
);
CREATE INDEX guardian_inbox_idx ON read_petregistry.guardian_inbox (guardian_id, status, created_at DESC);

CREATE TABLE read_partnernetwork.org_stats_monthly (
  organization_id uuid NOT NULL, month date NOT NULL, metrics jsonb NOT NULL, updated_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, month)
);
```

Búsqueda para solicitudes: índice existente `pets.microchip` (único) e `identity.user_accounts.email` (citext único); la consulta hace *join* por puerto (no SQL cruzado): primero `GuardianLookup.FindByEmail`, luego `pets WHERE guardian_id = $1`.

## 8. Retención, volumen y particionado

**[PP-F2-DAT-008] (SHOULD)**

- Estimación: clínica mediana ≈ 400 consultas/mes ≈ 4.000 eventos/mes; 200 clínicas ≈ 10 M eventos/año. `es.events` sin particionar hasta 50 M (F3 evalúa particionado por rango de `global_position`).
- `medical_timeline` ≈ 1,5 filas por evento clínico; índices cubren consultas por mascota.
- `audit_log` de accesos clínicos (SEC-005) puede crecer rápido: partición mensual desde F2 (`PARTITION BY RANGE (occurred_at)`), retención 2 años.
- `deliveries`/`scheduled`: limpieza diaria por job.

## 9. Semillas

**[PP-F2-DAT-009] (MUST)** `backend/seeds/ref/` (aplicadas por migración de datos idempotente, versionadas):

- `vaccines.yaml`: perro — Rabia, Polivalente (DHPPi), Polivalente + Leptospira (DHPPi-L), Bordetella/Tos de las perreras, Coronavirus (non_core); gato — Triple felina (FVRCP), Leucemia felina (FeLV), Rabia.
- `protocols_CO.yaml`: perro Polivalente `[{6–8 sem}, {+21 d}, {+21 d}]` refuerzo 12 m; Rabia primera dosis ≥ 12 sem, refuerzo 12 m (validez legal 12 m); gato Triple `[{8 sem}, {+21 d}, {+21 d}]` refuerzo 12 m; Rabia ≥ 12 sem, 12 m; FeLV 2 dosis + 12 m.
- `preventive_CO.yaml`: desparasitación interna cachorros cada 15 d hasta 3 m, luego cada 3 m; adultos cada 3–6 m; ectoparasitarios cada 30 d.
- Revisión obligatoria por un veterinario antes de activar en staging (QA-006); los valores anteriores son punto de partida, no verdad clínica.
