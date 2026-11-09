# Registro de Cambios (Changelog) - Partner API

Todas las modificaciones notables a la Partner API y contratos de integración B2B se documentarán en este archivo.
El formato se basa en [Keep a Changelog](https://keepachangelog.com/es-ES/1.0.0/) y este proyecto se adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [1.0.0] - 2026-11-09

### Añadido
- **Especificación Partner API v1 (`partner-v1.yaml`)**:
  - Endpoints de catálogo maestro: `GET /partner/v1/catalog/medications`.
  - Mapeo de productos y equivalencias: `GET`, `PUT /partner/v1/product-mappings/{catalogId}` y `POST /partner/v1/product-mappings/bulk` con soporte de idempotencia.
  - Bandeja de cotizaciones B2B: `GET /partner/v1/quote-requests` y `GET /partner/v1/quote-requests/{id}` con minimización estricta de datos (sin PII de tutores expuesta en etapa de cotización).
  - Emisión y rechazo de ofertas: `POST /partner/v1/quote-requests/{id}/quotes` (con soporte para propuestas de sustitución farmacéutica) y `POST /partner/v1/quote-requests/{id}/decline`.
  - Gestión operativa de pedidos: `GET /partner/v1/orders/{id}` y `POST /partner/v1/orders/{id}/status` con registro de dispensación (lote y vencimiento).
  - Métricas operativas de partner: `GET /partner/v1/me/stats`.
- **Autenticación y Seguridad (ARQ-005, SEC-002)**:
  - Autenticación OAuth2 Client Credentials contra realm Keycloak dedicado.
  - Separación estricta de audiencias: token emitido con audiencia `petpassport-partner-api`. Rechazo inmediato de tokens de usuario final en `/partner/v1` y viceversa.
  - Rate limiting adaptativo: 600 req/min para lectura, 120 req/min para escritura (`429 Too Many Requests` + cabecera `Retry-After`).
- **Sistema de Webhooks Criptográficos (ARQ-004, SEC-003, PTN-004)**:
  - Cabecera `X-PP-Signature` con algoritmo HMAC-SHA256 y prefijo de versión `v1=`.
  - Cabecera `X-PP-Timestamp` con validación estricta de ventana anti-replay de 300 segundos.
  - Soporte de rotación zero-downtime con firmas múltiples concurrentes (`v1=sig_current,v1=sig_previous`).
  - Despachador de reintentos exponencial con jitter y circuit breaker.
- **Herramientas de Integración y Pruebas (ARQ-007, ARQ-008, PTN-005, QA-002)**:
  - Simulador de referencia `partner-sim` con receptor de webhooks (`serve`) y validador de conformidad (`checklist` de 10 escenarios).
  - Realm de pruebas dedicado `petpassport-sandbox` con credenciales herméticas independientes de producción.
  - Portal para desarrolladores interactivo con explorador OpenAPI basado en Scalar.
