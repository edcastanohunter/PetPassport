# F5 · 09 — Seguridad, infraestructura y calidad

## 1. Seguridad

**[PP-F5-SEC-001] (MUST)** Pagos: PetPassport nunca recibe ni almacena datos de tarjeta (tokenización en el dominio del proveedor; widget/redirect); alcance PCI DSS SAQ-A documentado y cuestionario completado; CSP permite solo los orígenes del proveedor en las páginas de pago; sin registro de datos de pago en logs; `last4`/marca únicamente.

**[PP-F5-SEC-002] (MUST)** Webhooks de proveedores (pagos, facturación, mensajería, helpdesk): firma verificada por adaptador (HMAC/firmas propias del proveedor), ventana temporal, idempotencia por `provider_event_id`, IP allowlist cuando el proveedor la publique, endpoints públicos con rate limit; pruebas con vectores reales de cada proveedor; secretos en secret manager con rotación.

**[PP-F5-SEC-003] (MUST)** KMS y claves: KEK en KMS del proveedor con política de acceso solo para el rol del `worker`/`api` (operaciones `Encrypt/Decrypt`, no exportación), registro de uso del KMS, rotación anual de KEK (re-wrap de DEKs), separación de backups de claves (DAT-006), alertas por uso anómalo del KMS (picos de `Decrypt`), prueba periódica de que un backup de datos sin `subject_keys` no permite recuperar PII (test de laboratorio).

**[PP-F5-SEC-004] (MUST)** Impersonación segura: tokens internos firmados (ES256, clave en KMS/secret manager, rotación), audiencia `petpassport-support`, vida ≤ 30 min, no renovables, vinculados a la sesión concedida, revocables (lista en Redis + verificación en PG), `ReadOnly` por defecto, prohibición de impersonar `super_admin`/`support_agent`, auditoría doble (`actor_id` = usuario, `impersonator_id` = agente) en cada request y en `audit_log`, visibilidad para el usuario, alertas por uso fuera de ticket.

**[PP-F5-SEC-005] (MUST)** Threat model F5: STRIDE sobre billing (manipulación de importes → calculados en agregado; cupones; webhooks falsos; discrepancias), facturación (datos fiscales falsos; suplantación de cliente), privacidad (supresión maliciosa de otro usuario → re-auth + verificación; exfiltración vía exportación → enlace de un solo uso + contraseña por canal distinto; abuso de retención), soporte (impersonación sin consentimiento; agentes maliciosos → mínimo privilegio, auditoría, revisión), mensajería (SMS bombing → rate limit/OTP; suplantación de remitente), Kubernetes (RBAC, secretos, admisión, red). Revisión con parte externa antes de M5.5.

**[PP-F5-SEC-006] (MUST)** Prueba de penetración externa anual (primera antes de `v1.0.0`) sobre app, API, Partner API y portal de farmacia; hallazgos críticos/altos resueltos antes del release; informe resumido disponible para clientes Enterprise bajo NDA.

## 2. Infraestructura

**[PP-F5-INF-001] (MUST)** Helm de producción: valores por entorno, `HPA`, `PDB`, `NetworkPolicy`, `topologySpread`, recursos, política de admisión de imágenes firmadas, cert-manager/WAF, External Secrets, `priorityClass` para api; despliegue *canary* (OPS-010) con Argo Rollouts o equivalente; IaC (Terraform/OpenTofu) para clúster, bases gestionadas, redes, KMS, buckets con *object lock*, DNS/WAF.

**[PP-F5-INF-002] (MUST)** Secretos: External Secrets desde el secret manager; sin secretos en valores Helm ni en CI en claro; rotación programada (BD trimestral automática con doble credencial, proveedores semestral, KEK anual, tokens internos mensual); auditoría de acceso al secret manager.

**[PP-F5-INF-003] (MUST)** Observabilidad F5: dashboards "Billing" (cargos por estado, pagos, fallos por proveedor y código, dunning por etapa, facturas por estado DIAN, discrepancias, MRR), "Privacy" (DSR por estado y tiempos, erasures verificadas, retención ejecutada, KMS ops), "Support" (tickets, SLA, impersonaciones activas), "Messaging" (envíos por canal/estado/coste, opt-outs), "Platform" (K8s: pods, HPA, PDB, nodos por zona, despliegues canary); métricas `billing_charges_total{status,provider}`, `billing_invoices_total{status}`, `billing_discrepancies_open`, `billing_mrr`, `dsr_requests_total{kind,status}`, `dsr_duration_seconds`, `kms_operations_total`, `pii_shredded_reads_total`, `impersonation_sessions_active`, `messaging_sent_total{channel,status}`, `messaging_cost_cents_total`.

