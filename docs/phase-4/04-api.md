# F4 · 04 — API interna v1 (ampliación) y Partner API v1

Convenciones de F1–F3 vigentes. La **Partner API** es un contrato separado (`backend/api/partner-v1.yaml`) servido en `https://partners.<dominio>/partner/v1` (ARQ-005/010).

## A. API interna

### 1. Credenciales profesionales — **[PP-F4-API-001] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /me/credentials` | `me:read` | `{data: Credential[]}` |
| `POST /me/credentials` *(Idempotency-Key)* | `me:update` | `{country, kind: "veterinarian", registry_number, issuer, issued_at, expires_at?, evidence_object_key}` → `201 Credential{status: pending}` |
| `DELETE /me/credentials/{id}` | `me:update` | solo `pending`/`rejected` |
| `GET /organizations/{org_id}/members/credentials` | miembro | estado de credenciales de veterinarios de la org (sin documento) |
| `GET /admin/credentials?status=` / `POST /admin/credentials/{id}/verify` / `/reject` | `super_admin` | `{reason}` |

### 2. Recetas emitidas (veterinario) — **[PP-F4-API-002] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /organizations/{org_id}/prescriptions` *(Idempotency-Key)* | `prescription:issue` | `{pet_id, encounter_id?, items: Item[], validity?{valid_until?, refills_allowed?}, notes?}` → `201 Prescription{status: draft}`. `403 CREDENTIAL_REQUIRED`. |
| `GET /organizations/{org_id}/prescriptions?status=&pet_id=&issuer_id=&cursor` | `prescription:list` | |
| `GET /prescriptions/{id}` | `prescription:read` | `Prescription` (tutor, emisor/org, farmacia con pedido o solicitud) |
| `PATCH /prescriptions/{id}` *(If-Match)* | `prescription:issue` | borrador |
| `POST /prescriptions/{id}/issue` *(If-Match)* | `prescription:issue` | → `200 Prescription{status: issued, signature, pdf{status}}`. `422 PRESCRIPTION_INCOMPLETE`, `422 VALIDITY_OUT_OF_RULES` |
| `POST /prescriptions/{id}/revoke` | `prescription:revoke` | `{reason}` → `200`. `409 PRESCRIPTION_DISPENSED` |
| `GET /prescriptions/{id}/pdf` | `prescription:read` | `302`/`202` |
| `GET /encounters/{id}/prescriptions` | `encounter:read` | recetas derivadas del encuentro |

`Item`: `{catalog_id?, free_text?, dose{value, unit}, route, frequency{every?, unit?} | "once" | "as_needed", duration_days?, quantity{value, unit}, substitution_allowed, instructions?}`.

### 3. Recetas externas (tutor) — **[PP-F4-API-003] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /pets/{pet_id}/prescriptions/external` *(Idempotency-Key)* | `prescription:upload` (tutor) | multipart `images[]` (1–5), `country?` → `202 Prescription{status: draft, extraction{status: queued}}` |
| `POST /prescriptions/{id}/images` | `prescription:upload` | añadir página (antes de extraer/confirmar) |
| `GET /prescriptions/{id}/extraction` | `prescription:read` (tutor) | `{status: queued|processing|done|reupload_requested|failed, attempt, extraction?{legibility, warnings[], header{…}, items[{index, raw_text, product{text, candidates[{catalog_id, name, score}]}, dose, frequency, duration, quantity, confidence{…}, bbox?}]}, reason?}` |
| `POST /prescriptions/{id}/extraction/retry` | `prescription:upload` | re-encola (máx 3 intentos) |
| `POST /prescriptions/{id}/confirm` *(If-Match)* | `prescription:upload` | `{items: Item[] (con extraction_ref?), validity, external_meta{vet_name?, clinic_name?, issued_on?}, attestation: true}` → `200 Prescription{status: confirmed}`. `409 QUOTE_REQUEST_OPEN` |
| `POST /prescriptions/{id}/reopen` | `prescription:upload` | → `200` |
| `GET /prescriptions/{id}/images/{page}` | `prescription:read` | `302` prefirmada (tutor, farmacia con pedido/solicitud, vet vinculado) |

### 4. Mis recetas y verificación — **[PP-F4-API-004] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /me/prescriptions?pet_id=&status=&source=&cursor` | tutor | `{data: PrescriptionSummary[]}` (`dispensed_summary{items_remaining, refills_remaining}`) |
| `POST /prescriptions/{id}/archive` | tutor | oculta de la lista (no cambia estado) |
| `GET /verify/prescriptions/{id}?h=` (público) | — | `{valid, status, issued_at, valid_until, issuer{display_name, credential{country, registry_number}}, organization{display_name}, pet{name, species}, items[{name, quantity, dose, controlled}], dispensations[{at, lines[]}]}` |

