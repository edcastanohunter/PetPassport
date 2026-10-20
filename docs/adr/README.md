# Architecture Decision Records (ADR)

Registro de decisiones de arquitectura. Una decisión se registra cuando no es obvia, es costosa de revertir
o afecta a más de un módulo. Formato: contexto → decisión → consecuencias → alternativas descartadas.

| ADR | Título | Estado |
|---|---|---|
| [ADR-001](ADR-001-monolito-modular.md) | Monolito modular en Go con límites de contexto estrictos | Aceptada |
| [ADR-002](ADR-002-event-sourcing-desde-fase-1.md) | Event Store v0 desde la Fase 1; ES selectivo por agregado | Aceptada |
| [ADR-003](ADR-003-keycloak-authn-authz-en-app.md) | Keycloak solo autentica; autorización y membresías viven en la aplicación | Aceptada |
| [ADR-004](ADR-004-propiedad-de-datos-y-vinculos.md) | Modelo owner-centric: el pasaporte es del tutor; la clínica accede por CareLink | Aceptada |
| [ADR-005](ADR-005-api-rest-openapi.md) | API REST/JSON con OpenAPI 3.1 como contrato fuente | Aceptada |
| [ADR-006](ADR-006-gateway-y-waf.md) | WAF gestionado en el borde + Traefik como API Gateway; JWT se valida en la app | Aceptada |
| [ADR-007](ADR-007-monorepo-e-identificadores.md) | Monorepo; UUIDv7 como identificador universal | Aceptada |
| [ADR-008](ADR-008-despliegue-inicial.md) | Despliegue inicial en contenedores sobre VM + PostgreSQL gestionado; Kubernetes diferido | Aceptada |
| [ADR-009](ADR-009-proyecciones-asincronas-y-orden-de-eventos.md) | Proyecciones asíncronas por suscripción catch-up; outbox solo para efectos externos; orden total garantizado | Aceptada |
| [ADR-010](ADR-010-encounter-como-agregado-e-inmutabilidad-clinica.md) | `Encounter` y `VaccinationCard` como agregados; historial clínico como read model; inmutabilidad por firma y adendas | Aceptada |
| [ADR-011](ADR-011-agenda-y-reserva-de-franjas.md) | `Appointment` y `ProfessionalAgenda` como agregados; no-solapamiento por restricción de exclusión | Aceptada |
| [ADR-012](ADR-012-notificaciones-como-process-manager.md) | Notificaciones como process manager sobre el outbox; planificación en tabla; solo email en F2 | Aceptada |
| [ADR-013](ADR-013-opensearch-como-read-model-de-busqueda.md) | OpenSearch como read model de búsqueda; indexación catch-up; autorización denormalizada | Aceptada |
| [ADR-014](ADR-014-redis-usos-permitidos.md) | Redis para caché, rate limiting y trabajos no ordenados; PostgreSQL sigue siendo la fuente durable | Aceptada |
| [ADR-015](ADR-015-escalado-horizontal.md) | Escalado horizontal: `api` N réplicas, `worker` con leases; Helm en F3, Kubernetes en F5 | Aceptada (actualiza ADR-008) |
| [ADR-016](ADR-016-catalogo-de-medicamentos.md) | Catálogo de medicamentos como datos de referencia; `MedicationItem` v2 con `catalog_id` | Aceptada |
| [ADR-017](ADR-017-borde-partner-y-autenticacion-b2b.md) | Borde dedicado para partners, OAuth2 client-credentials, webhooks firmados; gateway diferido | Aceptada |
| [ADR-018](ADR-018-prescription-como-agregado.md) | `Prescription` como agregado con dos orígenes; validez, controladas, verificación | Aceptada |
| [ADR-019](ADR-019-pipeline-ocr-ia-con-confirmacion-humana.md) | Extracción con modelo de visión y salida estructurada; confianza por campo; confirmación humana | Aceptada |
| [ADR-020](ADR-020-saga-de-cotizacion-y-pedidos.md) | `QuoteRequest` como saga; `Order` como agregado; pago en la farmacia | Aceptada |
| [ADR-021](ADR-021-niveles-de-integracion-de-partners.md) | Tres niveles de integración de partners; ACL por adaptador; simulador | Aceptada |

## Plantilla

```markdown
# ADR-NNN: Título

- Estado: Propuesta | Aceptada | Reemplazada por ADR-XXX
- Fecha: AAAA-MM-DD
- Fase: F1

## Contexto
## Decisión
## Consecuencias
## Alternativas consideradas
```
