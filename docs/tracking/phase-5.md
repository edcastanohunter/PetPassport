# Tracking — Fase 5: Operación SaaS y Monetización

> Fuente de verdad del estado. Editar el estado **solo aquí**. Validar con `scripts/specs-status.sh --check`.
> Estados: `PENDIENTE` `EN_DISENO` `EN_PROGRESO` `IMPLEMENTADO` `VERIFICADO` `BLOQUEADO` `DESCARTADO`.

| ID | Área | Requisito | Prio | Hito | Estado | Issue | PR | Notas |
|---|---|---|---|---|---|---|---|---|
| PP-F5-PRD-001 | PRD | Página pública de planes y precios desde catálogo | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-PRD-002 | PRD | Prueba de 30 días y vencimiento | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-PRD-003 | PRD | Contratar, cambiar plan y asientos con prorrateo | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-PRD-004 | PRD | Métodos de pago y cobro recurrente | MUST | M5.2 | EN_PROGRESO | | #38 | Tokenización en el proveedor (widget en /org/:orgId/settings/billing), cobro recurrente y reintentos. Falta PSE y verificar Wompi contra su sandbox |
| PP-F5-PRD-005 | PRD | Facturas electrónicas válidas | MUST | M5.2 | EN_PROGRESO | | #38 | Emisión, estados, contingencia pdf_local y notas crédito con adaptador fake. Falta el adaptador DIAN (proveedor sin decidir, ADR-022 §4) |
| PP-F5-PRD-006 | PRD | Dunning con gracia y restricción | MUST | M5.2 | IMPLEMENTADO | | #38 | Reintentos día 0/3/7/12, gracia 14 días, restricción, cancelación a 60 días, emails de la categoría billing (no desactivable) y banner persistente para administradores |
| PP-F5-PRD-007 | PRD | Límites de plan comprensibles y upgrade en contexto | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-PRD-008 | PRD | Cuenta de facturación multi-organización (cadenas) | SHOULD | M5.2 | IMPLEMENTADO | | #38 | Invitación por código público, aceptación del admin de la organización invitada (vincula en la misma transacción), rechazo/revocación y retiro (nunca la última). Sin traslado entre cuentas ni email de invitación (ver decisiones del PR) |
| PP-F5-PRD-009 | PRD | Plan Enterprise con factura manual | SHOULD | M5.2 | IMPLEMENTADO | | #38 | Facturación manual sin dunning y pago manual de SuperAdmin (API). La pantalla va en FE-005 |
| PP-F5-PRD-010 | PRD | Panel de uso de la organización | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-PRD-011 | PRD | Administración de suscripciones, ajustes y reembolsos | MUST | M5.2 | IMPLEMENTADO | | #38 | API y consola de SuperAdmin con motivo y audit log: ajustes (incluido change_plan), pago manual, reembolso, cancelación |
| PP-F5-PRD-012 | PRD | Exportación de datos (tutor y organización) | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-PRD-013 | PRD | Eliminación de cuenta con gracia y excepciones | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-PRD-014 | PRD | Centro de ayuda y tickets con SLA | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-PRD-015 | PRD | Status page y banner de incidentes | MUST | M5.0 | IMPLEMENTADO | | | Status page externa, status.json y StatusBanner en app con enlace |
| PP-F5-PRD-016 | PRD | SLA por plan y créditos | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-PRD-017 | PRD | SMS/WhatsApp con verificación de teléfono | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-PRD-018 | PRD | Impersonación de soporte con consentimiento | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-PRD-019 | PRD | Métricas de negocio | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-PRD-020 | PRD | Textos legales por plan y país | MUST | M5.2 | PENDIENTE | |  | Requiere textos revisados por legal (prerrequisito humano del hito) |
| PP-F5-PRD-021 | PRD | Plan Tutor Plus | COULD | M5.5 | PENDIENTE | | | |
| PP-F5-PRD-022 | PRD | Cupones y descuentos | COULD | M5.2 | IMPLEMENTADO | | #38 | Catálogo con canje atómico y límite de usos, GET/POST /admin/coupons, cupón en checkout y pestaña de cupones en /admin/billing |
| PP-F5-DOM-001 | DOM | Glosario F5 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-002 | DOM | Context map F5 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-003 | DOM | Agregado BillingAccount | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-004 | DOM | Agregado Subscription: comandos e invariantes B1–B10 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-005 | DOM | Planes versionados y Value Objects | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-006 | DOM | Eventos Billing v1 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-007 | DOM | Facturas, pagos y conciliación | MUST | M5.2 | IMPLEMENTADO | | #38 | Libro de pagos, intentos, facturas, notas crédito y conciliación diaria; probado contra Postgres |
| PP-F5-DOM-008 | DOM | Uso y entitlements (read models) | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-009 | DOM | Privacy: SubjectKey, DataSubjectRequest, PIIEraser | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-DOM-010 | DOM | Support: tickets e impersonación | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-DOM-011 | DOM | Puertos F5 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DOM-012 | DOM | Process managers y políticas F5 | MUST | M5.2 | IMPLEMENTADO | | #38 | billing.charge-collector, billing.dunning, billing.timers y billing.invoicing en internal/billing/pm |
| PP-F5-DOM-013 | DOM | Canal SMS/WhatsApp: verificación, plantillas, costes | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-ARQ-001 | ARQ | Módulos nuevos y dependencias | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-ARQ-002 | ARQ | Integración de pagos: tokenización, cobro, webhooks, conciliación | MUST | M5.2 | IMPLEMENTADO | | #38 | Adaptadores wompi, manual y fake. Wompi solo probado contra servidor simulado: 3 puntos SIN VERIFICAR (ver QA-002) |
| PP-F5-ARQ-003 | ARQ | Facturación electrónica: emisión, estados, contingencia, notas crédito | MUST | M5.2 | EN_PROGRESO | | #38 | Puerto, fake y contingencia pdf_local. Falta el adaptador DIAN, compilar la plantilla Typst y la retención de 10 años en object storage |
| PP-F5-ARQ-004 | ARQ | Entitlements en el command bus | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-ARQ-005 | ARQ | Medición de uso por eventos | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-ARQ-006 | ARQ | Dunning con scheduler | MUST | M5.2 | IMPLEMENTADO | | #38 | Dunning sobre el scheduler de comandos de Postgres (no asynq): ver decisiones del PR |
| PP-F5-ARQ-007 | ARQ | Crypto-shredding: KMS, Registry cifrado, migración única, erasure | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-ARQ-008 | ARQ | DSR: exportación y verificación | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-ARQ-009 | ARQ | Retención automática | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-ARQ-010 | ARQ | SMS y WhatsApp: proveedor, plantillas, OTP, inbound | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-ARQ-011 | ARQ | Soporte: tickets/helpdesk e impersonación con token interno | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-ARQ-012 | ARQ | Status page y comunicaciones | MUST | M5.0 | IMPLEMENTADO | | | Proveedor externo independiente, checks regionales e integración SPA |
| PP-F5-ARQ-013 | ARQ | Métricas de negocio (proyección y definiciones) | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-ARQ-014 | ARQ | Revisiones pendientes de fases anteriores | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-API-001 | API | Planes (público y admin) | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-API-002 | API | Cuenta de facturación | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-API-003 | API | Suscripción: previsualizar, contratar, cambiar, cancelar | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-API-004 | API | Métodos de pago y webhooks de pago | MUST | M5.2 | IMPLEMENTADO | | #38 | setup, attach, DELETE (evento PaymentMethodRemoved, ver decisiones del PR) y webhook firmado |
| PP-F5-API-005 | API | Facturas y pagos | MUST | M5.2 | IMPLEMENTADO | | #38 | Listas con limit en lugar de cursor (desviación anotada en el PR) |
| PP-F5-API-006 | API | Uso y entitlements | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-API-007 | API | Administración de facturación | MUST | M5.2 | IMPLEMENTADO | | #38 | Cuentas con MRR, detalle con pagos, facturas, discrepancias y auditoría, ajustes con change_plan, pago manual, reembolso, cancelación, discrepancias, reemisión y cupones |
| PP-F5-API-008 | API | Derechos del titular y accesos de soporte | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-API-009 | API | Soporte: tickets, agentes, impersonación | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-API-010 | API | Teléfono y canales | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-API-011 | API | Métricas de negocio | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-API-012 | API | Catálogo de errores F5 | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DAT-001 | DAT | DDL billing | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DAT-002 | DAT | Planes y reglas fiscales con semillas CO | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-DAT-003 | DAT | DDL privacy | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-DAT-004 | DAT | DDL support | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-DAT-005 | DAT | DDL messaging y canales | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-DAT-006 | DAT | Retención, object lock y backup separado de claves | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-DAT-007 | DAT | Métricas diarias de negocio y soporte | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-FE-001 | FE | Pricing público | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-FE-002 | FE | Prueba, vencimiento y contratación | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-FE-003 | FE | Ajustes de facturación | MUST | M5.2 | EN_PROGRESO | | #38 | Resumen, cambio de plan con vista previa, método de pago con widget del proveedor, datos fiscales, facturas, pagos, cancelar y reactivar, banner. Falta PSE y verificar el modo tokenización del widget de Wompi (SIN VERIFICAR) |
| PP-F5-FE-004 | FE | Límites en contexto e indicadores de uso | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-FE-005 | FE | Admin de facturación y planes | MUST | M5.2 | IMPLEMENTADO | | #38 | Cuentas con filtros y MRR, detalle con auditoría, ajustes con motivo, facturas con reemisión, discrepancias, cupones y versiones de plan (crear, publicar con fecha, retirar) |
| PP-F5-FE-006 | FE | Privacidad: exportar, eliminar, accesos | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-FE-007 | FE | Centro de ayuda y tickets | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-FE-008 | FE | Impersonación: concesión y banner | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-FE-009 | FE | Estado del servicio en la app | MUST | M5.0 | IMPLEMENTADO | | | Banner global, /status embebida y detección de 5xx/503 persistentes |
| PP-F5-FE-010 | FE | Teléfono y canales | SHOULD | M5.4 | PENDIENTE | | | |
| PP-F5-FE-011 | FE | Métricas de negocio | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-OPS-001 | OPS | Migración a Kubernetes multi-zona | MUST | M5.0 | IMPLEMENTADO | | | Clúster EKS gestionado, nodepools general/jobs, plan de corte blue/green |
| PP-F5-OPS-002 | OPS | Datos multi-AZ y pooler | MUST | M5.0 | IMPLEMENTADO | | | PostgreSQL multi-AZ con failover < 60s, PgBouncer, Redis y OpenSearch multi-AZ |
| PP-F5-OPS-003 | OPS | Backups con copia cross-region y verificación | MUST | M5.0 | IMPLEMENTADO | | | S3 cross-region, retención 30d, backup separado de claves y verificación |
| PP-F5-OPS-004 | OPS | Runbooks DR y simulacros con RTO/RPO medidos | MUST | M5.0 | IMPLEMENTADO | | | Runbooks 5 escenarios en DR-RUNBOOKS.md y simulacro registrado en RESTORE-LOG.md |
| PP-F5-OPS-005 | OPS | On-call y escalado | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-OPS-006 | OPS | Gestión de incidentes y postmortems | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-OPS-007 | OPS | Status page externa | MUST | M5.0 | IMPLEMENTADO | | | Configuración externa, checks cada 1m desde 3 regiones e historial 90d |
| PP-F5-OPS-008 | OPS | SLA, medición y política de error budget | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-OPS-009 | OPS | Capacidad y FinOps | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-OPS-010 | OPS | Gestión de cambios: canary, flags, IaC, congelaciones | MUST | M5.0 | IMPLEMENTADO | | | Canary Argo Rollouts (10% 15m), tabla platform.feature_flags y motor de flags |
| PP-F5-OPS-011 | OPS | Game days semestrales | SHOULD | M5.5 | PENDIENTE | | | |
| PP-F5-CMP-001 | CMP | Registro de actividades de tratamiento | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-002 | CMP | DPIA de tratamientos de riesgo | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-003 | CMP | Política de retención aprobada y automatizada | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-004 | CMP | Derechos del titular y notificación de incidentes | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-005 | CMP | Encargados, DPAs y transferencias | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-006 | CMP | Revisión de accesos trimestral y offboarding | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-CMP-007 | CMP | Auditoría exportable con integridad | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-CMP-008 | CMP | Textos legales y roles de tratamiento | MUST | M5.2 | PENDIENTE | |  | Requiere textos revisados por legal (prerrequisito humano del hito) |
| PP-F5-CMP-009 | CMP | Cumplimiento fiscal DIAN y conciliación contable | MUST | M5.2 | EN_PROGRESO | | #38 | Conciliación diaria con discrepancias y notas crédito. Falta el adaptador DIAN y las reglas de IVA/retenciones del contador |
| PP-F5-CMP-010 | CMP | Marco de controles y auditoría interna | SHOULD | M5.5 | PENDIENTE | | | |
| PP-F5-CMP-011 | CMP | Consentimiento de telemetría y cookies | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-SEC-001 | SEC | Pagos: tokenización, alcance PCI SAQ-A | MUST | M5.2 | EN_PROGRESO | | #38 | Sin datos de tarjeta en el backend ni en logs (solo token, last4, vencimiento); CSP del widget solo en la página de facturación. Falta el documento SAQ-A |
| PP-F5-SEC-002 | SEC | Webhooks de proveedores firmados e idempotentes | MUST | M5.2 | IMPLEMENTADO | | #38 | Webhook de pagos: firma, ventana temporal, idempotencia por provider_event_id y contraste con el proveedor |
| PP-F5-SEC-003 | SEC | KMS y gestión de claves | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-SEC-004 | SEC | Impersonación segura | MUST | M5.4 | PENDIENTE | | | |
| PP-F5-SEC-005 | SEC | Threat model F5 | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-SEC-006 | SEC | Pentest externo anual | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-INF-001 | INF | Helm de producción, canary e IaC | MUST | M5.0 | IMPLEMENTADO | | | Terraform staging/prod multi-AZ, Helm prod con HPA, PDB, NetworkPolicy y admisión |
| PP-F5-INF-002 | INF | Secretos y rotación programada | MUST | M5.0 | IMPLEMENTADO | | | External Secrets Operator y runbook de rotación dual-user programada |
| PP-F5-INF-003 | INF | Observabilidad F5 | MUST | M5.2 | EN_PROGRESO | | #38 | Dashboard Billing y métricas billing_*. Privacy, Support y Messaging llegan con M5.3 y M5.4 |
| PP-F5-INF-004 | INF | Alertas F5 | MUST | M5.2 | EN_PROGRESO | | #38 | Alertas de billing con pruebas promtool y runbook. Las de DSR, KMS, soporte y mensajería llegan con M5.3 y M5.4 |
| PP-F5-INF-005 | INF | Runbook F5 | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-QA-001 | QA | Pruebas de billing con reloj simulado y property tests | MUST | M5.1 | IMPLEMENTADO | | | |
| PP-F5-QA-002 | QA | Contract tests de proveedores de pago y facturación | MUST | M5.2 | EN_PROGRESO | | #38 | Pruebas del adaptador Wompi contra servidor simulado e integración con fakes sobre Postgres. Faltan las pruebas contra los sandbox reales |
| PP-F5-QA-003 | QA | Crypto-shredding end-to-end y ensayo de migración | MUST | M5.3 | PENDIENTE | | | |
| PP-F5-QA-004 | QA | DR automatizado semanal | MUST | M5.0 | IMPLEMENTADO | | | Pipeline semanal dr-verify.sh y test de umbrales RPO <= 5m y RTO <= 1h |
| PP-F5-QA-005 | QA | E2E F5 | MUST | M5.5 | PENDIENTE | | | |
| PP-F5-QA-006 | QA | Gates F5 | MUST | M5.1 | IMPLEMENTADO | | | |
