-- +goose Up
-- F4 M4.4: Particiones y retención de partner_calls (PP-F4-DAT-006).

-- 1. Función para asegurar particiones mensuales de partner_calls
-- +goose StatementBegin
CREATE FUNCTION read_partnernetwork.ensure_partner_calls_partition(month date) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, read_partnernetwork AS $$
DECLARE
  from_d date := date_trunc('month', month)::date;
  to_d   date := (date_trunc('month', month) + interval '1 month')::date;
  part   text := 'partner_calls_' || to_char(from_d, 'YYYY_MM');
BEGIN
  IF to_regclass('read_partnernetwork.' || part) IS NOT NULL THEN
    RETURN;
  END IF;
  EXECUTE format('CREATE TABLE read_partnernetwork.%I PARTITION OF read_partnernetwork.partner_calls FOR VALUES FROM (%L) TO (%L)',
                 part, from_d, to_d);
  EXECUTE format('REVOKE ALL ON read_partnernetwork.%I FROM pp_app_writer, pp_app_reader', part);
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION read_partnernetwork.ensure_partner_calls_partition(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION read_partnernetwork.ensure_partner_calls_partition(date) TO pp_app_writer;

-- 2. Función para eliminar particiones anteriores al corte (retención 12 meses)
-- +goose StatementBegin
CREATE FUNCTION read_partnernetwork.drop_partner_calls_partitions_before(cutoff date) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, read_partnernetwork AS $$
DECLARE
  part  record;
  month date;
  n     integer := 0;
BEGIN
  FOR part IN
    SELECT c.relname
      FROM pg_inherits i
      JOIN pg_class c ON c.oid = i.inhrelid
     WHERE i.inhparent = 'read_partnernetwork.partner_calls'::regclass
       AND c.relname ~ '^partner_calls_[0-9]{4}_[0-9]{2}$'
  LOOP
    month := to_date(substr(part.relname, 15), 'YYYY_MM');
    IF (month + interval '1 month')::date <= cutoff THEN
      EXECUTE format('DROP TABLE read_partnernetwork.%I', part.relname);
      n := n + 1;
    END IF;
  END LOOP;
  RETURN n;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION read_partnernetwork.drop_partner_calls_partitions_before(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION read_partnernetwork.drop_partner_calls_partitions_before(date) TO pp_app_writer;

-- 3. Crear particiones para el mes actual y el siguiente
-- +goose StatementBegin
DO $$
DECLARE m date;
BEGIN
  FOR m IN
    SELECT date_trunc('month', now())::date
    UNION SELECT (date_trunc('month', now()) + interval '1 month')::date
  LOOP
    PERFORM read_partnernetwork.ensure_partner_calls_partition(m);
  END LOOP;
END $$;
-- +goose StatementEnd

-- Privilegios sobre la tabla particionada
REVOKE ALL ON read_partnernetwork.partner_calls FROM pp_app_writer, pp_app_reader;
GRANT SELECT, INSERT ON read_partnernetwork.partner_calls TO pp_app_writer;
GRANT SELECT ON read_partnernetwork.partner_calls TO pp_app_reader;

-- +goose Down
DROP FUNCTION IF EXISTS read_partnernetwork.drop_partner_calls_partitions_before(date);
DROP FUNCTION IF EXISTS read_partnernetwork.ensure_partner_calls_partition(date);
