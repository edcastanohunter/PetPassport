# Prompts de ejecución — Fase 4 (Integración B2B, OCR y farmacias)

Prerrequisito: Fase 3 cerrada; proveedor de IA y DPA decididos antes de M4.1. System prompt: `00-agent-operating-instructions.md`.

---

## M4.0 — Recetas emitidas

```text
Hito: F4 M4.0 Recetas emitidas. Rama: feat/f4-m4.0-prescriptions desde main.

Lee: ADR-018; docs/phase-4/01 (E20), 02 (§1–4 salvo 4.3, §6 DOM-012, §7), 03 (§2, §10 ARQ-011), 04 (§A.1, A.2, A.4, A.10),
05 (§1, §3 credenciales, §5), 06 (FE-001, FE-012), 08 (§6–7), 09 (QA-007).

IDs: PRD-001, PRD-002, PRD-003, PRD-004, PRD-020, DOM-001, DOM-002, DOM-003, DOM-004, DOM-005, DOM-006, DOM-012, DOM-013,
ARQ-001, ARQ-011, API-001, API-002, API-004, API-010, DAT-001, DAT-003, DAT-005, FE-001, FE-012, SEC-006, SEC-007, QA-007
(prefijo PP-F4-).

Entregables:
1. identity.professional_credentials con verificación admin y puerto CredentialCheck (DOM-003, API-001).
2. prescriptions/domain: agregado Prescription (origen issued completo; external solo comandos de subida/estado para
   que M4.1 añada extracción), invariantes R1–R9 con pruebas, VOs, eventos v1; ref.regulatory_rules con semilla CO
   (DOM-012, DAT-005); puertos EncounterPlanReader, RegulatoryRules, CatalogEquivalence.
3. Read models read_prescriptions (DAT-001), PDF Typst con QR y verificación pública que recomputa el hash (ARQ-011),
   contador de verificaciones; expiración diaria por scheduler.
4. Endpoints API-002 (emitir desde consulta o directa, revocar, PDF), API-004 (mis recetas, verify público), errores API-010.
5. Integridad y fraude (SEC-006), cumplimiento (SEC-007): controladas emitibles y marcadas; límites.
6. Frontend: credencial, emitir receta desde consulta, mis recetas de veterinario (FE-001); verificación pública (FE-012).
7. Módulos nuevos y lint de dependencias (ARQ-001); gates F4 (QA-007).

DoD: credencial → verificación → emitir → PDF → verificar por QR → revocar demostrables; R1–R9 probadas; tracking actualizado.
```

---

## M4.1 — Recetas externas + OCR

```text
Hito: F4 M4.1 OCR. Rama: feat/f4-m4.1-ocr desde main. Puede ir en paralelo con M4.2.
Prerrequisito humano: clave del proveedor de IA en el secret manager de staging; DPA revisado; presupuesto inicial.

Lee: ADR-019; docs/phase-4/01 (E21), 02 (§4.3), 03 (§11 ARQ-012), 04 (§A.3), 05 (§4), 06 (FE-002, FE-003), 07 (completo),
08 (§8), 09 (QA-003).

IDs: PRD-005, PRD-006, PRD-007, DOM-007, ARQ-012, API-003, DAT-004, FE-002, FE-003, OCR-001, OCR-002, OCR-003, OCR-004,
OCR-005, OCR-006, OCR-007, OCR-008, OCR-009, OCR-010, SEC-008, QA-003.

Entregables:
1. internal/ocr: pipeline asynq (OCR-001), preprocesado con quality_score (OCR-002), extractor Claude con el SDK oficial de
   Go — modelo por configuración (por defecto claude-opus-5-5), imágenes base64, output_config.format con el esquema
   backend/internal/ocr/schema/prescription_extraction.v1.json, effort medium, fallbacks "default", comprobación de
   stop_reason refusal/max_tokens, registro de usage y coste (OCR-003; compila contra el SDK real e itera, no adivines
   firmas) — extractor Tesseract de respaldo (OCR-004), emparejamiento con OpenSearch medications (OCR-005), umbrales
   configurables (OCR-006).
2. Prescription: RecordExtraction, RequestReupload, ConfirmExternalItems con diff, Reopen, ValidateByPharmacist (DOM-007);
   read model extraction_jobs; endpoints API-003 con reintentos limitados.
3. Presupuesto y uso (OCR-008, DAT-004) con corte duro AI_BUDGET_EXCEEDED y captura manual; privacidad y egress hacia
   api.anthropic.com por allowlist (OCR-009, SEC-008); métricas de edición desde el diff (OCR-010).
4. Dataset de evaluación inicial (≥ 60 sintéticas para arrancar; objetivo 200 al cierre) y harness `cmd/worker ocr-eval`
   con gate de no-regresión en CI con mocks grabados y muestra real en nightly (OCR-007, QA-003); Batches para
   reprocesos documentado.
5. Frontend: captura guiada y revisión lado a lado (FE-002), mis recetas del tutor (FE-003).

DoD: foto → extracción → revisión con corrección → confirmación demostrable; eval con F1 producto ≥ 0,85 sobre el dataset;
0 acciones automáticas sin confirmación (test); tracking actualizado.
```

