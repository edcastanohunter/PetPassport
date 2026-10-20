# ADR-017: Borde dedicado para partners (`partners.<dominio>`), OAuth2 client-credentials en Keycloak, webhooks firmados; producto de API Gateway diferido con criterio de activación

- Estado: Aceptada (cierra la revisión prevista en ADR-006)
- Fecha: 2026-10-20
- Fase: F4

## Contexto

F4 abre PetPassport a terceros (farmacias y tiendas): reciben solicitudes de cotización, responden precios,
confirman pedidos y notifican estados. Hay tráfico **entrante** (partners llamando a nuestra API y
respondiendo webhooks) y **saliente** (nuestros adaptadores llamando a APIs de partners). ADR-006 pospuso
la elección de un producto de gateway (Kong/APISIX) a esta fase.

## Decisión

1. **Borde separado**: hostname `partners.<dominio>` con su propio router Traefik, conjunto de reglas WAF
   dedicado (más estricto: solo rutas `/partner/v1/*`, JSON, sin navegador), rate limits por partner y
   *allowlist* de IP opcional por partner.
2. **Autenticación entrante**: OAuth2 *client credentials* en Keycloak — un *client* confidencial por partner
   (`partner-<slug>`) con *service account*, scope `partner-api` y *audience* `petpassport-partner-api`; el
   token lleva `partner_id` (mapper desde atributo del client). Rotación de secreto por el portal de farmacia
   (self-service) con doble secreto activo durante la rotación. **mTLS opcional** en Traefik para partners que
   lo exijan (certificado del partner en allowlist).
3. **Webhooks salientes** (nosotros → partner): firma HMAC-SHA256 sobre `timestamp + body` en cabecera
   `X-PP-Signature`, `X-PP-Timestamp` (ventana 5 min), secreto por partner con rotación (dos secretos activos),
   reintentos con backoff y *dead-letter*, idempotencia por `X-PP-Delivery-ID`.
4. **Webhooks entrantes** (partner → nosotros): misma firma con secreto del partner **o** el token OAuth2;
   idempotencia por `Idempotency-Key` obligatoria.
5. **Salida**: todo tráfico hacia partners pasa por un *egress proxy* con allowlist de dominios por partner
   (anti-SSRF), timeouts cortos, *circuit breaker* por partner y registro de latencias.
6. **Producto de API Gateway (Kong/APISIX) diferido**: se adopta cuando ocurra cualquiera de: > 15 partners
   activos, necesidad de portal de desarrollador self-service con planes/cuotas, o requisito de
   *API analytics* por partner que no cubran Prometheus + Grafana. Hasta entonces: Traefik + Keycloak + código.
7. **Documentación pública** del contrato: OpenAPI `partner/v1` publicado en `developers.<dominio>` (estático,
   Redoc/Scalar) con *changelog* y entorno **sandbox** (`partners-sandbox.<dominio>`) contra un simulador.

## Consecuencias

- Sin componente nuevo que operar en F4; la identidad de partners vive donde ya vive la identidad.
- Cada partner es un `client` Keycloak + una fila en `partnernetwork.partner_integrations`; alta semi-manual
  (SuperAdmin) en F4, self-service en F5.
- La firma de webhooks y el egress proxy son código propio y deben probarse con vectores de prueba publicados.

## Alternativas consideradas

- **API keys estáticas**: más simples pero sin expiración ni scopes; se mantienen solo como *fallback* para
  partners sin capacidad OAuth2 (con rotación trimestral obligatoria y marcadas como `legacy`).
- **Kong/APISIX en F4**: aporta portal y cuotas, pero añade un componente crítico para < 10 partners.
