# F1 · 10 — Calidad y pruebas

## 1. Estrategia

**[PP-F1-QA-001] (MUST)** Pirámide y responsabilidades:

| Nivel | Qué prueba | Herramientas | Dónde corre |
|---|---|---|---|
| Dominio (unit) | Invariantes de agregados, VOs, eventos emitidos, *replay* | `go test`, harness Given/When/Then (QA-002) | cada PR |
| Aplicación (unit) | Handlers de comando/consulta con puertos *fake*, authz por tabla, idempotencia | `go test` | cada PR |
| Proyecciones | Idempotencia y *rebuild* | `go test` + Postgres (testcontainers) | cada PR |
| Integración HTTP | Rutas reales contra Postgres real y JWT de prueba firmados localmente | `httptest` + testcontainers | cada PR |
| Contrato | Respuestas conformes al OpenAPI; *breaking changes* | `oapi-codegen` strict + `openapi-validator` middleware en tests, `oasdiff` | cada PR |
| Frontend unit | Componentes, stores, validación zod (fixture compartido) | Vitest + Testing Library | cada PR |
| E2E | Flujos críticos con Keycloak real | Playwright | nightly + antes de release |
| Seguridad | SAST, dependencias, imágenes, authz exhaustiva | SEC-008, SEC-005 | cada PR |
| Carga (ligera) | p95 con 50 usuarios | k6 | antes de M1.5 |

## 2. Harness de dominio

**[PP-F1-QA-002] (MUST)** `internal/shared/testkit`:

```go
func TestPet_AssignMicrochip_RejectsWhenDeceased(t *testing.T) {
    testkit.Scenario[*petregistry.Pet](t).
        Given(PetRegistered{...}, PetMarkedDeceased{...}).
        When(AssignMicrochip{Microchip: "941000012345678"}).
        ThenError(petregistry.ErrPetDeceased)
}
func TestPet_GrantCareLink_IsIdempotent(t *testing.T) {
    testkit.Scenario[*petregistry.Pet](t).
        Given(PetRegistered{...}, CareLinkGranted{OrgID: org}).
        When(GrantCareLink{OrgID: org}).
        ThenNoEvents()
}
```

- `Given` rehidrata por `Apply`; `When` ejecuta el comando; `Then` compara eventos por tipo y payload (ignorando `event_id`/`occurred_at`).
- Cada invariante de `02-modelo-ddd.md` (I1–I8, O1–O5) tiene al menos una prueba nombrada con su ID.
- Pruebas *golden* de serialización por evento (ARQ-007).

## 3. Integración

**[PP-F1-QA-003] (MUST)**

- `testcontainers-go` con `postgres:17`; migraciones aplicadas una vez por paquete; cada test en transacción con *rollback* o esquema por test (`search_path`), según coste.
- JWT de prueba: clave RSA generada en el test, JWKS servido por `httptest.Server`; `PP_OIDC_ISSUER` apunta al mock. Keycloak real solo en E2E.
- Pruebas de concurrencia del Event Store: N goroutines haciendo `Append` al mismo stream → exactamente una gana por versión, sin huecos ni duplicados; también sobre `microchip_reservations`.
- Pruebas de *rebuild*: truncar `read_*`, reproducir `es.events` por `global_position` con los proyectores → estado idéntico (comparación por *dump*).

## 4. Contrato

**[PP-F1-QA-004] (MUST)** Cada respuesta en pruebas de integración se valida contra el esquema OpenAPI (middleware `kin-openapi` en modo test). `oasdiff` en CI bloquea cambios incompatibles en `/v1`. Ejemplos del YAML se usan como fixtures del cliente TS.

## 5. E2E

**[PP-F1-QA-005] (MUST)** Playwright contra compose completo (Keycloak real, Mailpit para leer correos):

1. Registro de tutor → verificación por email (lee Mailpit) → onboarding → crear mascota → ver en lista.
2. Registrar clínica → SuperAdmin verifica → código público visible.
3. Invitar veterinario → aceptar invitación (segunda sesión) → aparece como miembro.
4. Tutor vincula mascota por código → veterinario ve el paciente y el teléfono del tutor → tutor revoca → el veterinario deja de verlo.
5. Transferencia de tutela entre dos usuarios.

Más: *smoke* de accesibilidad con `@axe-core/playwright` en las pantallas principales (sin violaciones *serious/critical*).

## 6. Umbrales y quality gates

**[PP-F1-QA-006] (MUST)**

| Gate | Umbral |
|---|---|
| Cobertura `domain/` | ≥ 90% líneas |
| Cobertura `app/` | ≥ 80% |
| Cobertura global backend | ≥ 70% |
| Cobertura frontend (stores, validación, composables) | ≥ 70% |
| Suite de autorización (SEC-005) | 100% de acciones × roles cubiertas |
| Lint | 0 errores; *warnings* no bloquean salvo seguridad |
| SAST/dependencias/imágenes | 0 CRITICAL/HIGH sin excepción documentada (`SECURITY-EXCEPTIONS.md` con fecha de caducidad) |
| `specs-status.sh --check` | OK |
| p95 API (k6, 50 VUs, 5 min, staging) | < 300 ms; 0 errores 5xx |

## 7. Convenciones de trabajo

**[PP-F1-QA-007] (MUST)**

- *Trunk-based*: ramas cortas (`feat/PP-F1-API-004-pets-endpoints`), PR pequeños (< 400 líneas netas salvo generado), *squash merge*.
- *Conventional commits*: `feat(petregistry): assign microchip with reservation` + trailer `Spec: PP-F1-DOM-003`.
- Revisión: 1 aprobación obligatoria (2 para `platform/eventstore`, `authz`, migraciones, Keycloak). Checklist de revisión:
  - ¿El cambio respeta las reglas de dependencia (ADR-001)?
  - ¿Comandos tocan un solo agregado? ¿Eventos con `schema_version`? ¿Proyector idempotente?
  - ¿`authz.Can` presente y probado? ¿`404` vs `403` correcto?
  - ¿Validación OpenAPI + VO? ¿Sin PII en logs?
  - ¿Migración compatible hacia atrás? ¿Índices necesarios?
  - ¿Tracking actualizado?
- Versionado semántico de releases (`v0.1.0` = M1.5); *changelog* generado desde commits.

## 8. Definition of Done (requisito)

**[PP-F1-QA-008] (MUST)** Un requisito pasa a `IMPLEMENTADO` cuando:

1. Código mergeado en `main` con PR que referencia el ID.
2. Pruebas automáticas que cubren sus criterios de aceptación (unit + integración; E2E si es flujo crítico).
3. OpenAPI, migraciones, `realm.yaml`, dashboards y docs actualizados en el mismo PR.
4. Gates de QA-006 verdes.
5. Fila de tracking actualizada en el PR.

Pasa a `VERIFICADO` cuando QA/PM valida los criterios de aceptación en staging y enlaza evidencia.
