# PetPassport — instrucciones para agentes

Este repositorio contiene las especificaciones (`docs/`) y, progresivamente, el código de PetPassport:
SaaS de gestión de salud de mascotas. Backend Go (monolito modular, CQRS-ES), frontend Vue 3, Keycloak,
PostgreSQL, OpenSearch, Redis. Lee `docs/README.md` y `docs/ROADMAP.md` antes de cualquier tarea.

## Fuente de verdad

1. **ADRs** (`docs/adr/`) prevalecen sobre cualquier otra consideración. No las contradigas; si crees que una
   está mal, escribe un ADR nuevo con estado `Propuesta` y detén el trabajo afectado.
2. **Specs de fase** (`docs/phase-N/`) definen el *qué*. Cada requisito tiene ID `PP-FN-ÁREA-nnn` y prioridad.
3. **Tracking** (`docs/tracking/phase-N.md`) es la única fuente del *estado*. Actualízalo en el mismo PR.
4. `docs/handoff/` contiene las instrucciones de operación y los prompts por hito.

## Reglas de trabajo (no negociables)

- Trabaja **por hito** (M1.0, M1.1…), nunca por fase completa. Un PR por hito o menor.
- Antes de codificar: lee los documentos que el prompt del hito indica, lista los IDs que vas a implementar y
  confirma que todos están en `PENDIENTE` o `EN_PROGRESO` en tracking.
- Respeta las reglas de dependencia entre módulos (ADR-001) y el glosario de cada fase (nombres de código en
  inglés, exactamente como en la columna "Código").
- Un comando toca un solo agregado. Eventos con `schema_version`. Proyectores idempotentes. `authz.Can` en
  cada handler. `404` para recursos no visibles. Sin PII en logs. Ver checklist en `docs/phase-1/10-calidad-y-pruebas.md`.
- Toda decisión no cubierta por las specs se registra en el PR bajo "Decisiones tomadas" y, si afecta a más
  de un módulo, como ADR `Propuesta`.
- No inventes firmas de SDKs ni APIs externas: compila e itera contra el error real.
- Ejecuta `scripts/specs-status.sh --check` antes de abrir PR; actualiza las filas de tracking que cambian de estado.
- Nunca ejecutes `UPDATE`/`DELETE` sobre `es.events` (única excepción documentada: ADR-023 §3, F5).
- Commits: *conventional commits* con trailer `Spec: PP-FN-ÁREA-nnn`.

## Comandos útiles

```
scripts/specs-status.sh --phase N      # avance de una fase
scripts/specs-status.sh --check        # consistencia specs ↔ tracking
make dev-up | make test | make lint    # cuando exista el código (F1 M1.0 los crea)
```
