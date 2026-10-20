# Prompts de ejecución — Fase 2 (Clínica y CQRS-ES)

Prerrequisito: Fase 1 cerrada (todos los MUST VERIFICADO). System prompt: `00-agent-operating-instructions.md`.

---

## M2.0 — Event Store v1

```text
Hito: F2 M2.0 Event Store v1. Rama: feat/f2-m2.0-es-v1 desde main. Cimiento de toda la fase: máxima atención.

Lee: ADR-009 (completo, incluida la sección de orden total), ADR-012 §1–4; docs/phase-2/02 (§1–2, §6–7), 03 (completo),
05 (§1, §6), 06 §4 (FE-014), 08 §1 (threat model, SEC-007), 09 (§1 INF-001/002, §2 QA-001/002/003/007).

IDs: DOM-001, DOM-002, DOM-014, DOM-015, ARQ-001, ARQ-002, ARQ-003, ARQ-005, ARQ-006, ARQ-007, ARQ-008, ARQ-009,
DAT-001, DAT-006, FE-014, SEC-007, INF-001, INF-002, QA-001, QA-002, QA-003, QA-007 (prefijo PP-F2-).

Entregables, en orden:
1. Orden total: pg_advisory_xact_lock en Append (ARQ-003) con métrica de espera; test QA-001 (50 escritores, lector sin
   huecos) como gate de CI.
2. cmd/worker (ARQ-001): lock de instancia, runners, apagado ordenado, readyz, métricas.
3. Suscripciones catch-up con checkpoint atómico por lote (ARQ-002), estados de projection_checkpoints (DAT-001),
   fail-stop con backoff y alerta (ARQ-007), rebuild/resume/pause (ARQ-006) con prueba QA-002 (dumps iguales).
4. Snapshots (ARQ-004 llega en M2.3; aquí deja la tabla es.snapshots y la interfaz), upcasters (ARQ-005) con el primer
   upcaster real PetRegistered v1→v2 (alerts: []), golden tests y verificación de cadena completa al arrancar.
5. Consumidor de outbox con orden por stream y dead-letter (ARQ-008, DAT-001 columnas nuevas), framework de process
   managers y scheduler de comandos (ARQ-009, DAT-006) con harness de pruebas QA-003 (reloj simulado).
6. Primera proyección asíncrona vacía pero operativa (clinical.medical_timeline con tablas de DAT-002 §medical_timeline
   creadas) para demostrar el pipeline extremo a extremo.
7. Frontend: componente de frescura (FE-014) reutilizable, cabecera X-PP-Freshness en el cliente API.
8. Compose y despliegue con worker (INF-001); dashboards Worker & Projections y métricas (INF-002).
9. Threat model F2 actualizado (SEC-007). Gates F2 (QA-007) configurados en CI.

Prohibido: mover a asíncrono las proyecciones primarias de F1 (siguen síncronas, ADR-009 §1).

DoD: worker consume y proyecta; rebuild y fail-stop demostrados con pruebas; test de orden total verde; tracking actualizado.
```

---

## M2.1 — Encuentros

