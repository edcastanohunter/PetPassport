# ADR-026: Notificaciones con token de un solo uso se encolan tras el commit; el SMTP solo lo abre el worker

- Estado: Propuesta
- Fecha: 2026-10-30
- Fase: F2 (M2.4)

## Contexto

ADR-012 define las notificaciones como un process manager (`notifications.rules`) que reacciona a eventos del
outbox. Dos correos no pueden derivarse de un evento: la **invitación a una organización** (`MemberInvited`) y la
**transferencia de tutela** (`GuardianshipTransferInitiated`). Su enlace lleva un token de un solo uso que el
sistema guarda solo como **hash** (SEC, DOM-011): el evento y el read model no contienen el token en claro, así
que el PM no puede construir el enlace. En F1 el comando enviaba el correo directamente por SMTP (best-effort).

PP-F2-ARQ-014 exige que el `api` no abra SMTP: los envíos pasan por el worker, con reintentos, límites y
supresiones.

## Decisión

1. **Puerto `notify.Enqueuer`** (`platform/notify`): los comandos con token encolan una `Notification`
   (regla, plantilla, categoría, destinatario, `Occurrence`, `Payload`, `Secret`) en `notifications.deliveries`
   **después** del commit del agregado. La fila es durable; el envío lo hace el worker (`notifications-sender`).
2. **Idempotencia**: `dedup_key = regla|sujeto|destinatario|occurrence`; para tokens, `occurrence` es el prefijo
   hexadecimal del hash del token, de modo que **reenviar** (que rota el token) genera una entrega nueva y
   encolar dos veces el mismo token no duplica.
3. **Secretos en la fila**: las claves listadas en `Secret` (`link_path`) y la dirección del destinatario sin
   cuenta (`_to`) se **purgan del payload** cuando la entrega llega a un estado terminal (enviada, suprimida o
   dead-letter). Mientras está en cola el token vive en la fila con el mismo nivel de protección que el resto de
   `notifications.*` (rol de aplicación, sin exposición por API).
4. **Resto de correos**: se derivan de eventos con el PM `notifications.rules` (sin token). Los comandos de
   transferencia y verificación dejaron de enviar correo; `transfer_done` y los avisos de organización los emite
   el PM.
5. **Destinatarios sin cuenta**: identificados por `GhostRecipientID(address)` (UUID v5 de la dirección
   normalizada) para aplicar supresiones y limitación sin guardar la dirección en claro.

## Consecuencias

- El api no tiene credenciales SMTP; una caída del proveedor no afecta a los comandos.
- **Brecha residual**: si el proceso muere entre el commit del agregado y el `Enqueue`, el correo no se encola.
  Mitigación: el administrador/tutor puede **reenviar** (rota el token); el fallo de encolado se registra sin
  PII. Cerrarla del todo exigiría guardar el token cifrado en el evento o un outbox transaccional de notificaciones
  (descartado en F2 por el coste de manejar un secreto en `es.events`, que es inmutable).
- Las plantillas con token (`org_invitation`, `transfer_invite`) dependen de que `link_path` esté en el payload.

## Alternativas descartadas

- *Token cifrado dentro del evento*: `es.events` es inmutable (crypto-shredding en F5, ADR-023); un secreto de
  un solo uso no debe vivir ahí.
- *Enviar desde el comando con el Mailer de F1*: contradice ARQ-014 y acopla el api al proveedor.
