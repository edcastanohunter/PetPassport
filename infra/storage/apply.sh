#!/bin/sh
# Versionado y ciclo de vida del object storage (PP-F1-INF-009, DAT-009), idempotente.
# Vale para MinIO de desarrollo y para cualquier proveedor S3 compatible:
#
#   infra/storage/apply.sh <alias-mc> <bucket-media> <bucket-backups>
#
# El alias se crea antes con `mc alias set <alias> https://<endpoint> <key> <secret>`
# usando una credencial de administración del bucket (no la de la API). Políticas:
#   - media: versionado; versiones antiguas de fotos (pets/) se borran a los 30 días
#     (PRD-008: una foto reemplazada o eliminada se retiene 30 días).
#   - backups: versionado; exportaciones de es.events retenidas 26 semanas.
set -eu
ALIAS=${1:?alias de mc}
MEDIA=${2:?bucket de media}
BACKUPS=${3:?bucket de respaldos}
DIR=$(cd "$(dirname "$0")" && pwd)

for b in "$MEDIA" "$BACKUPS"; do
  mc mb --ignore-existing "$ALIAS/$b"
  mc anonymous set none "$ALIAS/$b"
  mc version enable "$ALIAS/$b"
done
mc ilm import "$ALIAS/$MEDIA" < "$DIR/lifecycle-media.json"
mc ilm import "$ALIAS/$BACKUPS" < "$DIR/lifecycle-backups.json"

# Evidencia para el PR / RUNBOOK: estado final de cada bucket.
for b in "$MEDIA" "$BACKUPS"; do
  echo "== $b"
  mc version info "$ALIAS/$b"
  mc ilm rule ls "$ALIAS/$b"
done
