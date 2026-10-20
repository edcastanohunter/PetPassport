# F5 · 03 — Arquitectura: pagos, facturación, entitlements, privacidad, canales y soporte

Marco: ADR-022 (billing), ADR-023 (crypto-shredding), ADR-024 (Kubernetes/DR, detalle en 07), ADR-025 (entitlements).

## 1. Módulos nuevos

**[PP-F5-ARQ-001] (MUST)**

```
internal/
  billing/{domain,app,infra,http}        # BillingAccount, Subscription, planes, cargos, entitlements, usage, conciliación
  billing/providers/{wompi,mercadopago,stripe,manual,fake}     # PaymentProvider
  billing/invoicing/{dian_<prov>,pdf_local,fake}               # InvoicingProvider
  privacy/{app,infra}                    # SubjectKeys, DSR, erasers registry, export
  support/{app,infra,http}               # tickets (interno o adaptador), impersonación
  messaging/{twilio,meta,fake}           # MessagingProvider para Notifications
  platform/kms/                          # envelope encryption
  platform/entitlements/                 # cliente del puerto + caché + middleware de comandos
```

Reglas: ningún contexto importa `billing` salvo por el puerto `Entitlements` de `shared/kernel`; `privacy` importa solo puertos `PIIEraser` registrados por cada módulo en el arranque; `billing/providers/*` no conocen el dominio (reciben DTOs).

## 2. Integración de pagos

**[PP-F5-ARQ-002] (MUST)**

- Flujo de alta de método de pago: la SPA abre el *widget* del proveedor (tokenización en su dominio; PetPassport nunca recibe PAN) → token → `SetPaymentMethod`. Para PSE/transferencia: *redirect* al proveedor y confirmación por webhook.
- Cobro: asynq `billing:charge{charge_id}` (único por `charge_id`) → `PaymentProvider.Charge(idempotency_key = charge_id)` → resultado síncrono o pendiente → webhook `payment.updated` → `RecordChargeResult`. Webhooks: firma verificada por adaptador, idempotencia por `provider_event_id` (`billing.provider_events`), reintento con `409` si el agregado no está listo (orden de llegada).
- Conciliación diaria `billing:reconcile`: compara cargos/pagos de los últimos 35 días con el proveedor (`GetPayment`/listado) → `billing.discrepancies`; alerta; bloquea `Restrict` sobre cuentas con discrepancia.
- Selección de proveedor por país de la cuenta (`billing.provider_routing`): CO → `wompi` (por defecto) o `mercadopago`; resto → `stripe` si la entidad lo permite; `manual` siempre disponible para Enterprise.
- Sandbox de cada proveedor en staging con tarjetas/escenarios de prueba; `fake` en dev/CI con escenarios (éxito, fallo, pendiente, webhook tardío).

## 3. Facturación electrónica

**[PP-F5-ARQ-003] (MUST)**

- Tras `ChargePaid` (o `ChargeScheduled` en manual): asynq `billing:invoice{charge_id}` → construye documento (líneas, impuestos por regla del país, datos fiscales de la cuenta, numeración del proveedor) → `InvoicingProvider.Issue` → `Invoice{issued}` → consulta de estado hasta `accepted` (DIAN) o `rejected` (motivo) → reintento con corrección automática si es formato, alerta si es datos fiscales (se pide al cliente corregir; la suscripción no se restringe por ello).
- Contingencia: si el proveedor fiscal está caído > 24 h, se emite factura "pendiente de validación" (PDF propio) y se reintenta; registro de contingencia para la autoridad si la norma lo exige.
- Notas crédito por reembolsos y anulaciones; numeración y PDF/XML archivados 10 años (object storage con *object lock*).
- Impuestos: tabla `ref.tax_rules{country, rate, applies_to}` (CO: IVA 19 % sobre servicios digitales; exenciones según régimen del cliente, campo en `LegalEntity`).

## 4. Entitlements en el camino de comandos

**[PP-F5-ARQ-004] (MUST)**

- `platform/entitlements.Client.For(orgID)`: caché L1 (5 s) + Redis (60 s) + read model `billing.entitlements`; invalidación por eventos (PM `billing.entitlements-invalidator`).
- Declaración por comando: `type RequestQuotes struct { … } ; func (RequestQuotes) Consumes() entitlements.Requirement { return Quota("quotes_per_month", 1) }` (o `Feature("partner_api")`, `Seat()`); el command bus aplica el *middleware* `EnforceEntitlements` antes del handler: carga `EntitlementSet` de la org del comando, evalúa (con el 10 % blando para cuotas), y rechaza con `PLAN_LIMIT_REACHED{quota, limit, used, period_end, upgrade_url}` o `SUBSCRIPTION_RESTRICTED`.
- Las consultas no pasan por el middleware (RN-32). Los comandos de Billing y de Identity están exentos (hay que poder pagar).
- Tests: tabla de comandos × planes; prueba de que todo comando que consume límite declara `Consumes()` (lint por reflexión sobre el registro del bus).