### 5. Cotizaciones (tutor) — **[PP-F4-API-005] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /prescriptions/{id}/quote-options?mode=&lat=&lon=` | tutor | `{eligible_items[], excluded_items[{item_id, reason: controlled|free_text_unmapped}], pharmacies[{organization_id, display_name, city, distance_km?, modes[], sla{avg_response_minutes, quote_rate}, rating?, linked: bool}]}` |
| `POST /quote-requests` *(Idempotency-Key)* | `quote:request` | `{prescription_id, item_ids[], mode, area{city, zone?} \| {lat, lon}, organization_ids[] (1–10), expires_in_hours?}` → `201 QuoteRequest`. `409 QUOTE_REQUEST_LIMIT`, `422 PRESCRIPTION_NOT_QUOTABLE` |
| `GET /quote-requests?status=&cursor` / `GET /quote-requests/{id}` | tutor | `QuoteRequest{items[], invitations[{organization{…}, status, sent_at}], quotes[Quote], status, expires_at, selected_quote_id?, order_id?}` |
| `GET /quote-requests/{id}/events` (SSE) | tutor | eventos `invitation`, `quote`, `expired`, `closed` |
| `POST /quote-requests/{id}/select` *(If-Match)* | `quote:request` | `{quote_id, substitution_decisions[{item_id, accept: bool}], contact_phone, delivery_address? }` → `201 Order` (atómico: `QuoteSelected` + `OrderPlaced` vía PM; la respuesta espera al `Order` síncrono creado por el PM en la misma transacción? **No**: dos streams → la API devuelve `202 {quote_request, order: {status: "placing"}}` y la SPA sigue por SSE; `order_id` llega en < 1 s). |
| `POST /quote-requests/{id}/cancel` | tutor | `{reason?}` → `200` |

`Quote`: `{id, organization{id, display_name, city, rating?}, lines[{item_id, unit_price, qty, available: full|partial|none, substitution?{catalog_id, name, unit_price, reason}}], subtotal, delivery_fee?, total, currency, eta?, pickup{address, hours}?, valid_until, notes?, received_at, completeness: full|partial}`.

### 6. Pedidos (tutor) — **[PP-F4-API-006] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /orders?status=&cursor` / `GET /orders/{id}` | tutor | `Order{…, timeline[], pharmacy{display_name, phone, address?}, payment_notice: "in_pharmacy"}` |
| `GET /orders/{id}/events` (SSE) | tutor | cambios de estado |
| `POST /orders/{id}/cancel` | tutor | `{reason?}` → `200`. `409 ORDER_NOT_CANCELLABLE` |
| `POST /orders/{id}/rating` | tutor | `{score 1–5, comment?}` (COULD) |

### 7. Portal de farmacia — **[PP-F4-API-007] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /pharmacies/{org_id}/inbox?status=pending|quoted|declined|expired&cursor` | `pharmacy:quote` | `{data: InboxItem[{quote_request_id, received_at, expires_at, pet{species, weight_kg?, age_months?}, items[{item_id, catalog{id, name, form, concentration}, free_text?, dose, frequency, quantity, substitution_allowed, my_mapping?{sku, base_price}}], prescription{source, verified: bool, verify_url? \| image_urls[]}, mode, area}]}` |
| `POST /pharmacies/{org_id}/quote-requests/{id}/quotes` *(Idempotency-Key)* | `pharmacy:quote` | `{lines[{item_id, unit_price, qty, available, substitution?{catalog_id, unit_price, reason}}], delivery_fee?, eta?, pickup?{address_ref}, valid_until, notes?}` → `201 Quote`. `422 SUBSTITUTION_NOT_EQUIVALENT`, `409 QUOTE_REQUEST_CLOSED` |
| `POST /pharmacies/{org_id}/quote-requests/{id}/decline` | `pharmacy:quote` | `{reason}` |
| `POST /pharmacies/{org_id}/prescriptions/{id}/validate` | `pharmacy:validate` (`pharmacist`) | `{status: validated|rejected, note?}` |
| `GET /pharmacies/{org_id}/orders?status=&cursor` / `GET …/orders/{id}` | `pharmacy:orders` | `Order` con contacto del tutor y receta |
| `POST …/orders/{id}/confirm` `{partner_ref?, eta?}` · `/reject {reason}` · `/preparing` · `/ready` · `/out-for-delivery {eta?}` · `/deliver {dispensation{lines[{item_id, qty}]}}` · `/cancel {reason}` | `pharmacy:orders` | `200 Order`. `409 INVALID_TRANSITION`, `422 DISPENSATION_EXCEEDS` |

