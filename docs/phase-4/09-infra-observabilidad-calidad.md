# F4 · 09 — Infraestructura, observabilidad y calidad

## 1. Infraestructura

**[PP-F4-INF-001] (MUST)** Borde B2B: DNS `partners.`, `partners-sandbox.`, `auth-sandbox.`, `developers.`; WAF con conjunto de reglas B2B separado (ADR-017/ARQ-010); Traefik: routers y entrypoint `partners-mtls` (443 alternativo o SNI) con CA de clientes gestionada en `infra/traefik/clients-ca/`; sandbox como despliegue aparte (compose `sandbox` en la VM de staging o VM pequeña propia) con BD y realm propios y `partner-sim` desplegado.

**[PP-F4-INF-002] (MUST)** Egress: proxy saliente (Envoy o Squid) como única ruta a Internet del `worker`; reglas de red (compose: red interna sin `internet`; VM: iptables/nftables) que impiden salida directa; allowlist mantenida en `infra/egress/allowlist.yaml` (proveedor de IA, dominios de webhooks/adaptadores registrados — sincronizada desde `partner_integrations` por job); logs de egress con destino, código y latencia.

**[PP-F4-INF-003] (MUST)** Observabilidad: dashboards "Pharmacy Orders" (solicitudes por estado, tiempo a primera cotización, conversión, pedidos por estado, por farmacia), "Partners" (SLA por partner, webhooks: entregas/fallos/latencia, llamadas API por partner y código, circuit breakers), "OCR/IA" (volumen, estados, latencia por etapa, tokens y coste diario por modelo, refusals, tasas de edición por campo, presupuesto consumido), "Prescriptions" (emitidas, verificaciones, dispensaciones, revocaciones). Métricas: `quote_requests_total{status}`, `quote_first_response_seconds`, `quote_to_order_ratio`, `partner_invitations_total{org,channel,status}`, `webhook_deliveries_total{org,status}`, `webhook_latency_seconds`, `partner_api_requests_total{org,route,status}`, `egress_requests_total{dest,status}`, `ocr_jobs_total{stage,status}`, `ocr_stage_duration_seconds`, `ai_tokens_total{model,kind}`, `ai_cost_usd_total{model}`, `ai_budget_used_ratio`, `ocr_refusals_total`, `ocr_field_edit_ratio{field}`, `prescriptions_issued_total`, `prescription_verifications_total{via}`.

**[PP-F4-INF-004] (MUST)** Alertas: `AIBudget80/100` (80 % aviso, 100 % corte), `OCRFailureRateHigh` (> 10 % 1 h), `OCRRefusalsSpike` (> 5 en 1 h), `OCRLatencyHigh` (p95 > 60 s), `WebhookFailuresHigh` (> 20 % 15 min por partner), `PartnerCircuitOpen`, `EgressDenied` (> 0: intento de destino no permitido → revisar), `QuoteFanoutStuck` (invitaciones `pending` > 10 min), `PrescriptionPDFFailures`, `PartnerAPI5xx` (> 1 % 5 min), `SandboxDown`.

**[PP-F4-INF-005] (MUST)** Runbook F4: alta de partner (pasos, Keycloak, nivel, pruebas go-live), rotación de secretos de partner/webhook/IA, webhook en fallo (diagnóstico, reintento, suspensión), circuito abierto hacia un partner, proveedor de IA caído (degradar a captura manual, `PP_OCR_PROVIDER`), presupuesto agotado (ampliar/ajustar modelo), revocación masiva de recetas de un veterinario (credencial fraudulenta), incidente de datos con un partner (notificación, revocación de credenciales, auditoría `partner_calls`), regeneración del sandbox.

**[PP-F4-INF-006] (MUST)** Secretos por partner en el secret manager con rutas `partners/<org_id>/{webhook_current, webhook_previous, legacy_key}`; rotación automatizada desde el portal (escritura vía service account del backend con permisos limitados a ese prefijo); clave del proveedor de IA con rotación semestral y límite de gasto configurado en el proveedor.