```text
Hito: F2 M2.1 Encuentros. Rama: feat/f2-m2.1-encounters desde main. Puede ejecutarse en paralelo con M2.3.

Lee: ADR-010; docs/phase-2/01 (E8, E9), 02 (§3.1–3.3, §8), 03 (§10–12, §15), 04 (§1–3, §12–13), 05 §2, 06 (§1 FE-003/004/005,
§2 FE-009), 08 (§2 tabla, §4–6), 09 (INF-004).

IDs: PRD-001, PRD-002, PRD-003, PRD-004, PRD-005, PRD-006, PRD-008, PRD-009, PRD-025, DOM-003, DOM-004, DOM-005,
ARQ-010, ARQ-011, ARQ-012, API-001, API-002, API-003, API-012, API-013, DAT-002, FE-003, FE-004, FE-005, FE-009,
SEC-001, SEC-003, SEC-004, SEC-005, INF-004.

Entregables:
1. clinical/domain: agregado Encounter (comandos, eventos v1, invariantes E1–E9 con prueba por invariante), VOs DOM-004,
   golden tests. ContentHash sobre JSON canónico RFC 8785 del payload lógico (ARQ-010, y nota de F5 ARQ-007).
2. Puertos PetAccess (con niveles linked/custody) y Membership; proyectores síncronos encounters/attachments; asíncronos
   medical_timeline, weight_history, encounter_custody.
3. Adjuntos (ARQ-011, SEC-003): PDF validado con pdfcpu, imágenes con pipeline F1, prefirmadas, visibilidad para tutor.
4. PDF firmado con Typst en el worker (ARQ-010; haz el spike de Typst vs chromedp y documenta la decisión en el PR),
   document_jobs, endpoint de verificación pública.
5. Endpoints API-001/002/003/013 y catálogo de errores API-012; autorización clínica (SEC-001) ampliando la suite;
   auditoría de accesos clínicos (SEC-005) con partición mensual de audit_log si no existía.
6. Frontend: editor SOAP con autoguardado (FE-003), firma/adendas/PDF (FE-004), ficha de paciente (FE-005), historial del
   tutor (FE-009); gráfico de peso (PRD-025) con la guía dataviz del proyecto.

DoD: abrir → editar → firmar → adenda → PDF → verificación pública demostrables; custodia tras revocación probada; E1–E9
con pruebas; tracking actualizado.
```

---

## M2.2 — Carnet

```text
Hito: F2 M2.2 Carnet. Rama: feat/f2-m2.2-vaccination-card desde main.
Prerrequisito humano: aprobación veterinaria de protocolos CO (DAT-009) antes de activar semillas en staging.

Lee: ADR-010 §2, §4–5; docs/phase-2/01 (E10), 02 (§3.4–3.8), 03 (§13), 04 (§4–6), 05 §3, 06 (FE-006, FE-010, FE-013),
08 §3, 09 (QA-006).

IDs: PRD-010, PRD-011, PRD-012, PRD-013, PRD-014, PRD-015, DOM-006, DOM-007, DOM-008, DOM-009, DOM-016, ARQ-013,
API-004, API-005, API-006, DAT-003, DAT-009, FE-006, FE-010, FE-013, SEC-002, QA-006.

Entregables:
1. clinical/domain: agregado VaccinationCard (V1–V7 con pruebas), VOs, eventos v1; catálogos ref.vaccines/
   vaccination_protocols/preventive_protocols con semillas CO en backend/seeds/ref/ y APPROVALS.md; NextDueCalculator con
   pruebas por tabla.
2. Proyectores card_entries (síncrono) y vaccination_due (asíncrono); atajo POST /encounters/{id}/vaccinations.
3. Endpoints API-004/005/006; RecordShare con tokens, PIN Argon2id, rate limit y noindex (ARQ-013, SEC-002); PDF del carnet
   con QR; verificación pública de carnet.
4. Frontend: carnet en clínica (FE-006), carnet del tutor con declarar/compartir/QR (FE-010), páginas públicas (FE-013).
5. Sesión de validación clínica (QA-006): prepara guion y checklist; registra resultado en docs/phase-2/validation/.

DoD: registrar vacuna → próxima dosis → declarar → verificar → anular → PDF → enlace con PIN demostrables; V1–V7 probadas;
tracking actualizado.
```

---

## M2.3 — Citas

```text
Hito: F2 M2.3 Citas. Rama: feat/f2-m2.3-scheduling desde main. Puede ejecutarse en paralelo con M2.1.

Lee: ADR-011; docs/phase-2/01 (E11), 02 (§4, §7), 03 §5 (snapshots ARQ-004), 04 (§7–8), 05 §4, 06 (FE-001, FE-002, FE-007,
FE-011), 09 (QA-001 parte de reservas).

IDs: PRD-016, PRD-017, PRD-018, PRD-019, DOM-010, DOM-011, DOM-012, ARQ-004, API-007, API-008, DAT-004, FE-001, FE-002,
FE-007, FE-011.

Entregables:
1. scheduling/domain: Appointment (A1–A8 con pruebas, transiciones de la tabla) y ProfessionalAgenda (invariantes,
   snapshots cada 100 eventos: implementa ARQ-004 completo aquí), eventos v1.
2. slot_reservations con restricción de exclusión btree_gist (DAT-004) escrita en la misma tx que Append; prueba de
   100 intentos concurrentes sobre franjas solapadas → exactamente una confirmada.
3. Availability en Go (agendas + reservas), expiración de propuestas y no-show automático por scheduler de comandos.
4. Endpoints API-007/008; autorización de citas en la suite.
5. Frontend: agenda clínica día/semana (FE-001), gestión de citas (FE-002), configuración de agendas (FE-007), citas del
   tutor (FE-011).
6. Process manager scheduling.complete-on-sign (DOM-015) si M2.1 ya está mergeado; si no, deja el PM listo con prueba.

DoD: solicitar → confirmar → reprogramar → check-in → completar demostrables; exclusión probada; snapshots probados;
tracking actualizado.
```