### 8. Configuración de farmacia — **[PP-F4-API-008] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET/PUT /pharmacies/{org_id}/integration` | `pharmacy:admin` | `{capabilities{pickup, delivery, substitutions}, service_areas[{city, zone?, fee, eta_hours}], hours, auto_decline_outside_area}` (el `mode` lo fija SuperAdmin) |
| `GET /pharmacies/{org_id}/product-mappings?q=&cursor` · `PUT …/product-mappings/{catalog_id}` · `DELETE` · `POST …/product-mappings/import` (CSV, `dry_run`) | `pharmacy:admin` | `{sku, name, base_price?, active}` |
| `GET /pharmacies/{org_id}/api-credentials` · `POST …/api-credentials/rotate` · `POST …/api-credentials/sandbox` | `pharmacy:admin` | `{client_id, secret (solo al crear/rotar), previous_secret_expires_at}` |
| `GET/PUT /pharmacies/{org_id}/webhook` · `POST …/webhook/test` · `GET …/webhook/deliveries?status=&cursor` · `POST …/webhook/deliveries/{id}/retry` | `pharmacy:admin` | `{url, events[], secret (solo al crear/rotar), active}` |
| `GET /pharmacies/{org_id}/stats?period=` | `pharmacy:admin`, `pharmacist` | métricas PRD-016 |

### 9. Administración — **[PP-F4-API-009] (MUST)**

| Método y ruta | Cuerpo / respuesta |
|---|---|
| `GET /admin/partners?mode=&status=` · `POST /admin/partners/{org_id}` `{mode, adapter_slug?, reason}` · `POST …/suspend` · `/reactivate` | alta/gestión de integración; crea el client Keycloak vía service account |
| `GET/PUT /admin/regulatory-rules/{country}` | `{default_validity_days, max_validity_days, max_refills, controlled_orderable, requires_credential_verification, notes, reason}` |
| `GET /admin/ai/budget` · `PUT /admin/ai/budget` `{monthly_usd, hard_stop: bool, alert_pct}` · `GET /admin/ai/usage?from=&to=` | presupuesto y consumo por día/modelo |
| `GET /admin/ocr/monitor?from=&to=` | `{volume, success_rate, reupload_rate, refusal_count, avg_latency_ms, field_edit_rates{product, dose, frequency, duration, quantity}, cost_usd}` |
| `GET /admin/ocr/samples?flag=low_confidence|edited|refused` | muestras anonimizadas para revisión (auditado) |
| `POST /admin/prescriptions/{id}/revoke` `{reason}` | revocación tras dispensación |

### 10. Catálogo de errores (ampliación) — **[PP-F4-API-010] (MUST)**

| `code` | HTTP | Cuándo |
|---|---|---|
| `CREDENTIAL_REQUIRED` / `CREDENTIAL_EXPIRED` | 403 | Emisión sin credencial verificada. |
| `PRESCRIPTION_INCOMPLETE` / `VALIDITY_OUT_OF_RULES` / `ITEM_INVALID` | 422 | |
| `PRESCRIPTION_NOT_DRAFT` / `PRESCRIPTION_DISPENSED` / `PRESCRIPTION_NOT_QUOTABLE` | 409 / 409 / 422 | |
| `EXTRACTION_IN_PROGRESS` / `EXTRACTION_FAILED` / `REUPLOAD_REQUIRED` / `EXTRACTION_RETRY_LIMIT` | 409 / 422 / 422 / 429 | |
| `AI_BUDGET_EXCEEDED` | 503 | Corte duro; la UI ofrece captura manual. |
| `QUOTE_REQUEST_LIMIT` / `QUOTE_REQUEST_CLOSED` / `QUOTE_EXPIRED` / `NO_ELIGIBLE_PHARMACIES` | 409 / 409 / 410 / 422 | |
| `SUBSTITUTION_NOT_ALLOWED` / `SUBSTITUTION_NOT_EQUIVALENT` / `SUBSTITUTION_DECISION_REQUIRED` | 422 | |
| `ORDER_NOT_CANCELLABLE` / `DISPENSATION_EXCEEDS` / `INVALID_TRANSITION` | 409 / 422 / 409 | |
| `CONTROLLED_NOT_ORDERABLE` | 422 | Ítem controlado en solicitud. |
| `PARTNER_NOT_ACTIVE` / `PARTNER_MODE_MISMATCH` | 422 | |
| `WEBHOOK_URL_INVALID` | 422 | Falla validación anti-SSRF. |
| `MAPPING_IMPORT_INVALID` | 422 | |

---

## B. Partner API v1 (`partners.<dominio>/partner/v1`)

### 1. Contrato y versionado — **[PP-F4-PTN-001] (MUST)**