**[PP-F4-INF-007] (SHOULD)** `developers.<dominio>`: sitio estático generado en CI (`docs.yml` amplía) y publicado en la CDN; versionado por tag; `partner-v1.yaml` publicado como archivo descargable; estado del sandbox desde `readyz`.

## 2. Calidad

**[PP-F4-QA-001] (MUST)** Pruebas de saga: harness de process managers (F2) para `fanout`, expiración con/sin cotizaciones, reapertura por rechazo, cierre por revocación; invariantes Q1–Q8 y O1–O8 con pruebas nombradas; concurrencia: dos `SelectQuote` simultáneos → uno gana (versión); `RecordQuote` tras expiración → rechazado o aceptado según `selectable_until`.

**[PP-F4-QA-002] (MUST)** Contratos: Partner API validada contra `partner-v1.yaml` en integración; `partner-sim` ejecuta la **checklist go-live** (10 escenarios: auth, pull, cotizar, declinar, sustituir, pedido, estados, dispensación parcial, webhook firmado, replay) como suite E2E; adaptadores nivel 3 con cassettes y suite `partner-live` opcional.

**[PP-F4-QA-003] (MUST)** Evaluación OCR en CI (OCR-007): muestra de 30 en PRs relevantes con gate de no-regresión; nightly completo; informe adjunto al PR; mocks del proveedor (respuestas grabadas) para pruebas unitarias del pipeline sin coste.

**[PP-F4-QA-004] (MUST)** Seguridad: vectores de firma de webhook (positivos/negativos), replay, timestamp fuera de ventana; SSRF (lista de URLs maliciosas); separación de audiencias (token usuario en `/partner/v1` → 401; token partner en `/api/v1` → 401; partner A accediendo a solicitud de B → 404); payload builders con lista blanca; prompt injection en imagen (dataset de imágenes con texto adversarial → la salida sigue el esquema y no contiene instrucciones); `authz` tabla F4 al 100 %.

**[PP-F4-QA-005] (MUST)** E2E F4 (Playwright + partner-sim + Mailpit): (1) credencial → verificación → emitir receta desde consulta → PDF → verificación pública; (2) foto de receta externa (fixture) → extracción (mock del proveedor en CI, real en nightly) → revisión con corrección → confirmación; (3) cotización a 3 farmacias (portal manual en el test, sim nivel 2 ×2 con `delay` y `substitute`) → tablero SSE → selección con decisión de sustitución → pedido → estados hasta entregado → dispensación en la receta; (4) revocación de receta cierra solicitud y cancela pedido no confirmado; (5) webhook de prueba desde el portal recibido y verificado por el sim.

**[PP-F4-QA-006] (MUST)** Validación con piloto: ≥ 2 farmacias reales (nivel 1 o 2) y ≥ 2 veterinarios; sesión documentada sobre el formulario de cotización, la validación de recetas externas y el registro de dispensación; revisión de textos legales por asesor jurídico (recetas externas, pago en farmacia) antes de M4.5.

**[PP-F4-QA-007] (MUST)** Gates F4: cobertura `domain/` ≥ 90 % en `prescriptions` y `pharmacyorders`; suite de seguridad B2B obligatoria; checklist go-live del sim verde; eval OCR sin regresión; `oasdiff` en ambos contratos; p95 Partner API < 300 ms; coste medio por receta extraída ≤ 0,05 USD en nightly; `specs-status.sh --check`.

## 3. Costes orientativos F4 (adicionales)

| Partida | Rango mensual |
|---|---|
| Proveedor de IA (1.000–5.000 recetas) | 30–150 USD (Opus 5.5) · 15–75 USD (Sonnet 5.5) |
| Sandbox (VM pequeña o compose en staging) | 0–40 USD |
| Egress proxy / WAF reglas B2B | 0–30 USD |
| Secret manager (rutas por partner) | < 10 USD |
