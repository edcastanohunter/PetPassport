# F4 · 08 — Seguridad y cumplimiento

Controles de F1–F3 vigentes. F4 abre la plataforma a terceros y procesa documentos con valor legal.

## 1. Autorización (ampliación)

**[PP-F4-SEC-001] (MUST)** G tutor; V veterinario con credencial; VL veterinario de org vinculada; PA `pharmacy_admin`; PH `pharmacist`; PT token de partner (`PartnerPrincipal`); A `super_admin`.

| Acción | G | V | VL | PA | PH | PT | A |
|---|---|---|---|---|---|---|---|
| `prescription:issue`, `prescription:revoke` | — | ✔ (propias; org con acceso) | — | — | — | — | revoke |
| `prescription:read` | ✔ (propias) | ✔ (emitidas por él/su org) | ✔ (mascota vinculada) | solicitud/pedido vigente | solicitud/pedido vigente | solicitud/pedido vigente | ✔ (motivo) |
| `prescription:upload`, confirm, reopen | ✔ | — | — | — | — | — | — |
| `prescription:validate` (externa) | — | ✔ (VL) | ✔ | — | ✔ | — | — |
| `quote:request`, select, cancel | ✔ | — | — | — | — | — | — |
| `pharmacy:quote`, `pharmacy:validate` | — | — | — | ✔ | ✔ | ✔ | — |
| `pharmacy:orders` | — | — | — | ✔ | ✔ | ✔ | — |
| `pharmacy:admin` (config, mapeo, credenciales, webhook) | — | — | — | ✔ | — | mapeo | — |
| `order:read`, `order:cancel` (tutor) | ✔ | — | — | — | — | — | ✔ |
| `credential:manage` (propia) / verify | ✔ | ✔ | — | — | — | — | verify |
| `partner:admin`, `regulatory:admin`, `ai:admin` | — | — | — | — | — | — | ✔ |

Reglas: los tokens de partner (`aud: petpassport-partner-api`) **solo** valen en `/partner/v1`; los de usuario nunca valen allí (test en ambas direcciones). Un partner solo ve recursos donde su `organization_id` está invitado/adjudicado. Las imágenes de recetas externas se sirven a farmacias solo mientras exista solicitud abierta o pedido activo (URL prefirmada 24 h, regenerable).

## 2. Autenticación de partners y separación

**[PP-F4-SEC-002] (MUST)** Clients Keycloak `partner-<slug>` confidenciales con `client_credentials` únicamente (sin flujos de usuario), scope `partner-api`, audiencia dedicada, secreto ≥ 32 bytes, rotación con doble secreto; `sandbox` en realm separado. Credenciales legacy (API key) solo bajo excepción documentada, con rotación trimestral y `legacy = true`. Alta/baja de clients mediante la service account de F1 con permisos limitados a `clientId` con prefijo `partner-` (política en Keycloak).

## 3. Webhooks: firma y replay

**[PP-F4-SEC-003] (MUST)** Salientes: HMAC-SHA256 sobre `timestamp.body`, ventana 300 s, dos secretos activos en rotación, `X-PP-Delivery-ID` único; vectores de prueba publicados. Entrantes (nivel 3, webhooks de partners): verificación según el partner en el adaptador + idempotencia por `delivery id`; endpoint `/partner/v1/inbound/<slug>` acepta solo el partner esperado (IP allowlist opcional). Pruebas: firma inválida → 401; timestamp fuera de ventana → 401; replay → 200 idempotente sin efecto.

## 4. Egress y anti-SSRF

**[PP-F4-SEC-004] (MUST)** `platform/egress` (ARQ-006) como único cliente saliente; validación de URL al guardar (`https`, host público resuelto, sin IP literal, sin puertos no estándar salvo 443/8443, sin credenciales en URL), re-resolución DNS por request y bloqueo de rangos privados/`169.254.*`/IPv6 ULA, sin seguir redirecciones en webhooks, allowlist por partner en nivel 3, tamaño/timeout acotados; proxy saliente obligatorio en infra (el worker no tiene ruta directa). Pruebas: lista de URLs maliciosas (metadata, localhost, DNS rebinding simulado) rechazadas.