**[PP-F5-INF-004] (MUST)** Alertas F5: `ChargeFailureRateHigh` (> 20 % 1 h o proveedor caído), `InvoiceRejected` (> 0 en 1 h), `InvoicingProviderDown`, `DiscrepanciesOpen` (> 0 > 24 h), `RenewalJobStalled`, `DSRErasureOverdue` (> 24 h tras gracia), `DSRExportOverdue` (> 72 h), `RetentionJobFailed`, `KMSErrors`, `KMSDecryptSpike`, `ImpersonationWithoutTicket`, `ImpersonationLong` (> 25 min activa), `MessagingFailureHigh`, `MessagingCostSpike`, `PodCrashLoop`, `ZoneDown`, `BackupVerificationFailed`, `ErrorBudgetBurn` (F3).

**[PP-F5-INF-005] (MUST)** Runbook F5: proveedor de pagos caído (no restringir; comunicar), rechazo masivo de facturas (contingencia DIAN), discrepancias (procedimiento de conciliación manual), reembolsos, cambio de proveedor de pagos de una cuenta, ejecución y rollback de la migración única de cifrado (ensayo + prod), erasure fallido parcialmente (reintento por contexto, informe), rotación de KEK, revocación de impersonación, incidente de datos personales (CMP-004), failover de zona, restauración completa (OPS-004), escalado manual.

## 3. Calidad

**[PP-F5-QA-001] (MUST)** Billing: pruebas de dominio con reloj simulado para ciclos mensual/anual, prorrateo (tabla de casos con importes esperados al céntimo), cambios de plan y asientos, dunning completo (d0→d60), cancelación al fin de periodo, cupones; invariantes B1–B10 nombradas; pruebas de redondeo y moneda; *property tests* de que `Σ cargos − reembolsos = Σ pagos netos` por cuenta.

**[PP-F5-QA-002] (MUST)** Proveedores: contract tests contra sandbox de cada proveedor de pagos (escenarios éxito/fallo/pendiente/webhook duplicado/tardío) y contra el entorno de habilitación DIAN del proveedor fiscal (factura aceptada, rechazada por datos, nota crédito); `fake` providers con los mismos escenarios en CI; conciliación con discrepancias inyectadas.

**[PP-F5-QA-003] (MUST)** Crypto-shredding end-to-end: usuario sintético con mascotas, encuentros, recetas, pedidos → export → erasure → verificación automática de ausencia de PII en PG (todas las tablas, búsqueda por valores conocidos), OpenSearch, Redis, storage, Keycloak y en un `ReadAll` de eventos (campos `pii` = null); historia clínica del animal intacta y hashes de encuentros válidos; ensayo de la migración única sobre copia de staging con verificación por hash; test que falla si existe una migración posterior que toque `es.events`.

**[PP-F5-QA-004] (MUST)** DR automatizado: pipeline semanal que restaura el último backup en un entorno efímero, ejecuta `rebuild --all`, compara conteos con producción (tolerancia por retraso) y mide tiempo (RTO) y última transacción (RPO); simulacro trimestral manual completo (OPS-004).

**[PP-F5-QA-005] (MUST)** E2E F5: registro de clínica → prueba → vencimiento → contratación con pago sandbox → factura (fake DIAN) → añadir asiento → límite alcanzado → upgrade; impago simulado → gracia → restricción → pago → reactivación; tutor: exportar datos → eliminar cuenta (gracia acelerada en test) → verificación; soporte: ticket → concesión → impersonación con banner → revocación; incidente simulado → banner de status.

**[PP-F5-QA-006] (MUST)** Gates F5: cobertura `billing/domain` ≥ 95 % (dinero); contract tests de proveedores verdes en staging; E2E; test de crypto-shredding; DR automatizado verde 4 semanas consecutivas; pentest sin críticos/altos abiertos; `specs-status.sh --check`; auditoría interna de controles (CMP-010) con hallazgos mayores cerrados.
