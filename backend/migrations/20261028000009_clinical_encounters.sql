-- +goose Up
-- rebuild: clinical.weight_history
-- rebuild: clinical.encounter_custody
-- Clinical: encuentros (PP-F2-DAT-002). Proyección síncrona de `encounters` y
-- `encounter_attachments` (misma tx que el Append); asíncronas
-- `weight_history` y `encounter_custody` (ARQ-012); `document_jobs` para el
-- worker de PDF (ARQ-010).

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
CREATE INDEX encounter_attachments_encounter_idx ON read_clinical.encounter_attachments (encounter_id);

-- Custodia: la org conserva lectura de lo que firmó (ADR-010).
CREATE TABLE read_clinical.encounter_custody (
  organization_id uuid NOT NULL, pet_id uuid NOT NULL, encounter_id uuid NOT NULL, signed_at timestamptz NOT NULL,
  PRIMARY KEY (organization_id, encounter_id)
);
CREATE INDEX custody_org_pet_idx ON read_clinical.encounter_custody (organization_id, pet_id);

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
CREATE INDEX document_jobs_queue_idx ON read_clinical.document_jobs (created_at) WHERE status = 'pending';

-- Privilegios (mínimo privilegio, F1 SEC-010): las proyecciones escriben, la
-- API lee y escribe document_jobs (encola). Las tablas de eventos siguen
-- append-only. Los DEFAULT PRIVILEGES de la migración del Event Store ya
-- cubren read_clinical; se repiten explícitamente para esta migración.
GRANT SELECT, INSERT, UPDATE, DELETE ON read_clinical.encounters,
  read_clinical.encounter_attachments, read_clinical.encounter_custody,
  read_clinical.weight_history, read_clinical.document_jobs TO pp_app_writer;
GRANT SELECT ON read_clinical.encounters, read_clinical.encounter_attachments,
  read_clinical.encounter_custody, read_clinical.weight_history,
  read_clinical.document_jobs TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS read_clinical.document_jobs;
DROP TABLE IF EXISTS read_clinical.weight_history;
DROP TABLE IF EXISTS read_clinical.encounter_custody;
DROP TABLE IF EXISTS read_clinical.encounter_attachments;
DROP TABLE IF EXISTS read_clinical.encounters;