- OpenAPI 3.1 propio (`partner-v1.yaml`), inglés, publicado en `developers.<dominio>`; `operationId` estables; cambios compatibles sin versión; incompatibles → `/partner/v2` con 12 meses de convivencia; `oasdiff` en CI.
- JSON `snake_case`, UTC ISO-8601, `problem+json`, `Idempotency-Key` obligatoria en `POST`, cursor pagination, `X-Correlation-ID`.

### 2. Autenticación — **[PP-F4-PTN-002] (MUST)**

- `POST https://auth.<dominio>/realms/petpassport/protocol/openid-connect/token` con `grant_type=client_credentials`, `client_id=partner-<slug>`, `client_secret`, `scope=partner-api` → access token (15 min). Token con `aud: petpassport-partner-api`, `partner_org_id`.
- Rotación de secreto desde el portal (doble secreto 24 h). mTLS opcional (entrypoint `partners-mtls`).
- Sandbox: mismo flujo contra `auth-sandbox.<dominio>` y `partners-sandbox.<dominio>`.

### 3. Endpoints — **[PP-F4-PTN-003] (MUST)**

| Método y ruta | Descripción |
|---|---|
| `GET /quote-requests?status=pending\|quoted\|declined\|expired&since=&cursor` | Solicitudes donde el partner está invitado (payload de ARQ-013 etapa 1). |
| `GET /quote-requests/{id}` | Detalle. |
| `POST /quote-requests/{id}/quotes` | Enviar cotización (misma forma que portal API-007). → `201 {quote_id}`. |
| `POST /quote-requests/{id}/decline` | `{reason}`. |
| `GET /orders?status=&since=&cursor` · `GET /orders/{id}` | Pedidos adjudicados (payload etapa 2). |
| `POST /orders/{id}/status` | `{status: confirmed\|rejected\|preparing\|ready\|out_for_delivery\|delivered\|cancelled, partner_ref?, eta?, reason?, dispensation?{lines[]}}`. |
| `GET /catalog/medications?q=&cursor` · `GET /catalog/medications/{catalog_id}` | Catálogo PetPassport para mapear. |
| `GET /product-mappings?cursor` · `PUT /product-mappings/{catalog_id}` · `DELETE …` · `POST /product-mappings/bulk` | Mapeo SKU ↔ `catalog_id`. |
| `GET /prescriptions/{id}/verify?h=` | Verificación (equivalente al público, autenticado). |
| `GET /me` | `{organization_id, display_name, mode, capabilities, webhook{configured, events[]}, rate_limits}`. |
| `POST /webhook/test` | Dispara `ping` al endpoint configurado. |

### 4. Webhooks — **[PP-F4-PTN-004] (MUST)**

- Configuración en portal o `PUT /me/webhook` `{url, events[], secret?}` (si no se envía secreto, se genera y devuelve una vez).
- Eventos y payloads: `quote_request.created {quote_request_id, expires_at, summary{items_count, mode, area}}` (el partner hace `GET` para el detalle — patrón *thin webhook*), `quote_request.cancelled`, `quote_request.expired`, `order.placed {order_id}`, `order.cancelled {order_id, reason}`, `prescription.revoked {prescription_id, affected_order_ids[]}`, `ping`.
- Firma (ARQ-004): `X-PP-Signature: v1=<HMAC-SHA256 hex(secret, "<timestamp>.<raw body>")>`; rechazar si `|now - timestamp| > 300 s`; comparar en tiempo constante; `X-PP-Delivery-ID` para idempotencia; respuesta `2xx` en < 10 s (procesar asíncrono).
- Vectores de prueba y ejemplos de verificación en Go, Node, PHP y Python en la documentación.

### 5. Sandbox y simulador — **[PP-F4-PTN-005] (MUST)** Ver ARQ-007/008: datos ficticios, tráfico generado, `partner-sim` como farmacia de referencia cuyo código fuente (cliente de ejemplo) se publica.

### 6. Límites — **[PP-F4-PTN-006] (MUST)** 600 lecturas/min, 120 escrituras/min por partner; `429` + `Retry-After`; cuotas superiores bajo acuerdo (configurable por partner).

### 7. Onboarding — **[PP-F4-PTN-007] (MUST)** Checklist en docs: (1) organización `pharmacy` verificada, (2) nivel asignado por SuperAdmin, (3) credenciales sandbox desde portal, (4) implementar: auth, `GET /quote-requests`, `POST quotes`, `POST orders/{id}/status`, webhook con firma, (5) pruebas contra `partner-sim` (checklist automatizada "go-live": 10 escenarios), (6) credenciales de producción, (7) monitorización de SLA.

### 8. SLA y métricas — **[PP-F4-PTN-008] (SHOULD)** `GET /me/stats` para el partner y panel en portal; umbrales de calidad (tasa de respuesta ≥ 70 %, tiempo medio ≤ 6 h laborables) comunicados en el onboarding.
