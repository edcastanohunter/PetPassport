# ADR-021: Tres niveles de integración de partners — portal (sin integración), API de PetPassport (pull/webhooks) y adaptador saliente; capa anticorrupción por partner

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F4

## Contexto

No hay partners identificados con API al redactar esta fase. Exigir integración técnica para empezar
bloquearía la adopción; depender de APIs ajenas inestables bloquearía el producto. Hace falta un camino
de entrada sin código y una ruta de crecimiento.

## Decisión

| Nivel | Nombre | Cómo responde la farmacia | Para quién |
|---|---|---|---|
| **1** | **Portal** | Un `pharmacist` recibe la solicitud en la bandeja del portal (y por email/push), cotiza ítem a ítem en un formulario, gestiona pedidos y estados a mano. Cero integración. | Toda farmacia desde el día 1 |
| **2** | **API PetPassport** | La farmacia consume `partner/v1` (pull de solicitudes o recepción de webhooks), responde con `POST /quotes`, actualiza pedidos. Autenticación ADR-017. Sandbox y simulador. | Farmacias/cadenas con equipo técnico |
| **3** | **Adaptador saliente** | PetPassport llama a la API del partner mediante un adaptador específico (`internal/partners/adapters/<slug>`) que traduce nuestro modelo al suyo (ACL). | Grandes distribuidores con API propia |

1. **Un único modelo de dominio** (`QuoteRequest`, `Quote`, `Order`) y un único puerto `PartnerGateway{SendQuoteRequest, SendOrder, CancelOrder}`; el nivel es una configuración por partner (`partner_integrations.mode`).
2. **Capa anticorrupción** por adaptador (nivel 3): mapeo de productos (`partner_product_mappings`), de estados y de errores; nunca se filtra el modelo del partner al dominio; cada adaptador tiene *contract tests* contra grabaciones (cassettes) y un simulador.
3. **Simulador de partner** (`cmd/partner-sim`): implementa el nivel 2 como si fuera una farmacia, con comportamientos configurables (latencia, declinar, sustituir, fallar) para E2E, sandbox público y demos.
4. **SLA por partner**: tiempo medio de respuesta y tasa de cotización se miden y muestran al tutor ("esta farmacia suele responder en 2 h"); partners por debajo de umbral se ordenan al final y, con acuerdo, se desactivan.
5. **Alta de partner** (F4): SuperAdmin crea la organización `pharmacy` (ya existe desde F1), verifica, activa `partner_integrations` con nivel 1; niveles 2/3 se activan por solicitud con credenciales emitidas desde el portal.

## Consecuencias

- La primera farmacia puede estar operativa el mismo día (nivel 1), lo que desbloquea el piloto de F4 sin
  depender de terceros.
- El dominio no cambia al añadir partners; cambia la configuración y, en nivel 3, un adaptador aislado.
- El simulador es parte del producto (sandbox para partners), no solo de las pruebas.

## Alternativas consideradas

- Solo nivel 3 (integrarnos nosotros con cada farmacia): inviable sin partners y no escala.
- Solo nivel 2 (obligar a integrar): bloquea la adopción inicial.
