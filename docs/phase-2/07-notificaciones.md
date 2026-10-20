# F2 · 07 — Notificaciones y recordatorios

Marco: ADR-012. Módulo `internal/notifications` (no ES). Nueva área de requisitos `NOT`.

## 1. Process manager y reglas reactivas

**[PP-F2-NOT-001] (MUST)** `notifications.rules` es un process manager (ARQ-009) suscrito al outbox. Cada regla:
`{Name, Event, Category, Recipients(event) []UserID, Template, Payload(event), Mode immediate|scheduled{offsets}}`.

Catálogo de reglas F2 (destinatario → plantilla):

| Evento | Destinatarios | Categoría | Plantilla | Modo |
|---|---|---|---|---|
| `MemberInvited` (F1, migrada) | invitado (email) | account | `org_invitation` | inmediato |
| `GuardianshipTransferInitiated` / `PetGuardianshipTransferred` | receptor / ambos | account | `transfer_invite` / `transfer_done` | inmediato |
| `OrganizationVerified` / `Rejected` / `Suspended` | admins de la org | account | `org_status` | inmediato |
| `CareLinkRequested` | tutor | account | `care_link_request` | inmediato |
| `CareLinkRequestApproved` / `Declined` | solicitante | account | `care_link_result` | inmediato |
| `AppointmentRequested` | admins + recepcionistas de la org | appointments | `appointment_requested` | inmediato |
| `AppointmentConfirmed` | tutor | appointments | `appointment_confirmed` | inmediato + programados T-24h, T-2h (`appointment_reminder`) |
| `AppointmentAlternativeProposed` | tutor | appointments | `appointment_proposed` | inmediato |
| `AppointmentRescheduled` | tutor | appointments | `appointment_rescheduled` | inmediato + reprograma recordatorios |
| `AppointmentCancelled{by: clinic}` / `{by: guardian}` | tutor / org | appointments | `appointment_cancelled` | inmediato + cancela recordatorios |
| `AppointmentRejected` | tutor | appointments | `appointment_rejected` | inmediato |
| `EncounterSigned` | tutor | clinical | `encounter_signed` | inmediato (agrupado: máx 1/hora por mascota) |
| `EncounterAddendumAdded` | tutor | clinical | `encounter_addendum` | inmediato |
| `FollowUpRecommended` (si firmado) | tutor | preventive_care | `follow_up_reminder` | programados T-3d, T-0 |
| `VaccinationEntryRecorded` / `PreventiveEntryRecorded` | tutor | preventive_care | `vaccine_recorded` | inmediato + programados T-14d, T-0 sobre `next_due` |
| `NextDueAdjusted` | — | preventive_care | — | reprograma |
| `VaccinationEntryVoided` / `PetMarkedDeceased` | — | — | — | cancela programados del sujeto |
| `OrganizationSuspended` | tutores con citas futuras confirmadas | appointments | `clinic_suspended_notice` | inmediato |
| Diario 07:00 TZ org (job) | `clinic_admin` con `digest.clinic_daily_agenda` | appointments | `clinic_daily_agenda` | programado |

Reglas de agrupación y *throttling*: por `(recipient, template)` máximo 1 email cada 60 min salvo `security` y `appointment_reminder`.

## 2. Recordatorios programados

**[PP-F2-NOT-002] (MUST)**

- Al aplicar una regla `scheduled`, el PM hace *upsert* en `notifications.scheduled` por `dedup_key = rule|subject_id|recipient|offset` con `due_at`; eventos de cancelación marcan `cancelled`; reprogramaciones actualizan `due_at`.
- Runner `notifications-dispatch`: cada 5 s toma `pending` con `due_at ≤ now()` (`SKIP LOCKED`, lote 100), re-valida el sujeto (cita sigue confirmada, dosis aún `upcoming`, preferencia activa) y crea `deliveries`; marca `dispatched`.
- `due_at` se calcula en la zona horaria de la organización (citas) o del usuario (vacunas: 09:00 hora local del tutor para T-14d/T-0).
- Métrica `reminder_dispatch_delay_seconds` (now − due_at al despachar); alerta p95 > 60 s.

## 3. Plantillas

**[PP-F2-NOT-003] (MUST)**

- `backend/internal/notifications/templates/<name>/<locale>.{subject.txt,body.html,body.txt}` con `template_version` en un manifiesto; MJML compilado a HTML en build para los `.html`.
- Variables tipadas por plantilla (struct Go); render falla en tests si falta alguna.
- Contenido mínimo de PII: nombre de la mascota, nombre de la clínica, fecha/hora; **nunca** diagnósticos, textos clínicos ni datos de contacto en el cuerpo del correo (se enlaza a la app).
- Pie común: motivo del envío, enlace "Gestionar preferencias" y "Dejar de recibir <categoría>" (token de un clic), dirección física, idioma = `locale` del destinatario.
- *Snapshot tests* de cada plantilla en es/en (QA-005).

## 4. Preferencias y categorías

**[PP-F2-NOT-004] (MUST)** Categorías `appointments`, `preventive_care`, `clinical`, `account`, `security`. `security` siempre activa. Baja por enlace: `POST /unsubscribe/{token}` (token HMAC con `user_id|category|exp`, sin sesión). Las preferencias se consultan en el momento del despacho (no al programar). Entregas suprimidas por preferencia se registran con `status = suppressed` (no se envían).

## 5. Entrega, reintentos y dead-letter

**[PP-F2-NOT-005] (MUST)**

- `Channel.Email` vía SMTP del proveedor transaccional (F1 IAM-012) con `Message-ID` propio y `List-Unsubscribe` + `List-Unsubscribe-Post`.
- Reintentos con backoff 30 s, 2 m, 10 m, 30 m, 2 h (5 intentos); errores 5xx/timeout reintentan; 4xx permanentes (dirección inválida) → `failed` + `suppressions` si procede.
- `failed` definitivo → alerta `NotificationDeadLetter` si > 10 en 1 h; panel admin lista entregas fallidas con reintento manual (COULD).
- Métricas: `notifications_deliveries_total{template,status}`, `notifications_delivery_latency_seconds`, `notifications_dead_letter_total`.

## 6. Idempotencia

**[PP-F2-NOT-006] (MUST)** `deliveries.dedup_key` único (`rule|subject|recipient|occurrence`, donde `occurrence` = `event_id` para inmediatas y `offset` para programadas). Reprocesar el outbox (reintento, rebuild del consumidor) no produce envíos duplicados. Pruebas: reentregar el mismo evento 3 veces → 1 fila.

## 7. Proveedor de email

**[PP-F2-NOT-007] (SHOULD)** Proveedor transaccional con webhooks de *bounce/complaint* → `notifications.suppressions` (endpoint `POST /webhooks/email/{provider}` con firma verificada). SPF/DKIM/DMARC (`p=quarantine`) verificados antes de M2.4; dominio de envío `notify.<dominio>` separado del corporativo; *warm-up* de reputación durante el piloto.
