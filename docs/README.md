# PetPassport — Documentación técnica y de producto

PetPassport es un SaaS público para la gestión integral de la salud de mascotas:
pasaporte digital propiedad del tutor, compartido con clínicas veterinarias y farmacias aliadas.

Esta carpeta contiene **las especificaciones (specs)** organizadas en un roadmap de ejecución
progresiva. Cada fase es autocontenida: modelado DDD, arquitectura, API, datos, frontend,
seguridad, infraestructura y calidad.

## Cómo navegar

| Documento | Qué contiene |
|---|---|
| [`ROADMAP.md`](ROADMAP.md) | Visión del producto, mapa de contextos, fases, criterios de salida y riesgos. |
| [`adr/`](adr/README.md) | Decisiones de arquitectura (ADR). Leer antes de implementar. |
| [`phase-1/`](phase-1/00-vision-y-alcance.md) | Especificación completa de la Fase 1 (Fundación y MVP Core). |
| [`phase-2/`](phase-2/00-vision-y-alcance.md) | Especificación completa de la Fase 2 (Clínica y CQRS-ES). |
| [`phase-3/`](phase-3/00-vision-y-alcance.md) | Especificación completa de la Fase 3 (Search & Performance). |
| [`phase-4/`](phase-4/00-vision-y-alcance.md) | Especificación completa de la Fase 4 (Integración B2B, OCR y farmacias). |
| [`tracking/`](tracking/README.md) | Mecanismo de seguimiento del avance de cada spec (fuente de verdad + script). |
| `phase-5/` … `phase-N/` | Se generan al cerrar la revisión de la fase anterior. |

## Convenciones de las specs

- **Identificador de requisito**: `PP-F<fase>-<ÁREA>-<nnn>` (ej. `PP-F1-IAM-007`).
  Áreas: `PRD` producto, `DOM` dominio/DDD, `ARQ` arquitectura, `IAM` identidad y acceso,
  `API`, `PTN` API de partners, `DAT` datos, `FE` frontend, `NOT` notificaciones, `OCR` extracción/IA, `PRF` rendimiento, `SEC` seguridad, `INF` infraestructura/DevOps, `QA` calidad.
- **Prioridad (MoSCoW)**: `MUST` (bloquea la fase), `SHOULD` (esperado), `COULD` (si hay capacidad).
- **Estado**: vive únicamente en `tracking/phase-N.md`; las specs describen el *qué*, el tracking el *cuánto falta*.
- **Lenguaje**: los documentos están en español; los nombres de código (agregados, eventos,
  endpoints, paquetes) están en inglés y son los del glosario de cada fase.
- **Palabras clave normativas**: *debe/MUST*, *debería/SHOULD*, *puede/COULD* siguen RFC 2119.

## Stack (resumen)

| Capa | Tecnología |
|---|---|
| Backend | Go 1.25+ (monolito modular, hexagonal por módulo, CQRS-ES) |
| Frontend | Vue 3.5 + TypeScript + Vite + Pinia + PrimeVue |
| IAM | Keycloak 26.x (OIDC / PKCE) |
| Datos | PostgreSQL 17 (Event Store + read models), OpenSearch (F3), Redis (F3) |
| Borde | WAF gestionado + Traefik v3 como API Gateway |
| Infra | Docker, GitHub Actions, OpenTelemetry, Prometheus/Grafana |

## Cómo contribuir a las specs

1. Un cambio de alcance se propone como PR sobre `docs/` y se referencia en el ADR o spec afectado.
2. Nunca se borra un ID de requisito; se marca `DESCARTADO` en tracking con la razón.
3. Toda decisión no obvia se registra como ADR (plantilla en `adr/README.md`).
4. Antes de abrir PR de docs: `scripts/specs-status.sh --check` debe pasar.
