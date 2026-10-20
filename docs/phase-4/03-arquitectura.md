# F4 · 03 — Arquitectura: sagas, integración y borde B2B

Marco: ADR-017 (borde partner), ADR-019 (OCR), ADR-020 (saga), ADR-021 (niveles).

## 1. Vista general F4

```
Internet ─► WAF (reglas app) ─► Traefik ─► app · api · auth                      (como F3)
Partners ─► WAF (reglas B2B) ─► Traefik ─► api (router partners.<dominio>, /partner/v1)   mTLS opcional
Partners sandbox ───────────────────────► api-sandbox (datos ficticios) + partner-sim
Developers ─────────────────────────────► developers.<dominio> (docs estáticas)

api ──► PostgreSQL · Redis · OpenSearch (F3)
worker ──► asynq colas: ocr · partner:out · webhooks · doc:render …
worker ──► egress proxy (allowlist por partner) ──► APIs de partners (nivel 3)
worker ──► proveedor de extracción (Anthropic API) vía egress proxy
```

## 2. Módulos nuevos

**[PP-F4-ARQ-001] (MUST)**

```
internal/
  prescriptions/{domain,app,infra,http}      # Prescription, extracción (comandos), PDF, verificación
  pharmacyorders/{domain,app,infra,http}     # QuoteRequest, Order, process managers, read models
  partners/                                  # Partner Integration
    app/            # PartnerIntegration CRUD, gateway dispatcher, webhooks, SLA
    gateway/        # PartnerGateway port + implementaciones portal|api|adapter
    adapters/<slug>/# ACL por partner (nivel 3): client, mapper, contract tests, cassettes
    http/partner/   # Partner API v1 (handlers generados desde api/partner-v1.yaml)
    sim/            # simulador (cmd/partner-sim)
  ocr/{app,infra}                            # pipeline de extracción, extractores, matcher, eval
  platform/egress/                           # cliente HTTP saliente con allowlist, circuit breaker, firma
```

Reglas de dependencia (ADR-001) ampliadas: `partners/adapters/*` solo importa `partners/gateway` y `shared`; `ocr` no importa `prescriptions/domain` (devuelve DTO `Extraction`); `prescriptions` y `pharmacyorders` no importan `partners`.

## 3. Saga de cotización

**[PP-F4-ARQ-002] (MUST)**

```
POST /quote-requests ─► RequestQuotes ─► [QuoteRequested] (sync: read_pharmacyorders.quote_requests, quote_board)
outbox ─► PM pharmacyorders.fanout:
   for org in invited: asynq partner:out {qr_id, org_id, attempt}   (dedup por qr|org)
   scheduler: ExpireQuoteRequest @ expires_at ; RemindPartners @ expires_at/2
asynq partner:out ─► PartnerGateway(org.mode).SendQuoteRequest(payload minimizado)
   portal  → nada que enviar (la bandeja es el read model) + notificación (email/push) → MarkInvitationSent{portal}
   api     → webhook `quote_request.created` (si configurado) ; siempre disponible por pull → MarkInvitationSent{api}
   adapter → HTTP al partner vía egress ; éxito → MarkInvitationSent{adapter} ; fallo definitivo → MarkInvitationFailed
respuestas:
   portal  → POST /pharmacy/quotes (UI) → RecordQuote
   api     → POST /partner/v1/quote-requests/{id}/quotes → RecordQuote
   adapter → webhook del partner / polling → mapper → RecordQuote
SelectQuote ─► PM: Order.Place → OrderPlaced → PartnerGateway.NotifyOrder (+ webhook `order.placed`) → CloseQuoteRequest
```

- Idempotencia extremo a extremo: `Idempotency-Key` en API interna; `dedup_key` en asynq; comandos `RecordQuote` idempotentes por `(qr_id, org_id, quote_hash)`; webhooks con `X-PP-Delivery-ID`.
- Timeouts: invitación nivel 3 con 3 intentos (1 min, 5 min, 30 min) → `failed`; saga expira a `expires_at` pero sigue seleccionable 24 h si hay cotizaciones (DOM-008).
- Consistencia: la saga nunca bloquea; estados parciales son válidos y visibles en el tablero (`quote_board` read model síncrono + push de cambios por SSE a la SPA, FE-005).

## 4. `PartnerGateway` y niveles

**[PP-F4-ARQ-003] (MUST)**

```go
type PartnerGateway interface {
    SendQuoteRequest(ctx, org OrganizationID, req QuoteRequestPayload) (SendResult, error)
    NotifyOrder(ctx, org, order OrderPayload) (SendResult, error)
    CancelOrder(ctx, org, orderID, reason) error
    Capabilities(org) Capabilities
}
```