## 5. Medición de uso

**[PP-F5-ARQ-005] (MUST)** Proyección asíncrona `billing.usage_counters` (ADR-009) con `UPSERT … used = used + delta` idempotente por `(projection, event_id)` (tabla de eventos aplicados por periodo para evitar doble conteo en rebuild: el rebuild trunca y recuenta). Periodos alineados al ciclo de la suscripción (no al mes natural) salvo orgs sin suscripción (mes natural). Exposición en `GET /organizations/{id}/usage`.

## 6. Dunning

**[PP-F5-ARQ-006] (MUST)** Máquina de estados en el agregado + scheduler de comandos (F2): `ChargeFailed` → `EnterGrace` + jobs `RetryCharge` d+3, d+7, d+12 y `Restrict` d+14, `Cancel` d+60; cada job verifica que el cargo siga `failed` y que no haya discrepancia; emails con plantillas `billing_*` (categoría `account`, no desactivable salvo `security`→ aquí sí: `billing` es categoría nueva no desactivable); pago manual o cambio de método dispara `RetryCharge` inmediato.

## 7. Crypto-shredding

**[PP-F5-ARQ-007] (MUST)**

- `platform/kms`: `EncryptDEK/DecryptDEK` con KEK en KMS cloud (`kek_id` en config; rotación anual → re-cifra DEKs en lote). DEK por sujeto generada en `EnsureUserAccount` (F1 JIT) si no existe; caché de DEK descifradas en memoria por proceso (TTL 5 min, LRU 10k) para no llamar al KMS por evento.
- `eventstore.Registry`: cada tipo de evento declara `PIIFields() []FieldPath` y `PIISubject(payload) UserID`; al serializar, los campos se sustituyen por `{"$enc":"v1","kid":<kek>,"iv":…,"ct":…}`; al deserializar se descifran si la DEK está activa; si está `shredded` → `null` (+ métrica `pii_shredded_reads_total`). Upcasters operan sobre el payload ya descifrado.
- Migración única `privacy-encrypt-history` (ADR-023): ensayo en staging con copia de producción; en prod: backup + verificación → `ALTER TABLE es.events DISABLE TRIGGER events_append_only` dentro de la ventana → lotes de 10k por `global_position` con `UPDATE … SET payload = jsonb_set(...)` solo en eventos con PII y sujeto con DEK → verificación hash por lote → `ENABLE TRIGGER` → prueba de integridad (`ReadAll` + hash de encuentros firmados sigue válido porque el hash se calcula sobre el payload **descifrado** canónico: regla fijada ahora para F2 ARQ-010: el `content_hash` se calcula sobre el payload lógico, no sobre el cifrado). Registro en `audit_log` y en `docs/phase-5/compliance/encrypt-history-run.md`.
- Erasure: `privacy:erase` → `SubjectKeys.shred` (estado `shredding`, borrado efectivo del KMS a los 7 días) → `PIIEraser` de cada contexto (idempotentes, con informe por contexto) → OpenSearch `update_by_query` → caché `DEL` → storage (fotos de perfil, documentos de credencial, imágenes de recetas sin pedido) → Keycloak `disable` + `delete` → `completed`. Verificación automática post-erasure: consulta de PII conocida (email, teléfono, nombre) en todas las tablas y en el índice → 0 resultados (test E2E en staging con usuario sintético).

## 8. Derechos del titular (DSR)

**[PP-F5-ARQ-008] (MUST)** Flujo de exportación: `RequestExport` → verificación (re-auth ≤ 10 min, `acr`/`auth_time` del token) → asynq `privacy:export` (cola `low`) → recopila por contexto (`DataExporter` por módulo: cuenta, mascotas, timeline, carnet, citas, recetas, pedidos, notificaciones, auditoría de accesos) → JSON por contexto + PDFs existentes → ZIP cifrado con contraseña enviada por canal distinto (SMS si verificado, si no, mostrada una vez en la app) → storage con caducidad 7 días → email con enlace de un solo uso → `completed`. Organizaciones: `OrganizationDataExporter` (admin), limitado a datos propios y custodia (ADR-010).

