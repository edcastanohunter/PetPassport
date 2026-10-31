-- +goose Up
-- Retención de audit_log y nuevo aviso de privacidad (PP-F2-DAT-008, SEC-006).

-- Retención de 2 años del audit_log de accesos clínicos: el worker llama a
-- esta función a diario. SECURITY DEFINER porque la app no tiene DROP; solo
-- toca particiones mensuales `audit_log_YYYY_MM` cuyo mes termina antes del
-- corte (nunca la partición DEFAULT ni la del mes en curso).
-- +goose StatementBegin
CREATE FUNCTION platform.drop_audit_partitions_before(cutoff date) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, platform AS $$
DECLARE
  part  record;
  month date;
  n     integer := 0;
BEGIN
  FOR part IN
    SELECT c.relname
      FROM pg_inherits i
      JOIN pg_class c ON c.oid = i.inhrelid
     WHERE i.inhparent = 'platform.audit_log'::regclass
       AND c.relname ~ '^audit_log_[0-9]{4}_[0-9]{2}$'
  LOOP
    month := to_date(substr(part.relname, 11), 'YYYY_MM');
    IF (month + interval '1 month')::date <= cutoff THEN
      EXECUTE format('DROP TABLE platform.%I', part.relname);
      n := n + 1;
    END IF;
  END LOOP;
  RETURN n;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.drop_audit_partitions_before(date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.drop_audit_partitions_before(date) TO pp_app_writer;

-- Nueva versión del aviso de privacidad: describe la compartición clínica, los
-- enlaces públicos, los recordatorios y la solicitud de vínculo de las
-- clínicas. Los usuarios existentes la re-aceptan en su próximo acceso
-- (PRD-003 de F1). El texto definitivo lo aporta legal (prerrequisito humano).
INSERT INTO identity.consent_versions (kind, version, url, effective_from) VALUES
  ('privacy', '2026-11-draft', '/legal/privacy', '2026-11-01');

-- +goose Down
DELETE FROM identity.consent_versions WHERE kind = 'privacy' AND version = '2026-11-draft';
DROP FUNCTION IF EXISTS platform.drop_audit_partitions_before(date);
