# PCI DSS — alcance SAQ A de PetPassport (PP-F5-SEC-001)

> **Estado: borrador técnico para la autoevaluación.** Este documento describe el alcance y los
> controles que el código y la infraestructura implementan, con su evidencia. **No es** el
> cuestionario SAQ A ni la Attestation of Compliance (AOC): esos se completan sobre el documento
> oficial vigente de la biblioteca de PCI SSC y los firma el responsable de PetPassport. Qué SAQ
> aplica lo decide el adquirente o la marca de pago; confírmalo con Wompi antes de presentar nada.
>
> Revisión: anual, y en cualquier cambio de proveedor de pagos, del widget o de la página de
> facturación (`/org/:orgId/settings/billing`).

## 1. Cómo cobra PetPassport

| Medio | Flujo | ¿Pasan datos de tarjeta por PetPassport? |
|---|---|---|
| Tarjeta (CO) | Widget de Wompi embebido en la página de facturación (`checkout.wompi.co/widget.js`). El widget tokeniza en el dominio de Wompi y devuelve un token de un solo uso; el backend lo cambia por una fuente de pago (`payment_source`) y cobra con ella. | No. Solo token, `last4`, marca y vencimiento. |
| PSE (CO) | Redirect de página completa al banco del pagador (`POST /charges/{id}/pse-payment` → `redirect_url`). El pagador se autentica y aprueba en su banco; el resultado llega por webhook firmado. | No. Es una transferencia bancaria: no hay tarjeta. Se envían banco, tipo de persona y documento (datos personales, Ley 1581, no datos de tarjeta). |
| Transferencia manual / Enterprise | Fuera de línea; un SuperAdmin asienta el pago (`/admin/charges/{id}/manual-payment`). | No. |

PetPassport no tiene formulario propio de tarjeta en ninguna página, no recibe el PAN, el CVV ni
datos de banda o chip, y no los almacena, procesa ni transmite.

## 2. Criterios de elegibilidad de SAQ A

Confirmados en fuentes de PCI SSC (FAQ 1588 y 1604, consultadas el 2026-11-13). El resto de
criterios se toma del SAQ A oficial al completarlo.

| Criterio | Cómo lo cumple PetPassport | Evidencia |
|---|---|---|
| Comercio sin tarjeta presente (e-commerce) que externaliza todo el tratamiento de datos de tarjeta en un proveedor validado PCI DSS | Wompi tokeniza y cobra. **Pendiente:** obtener el AOC vigente de Wompi como proveedor de servicios. | §1; `backend/internal/billing/providers/wompi` |
| Sin almacenamiento electrónico de datos de tarjeta | El puerto `PaymentProvider` solo transporta tokens; `billing.billing_accounts` guarda `PaymentMethodRef{provider, token, kind, last4, expires_at}`. | `backend/internal/billing/providers/provider.go` (doc del paquete), `domain/vo.go` |
| **Página con formulario embebido del proveedor (iframe/script): el comercio confirma que su sitio no es susceptible a ataques de scripts que afecten a su sistema de e-commerce** (criterio añadido en enero de 2025; FAQ 1588). Con redirect completo (PSE) el criterio no aplica. | El widget solo se carga en la página de facturación, que se abre con carga completa del documento, y su CSP admite como scripts externos únicamente `https://checkout.wompi.co`. El resto de la SPA tiene `script-src 'self'`. La SPA solo inyecta la URL exacta del widget. Alternativa que admite la FAQ: confirmación escrita de Wompi de que su implementación protege la página. | Ver §3 (CSP) |

## 3. Controles técnicos y evidencia

