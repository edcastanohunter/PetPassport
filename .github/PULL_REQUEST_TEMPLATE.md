## Qué cambia

<!-- Resumen en 2–4 líneas. -->

## Specs

| ID | Estado anterior | Estado nuevo |
|---|---|---|
| PP-F1-XXX-000 | PENDIENTE | IMPLEMENTADO |

- [ ] `docs/tracking/phase-N.md` actualizado en este PR
- [ ] `scripts/specs-status.sh --check` pasa

## Checklist técnico

- [ ] Respeta reglas de dependencia entre módulos (ADR-001)
- [ ] Comandos tocan un solo agregado; eventos con `schema_version`; proyectores idempotentes
- [ ] `authz.Can` presente y probado; `404` vs `403` correcto
- [ ] Validación OpenAPI + VO; sin PII en logs
- [ ] OpenAPI / migraciones / `realm.yaml` / dashboards actualizados si aplica
- [ ] Migración compatible hacia atrás una versión
- [ ] Pruebas: unit / integración / E2E según corresponda

## Evidencia

<!-- Capturas, salida de pruebas, enlaces a runs. -->
