-- +goose Up
-- Roles de aplicación como roles de grupo (NOLOGIN). Los usuarios con LOGIN se crean
-- fuera de las migraciones (infra/postgres/init o consola del proveedor gestionado) y
-- se hacen miembros de estos grupos. Ver docs/phase-1/06-datos-persistencia.md §1.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pp_app_writer') THEN
    CREATE ROLE pp_app_writer NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pp_app_reader') THEN
    CREATE ROLE pp_app_reader NOLOGIN;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
-- Los roles se conservan (pueden tener miembros); las extensiones también.
SELECT 1;
