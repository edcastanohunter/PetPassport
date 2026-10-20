# Fase 4 — Integración B2B y Automatización (Pharmacy & OCR)

> Estado: v1.0 en revisión · Fecha: 2026-10-20 · Requiere: Fase 3 cerrada
> Tracking: [`docs/tracking/phase-4.md`](../tracking/phase-4.md)

> **Supuesto de partida**: al redactar esta fase no hay farmacias ni distribuidores con API identificados.
> Por eso el diseño arranca con farmacias operando desde el **portal** sin integración (ADR-021, nivel 1),
> publica un contrato `partner/v1` con sandbox y simulador (nivel 2) y deja los adaptadores salientes
> (nivel 3) como trabajo por partner. Si aparece un partner con API antes de M4.3, su adaptador entra como
> requisito `SHOULD` adicional en tracking.

## 1. Objetivo

Cerrar el ciclo **receta → cotización → pedido**:

- El veterinario **emite recetas** firmadas desde la consulta; el tutor **sube recetas externas** que la IA
  extrae y él confirma.
- El tutor **pide cotizaciones** a farmacias aliadas, compara y **hace el pedido** (recogida o envío; pago en la farmacia).
- La farmacia opera desde un **portal** (bandeja de cotizaciones, pedidos, catálogo propio) o por **API** segura.
- La plataforma expone un **borde B2B** dedicado (`partners.<dominio>`) con OAuth2, webhooks firmados, sandbox y documentación.

## 2. Documentos de la fase

| # | Documento | Área |
|---|---|---|
| 01 | [Producto](01-producto.md) | PRD |
| 02 | [Modelo DDD: Prescriptions, Pharmacy Orders, Partner Integration](02-modelo-ddd.md) | DOM |
| 03 | [Arquitectura: sagas, integración, borde B2B](03-arquitectura.md) | ARQ |
| 04 | [API interna y Partner API v1](04-api.md) | API / PTN |
| 05 | [Datos](05-datos.md) | DAT |
| 06 | [Frontend: tutor, veterinario, portal de farmacia](06-frontend.md) | FE |
| 07 | [OCR / IA: pipeline de extracción](07-ocr-ia.md) | OCR |
| 08 | [Seguridad y cumplimiento](08-seguridad.md) | SEC |
| 09 | [Infraestructura, observabilidad y calidad](09-infra-observabilidad-calidad.md) | INF / QA |

Nuevas áreas: `OCR` (extracción) y `PTN` (integración de partners). IDs: `PP-F4-<ÁREA>-nnn`.

## 3. Alcance

### Dentro (IN)

- **Prescriptions**: `Prescription` emitida (desde encuentro o directa) con firma, PDF, QR y verificación pública; receta externa con imagen; validez, refills, dispensaciones, revocación; `ProfessionalCredential` verificada para emitir; reglas regulatorias por país (controladas excluidas del pedido).
- **OCR/IA**: pipeline con modelo multimodal y salida estructurada, confianza por campo, emparejamiento con catálogo, revisión y confirmación del tutor, evaluación continua, presupuesto y métricas.
- **Pharmacy Orders**: `QuoteRequest` (saga) y `Order`; selección de farmacias (vinculadas, cercanas, todas las activas en la ciudad), plazos, sustituciones, estados de pedido, dispensación → receta; notificaciones.
- **Partner Integration**: niveles 1–3; `partner/v1` (pull + webhooks); OAuth2 client-credentials por partner; firma HMAC; sandbox + simulador; mapeo de productos; SLA por partner; egress proxy con allowlist.
- **Portal de farmacia**: bandeja de cotizaciones, formulario de cotización, pedidos, catálogo/mapeo, zonas y horarios de entrega, credenciales API, métricas.
- **Borde B2B**: hostname y WAF dedicados, rate limits por partner, mTLS opcional, documentación pública.

### Fuera (OUT) — y dónde cae

| Fuera de F4 | Fase |
|---|---|
| Pasarela de pago, comisiones, facturación a farmacias | F5 (suscripción) / F6 (pagos de pedidos) |
| Portal de desarrollador self-service con planes y cuotas (Kong/APISIX) | F5 si se cumple el criterio de ADR-017 |
| Inventario en tiempo real de farmacias | depende del partner (nivel 3) |
| Receta electrónica con firma cualificada / integración con registros oficiales | F5/F6 según país |
| Recomendación de medicamentos / interacciones | F6 |
| Logística propia de entregas | nunca (la farmacia entrega) |

## 4. Hitos

| Hito | Nombre | Resultado observable |
|---|---|---|
| M4.0 | Recetas emitidas | Credencial profesional; emitir receta desde consulta; PDF/QR; verificación pública; dispensación manual. |
| M4.1 | Recetas externas + OCR | Subida, extracción estructurada, emparejamiento, revisión y confirmación; evaluación con dataset; presupuesto. |
| M4.2 | Portal de farmacia + cotización (nivel 1) | Saga de cotización con farmacias en portal; comparación; pedido; estados; dispensación registrada. |
| M4.3 | Partner API (nivel 2) | `partner/v1` con OAuth2, webhooks firmados, sandbox, simulador, docs públicas; al menos el simulador como partner nivel 2 en staging. |
| M4.4 | Borde B2B y hardening | `partners.<dominio>` con WAF dedicado, egress proxy, rate limits, threat model, auditoría B2B. |
| M4.5 | Release | Piloto con ≥ 2 farmacias reales (nivel 1 o 2); E2E; `v0.4.0`. |

## 5. Criterios de salida

1. Todos los `MUST` en `VERIFICADO`.
2. E2E: consulta firmada → receta emitida → cotización a 3 farmacias (2 simuladas nivel 2 + 1 portal) → comparación → pedido → confirmación → entrega → dispensación reflejada en la receta.
3. E2E: foto de receta externa → extracción → confirmación del tutor con ≥ 1 corrección → cotización → pedido.
4. Precisión de extracción sobre el dataset de evaluación: F1 por campo ≥ 0,85 en `producto`, ≥ 0,80 en `dosis/frecuencia`, con 0 pedidos realizados sin confirmación humana (test).
5. Seguridad B2B: pruebas de firma de webhooks (vectores publicados), replay rechazado, SSRF bloqueado por egress proxy, tokens de partner sin acceso a rutas internas (suite de authz).
6. Piloto: ≥ 2 farmacias reales operando, ≥ 20 cotizaciones respondidas, tiempo medio de respuesta medido.

## 6. Riesgos específicos

| Riesgo | Mitigación |
|---|---|
| Sin farmacias dispuestas a integrarse | Nivel 1 (portal) sin integración; simulador para demos; SLA visible para incentivar. |
| Extracción imprecisa genera pedidos erróneos | Confirmación humana obligatoria; validación farmacéutica; umbrales; evaluación continua. |
| Responsabilidad legal por recetas externas | Marcadas `unverified`; farmacia valida; términos claros; controladas excluidas. |
| Fuga de datos a partners | Minimización por etapa; auditoría de cada envío; egress allowlist. |
| Coste de IA descontrolado | Presupuesto mensual con alerta y corte; modelo configurable; Batches para reprocesos. |
| Partners con APIs inestables | ACL por adaptador, circuit breaker, SLA, degradación a nivel 1. |
