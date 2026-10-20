# Handoff: cómo ejecutar el roadmap con agentes (o personas)

## 1. Qué hay y qué falta

| Hay | Falta (y quién lo cubre) |
|---|---|
| 25 ADRs, specs completas F1–F5 (≈ 580 requisitos con ID, prioridad, hito), tracking verificable por script. | **Código**: nada implementado. Los prompts de este directorio producen el código hito a hito. |
| Instrucciones de operación para agentes (`/CLAUDE.md`, este directorio). | **Decisiones de negocio abiertas** (ver §4): precios, entidad legal, partners con API, proveedor de pagos/facturación, proveedor de helpdesk. Bloquean hitos concretos, no el arranque. |
| Prompts por hito (30) con alcance, lecturas, entregables y DoD. | **Fase 6** (Escala y Evolución): no especificada; es un portafolio de opciones a priorizar. |
| | **Artefactos referenciados por las specs** que se crean durante la implementación: `openapi.yaml`, `partner-v1.yaml`, `realm.yaml`, fixtures de validación, mappings de OpenSearch, prompts/esquema de OCR, plantillas. Cada prompt de hito los lista como entregables. |

## 2. Cómo usar los prompts

1. Pega el contenido de `00-agent-operating-instructions.md` como *system prompt* (o confía en `/CLAUDE.md` si el agente lo lee).
2. Lanza **un hito por sesión** con el prompt correspondiente de `phase-N-prompts.md`. Los hitos de una fase son secuenciales salvo donde el prompt indica paralelización (tareas FE suelen poder ir en paralelo con API una vez fijado el contrato OpenAPI).
3. El agente debe terminar con un PR por hito (o varios pequeños) y tracking actualizado. **Revisión humana obligatoria en cada hito** antes de lanzar el siguiente: es la compuerta de calidad.
4. Si el agente encuentra una ambigüedad, debe elegir la opción más simple compatible con los ADRs, documentarla en el PR y seguir; si contradice un ADR, debe parar y proponer.

## 3. Qué modelo para qué

| Tipo de hito | Recomendación | Motivo |
|---|---|---|
| Cimientos transversales: F1 M1.0–M1.3, F2 M2.0, F3 M3.0, F5 M5.3 (cifrado) y M5.1 (dinero) | El modelo más capaz disponible (p. ej. Claude Opus 5.5 con *effort* alto) y revisión de arquitectura humana. Si yo (el modelo que escribió las specs) sigo disponible, son los que preferiría hacer. | Errores aquí se propagan a todo; hay decisiones implícitas que requieren leer varios ADRs a la vez. |
| Funcionalidad sobre cimientos estables: pantallas, endpoints, proyecciones, plantillas, runbooks | Modelo de coste medio (p. ej. Claude Sonnet 5.5). Varios agentes en paralelo por área (API / FE / DAT) con el contrato OpenAPI fijado primero. | Alcance cerrado por ID; el riesgo está acotado por las pruebas y la suite de authz. |
| Documentación operativa, semillas, plantillas de email, fixtures | Modelo económico (p. ej. Claude Haiku 5.5) con revisión. | Tareas mecánicas con criterios claros. |
| Revisión de PRs | Un modelo distinto del que escribió el código, con el checklist de `docs/phase-1/10-calidad-y-pruebas.md` §7 y la suite de seguridad. | Evita puntos ciegos compartidos. |

## 4. Decisiones que debes tomar (bloquean hitos)

| Decisión | Bloquea | Dónde se documenta |
|---|---|---|
| Dominio y proveedor de WAF/CDN; cuenta cloud | F1 M1.4 | `docs/phase-1/09` |
| Textos legales (términos, privacidad) | F1 M1.5 | `docs/phase-1/01` PRD-020 |
| Proveedor SMTP | F1 M1.5 / F2 M2.4 | `docs/phase-2/07` NOT-007 |
| Aprobación veterinaria de protocolos de vacunación | F2 M2.2 | `docs/phase-2/05` DAT-009 |
| Partners con API (si existen) | F4 M4.3 (añade adaptadores nivel 3) | `docs/phase-4/00` |
| Proveedor de IA y acuerdo de datos | F4 M4.1 | `docs/phase-4/07` OCR-009 |
| Entidad legal, proveedor de pagos y de facturación fiscal | F5 M5.1–M5.2 | ADR-022 |
| Precios de planes | F5 M5.1 (`ref.plans` queda `draft`) | `docs/phase-5/01` §3 |
| Helpdesk interno vs externo | F5 M5.4 | `docs/phase-5/03` ARQ-011 |
| Prioridades de F6 | redacción de F6 | — |

## 5. Orden recomendado y paralelización

```
F1: M1.0 → M1.1 → M1.2 → M1.3 → M1.4 → M1.5           (secuencial; FE puede ir en paralelo desde M1.1 con OpenAPI fijado)
F2: M2.0 → (M2.1 ∥ M2.3) → M2.2 → M2.4 → M2.5          (encuentros y citas son independientes tras ES v1)
F3: M3.0 → (M3.1 ∥ M3.2) → M3.3 → M3.4 → M3.5
F4: M4.0 → (M4.1 ∥ M4.2) → M4.3 → M4.4 → M4.5
F5: M5.0 (puede empezar durante F4) → M5.1 → M5.2 → (M5.3 ∥ M5.4) → M5.5
```

## 6. Señales de que un hito no está listo para cerrar

- Tracking con IDs del hito aún en `PENDIENTE` sin nota.
- `make test` verde pero sin pruebas nombradas por invariante (I1…, E1…, Q1…, B1…).
- Handlers sin `authz.Can` (el test de cobertura de rutas debe existir desde M1.2).
- PR sin sección "Decisiones tomadas" cuando hubo ambigüedad.
- Cambios en `docs/adr/` dentro de un PR de implementación (los ADRs se cambian en PRs propios).
