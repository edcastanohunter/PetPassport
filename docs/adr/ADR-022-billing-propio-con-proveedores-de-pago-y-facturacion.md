# ADR-022: Suscripciones y entitlements propios (`BillingAccount`, `Subscription`); proveedor de pagos y proveedor de facturación fiscal abstraídos y elegidos por jurisdicción

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F5

## Contexto

PetPassport cobra a clínicas (por profesional) y a farmacias (por volumen); los tutores son gratuitos. Hay
dos problemas distintos que suelen confundirse:

1. **Cobrar**: tarjetas, débitos, reintentos, *dunning*. Lo resuelve un proveedor de pagos.
2. **Facturar fiscalmente**: en Colombia la factura electrónica debe emitirse a través de un proveedor
   tecnológico autorizado por la DIAN; el proveedor de pagos **no** la emite. Otros países tienen reglas propias.

Además, la disponibilidad de proveedores depende de dónde esté constituida la empresa (Stripe no opera como
adquirente para empresas constituidas en Colombia al redactar esto; sí lo hacen Wompi, Mercado Pago, PayU).

## Decisión

1. **El estado de suscripción es nuestro**: `BillingAccount` (titular fiscal, método de pago, moneda, país)
   y `Subscription` (plan, asientos/volumen, ciclo, estado, periodo de prueba, cambios) son agregados
   event-sourced en el contexto *Billing*. El proveedor de pagos es un detalle de infraestructura.
2. **Entitlements** (qué puede hacer una organización según su plan) se derivan de la suscripción y se
   consultan por puerto desde los contextos de negocio (ADR-025). Nunca se consulta al proveedor en el
   camino de un comando.
3. **Puerto `PaymentProvider`** (`CreateCustomer`, `AttachPaymentMethod`, `Charge`, `Refund`, `Webhook
   parsing`) con adaptadores: `stripe` (empresa en jurisdicción soportada; incluye *Stripe Billing* solo
   para cobros, no para el estado), `wompi` y `mercadopago` (Colombia / LatAm), `manual` (transferencia
   bancaria con conciliación manual para planes *Enterprise*). La elección es configuración por país de
   facturación de la cuenta.
4. **Puerto `InvoicingProvider`** (`IssueInvoice`, `IssueCreditNote`, `GetPDF/XML`) con adaptadores por
   país: Colombia → proveedor DIAN (Alegra/Siigo/Facturatech u otro; decisión comercial), otros → factura
   PDF propia hasta tener requisito local. La factura fiscal se emite **después** del cobro exitoso (o
   antes, para `manual`) y se enlaza al `Invoice` interno.
5. **Ciclo**: mensual o anual (descuento), prueba de 30 días sin tarjeta para clínicas, prorrateo en cambios
   de asientos, *dunning* 3 intentos en 14 días → `past_due` → `suspended` (lectura permitida, escritura
   bloqueada) → `cancelled` a los 60 días. Impuestos calculados por el proveedor de facturación (IVA CO 19 %).
6. **Idempotencia y conciliación**: webhooks de pago idempotentes por `provider_event_id`; conciliación
   diaria (`billing:reconcile`) entre nuestro estado y el del proveedor con alerta por discrepancia; nunca se
   desactiva un servicio por un webhook aislado sin conciliación.

## Consecuencias

- Cambiar de proveedor de pagos no toca el dominio ni los entitlements.
- Cumplimiento fiscal por país es un adaptador; el core no sabe de DIAN.
- Hay que operar dos integraciones (pago + factura) desde el primer país.

## Alternativas consideradas

- **Stripe Billing como fuente de verdad del estado**: cómodo, pero inviable para empresa constituida en
  Colombia y acopla entitlements a un proveedor.
- **Merchant of Record (Paddle, Lemon Squeezy)**: resuelve impuestos globales, pero exige constitución en
  países soportados y no emite factura DIAN; reconsiderar para expansión fuera de LatAm.