| Control | Implementación | Evidencia verificable |
|---|---|---|
| Tokenización en el proveedor | El backend nunca recibe datos de tarjeta; `AttachPaymentMethod` solo acepta el token del widget y las aceptaciones de términos. | `providers/wompi/wompi.go` (`AttachPaymentMethod`); prueba `TestAttachPaymentMethod_PP_F5_SEC_001_ExchangesTokenForSource` |
| Solo valores públicos hacia la SPA | `SetupPaymentMethod` devuelve llave pública y tokens de aceptación; nunca llave privada ni secretos de integridad/eventos. | Pruebas `TestSetupPaymentMethod_PP_F5_SEC_001_OnlyPublicValues` y `TestSetupPaymentMethod_PP_F5_PRD_004_PSEHasNoWidget` |
| CSP de la página de pago | Traefik: router `frontend-billing` (`PathRegexp ^/org/[^/]+/settings/billing/?$`) con el middleware `payment-page-headers`: `script-src 'self' https://checkout.wompi.co; frame-src https://checkout.wompi.co`, `frame-ancestors 'none'`. nginx aplica la misma política por ruta. | `infra/traefik/staging/routes.yml`, `infra/traefik/dynamic/routes.yml`, `frontend/nginx.conf` (mapa `$pp_csp`); `curl -sI https://app.<dominio>/org/x/settings/billing \| grep -i content-security` |
| Un solo script de terceros | `loadWompi` rechaza cualquier URL distinta de `WOMPI_WIDGET_URL`. | `frontend/src/features/billing/paymentWidget.ts`; `paymentWidget.spec.ts` |
| Carga completa de la página de pago | El router fuerza navegación de documento a la página de facturación para que se aplique su CSP. | `frontend/src/app/router/guards.ts`; `routes.spec.ts` |
| Sin datos de pago en logs | Los logs de Billing registran ids e importes (nunca token, documento ni datos del pagador); el catálogo de errores no incluye cuerpos del proveedor. | `backend/internal/billing/app/payments.go` (`flag`), revisión de `logger.*` en `internal/billing` |
| Webhooks autenticados | Checksum SHA-256 de Wompi verificado en tiempo constante, ventana temporal e idempotencia por evento (PP-F5-SEC-002). | `providers/wompi/wompi.go` (`ParseWebhook`); pruebas `TestParseWebhook_PP_F5_SEC_002_*` |
| Secretos del proveedor | `PP_WOMPI_PRIVATE_KEY`, secreto de integridad y de eventos solo en el entorno del backend (secret manager); sin ellos el adaptador no se registra. | `infra/deploy/staging.env.example`, `backend/internal/billing/setup` |
| Egress restringido | El backend solo alcanza `production.wompi.co` / `sandbox.wompi.co` como proveedor de pagos. | `infra/egress/allowlist.yaml` |

## 4. Escaneo externo trimestral (ASV)

SAQ A incluye los requisitos 11.3.2 y 11.3.2.1: escaneo de vulnerabilidades externo trimestral, y
tras cambios significativos, hecho por un ASV listado por PCI SSC, tanto con iframe como con
redirect (FAQ 1604). Alcance mínimo: `app.<dominio>` (sirve la página de pago) y `api.<dominio>`.

**Pendiente (humano):** contratar el ASV, programar el primer escaneo antes de activar el cobro
real en producción y archivar los informes aprobados junto al AOC.

## 5. Pendientes para cerrar SEC-001

1. Confirmar con Wompi (adquirente/proveedor) que SAQ A es el cuestionario aplicable.
2. Obtener el AOC vigente de Wompi como proveedor de servicios PCI DSS.
3. Escaneos ASV trimestrales (§4).
4. Completar el SAQ A oficial vigente (biblioteca de PCI SSC) usando §2–§3 como evidencia, y
   responder los requisitos que no dependen del código (cuentas por defecto del proveedor de
   hosting, gestión de accesos administrativos, respuesta a incidentes: `infra/deploy/RUNBOOK.md`).
5. Firma del AOC por el responsable de PetPassport y archivo con este documento.
6. Verificar en el sandbox de Wompi que el widget funciona con la CSP de §3 (incluido
   `Cross-Origin-Opener-Policy: same-origin`, por si el flujo 3-D Secure abre ventanas).
