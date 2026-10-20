# Prompts de ejecución — Fase 5 (Operación SaaS y Monetización)

Prerrequisitos: Fase 4 cerrada; decisiones de docs/handoff/README.md §4 (entidad legal, proveedores de pago y facturación,
precios, helpdesk). M5.0 puede comenzar durante F4. System prompt: `00-agent-operating-instructions.md`.

---

## M5.0 — Plataforma y continuidad

```text
Hito: F5 M5.0. Rama: feat/f5-m5.0-platform-continuity desde main (cambios mayormente en infra/).
Prerrequisito humano: clúster Kubernetes gestionado y bases multi-AZ aprovisionados (IaC en este mismo hito).

Lee: ADR-024; docs/phase-5/01 (PRD-015), 03 §12 (ARQ-012), 06 (FE-009), 07 (§1–4, §7, §10), 09 (INF-001, INF-002, QA-004).

IDs: PRD-015, ARQ-012, FE-009, OPS-001, OPS-002, OPS-003, OPS-004, OPS-007, OPS-010, INF-001, INF-002, QA-004 (prefijo PP-F5-).

Entregables:
1. IaC (Terraform/OpenTofu) para clúster, bases gestionadas multi-AZ, redes, KMS, buckets con object lock, DNS/WAF (INF-001).
2. Helm de producción con HPA, PDB, NetworkPolicy, topologySpread, admisión de imágenes firmadas, External Secrets,
   canary (Argo Rollouts o equivalente) y feature flags (OPS-010, INF-001, INF-002 con rotación programada).
3. Migración blue/green VM → Kubernetes en staging primero y luego prod con réplica lógica y plan de corte (OPS-001);
   datos multi-AZ y pooler (OPS-002).
4. Backups con copia cross-region, verificación semanal automatizada en entorno efímero midiendo RPO/RTO (OPS-003, QA-004);
   runbooks DR por escenario y primer simulacro completo registrado (OPS-004).
5. Status page externa con checks, status.json y banner en la app (OPS-007, ARQ-012, PRD-015, FE-009).

DoD: producción en Kubernetes con SLOs cumplidos 7 días; simulacro con RPO ≤ 5 min y RTO ≤ 1 h registrado; tracking actualizado.
```

---

## M5.1 — Billing core

```text
Hito: F5 M5.1 Billing core. Rama: feat/f5-m5.1-billing-core desde main. Dinero: máxima atención y cobertura ≥ 95 % en domain.
Prerrequisito humano: estructura de planes aprobada (precios pueden seguir como draft).

Lee: ADR-022, ADR-025; docs/phase-5/01 (E25 salvo PRD-008/009), 02 (§1–3.4, §3.6, §6, §7), 03 (§1, §4, §5), 04 (§1–3, §6, §12),
05 (§1–2), 06 (FE-001, FE-002, FE-004), 09 (QA-001, QA-006).

IDs: PRD-001, PRD-002, PRD-003, PRD-007, PRD-010, DOM-001, DOM-002, DOM-003, DOM-004, DOM-005, DOM-006, DOM-008, DOM-011,
ARQ-001, ARQ-004, ARQ-005, API-001, API-002, API-003, API-006, API-012, DAT-001, DAT-002, FE-001, FE-002, FE-004, QA-001, QA-006.

Entregables:
1. billing/domain: BillingAccount y Subscription (B1–B10 con pruebas, reloj simulado, tabla de prorrateo al céntimo,
   property test Σ cargos − reembolsos = Σ pagos), planes versionados con semillas CO en draft (DAT-002), VOs, eventos v1.
2. Entitlements: read model, caché, invalidación, middleware EnforceEntitlements en el command bus con Consumes() por comando
   y lint de cobertura (ARQ-004); usage_counters por eventos con usage_applied (ARQ-005); EntitlementSet por defecto para
   orgs sin cuenta.
3. Proveedores `fake` de pago y facturación con escenarios; routing por país (los reales llegan en M5.2).
4. Endpoints planes, cuenta, suscripción (preview/contratar/cambiar/cancelar), uso y entitlements; errores F5.
5. Frontend: pricing desde catálogo (FE-001), prueba/vencimiento/contratación (FE-002), límites en contexto (FE-004).
6. Gates F5 en CI (QA-006).

DoD: prueba → vencimiento → contratación (fake) → límite alcanzado → upgrade demostrables; B1–B10 probadas; cobertura
billing/domain ≥ 95 %; tracking actualizado.
```