---

## M2.4 — Notificaciones

```text
Hito: F2 M2.4 Notificaciones. Rama: feat/f2-m2.4-notifications desde main.
Prerrequisito humano: proveedor SMTP transaccional con webhooks de bounce (NOT-007).

Lee: ADR-012; docs/phase-2/01 (E8 PRD-007, E13), 03 §14, 04 §10, 05 §5, 06 (FE-012), 07 (completo), 09 (INF-003, QA-005).

IDs: PRD-007, PRD-023, PRD-024, ARQ-014, API-010, DAT-005, FE-012, NOT-001, NOT-002, NOT-003, NOT-004, NOT-005,
NOT-006, NOT-007, INF-003, QA-005.

Entregables:
1. notifications: process manager de reglas (NOT-001 catálogo completo), scheduled + dispatch (NOT-002), plantillas es/en
   MJML con manifiesto y snapshot tests (NOT-003, QA-005), preferencias y baja por token (NOT-004, API-010), entregas con
   reintentos y dead-letter (NOT-005), idempotencia (NOT-006), proveedor con webhooks de bounce (NOT-007).
2. Migración de los emails síncronos de F1 al PM y eliminación del Mailer de reacciones (ARQ-014): ninguna transacción
   de comando abre SMTP (añade un test que lo verifique con un Mailer que falla si se llama dentro de uow).
3. Control recomendado con recordatorios (PRD-007) en Encounter + reglas.
4. Frontend: bandeja y preferencias (FE-012).
5. Alertas F2 (INF-003) y dashboard Notifications.

DoD: cita confirmada → recordatorio T-24h en scheduled → despacho con reloj simulado → email en Mailpit en el idioma
del tutor; reentrega triple → una fila; tracking actualizado.
```

---

## M2.5 — Vínculo solicitado por clínica + release

```text
Hito: F2 M2.5. Rama: feat/f2-m2.5-care-link-requests-release desde main.

Lee: ADR-004 §2; docs/phase-2/00 §5 (criterios de salida), 01 (E9 PRD-026, E12, E14), 02 §5, 04 (§9, §11), 05 (§7–8),
06 (FE-008), 08 §7 (SEC-006), 09 (INF-005, QA-004).

IDs: PRD-020, PRD-021, PRD-022, PRD-026, DOM-013, API-009, API-011, DAT-007, DAT-008, FE-008, SEC-006, INF-005, QA-004.

Entregables:
1. Pet: CareLinkRequest (comandos, eventos, expiración por scheduler), alertas de mascota, búsqueda con resultados mínimos
   y rate limit; bandeja del tutor; proyección org_stats y endpoint de estadísticas (PRD-026).
2. Frontend FE-008 y aprobación en la bandeja del tutor.
3. Co-tutor (PRD-021) e importación CSV (PRD-022) solo si hay capacidad: COULD. Si no, DESCARTADO con nota "F3".
4. Privacidad clínica (SEC-006): exportación semiautomática `cmd/worker export --user`, aviso de privacidad actualizado.
5. Retención y particionado de audit_log (DAT-008). Runbook F2 (INF-005). E2E F2 completo (QA-004).
6. Ejecuta los criterios de salida de docs/phase-2/00 §5 (rebuild en staging con > 10k eventos, concurrencia, lag) y adjunta
   evidencia; tag v0.2.0 tras aprobación humana.

DoD: flujo solicitud → aprobación → acceso → revocación → custodia demostrable; criterios de salida con evidencia;
tracking actualizado.
```