## 5. Minimización y auditoría de envíos a partners

**[PP-F4-SEC-005] (MUST)** *Payload builders* únicos por etapa (ARQ-013) con test de campos permitidos (lista blanca: cualquier campo nuevo rompe el test hasta aprobarse); `partner_calls` registra digest, endpoint, código y latencia (no el cuerpo); el tutor ve en el pedido "Datos compartidos con la farmacia" (lista exacta). Consentimiento específico al hacer el pedido para compartir contacto y dirección.

## 6. Integridad y fraude de recetas

**[PP-F4-SEC-006] (MUST)** Recetas `issued`: hash de eventos en el documento y verificación pública (ARQ-011); QR enlaza a `verify` (no contiene datos); detección de doble dispensación (cantidades por refill en el agregado; una farmacia ve dispensaciones previas al verificar); recetas `external`: siempre `unverified` y así se marcan en API, portal, PDF/imagen (marca de agua) y webhooks; límite de recetas externas por tutor/día (20) y por mascota activa; imágenes con hash para detectar reenvíos; farmacéutico puede rechazar y marcar `suspicious` (señal para SuperAdmin tras 3 rechazos de un mismo tutor).

## 7. Cumplimiento regulatorio

**[PP-F4-SEC-007] (MUST)** Reglas por país en `ref.regulatory_rules` (validez, refills, controladas, verificación de credencial) aplicadas en el dominio (R4, Q3); credenciales profesionales verificadas por humano con evidencia; sustancias controladas no pedibles (RN-26) y marcadas en verificación; textos legales: términos para farmacias (responsabilidad de validación y dispensación), aviso al tutor sobre recetas externas, aviso de "pago en farmacia"; revisión legal por país antes de activar `controlled_orderable` (nunca en F4).

## 8. Proveedor de IA y datos

**[PP-F4-SEC-008] (MUST)** DPA firmado; configuración de retención del proveedor revisada y documentada en `docs/phase-4/compliance/ai-provider.md`; sin PII del tutor en las peticiones (OCR-009); clave API en secret manager con rotación semestral y límite de gasto en el proveedor además del presupuesto interno; tráfico vía egress con allowlist (`api.anthropic.com`); logs sin contenido de imágenes ni extracciones; muestras de evaluación solo sintéticas o con consentimiento; acceso a muestras auditado.

## 9. Threat model F4

**[PP-F4-SEC-009] (MUST)** STRIDE sobre: Partner API (suplantación de partner, elevación entre partners, fuga de solicitudes ajenas), webhooks (falsificación, replay, DoS por endpoints lentos → timeouts y circuit breaker), adaptadores (SSRF, inyección en mapeos), OCR (prompt injection desde la imagen: el modelo solo produce JSON con esquema cerrado; validación estricta; ninguna acción automática), recetas (falsificación, reutilización, doble dispensación), pedidos (PII a farmacias, cancelaciones abusivas), portal (farmacéutico accediendo a solicitudes no invitadas). Revisión antes de M4.4 con al menos un revisor externo al equipo.

## 10. Abuso

**[PP-F4-SEC-010] (MUST)** Límites: solicitudes de cotización (3 abiertas/receta, 10/día), recetas externas (20/día), reintentos OCR (3), cancelaciones de pedidos por tutor (> 3 en 30 días → revisión), farmacias con tasa de rechazo anómala (> 50 %) → alerta; farmacias falsas: verificación de organización (F1) + credencial de farmacéutico responsable (COULD: `pharmacist` con registro profesional verificado como requisito para cotizar, activable por país); partners con webhooks que fallan > 7 días → integración suspendida con aviso.
