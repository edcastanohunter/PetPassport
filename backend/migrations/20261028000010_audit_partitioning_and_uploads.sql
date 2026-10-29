-- +goose Up
-- Auditoría de accesos clínicos (PP-F2-SEC-005): platform.audit_log pasa a
-- particionado mensual por occurred_at (DAT-008). La PK incluye la clave de
-- partición (requisito de PostgreSQL). Las particiones futuras las crea
-- platform.ensure_audit_partition (SECURITY DEFINER: la app no tiene CREATE),
-- que el worker invoca al arrancar y a diario; la partición DEFAULT evita
-- perder inserciones si el worker no la creó a tiempo.
ALTER TABLE platform.audit_log RENAME TO audit_log_unpartitioned;
ALTER INDEX platform.audit_log_target_idx RENAME TO audit_log_unpartitioned_target_idx;
ALTER INDEX platform.audit_log_actor_idx RENAME TO audit_log_unpartitioned_actor_idx;
ALTER INDEX platform.audit_log_occurred_idx RENAME TO audit_log_unpartitioned_occurred_idx;

CREATE TABLE platform.audit_log (
  id              uuid NOT NULL,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  actor_id        uuid,
  actor_roles     text[] NOT NULL DEFAULT '{}',
  action          text NOT NULL,
  target_type     text,
  target_id       uuid,
  reason          text,
  details         jsonb,
  correlation_id  uuid,
  ip_hash         bytea,
  PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);
CREATE INDEX audit_log_target_idx   ON platform.audit_log (target_type, target_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx    ON platform.audit_log (actor_id, occurred_at DESC);
CREATE INDEX audit_log_occurred_idx ON platform.audit_log (occurred_at DESC, id DESC);
CREATE TABLE platform.audit_log_default PARTITION OF platform.audit_log DEFAULT;
REVOKE ALL ON platform.audit_log_default FROM pp_app_writer, pp_app_reader;

-- +goose StatementBegin
CREATE FUNCTION platform.ensure_audit_partition(month date) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, platform AS $$
DECLARE
  from_d date := date_trunc('month', month)::date;
  to_d   date := (date_trunc('month', month) + interval '1 month')::date;
  part   text := 'audit_log_' || to_char(from_d, 'YYYY_MM');
BEGIN
  IF to_regclass('platform.' || part) IS NOT NULL THEN
    RETURN;
  END IF;
  EXECUTE format('CREATE TABLE platform.%I PARTITION OF platform.audit_log FOR VALUES FROM (%L) TO (%L)',
                 part, from_d, to_d);
  -- Los DEFAULT PRIVILEGES del esquema darían UPDATE/DELETE a la app sobre
  -- la partición: auditoría inmutable (F1 SEC-010).
  EXECUTE format('REVOKE ALL ON platform.%I FROM pp_app_writer, pp_app_reader', part);
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.ensure_audit_partition(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.ensure_audit_partition(date) TO pp_app_writer;

-- Particiones para los datos existentes, el mes actual y el siguiente.
-- +goose StatementBegin
DO $$
DECLARE m date;
BEGIN
  FOR m IN
    SELECT DISTINCT date_trunc('month', occurred_at)::date FROM platform.audit_log_unpartitioned
    UNION SELECT date_trunc('month', now())::date
    UNION SELECT (date_trunc('month', now()) + interval '1 month')::date
  LOOP
    PERFORM platform.ensure_audit_partition(m);
  END LOOP;
END $$;
-- +goose StatementEnd

INSERT INTO platform.audit_log SELECT * FROM platform.audit_log_unpartitioned;
DROP TABLE platform.audit_log_unpartitioned;

-- Mismos privilegios que la tabla original (F1 SEC-010): inmutable para la
-- app. Se accede solo por la tabla padre (las particiones no tienen grants).
REVOKE ALL ON platform.audit_log FROM pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT ON platform.audit_log TO pp_app_writer;
GRANT SELECT ON platform.audit_log TO pp_app_reader;

-- Adjuntos subidos a una consulta firmada, pendientes de adenda (E9). Tabla
-- operacional (no read model): la adenda los referencia por attachment_ids y
-- la API los borra al confirmarse; los huérfanos > 24 h los purga el worker.
CREATE TABLE read_clinical.attachment_uploads (
  id                  uuid PRIMARY KEY,
  encounter_id        uuid NOT NULL,
  uploaded_by         uuid NOT NULL,
  object_key          text NOT NULL,
  content_type        text NOT NULL,
  size_bytes          int NOT NULL,
  description         text,
  visible_to_guardian boolean NOT NULL DEFAULT true,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attachment_uploads_encounter_idx ON read_clinical.attachment_uploads (encounter_id, created_at);
REVOKE ALL ON read_clinical.attachment_uploads FROM pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT, DELETE ON read_clinical.attachment_uploads TO pp_app_writer;
GRANT SELECT ON read_clinical.attachment_uploads TO pp_app_reader;

-- +goose Down
DROP TABLE IF EXISTS read_clinical.attachment_uploads;
ALTER TABLE platform.audit_log RENAME TO audit_log_partitioned;
ALTER INDEX platform.audit_log_target_idx RENAME TO audit_log_partitioned_target_idx;
ALTER INDEX platform.audit_log_actor_idx RENAME TO audit_log_partitioned_actor_idx;
ALTER INDEX platform.audit_log_occurred_idx RENAME TO audit_log_partitioned_occurred_idx;
CREATE TABLE platform.audit_log (
  id              uuid PRIMARY KEY,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  actor_id        uuid,
  actor_roles     text[] NOT NULL DEFAULT '{}',
  action          text NOT NULL,
  target_type     text,
  target_id       uuid,
  reason          text,
  details         jsonb,
  correlation_id  uuid,
  ip_hash         bytea
);
INSERT INTO platform.audit_log SELECT * FROM platform.audit_log_partitioned;
DROP TABLE platform.audit_log_partitioned;
DROP FUNCTION IF EXISTS platform.ensure_audit_partition(date);
CREATE INDEX audit_log_target_idx   ON platform.audit_log (target_type, target_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx    ON platform.audit_log (actor_id, occurred_at DESC);
CREATE INDEX audit_log_occurred_idx ON platform.audit_log (occurred_at DESC, id DESC);
REVOKE ALL ON platform.audit_log FROM pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT ON platform.audit_log TO pp_app_writer;
GRANT SELECT ON platform.audit_log TO pp_app_reader;
