# Seguimiento del avance de las specs

Objetivo: saber en todo momento **qué requisitos de cada fase están implementados, verificados o bloqueados**,
sin depender de herramientas externas. La fuente de verdad es un archivo Markdown por fase, verificado por script en CI.

## 1. Fuente de verdad

`docs/tracking/phase-N.md` contiene una tabla con **una fila por requisito**:

| Columna | Contenido |
|---|---|
| `ID` | `PP-F1-IAM-007`. Debe existir en la spec correspondiente. |
| `Área` | PRD, DOM, ARQ, IAM, API, DAT, FE, SEC, INF, QA. |
| `Requisito` | Resumen de una línea. |
| `Prio` | MUST / SHOULD / COULD. |
| `Hito` | M1.0 … M1.5. |
| `Estado` | Uno de los tokens de la sección 2. |
| `Issue` | `#123` o vacío. |
| `PR` | `#456` o vacío. |
| `Notas` | Bloqueos, decisiones, enlaces. |

Las specs (`docs/phase-N/*.md`) describen el requisito; **el estado solo se edita en tracking**.

## 2. Estados (tokens exactos)

| Token | Significado | Quién lo pone |
|---|---|---|
| `PENDIENTE` | Aprobado en la spec, sin empezar. | Al crear la fila. |
| `EN_DISENO` | Se está refinando (diseño técnico, prototipo, spike). | Dev responsable. |
| `EN_PROGRESO` | Hay rama/PR abierta. | Dev responsable. |
| `IMPLEMENTADO` | PR mergeada con pruebas unitarias/integración verdes. | El PR que lo implementa actualiza la fila. |
| `VERIFICADO` | Criterios de aceptación validados en staging (QA/PM) con evidencia enlazada. | QA / PM. |
| `BLOQUEADO` | No puede avanzar; la nota dice por qué y quién desbloquea. | Cualquiera. |
| `DESCARTADO` | Fuera de alcance; la nota enlaza la decisión (ADR o issue). | PM + Arquitectura. |

Transiciones permitidas: `PENDIENTE → EN_DISENO → EN_PROGRESO → IMPLEMENTADO → VERIFICADO`.
`BLOQUEADO` y `DESCARTADO` pueden entrar desde cualquier estado; de `BLOQUEADO` se vuelve al estado anterior.

### Definition of Done por transición

- **→ IMPLEMENTADO**: código mergeado en `main`; pruebas automáticas cubren los criterios de aceptación;
  OpenAPI/migraciones actualizados; sin findings críticos de seguridad; PR referencia el ID.
- **→ VERIFICADO**: desplegado en staging; caso de aceptación ejecutado (manual o E2E) con enlace a evidencia
  (run de Playwright, captura o issue de QA); no hay bugs abiertos de severidad alta sobre el requisito.

## 3. Script de verificación y resumen

```bash
scripts/specs-status.sh              # resumen de todas las fases: conteo por estado y por área
scripts/specs-status.sh --phase 1    # solo Fase 1
scripts/specs-status.sh --check      # validación (sale con código ≠ 0 si falla)
```

`--check` valida:
1. Todo ID mencionado en `docs/phase-N/*.md` existe en `docs/tracking/phase-N.md` y viceversa.
2. No hay IDs duplicados.
3. Todos los estados son tokens válidos.
4. Todo requisito `MUST` con estado `DESCARTADO` tiene nota no vacía.

Se ejecuta en CI en cada PR que toque `docs/`.

## 4. Convenciones en GitHub

- **Milestones**: uno por hito (`M1.0 Setup`, `M1.1 Identity`, …). Fecha objetivo opcional.
- **Issues**: título `[PP-F1-IAM-007] Validación JWT en backend`; etiquetas `phase/1`, `area/iam`, `type/spec`.
  Plantilla en `.github/ISSUE_TEMPLATE/spec.md`.
- **PRs**: la plantilla pide los IDs implementados y el cambio de estado en tracking **en el mismo PR**.
- **Commits**: *conventional commits* con *trailer* `Spec: PP-F1-IAM-007` cuando aplique.

## 5. Ritual semanal (15 min)

1. Ejecutar `scripts/specs-status.sh --phase N` y pegar el resumen en el canal del equipo.
2. Revisar `BLOQUEADO`: cada bloqueo tiene dueño y fecha.
3. Revisar `IMPLEMENTADO` sin `VERIFICADO` > 1 semana: QA prioriza.
4. Decidir si algún `SHOULD/COULD` pasa a `DESCARTADO` para proteger el hito.

## 6. Criterio de cierre de fase

Una fase se cierra cuando **todos los `MUST` están `VERIFICADO`** y los `SHOULD` están `VERIFICADO` o `DESCARTADO`
con decisión registrada. Al cerrar, se genera la spec de la siguiente fase.
