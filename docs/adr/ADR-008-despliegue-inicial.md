# ADR-008: Despliegue inicial en contenedores sobre VM + PostgreSQL gestionado; Kubernetes diferido

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1 (revisar en F3/F5)

## Contexto

Equipo pequeño, un solo binario (ADR-001), carga inicial baja. Kubernetes desde F1 añade semanas de
plataforma antes de la primera funcionalidad.

## Decisión

- **Staging y producción F1–F2**: una VM por entorno (2–4 vCPU) con Docker Compose gestionado por un
  script de despliegue idempotente (`infra/deploy/`), detrás del WAF gestionado. Servicios en la VM:
  Traefik, `api`, `worker` (F2), Keycloak, MinIO (solo staging).
- **Datos gestionados desde el día 1**: PostgreSQL gestionado por el proveedor (backups automáticos, PITR),
  object storage gestionado (S3/R2/GCS) en producción; MinIO solo en dev/staging.
- **Imágenes** multi-stage, usuario no root, base `distroless`/`alpine`, firmadas y escaneadas (Trivy).
- **Kubernetes** se adopta cuando ocurra lo primero: necesidad de escalado horizontal del `worker` (F3),
  extracción de servicios (F6) o requisito de alta disponibilidad multi-zona (F5). Las imágenes y la
  configuración 12-factor ya son compatibles.

## Consecuencias

- Tiempo-a-MVP mínimo; coste bajo.
- Alta disponibilidad limitada en F1 (una VM). Aceptado para MVP; mitigado con PostgreSQL gestionado y backups.
- Los *health checks*, *graceful shutdown* y la configuración por entorno se diseñan ya pensando en K8s.