- `gateway.Dispatcher` resuelve la implementación por `partner_integrations.mode`.
- **Portal**: `SendQuoteRequest` = `nil` + evento de notificación; el read model `pharmacy_inbox` ya contiene la solicitud (proyección síncrona de `QuoteRequested`).
- **API**: entrega webhook (si hay endpoint) y marca `sent`; el partner también puede hacer *pull* (`GET /partner/v1/quote-requests?status=pending`).
- **Adapter**: `adapters/<slug>` implementa `PartnerGateway` con un cliente HTTP generado del contrato del partner, `Mapper` (nuestro modelo ↔ suyo, `ProductMapping` para SKUs), `StatusMapper`, `ErrorMapper` (errores → `retryable|permanent`); respuestas asíncronas por webhook del partner (`/partner/v1/inbound/<slug>`, firma verificada por el adaptador) o polling programado.
- Contract tests por adaptador con cassettes (`go-vcr`) + ejecución opcional contra sandbox real del partner (etiqueta `partner-live`).

## 5. Webhooks salientes

**[PP-F4-ARQ-004] (MUST)**

- Eventos: `quote_request.created`, `quote_request.cancelled`, `quote_request.expired`, `order.placed`, `order.cancelled`, `prescription.revoked` (afecta a pedidos), `ping`.
- Entrega por asynq `webhooks` (cola `critical`): `POST <url>` con cabeceras `X-PP-Event`, `X-PP-Delivery-ID` (UUIDv7), `X-PP-Timestamp` (unix), `X-PP-Signature: v1=<hex HMAC-SHA256(secret, timestamp + "." + body)>` (una firma por secreto activo, separadas por coma durante rotación), `Content-Type: application/json`, timeout 10 s; éxito = 2xx.
- Reintentos: 1 m, 5 m, 30 m, 2 h, 12 h (5 intentos); luego `failed` + aviso al partner por email + visible en portal; reintento manual.
- Registro `webhook_deliveries` con hash del payload (no el payload) y respuesta (código, 200 primeros bytes).
- Vectores de prueba publicados en docs (payload, secreto, firma esperada) y endpoint "Enviar evento de prueba".

## 6. Partner API entrante

**[PP-F4-ARQ-005] (MUST)**

- Contrato `backend/api/partner-v1.yaml` (OpenAPI 3.1 independiente del interno), handlers generados, versionado por ruta (`/partner/v1`), *breaking changes* bloqueados por `oasdiff`.
- Auth: `Authorization: Bearer <token>` de Keycloak (`client_credentials`, realm `petpassport`, client `partner-<slug>`), validación con `aud = petpassport-partner-api`, scope `partner-api`, claim `partner_org_id`; middleware construye `PartnerPrincipal{OrgID, ClientID, Scopes}`; **nunca** se acepta un token de usuario en rutas partner ni un token partner en rutas internas (audiencias distintas; test de authz).
- Idempotencia obligatoria en todos los `POST` (`Idempotency-Key`); paginación por cursor; errores `problem+json` con catálogo propio (`PTN`).
- Rate limit por `partner_org_id` (ARQ-007 de F3): 600/min lectura, 120/min escritura; cabeceras `RateLimit-*`.
- Alcance de datos: un partner solo ve solicitudes donde está invitado, sus cotizaciones, sus pedidos y su mapeo.

## 7. Egress: anti-SSRF y resiliencia

**[PP-F4-ARQ-006] (MUST)** `platform/egress`:

- Todo HTTP saliente hacia terceros (adaptadores, webhooks, proveedor de IA) usa `egress.Client` con: allowlist de hosts por destino (`partner_integrations.webhook_endpoint.url` validada al guardar: `https` obligatorio, host público, sin IP literal, sin redirecciones a privadas), resolución DNS con bloqueo de rangos privados/link-local/metadata, `MaxRedirects 0` para webhooks (3 para adaptadores con revalidación), timeouts (connect 3 s, total 10 s; IA 60 s), tamaño máximo de respuesta 1 MB (IA 5 MB), *circuit breaker* por destino (5 fallos/60 s → abierto 5 min), métricas por destino.
- En compose/VM: proxy HTTP(S) saliente dedicado (p. ej. Envoy/Squid) como única ruta a Internet para `worker`; `api` sin salida a Internet salvo Keycloak/JWKS internos.

## 8. Simulador y sandbox

**[PP-F4-ARQ-007] (MUST)** `cmd/partner-sim`: servicio Go que se comporta como un partner **nivel 2** (consume `partner/v1` con client credentials, recibe webhooks, responde cotizaciones y avanza pedidos) con comportamiento configurable por cabecera/config: `delay`, `decline`, `partial`, `substitute`, `fail_webhook`, `slow`. Se usa en E2E, demos y como "partner de ejemplo" en sandbox.

**[PP-F4-ARQ-008] (MUST)** Sandbox: despliegue separado `partners-sandbox.<dominio>` con base de datos propia (datos ficticios regenerados a diario), realm Keycloak `petpassport-sandbox`, mismo contrato; credenciales emitidas desde el portal (modo "sandbox") sin revisión; el simulador actúa como farmacia y como tutor que genera solicitudes periódicas para que el partner tenga tráfico.

