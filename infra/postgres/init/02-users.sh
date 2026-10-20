#!/bin/sh
# Usuarios con LOGIN (contraseñas desde el entorno del contenedor) y base de Keycloak.
set -e
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE ROLE petpassport_migrator LOGIN PASSWORD '${PP_MIGRATOR_PASSWORD}';
GRANT ALL PRIVILEGES ON DATABASE petpassport TO petpassport_migrator;
ALTER DATABASE petpassport OWNER TO petpassport_migrator;

CREATE ROLE petpassport_api LOGIN PASSWORD '${PP_API_PASSWORD}';
GRANT pp_app_writer TO petpassport_api;
GRANT CONNECT ON DATABASE petpassport TO petpassport_api;

CREATE ROLE petpassport_reader LOGIN PASSWORD '${PP_API_PASSWORD}';
GRANT pp_app_reader TO petpassport_reader;
GRANT CONNECT ON DATABASE petpassport TO petpassport_reader;

CREATE ROLE keycloak LOGIN PASSWORD '${KC_DB_PASSWORD}';
CREATE DATABASE keycloak OWNER keycloak;
SQL
