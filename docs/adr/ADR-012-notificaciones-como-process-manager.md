# ADR-012: Notificaciones como *process manager* sobre el outbox con planificación en tabla; solo email en F2

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F2 (canales adicionales en F3/F5)

## Contexto

Las notificaciones son de dos tipos: **reactivas** (un evento ocurre → se notifica) y **programadas**
(recordatorio 24 h antes de la cita; vacuna que vence en 14 días). No contienen reglas de negocio del
dominio clínico; son un subdominio genérico que debe ser fiable, idempotente y observable.

## Decisión

1. **Notifications no es event-sourced.** Es un módulo con un *process manager* que consume el **outbox**
   (ADR-009) y dos tablas: `notifications.scheduled` (pendientes con `due_at`) y `notifications.deliveries`
   (registro de envíos e intentos).
2. **Reactivas**: regla `evento → plantilla + destinatarios`; se encola una entrega inmediata.
3. **Programadas**: regla `evento → crear/actualizar/cancelar entradas en scheduled` (p. ej.
   `AppointmentConfirmed` crea recordatorios `T-24h` y `T-2h`; `AppointmentCancelled` los cancela;
   `VaccinationEntryRecorded` con `next_due_at` crea `T-14d` y `T-0`). El worker despacha por `due_at`
   con `FOR UPDATE SKIP LOCKED`.
4. **Idempotencia** por clave natural `(rule, subject_id, recipient_id, occurrence)`; reprocesar el outbox
   nunca duplica un envío.
5. **Canal F2: email** (SMTP transaccional). La abstracción `Channel` permite añadir SMS/WhatsApp/push en
   F3/F5 sin tocar reglas.
6. **Preferencias** por usuario y categoría (`appointments`, `preventive_care`, `clinical`, `account`);
   las de categoría `security` no se pueden desactivar.
7. **Dead-letter**: tras 5 intentos con *backoff* exponencial la entrega pasa a `failed` y alerta; no bloquea
   la cola.

## Consecuencias

- El dominio clínico y de citas no sabe nada de emails; solo emite eventos.
- Los recordatorios son *best-effort* con observabilidad (métricas de retraso de entrega).
- Las plantillas (es/en) se versionan en el repo y se prueban con *snapshots*.

## Alternativas consideradas

- Enviar emails síncronos desde reacciones (como en F1 para unos pocos casos): se migra todo al process
  manager; la ruta síncrona de F1 se elimina en F2.
- Servicio externo de notificaciones (Courier, Novu): se reconsidera en F5 cuando haya multicanal.