---

## M5.2 — Facturación y cobro real

```text
Hito: F5 M5.2. Rama: feat/f5-m5.2-payments-invoicing desde main.
Prerrequisitos humanos: cuentas sandbox en el proveedor de pagos (Wompi/Mercado Pago/Stripe según ADR-022) y en el proveedor
fiscal (entorno de habilitación DIAN); contador define IVA/retenciones; asesor legal revisa textos (CMP-008).

Lee: ADR-022; docs/phase-5/01 (E26, PRD-008, PRD-009, PRD-020, PRD-022), 02 (§3.5, §7), 03 (§2–3, §6), 04 (§4–5, §7), 05 §1,
06 (FE-003, FE-005), 08 (CMP-008, CMP-009), 09 (SEC-001, SEC-002, INF-003, INF-004, QA-002).

IDs: PRD-004, PRD-005, PRD-006, PRD-008, PRD-009, PRD-011, PRD-020, PRD-022, DOM-007, DOM-012, ARQ-002, ARQ-003, ARQ-006,
API-004, API-005, API-007, FE-003, FE-005, CMP-008, CMP-009, SEC-001, SEC-002, INF-003, INF-004, QA-002.

Entregables:
1. Adaptadores de pago reales (al menos uno para CO + manual) con tokenización en el proveedor, cobro idempotente,
   webhooks firmados e idempotentes (ARQ-002, SEC-001, SEC-002); conciliación diaria con discrepancias.
2. Adaptador de facturación fiscal (ARQ-003): emisión, estados, contingencia, notas crédito, archivo con object lock;
   reglas fiscales (CMP-009); conciliación contable exportable.
3. Dunning completo con scheduler y categoría `billing` (ARQ-006); PMs de M5 (DOM-012).
4. Cuentas multi-organización (PRD-008) y Enterprise manual (PRD-009); administración de facturación (API-007, FE-005);
   cupones si hay capacidad (PRD-022).
5. Frontend de facturación (FE-003); textos legales por plan y país con re-aceptación (PRD-020, CMP-008).
6. Contract tests contra sandbox de proveedores (QA-002); dashboards y alertas Billing (INF-003, INF-004).

DoD: cobro sandbox → factura aceptada en entorno de habilitación → impago simulado → gracia → restricción → pago → reactivación
demostrables; 0 discrepancias en 7 días de staging; tracking actualizado.
```

---

## M5.3 — Privacidad y cumplimiento

```text
Hito: F5 M5.3. Rama: feat/f5-m5.3-privacy desde main. Puede ir en paralelo con M5.4. Incluye la operación más delicada del
roadmap (migración única de es.events): ensáyala sobre copia de producción antes de proponerla.

Lee: ADR-002, ADR-023 (completo); docs/phase-5/01 (E27 PRD-012/013), 02 (§4), 03 (§7–9), 04 §8, 05 (§3, §6), 06 (FE-006),
08 (CMP-001..005, 007, 011), 09 (SEC-003, QA-003).

IDs: PRD-012, PRD-013, DOM-009, ARQ-007, ARQ-008, ARQ-009, API-008, DAT-003, DAT-006, FE-006, CMP-001, CMP-002, CMP-003,
CMP-004, CMP-005, CMP-007, CMP-011, SEC-003, QA-003.

Entregables:
1. platform/kms y privacy.subject_keys con backup separado (SEC-003, DAT-003, DAT-006); DEK por sujeto en JIT.
2. Registry cifrado: PIIFields/PIISubject por evento, cifrado al serializar, null si shredded, upcasters sobre payload
   descifrado; content_hash sobre payload lógico (ARQ-007).
3. `cmd/migrate privacy-encrypt-history`: ensayo en staging con copia de prod, verificación por hash, informe en
   docs/phase-5/compliance/encrypt-history-run.md; test que falla si existe otra migración que toque es.events.
   La ejecución en producción requiere aprobación humana explícita en el PR.
4. PIIEraser por contexto (petregistry, partnernetwork, clinical, scheduling, prescriptions, pharmacyorders, billing,
   notifications, search, storage, cache, keycloak) con informe; DSR export/erasure con re-auth, gracia, legal holds (ARQ-008,
   DOM-009, API-008); retención automática con reglas y runs (ARQ-009, CMP-003).
5. Frontend privacidad (FE-006): exportar, eliminar, accesos.
6. Cumplimiento documental: registro de tratamientos (CMP-001, tabla + export), DPIA (CMP-002), procedimientos de derechos y
   notificación de incidentes (CMP-004), inventario de encargados (CMP-005), auditoría exportable con hash encadenado (CMP-007),
   consentimiento de telemetría (CMP-011).
7. Test E2E de crypto-shredding (QA-003): usuario sintético completo → export → erasure → verificación de ausencia de PII
   en PG/OpenSearch/Redis/storage/Keycloak y eventos; historia del animal y hashes intactos.

DoD: QA-003 verde en staging; migración ensayada con informe; documentos CMP publicados; tracking actualizado.
```

