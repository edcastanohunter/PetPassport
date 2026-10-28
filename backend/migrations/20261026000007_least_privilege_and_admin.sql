-- +goose Up
-- Mínimo privilegio en BD (PP-F1-SEC-010, DAT-001) y soporte de consultas admin
-- (PP-F1-API-008). Los privilegios por defecto de 20261020000002 dan a
-- pp_app_writer solo SELECT/INSERT en `es`, pero el Event Store necesita
-- UPDATE en es.streams (concurrencia optimista, ARQ-004). Aquí se fijan los
-- privilegios explícitos por tabla:
--   es.streams    SELECT, INSERT, UPDATE (versión del stream)
--   es.events     SELECT, INSERT        (append-only: sin UPDATE/DELETE/TRUNCATE)
--   es.outbox     SELECT, INSERT        (el consumidor de F2 recibirá UPDATE propio)
--   platform.audit_log SELECT, INSERT   (auditoría inmutable para la app; la purga
--                                        por retención ≥ 2 años la hace el migrador)
GRANT SELECT, INSERT, UPDATE ON es.streams TO pp_app_writer;
REVOKE UPDATE, DELETE, TRUNCATE ON es.events FROM pp_app_writer;
REVOKE UPDATE, DELETE, TRUNCATE ON es.outbox FROM pp_app_writer;
REVOKE UPDATE, DELETE, TRUNCATE ON platform.audit_log FROM pp_app_writer;
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA es, identity, read_petregistry, read_partnernetwork, platform FROM pp_app_reader;
REVOKE CREATE ON SCHEMA es, identity, read_petregistry, read_partnernetwork, platform FROM PUBLIC;

-- Búsqueda de usuarios por email/nombre en el panel admin (PRD-018) con
-- paginación por (created_at, id) descendente.
CREATE INDEX user_accounts_email_trgm ON identity.user_accounts USING gin ((email::text) gin_trgm_ops);
CREATE INDEX user_accounts_created_idx ON identity.user_accounts (created_at DESC, id DESC);

-- Auditoría filtrable por fechas sin actor/objetivo (GET /admin/audit-log).
CREATE INDEX audit_log_occurred_idx ON platform.audit_log (occurred_at DESC, id DESC);

-- Cola de verificación ordenada por antigüedad y búsqueda por razón social (PRD-017).
CREATE INDEX organizations_legal_name_trgm ON read_partnernetwork.organizations USING gin (legal_name gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS read_partnernetwork.organizations_legal_name_trgm;
DROP INDEX IF EXISTS platform.audit_log_occurred_idx;
DROP INDEX IF EXISTS identity.user_accounts_created_idx;
DROP INDEX IF EXISTS identity.user_accounts_email_trgm;
-- Los privilegios no se amplían en Down: revertir a SELECT/INSERT en
-- es.streams rompería el Event Store de la versión anterior igualmente.
SELECT 1;
