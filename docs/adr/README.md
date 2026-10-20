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
