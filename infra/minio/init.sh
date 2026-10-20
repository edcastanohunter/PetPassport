#!/bin/sh
# Crea el bucket de media (privado; acceso por URLs prefirmadas, docs/phase-1/03 §9).
set -e
mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
mc mb --ignore-existing local/petpassport-media
mc anonymous set none local/petpassport-media
echo "bucket petpassport-media listo"
