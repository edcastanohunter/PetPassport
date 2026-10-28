#!/bin/sh
# Buckets de desarrollo (privados; media por URLs prefirmadas, docs/phase-1/03 §9) con
# el mismo versionado y ciclo de vida que staging/prod (infra/storage, INF-009).
set -e
# La imagen de MinIO no trae shell ni mc para un healthcheck: se espera aquí.
MINIO_URL=${MINIO_URL:-http://minio:9000}
i=0
until mc alias set local "$MINIO_URL" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1 && mc ready local >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 60 ] || { echo "MinIO no responde en $MINIO_URL" >&2; exit 1; }
  sleep 2
done
/bin/sh /policies/apply.sh local petpassport-media petpassport-backups
echo "buckets petpassport-media y petpassport-backups listos"