---

## M4.2 — Portal de farmacia y cotización (nivel 1)

```text
Hito: F4 M4.2 Cotización. Rama: feat/f4-m4.2-pharmacy-orders desde main. Puede ir en paralelo con M4.1.

Lee: ADR-020, ADR-021 (nivel 1); docs/phase-4/01 (E22, E23 PRD-012..014, PRD-017, PRD-019, PRD-021), 02 (§5, §6 DOM-011, §8),
03 (§3–4 nivel portal, §12–13), 04 (§A.5–A.7), 05 §2, 06 (FE-004..FE-009), 08 (§1 tabla, §5), 09 (QA-001).

IDs: PRD-008, PRD-009, PRD-010, PRD-011, PRD-012, PRD-013, PRD-014, PRD-017, PRD-019, PRD-021, DOM-008, DOM-009, DOM-010,
DOM-011, DOM-014, ARQ-002, ARQ-003, ARQ-013, ARQ-014, API-005, API-006, API-007, DAT-002, FE-004, FE-005, FE-006, FE-007,
FE-008, FE-009, SEC-001, SEC-005, QA-001.

Entregables:
1. pharmacyorders/domain: QuoteRequest (saga, Q1–Q8 con pruebas) y Order (O1–O8, transiciones), eventos v1;
   PartnerIntegration/ProductMapping/PartnerCall (DOM-011, modo portal) y CatalogEquivalence para sustituciones.
2. Saga (ARQ-002): fanout por PM, PartnerGateway con implementación `portal` (ARQ-003), expiración y recordatorio por
   scheduler, reapertura por rechazo, cierre por revocación; payload builders con lista blanca de campos (ARQ-013, SEC-005).
3. Read models (DAT-002, ARQ-014) incl. quote_board, pharmacy_inbox, partner_sla básico; SSE para tablero y pedidos.
4. Endpoints tutor (API-005/006) y portal (API-007); autorización F4 (SEC-001) en la suite.
5. Dispensación → Prescription.RecordDispensation por PM; notificaciones del ciclo (PRD-019) como reglas en Notifications.
6. Frontend: wizard de cotización (FE-004), tablero SSE (FE-005), pedidos (FE-006); portal: bandeja/formulario (FE-007),
   pedidos y dispensación (FE-008), configuración y mapeo (FE-009); farmacias cercanas desde directorio (PRD-021).
7. Pruebas de saga con harness de PM y concurrencia de SelectQuote (QA-001).

DoD: receta → cotización a 3 farmacias (portal) → comparación → pedido → entregado → dispensación en la receta demostrable;
Q1–Q8 y O1–O8 probadas; tracking actualizado.
```

---

## M4.3 — Partner API (nivel 2)