**[PP-F4-ARQ-009] (SHOULD)** `developers.<dominio>`: sitio estático generado en CI desde `partner-v1.yaml` (Scalar/Redoc), guías (autenticación, webhooks con vectores, niveles, go-live checklist), *changelog*, estado del sandbox; sin autenticación.

## 9. Borde `partners.<dominio>`

**[PP-F4-ARQ-010] (MUST)** Traefik: router por `Host(partners.<dominio>)` → `api` con `PathPrefix(/partner/v1)` únicamente (todo lo demás 404); middlewares: `headers` (sin CSP de navegador, `Cache-Control: no-store`), `buffering` 2 MiB, rate limit de respaldo por IP, `ipAllowList` opcional por partner (etiqueta), mTLS opcional (`tls.options` con CA de clientes y `clientAuth: RequireAndVerifyClientCert` en un entrypoint dedicado `partners-mtls`); WAF: conjunto de reglas B2B (JSON-only, sin cookies, bloqueo de user-agents de navegador, geo-reglas por partner, límites más estrictos). Logs de acceso con `partner_org_id` (del token, inyectado por la app en cabecera de respuesta `X-PP-Partner` para correlación).

## 10. PDF y verificación de recetas

**[PP-F4-ARQ-011] (MUST)** Reutiliza `document_jobs` y Typst (F2): plantilla `prescription.typ` con datos del animal, tutor (nombre), veterinario (nombre + credencial), clínica, ítems, validez, notas, QR → `/verify/prescriptions/{id}?h=<hash>`, hash en pie. Verificación pública recomputa el hash desde `es.events` y devuelve estado + dispensaciones (sin farmacia). Para recetas externas no hay PDF "oficial": se sirve la imagen con marca de agua "Receta externa · no verificada por PetPassport".

## 11. Pipeline OCR (resumen; detalle en 07)

**[PP-F4-ARQ-012] (MUST)** Cola asynq `ocr` (concurrencia 4/réplica, timeout 120 s por job): `ocr:preprocess` → `ocr:extract` → `ocr:match` → comando `RecordExtraction`/`RequestReupload`. Artefactos en storage `rx/<prescription>/<attempt>/…`; estado visible en `read_prescriptions.extraction_jobs`; presupuesto consultado antes de llamar al proveedor (corte duro + alerta).

## 12. Minimización de datos hacia partners

**[PP-F4-ARQ-013] (MUST)** Contratos de datos por etapa, aplicados por los *payload builders* (única función por etapa, con test de campos):

| Etapa | Datos enviados al partner |
|---|---|
| Solicitud de cotización | `quote_request_id`, ítems (`catalog_id`, nombre, dosis/posología, cantidad, `substitution_allowed`, `free_text`), animal (`species`, `weight_kg?`, `age_months?`), `mode`, `area` (ciudad/zona), `expires_at`, receta: tipo (`issued`/`external`) y, para `issued`, enlace de verificación. |
| Pedido | + `order_id`, nombre del tutor, teléfono, dirección (solo delivery), PDF/QR de receta `issued` o imagen de receta `external` (URL prefirmada 24 h), notas para la farmacia. |
| Cancelación / revocación | ids y motivo. |

Nunca: historial clínico, diagnósticos, email del tutor (salvo que el tutor lo autorice para envío, COULD), datos de otras farmacias.

## 13. Read models F4

**[PP-F4-ARQ-014] (MUST)** Síncronos: `read_prescriptions.prescriptions` (+ `items`, `dispensations`, `extraction_jobs`), `read_pharmacyorders.quote_requests` (+ `invitations`, `quotes`, `quote_board` para el tutor, `pharmacy_inbox` para farmacias), `read_pharmacyorders.orders`. Asíncronos: `partner_sla`, `prescriptions_by_vet` (estado agregado), `ocr_metrics_daily`, `ai_budget_usage`. Timeline clínica (F2) añade ítems `prescription_issued` y `order_delivered` (visibilidad tutor).

## 14. Secuencia — foto a pedido

```
tutor: POST /pets/{id}/prescriptions/external (imágenes) → ExternalPrescriptionUploaded → 202
worker ocr: preprocess → extract (Claude, JSON schema) → match (OpenSearch) → RecordExtraction → push "lista para revisar"
tutor: revisa, corrige → POST /prescriptions/{id}/confirm → ExternalPrescriptionConfirmed
tutor: POST /quote-requests {items, mode, orgs} → QuoteRequested → fanout (portal/api/adapter)
farmacia A (portal): cotiza → QuoteReceived ; farmacia B (api): webhook → POST quotes → QuoteReceived ; C: expira
tutor: SSE tablero → SelectQuote{A} → OrderPlaced → farmacia A notificada (inbox + email/push)
farmacia A: confirmed → preparing → ready ; tutor recoge y paga ; farmacia: delivered + dispensación
PM: Prescription.RecordDispensation → receta "dispensada" ; SLA actualizado ; valoración (COULD)
```