## 9. Retención automática

**[PP-F5-ARQ-009] (MUST)** Job diario `privacy:retention` que ejecuta las reglas de `privacy.retention_rules{data_class, action anonymize|delete, after, legal_basis}` sobre: imágenes de recetas (F4 DAT-006), PII de pedidos (180 d), `deliveries` (180 d), `webhook_deliveries` payloads (7 d), `partner_calls` (12 m), `audit_log` (2 años salvo seguridad: 5), exportaciones DSR (7 d), fotos de mascotas reemplazadas (30 d), sesiones de impersonación (2 años), facturas (10 años, *object lock*). Cada ejecución produce un informe (`privacy.retention_runs`).

## 10. SMS y WhatsApp

**[PP-F5-ARQ-010] (SHOULD)** `MessagingProvider` con adaptadores Twilio (SMS/WhatsApp) y Meta Cloud API (WhatsApp); plantillas de WhatsApp registradas y aprobadas por proveedor (tabla `messaging.templates`); envío por asynq `notify:sms`/`notify:whatsapp` con dedup; *inbound* (STOP, respuestas) por webhook firmado; costes por mensaje desde el proveedor → `deliveries.cost_cents` → `usage_counters.sms_per_month`; OTP de verificación de teléfono (6 dígitos, 10 min, 5 intentos, rate limit) por SMS; preferencia por canal y categoría; respeto a horarios (no SMS 21:00–08:00 hora local salvo citas en < 12 h).

## 11. Soporte e impersonación

**[PP-F5-ARQ-011] (MUST)**

- Tickets: implementación interna mínima (tabla + email bidireccional por `support@` con `Message-ID` threading) **o** adaptador a helpdesk externo (p. ej. Zendesk/Freshdesk/Help Scout) sincronizando estado; decisión por coste en M5.4 (el puerto `Helpdesk` permite ambas). Centro de ayuda: sitio estático (misma herramienta que `developers.`) con artículos versionados en `docs/help/`.
- Impersonación: el usuario concede (`POST /me/support-access` con alcance y duración) → `ImpersonationSession`; el agente solicita un token de impersonación (`POST /admin/support/impersonate`) → la API emite un **token interno** firmado (no de Keycloak) con `sub = usuario`, `act = agente`, `scope`, `exp ≤ 30 min`; el middleware de auth acepta este token solo desde la SPA de soporte (audiencia `petpassport-support`) y construye `Principal{UserID: usuario, Impersonator: agente, ReadOnly, ClinicalAllowed}`; `authz` deniega escritura si `ReadOnly` y datos clínicos si no `ClinicalAllowed`; cabecera `X-PP-Impersonation` en respuestas para el banner; todo request auditado con ambos ids; el usuario ve el registro en "Accesos de soporte" y puede revocar.

## 12. Status page y comunicaciones

**[PP-F5-ARQ-012] (MUST)** Status page en proveedor externo (independiente de nuestra infraestructura) con componentes (App, API, Partner API, Notificaciones, Keycloak) actualizados por *checks* externos + manual; incidentes y mantenimientos publicados desde el proceso de incidentes (07-operaciones); la SPA consulta un JSON público del status (`/status.json` en CDN) para el banner; suscripción por email/RSS para clientes.

## 13. Métricas de negocio

**[PP-F5-ARQ-013] (MUST)** Proyección `billing.metrics_daily` (MRR por plan/país, cuentas por estado, trials, conversiones, churn, cobros fallidos/recuperados, créditos SLA) + `support.metrics_daily` (tickets, SLA, CSAT) → panel admin (F3 dashboard ampliado) y exportación CSV; definiciones de métricas en `docs/phase-5/metrics.md` (fórmulas exactas para que ventas y finanzas usen las mismas).

## 14. Revisiones pendientes de fases anteriores

**[PP-F5-ARQ-014] (MUST)** Decisiones registradas en tracking con nota:
- Gateway de partners (ADR-017 §6): evaluar criterio con el número real de partners; si se activa, spike Kong/APISIX de 1 semana y ADR nuevo.
- Keycloak Organizations (ADR-003): revisar si simplifica cuentas multi-organización; por defecto se mantiene membresía en app.
- Lectura tolerante a huecos (F3 ARQ-011): confirmar estado.
- Firma JWS de documentos (F2 SEC-004): activar si clientes Enterprise lo exigen (COULD aquí).
