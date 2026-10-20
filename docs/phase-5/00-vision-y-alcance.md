# Fase 5 — Operación SaaS y Monetización

> Estado: v1.0 en revisión · Fecha: 2026-10-20 · Requiere: Fase 4 cerrada
> Tracking: [`docs/tracking/phase-5.md`](../tracking/phase-5.md)

> **Supuestos de partida**: empresa constituida en Colombia (afecta a proveedores de pago y a la factura
> electrónica DIAN); precios en COP con equivalente USD; un solo país operativo al inicio de F5. Si la
> entidad o el país cambian, se revisan ADR-022 y las semillas de planes, no el modelo.

## 1. Objetivo

Convertir el producto en un negocio operable:

- **Monetización**: planes para clínicas (por profesional) y farmacias (por volumen); tutores gratuitos;
  prueba de 30 días; cobro recurrente; factura electrónica; entitlements aplicados en el dominio.
- **Continuidad**: Kubernetes multi-zona, DR con RPO/RTO medidos, backups cross-region, simulacros.
- **Cumplimiento**: crypto-shredding, derechos del titular automatizados, registro de actividades de
  tratamiento, DPIA, retención automática, controles de acceso revisados, auditoría exportable.
- **Operación y soporte**: status page, on-call, gestión de incidentes, SLA por plan, centro de ayuda,
  soporte en la app, métricas de negocio.
- **Canales**: SMS/WhatsApp para recordatorios (de pago en planes Pro).

Al terminar F5, F6 escala y evoluciona (extracción de servicios, móvil nativo, telemedicina) sobre una
empresa que factura y cumple.

## 2. Documentos de la fase

| # | Documento | Área |
|---|---|---|
| 01 | [Producto: planes, facturación, soporte, SLA](01-producto.md) | PRD |
| 02 | [Modelo DDD: Billing, Privacy, Support](02-modelo-ddd.md) | DOM |
| 03 | [Arquitectura: pagos, facturación, entitlements, crypto-shredding, canales](03-arquitectura.md) | ARQ |
| 04 | [API REST v1 (ampliación)](04-api.md) | API |
| 05 | [Datos](05-datos.md) | DAT |
| 06 | [Frontend](06-frontend.md) | FE |
| 07 | [Operaciones: Kubernetes, DR, on-call, incidentes, status, costes](07-operaciones.md) | OPS |
| 08 | [Cumplimiento y privacidad](08-cumplimiento-privacidad.md) | CMP |
| 09 | [Seguridad, infraestructura y calidad](09-seguridad-infra-calidad.md) | SEC / INF / QA |

Nuevas áreas: `OPS` (operaciones) y `CMP` (cumplimiento). IDs: `PP-F5-<ÁREA>-nnn`.

## 3. Alcance

### Dentro (IN)

- **Billing**: `BillingAccount` (titular fiscal, puede agrupar varias organizaciones — cadenas), `Subscription`
  (plan, asientos/volumen, ciclo, estado, prueba, cambios con prorrateo), catálogo de planes versionado,
  cupones (COULD), proveedor de pagos (Wompi / Mercado Pago / Stripe / manual), proveedor de facturación DIAN,
  *dunning*, conciliación, medición de uso, entitlements, panel de facturación en la app y en admin.
- **Privacy**: crypto-shredding (claves por sujeto, migración única), `DataSubjectRequest` (exportación,
  supresión, rectificación) con verificación, plazos y excepciones legales; retención automática por tipo de dato.
- **Compliance**: registro de actividades de tratamiento, DPIA, política de retención, revisión de accesos
  trimestral, auditoría exportable, acuerdos con encargados (IA, pagos, facturación, email/SMS), textos legales
  por plan y país, cookies/consentimientos.
- **Operaciones**: migración a Kubernetes, multi-AZ, DR, backups cross-region, simulacros, on-call y escalado,
  gestión de incidentes y postmortems, status page pública, SLA y créditos por incumplimiento, capacidad y
  costes (FinOps básico).
