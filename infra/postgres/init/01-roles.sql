-- Inicialización de PostgreSQL en desarrollo (docker-entrypoint-initdb.d).
-- En entornos gestionados estos roles se crean a mano con las mismas membresías.
-- Grupos (NOLOGIN) idénticos a los que crea la migración 20261020000001.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pp_app_writer') THEN CREATE ROLE pp_app_writer NOLOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pp_app_reader') THEN CREATE ROLE pp_app_reader NOLOGIN; END IF;
END $$;