---

## M5.4 — Soporte, canales y operación

```text
Hito: F5 M5.4. Rama: feat/f5-m5.4-support-channels-ops desde main. Puede ir en paralelo con M5.3. Líneas repartibles:
A soporte e impersonación; B canales SMS/WhatsApp; C on-call/incidentes/SLA; D métricas de negocio.
Prerrequisitos humanos: helpdesk interno vs externo; proveedor de mensajería; herramienta de guardias; plantillas WhatsApp aprobadas.

Lee: docs/phase-5/01 (E28, E29), 02 (§5, §8), 03 (§10–11, §13), 04 (§9–11), 05 (§4–5, §7), 06 (FE-007, FE-008, FE-010, FE-011),
07 (§5–6, §8), 08 (CMP-006), 09 (SEC-004).

IDs: PRD-014, PRD-016, PRD-017, PRD-018, PRD-019, DOM-010, DOM-013, ARQ-010, ARQ-011, ARQ-013, API-009, API-010, API-011,
DAT-004, DAT-005, DAT-007, FE-007, FE-008, FE-010, FE-011, OPS-005, OPS-006, OPS-008, CMP-006, SEC-004.

Entregables (por línea):
A. Tickets con SLA por plan y centro de ayuda estático (DOM-010, ARQ-011, API-009, DAT-004, FE-007); impersonación con token
   interno, banner, auditoría doble y revocación (SEC-004, FE-008); revisión de accesos trimestral y offboarding (CMP-006).
B. Verificación de teléfono por OTP, canales sms/whatsapp en Notifications con proveedor, plantillas, inbound STOP, costes
   a cuotas (DOM-013, ARQ-010, API-010, DAT-005, FE-010).
C. On-call con escalado y runbooks enlazados (OPS-005), proceso de incidentes y plantillas de postmortem (OPS-006), SLA
   medido con créditos automáticos y política de error budget (OPS-008, PRD-016).
D. Proyecciones de métricas de negocio y soporte con definiciones en docs/phase-5/metrics.md (ARQ-013, DAT-007, API-011,
   FE-011, PRD-019).

DoD: cada línea demostrable; un incidente simulado con postmortem completo; tracking actualizado por línea.
```

---

## M5.5 — Release v1.0.0

```text
Hito: F5 M5.5 Release. Rama: feat/f5-m5.5-release desde main.
Prerrequisitos humanos: pentest externo contratado; auditoría interna de controles programada.

Lee: docs/phase-5/00 §5, 01 (PRD-021), 03 §14 (ARQ-014), 07 (§9, §11), 08 (CMP-010), 09 (SEC-005, SEC-006, INF-005, QA-005).

IDs: PRD-021, ARQ-014, OPS-009, OPS-011, CMP-010, SEC-005, SEC-006, INF-005, QA-005.

Entregables:
1. Revisiones pendientes de fases anteriores con decisión registrada en tracking (ARQ-014).
2. Capacidad y FinOps con panel de costes y métricas unitarias (OPS-009); primer game day semestral (OPS-011).
3. Marco de controles con evidencias y auditoría interna (CMP-010); threat model F5 con revisor externo (SEC-005); pentest
   con hallazgos críticos/altos cerrados (SEC-006); runbook F5 (INF-005).
4. E2E F5 completo (QA-005). Plan Tutor Plus (PRD-021) solo con decisión de negocio basada en datos; si no, DESCARTADO con nota.
5. Criterios de salida de 00 §5 con evidencia; tag v1.0.0 (salida de beta) tras aprobación humana.

DoD: criterios 1–7 con evidencia; tracking de las cinco fases sin MUST pendientes; tracking actualizado.
```