- **Soporte**: centro de ayuda, soporte en la app (tickets por email/portal con SLA por plan), herramientas de
  soporte interno (impersonación con consentimiento y auditoría, búsqueda de cuentas), métricas de negocio
  (MRR, churn, activación) en el panel admin.
- **Canales**: SMS y WhatsApp (proveedor abstraído) para recordatorios y OTP de verificación de teléfono.
- Revisiones pendientes: gateway de partners (criterio ADR-017), Keycloak Organizations (ADR-003), gap-tolerant
  reader (si no se resolvió en F3).

### Fuera (OUT) — y dónde cae

| Fuera de F5 | Fase |
|---|---|
| Pagos de pedidos de farmacia y comisiones | F6 |
| Plan *Plus* para tutores | COULD (PRD-021); decisión con datos |
| Multi-región activa-activa | F6+ |
| Marketplace de servicios, telemedicina, app nativa | F6 |
| Certificaciones formales (ISO 27001, SOC 2) | F6 (F5 deja los controles y evidencias) |

## 4. Hitos

| Hito | Nombre | Resultado observable |
|---|---|---|
| M5.0 | Plataforma y continuidad | Producción en Kubernetes multi-zona; backups cross-region; primer simulacro de restauración con RPO/RTO medidos; status page. |
| M5.1 | Billing core | Planes, prueba, suscripción, entitlements aplicados en comandos, medición de uso; proveedor de pagos en sandbox; conciliación. |
| M5.2 | Facturación y cobro real | Factura electrónica DIAN; cobro recurrente en producción; dunning; panel de facturación; primeras clínicas de pago. |
| M5.3 | Privacidad y cumplimiento | Crypto-shredding en producción (migración única ejecutada); DSR automatizados; retención automática; registro de tratamientos; DPIA. |
| M5.4 | Soporte, canales y operación | Centro de ayuda, tickets, impersonación auditada, SMS/WhatsApp, on-call e incidentes operando, SLA publicado. |
| M5.5 | Release | Auditoría interna de controles; `v1.0.0` (salida de beta). |

## 5. Criterios de salida

1. Todos los `MUST` en `VERIFICADO`.
2. ≥ 10 organizaciones de pago activas con cobro recurrente y factura electrónica válida (aceptada por DIAN).
3. Simulacro de restauración completa: RPO medido ≤ 5 min, RTO ≤ 1 h (zona) — evidencia en `RESTORE-LOG.md`.
4. Supresión de un usuario de prueba: PII irrecuperable en eventos, read models, OpenSearch, caché y exportación; historia clínica del animal intacta (test automatizado).
5. Conciliación de pagos sin discrepancias durante 30 días consecutivos.
6. Disponibilidad ≥ 99,9 % durante 60 días en producción con error budget publicado.
7. Postmortem de al menos un incidente (real o simulado) con acciones cerradas.

## 6. Riesgos específicos

| Riesgo | Mitigación |
|---|---|
| Proveedor de pagos/facturación no disponible o cambia reglas | Puertos abstraídos; dos proveedores de pago soportados; `manual` como respaldo. |
| Rechazo de facturas por DIAN | Validación previa (esquema UBL del proveedor), entorno de habilitación, reintentos y alerta; facturación `manual` de contingencia. |
| Migración única de eventos sale mal | Backup verificado, ensayo en staging con copia de prod, verificación por hash, ventana con rollback. |
| Bloquear clientes por error de cobro | Gracia 14 días, lectura nunca bloqueada, conciliación antes de restringir. |
| Migración a Kubernetes con corte largo | Réplica lógica, ensayo completo en staging, ventana nocturna, rollback 24 h. |
| Carga operativa de on-call en equipo pequeño | Alertas accionables únicamente, runbooks, soporte gestionado del proveedor cloud. |