```text
Hito: F4 M4.3 Partner API. Rama: feat/f4-m4.3-partner-api desde main.
Si existe un partner real con API: añade su adaptador nivel 3 como IDs SHOULD en tracking con nota.

Lee: ADR-017, ADR-021 (niveles 2–3); docs/phase-4/01 (PRD-015, PRD-018), 03 (§5–9), 04 (§A.8–A.9, §B completo), 06 (FE-010,
FE-011, FE-013), 08 (§2–4), 09 (INF-002, INF-006, INF-007, QA-002).

IDs: PRD-015, PRD-018, ARQ-004, ARQ-005, ARQ-006, ARQ-007, ARQ-008, ARQ-009, API-008, API-009, PTN-001, PTN-002, PTN-003,
PTN-004, PTN-005, PTN-006, PTN-007, FE-010, FE-011, FE-013, SEC-002, SEC-003, SEC-004, INF-002, INF-006, INF-007, QA-002.

Entregables:
1. backend/api/partner-v1.yaml (PTN-001) y handlers generados; auth client-credentials con audiencia propia y
   PartnerPrincipal (ARQ-005, PTN-002, SEC-002); clients Keycloak creados por service account con prefijo partner-;
   endpoints PTN-003; rate limits por partner (PTN-006).
2. Webhooks salientes firmados con rotación, reintentos, DLQ y vectores de prueba (ARQ-004, PTN-004, SEC-003).
3. platform/egress con anti-SSRF y circuit breaker (ARQ-006, SEC-004) y proxy saliente en infra (INF-002).
4. cmd/partner-sim (ARQ-007) con comportamientos configurables; sandbox desplegado con realm propio (ARQ-008, PTN-005);
   checklist go-live automatizada (PTN-007, QA-002); developers site generado en CI (ARQ-009, FE-013, INF-007).
5. Portal: credenciales, webhooks, sandbox, registro de entregas (API-008, FE-010); admin de partners, reglas, presupuesto,
   monitor OCR (API-009, FE-011); secretos por partner (INF-006).
6. Gateway `api` implementación de PartnerGateway (ARQ-003) y, si procede, primer adaptador nivel 3 con cassettes.

DoD: partner-sim completa la checklist go-live contra staging; vectores de firma y replay probados; tracking actualizado.
```

---

## M4.4 — Borde B2B y hardening

```text
Hito: F4 M4.4. Rama: feat/f4-m4.4-b2b-edge desde main.
Prerrequisito humano: DNS partners./partners-sandbox./developers. y reglas WAF B2B en el proveedor.

Lee: ADR-017 §1, §5; docs/phase-4/03 §9 (ARQ-010), 05 §6 (DAT-006), 08 (§9–10), 09 (INF-001, INF-003, INF-004, QA-004).

IDs: ARQ-010, DAT-006, SEC-009, SEC-010, INF-001, INF-003, INF-004, QA-004.

Entregables:
1. Traefik: router partners.<dominio> solo /partner/v1, entrypoint partners-mtls opcional con CA de clientes, middlewares;
   WAF con reglas B2B (ARQ-010, INF-001); sandbox y developers publicados.
2. Retención (DAT-006): jobs para imágenes, payloads, PII de pedidos; particiones de partner_calls.
3. Suite de seguridad B2B (QA-004): separación de audiencias, partner A → recurso de B = 404, firma/replay/timestamp,
   SSRF, payload builders, prompt injection en imagen; threat model F4 con revisor externo (SEC-009); límites de abuso (SEC-010).
4. Dashboards y alertas F4 (INF-003, INF-004).

DoD: suite B2B verde en CI; partners.<dominio> accesible solo con token de partner; tracking actualizado.
```

---

## M4.5 — Release

```text
Hito: F4 M4.5 Release. Rama: feat/f4-m4.5-release desde main.
Prerrequisito humano: ≥ 2 farmacias reales dispuestas al piloto; revisión legal de textos (recetas externas, pago en farmacia).

Lee: docs/phase-4/00 §5, 01 (PRD-016, PRD-022), 02 §8 (DOM-015), 04 §B.8, 09 (INF-005, QA-005, QA-006).

IDs: PRD-016, PRD-022, DOM-015, PTN-008, INF-005, QA-005, QA-006.

Entregables:
1. SLA por partner completo (DOM-015, PTN-008) y métricas del portal (PRD-016); valoración (PRD-022) si hay capacidad.
2. Runbook F4 (INF-005). E2E F4 con partner-sim y mock del proveedor de IA en CI, real en nightly (QA-005).
3. Validación con piloto y revisión legal documentadas (QA-006).
4. Criterios de salida de 00 §5 con evidencia; tag v0.4.0 tras aprobación humana.

DoD: criterios 1–6 con evidencia; tracking actualizado.
```
