-- +goose Up
-- Esquemas por contexto (ADR-001: cada contexto posee sus esquemas). El DDL de
-- tablas llega en los hitos M1.1 (identity, platform), M1.2 (es, read_petregistry)
-- y M1.3 (read_partnernetwork).
CREATE SCHEMA IF NOT EXISTS es;
CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS read_petregistry;
CREATE SCHEMA IF NOT EXISTS read_partnernetwork;
CREATE SCHEMA IF NOT EXISTS platform;

COMMENT ON SCHEMA es IS 'Event Store: streams, events (append-only), outbox';
COMMENT ON SCHEMA identity IS 'Identity & Access: UserAccount (CRUD, espejo de Keycloak)';
COMMENT ON SCHEMA read_petregistry IS 'Read models de Pet Registry';
COMMENT ON SCHEMA read_partnernetwork IS 'Read models de Partner Network';
COMMENT ON SCHEMA platform IS 'Idempotencia, auditoría, checkpoints, migraciones';

-- Privilegios: writer opera read models, identity y platform; en es solo
-- inserta (append-only); reader solo lee. Los privilegios por tabla se
-- aplican por defecto a las tablas futuras de cada esquema.
GRANT USAGE ON SCHEMA es, identity, read_petregistry, read_partnernetwork, platform TO pp_app_writer, pp_app_reader;

ALTER DEFAULT PRIVILEGES IN SCHEMA identity, read_petregistry, read_partnernetwork, platform
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA identity, read_petregistry, read_partnernetwork, platform
  GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA es
  GRANT SELECT, INSERT ON TABLES TO pp_app_writer;
ALTER DEFAULT PRIVILEGES IN SCHEMA es
  GRANT USAGE, SELECT ON SEQUENCES TO pp_app_writer;

ALTER DEFAULT PRIVILEGES IN SCHEMA identity, read_petregistry, read_partnernetwork, platform, es
  GRANT SELECT ON TABLES TO pp_app_reader;

-- +goose Down
DROP SCHEMA IF EXISTS read_partnernetwork CASCADE;
DROP SCHEMA IF EXISTS read_petregistry CASCADE;
DROP SCHEMA IF EXISTS identity CASCADE;
DROP SCHEMA IF EXISTS es CASCADE;
-- platform contiene schema_migrations de goose: no se elimina en Down.
