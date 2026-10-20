# F2 · 04 — API REST v1 (ampliación)

Convenciones de F1 (API-001) vigentes. Todos los endpoints bajo `/api/v1`. `Authz` = acción de 08-seguridad §2.

## 1. Encuentros — **[PP-F2-API-001] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /organizations/{org_id}/encounters` *(Idempotency-Key)* | `encounter:open` | `{pet_id, type, reason, started_at?, attending_vet_id?, appointment_id?}` → `201 Encounter`. `403 PET_NOT_ACCESSIBLE`, `409 APPOINTMENT_NOT_CHECKED_IN`. |
| `GET /organizations/{org_id}/encounters?status=&pet_id=&professional_id=&from=&to=&cursor` | `encounter:list` | → `{data: EncounterSummary[]}`; incluye `abandoned: true` para borradores > 7 d. |
| `GET /encounters/{id}` | `encounter:read` | → `200 Encounter` (tutor: solo `signed`, adjuntos visibles). |
| `PATCH /encounters/{id}` *(If-Match)* | `encounter:edit` | `{started_at?, reason?, subjective?, objective?, attending_vet_id?}` → `200`. `409 ENCOUNTER_NOT_DRAFT`. |
| `PUT /encounters/{id}/vitals` *(If-Match)* | `encounter:edit` | `Vitals` → `200`; respuesta incluye `warnings[]` por rangos de especie. |
| `PUT /encounters/{id}/diagnoses` *(If-Match)* | `encounter:clinical_edit` | `{diagnoses: Diagnosis[]}` → `200`. |
| `PUT /encounters/{id}/plan` *(If-Match)* | `encounter:clinical_edit` | `Plan` → `200`. |
| `PUT /encounters/{id}/follow-up` / `DELETE` | `encounter:clinical_edit` | `{due_date, reason}` → `200`. |
| `POST /encounters/{id}/sign` *(If-Match)* | `encounter:sign` | → `200 Encounter{signature, pdf{status}}`. `422 ENCOUNTER_INCOMPLETE`, `403 NOT_ATTENDING_VET`. |
| `POST /encounters/{id}/addenda` *(Idempotency-Key)* | `encounter:sign` | `{text, reason, attachment_ids?}` → `201`. `409 ENCOUNTER_NOT_SIGNED`. |
| `POST /encounters/{id}/discard` | `encounter:discard` | `{reason}` → `200`. |
| `GET /encounters/{id}/pdf` | `encounter:read` | → `302` a URL prefirmada o `202 {status: pending}`. |
| `POST /encounters/{id}/vaccinations` *(Idempotency-Key)* | `card:record` | atajo: `RecordEntry` con `encounter_id` (+ procedimiento en borrador) → `201 CardEntry`. |

`Encounter` DTO: `{id, version, pet{id, name, species}, organization_id, appointment_id?, type, status, started_at, reason, attending_vet{id, display_name}?, vitals?, subjective?, objective?, diagnoses[], plan{…}, follow_up?, attachments[{id, content_type, size, description, visible_to_guardian, url?}], signature?{signed_by{…}, signed_at, stream_version, content_hash}, addenda[], pdf?{status, url?, expires_at?}, created_at, updated_at}`.

## 2. Adjuntos — **[PP-F2-API-002] (SHOULD)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /encounters/{id}/attachments` | `encounter:edit` | multipart `file`, `description?`, `visible_to_guardian?` → `201 Attachment`. `413`, `415`, `409 ATTACHMENT_LIMIT`. |
| `DELETE /encounters/{id}/attachments/{att_id}` | `encounter:edit` | → `204` (solo borrador). |
| `PATCH /encounters/{id}/attachments/{att_id}` | `encounter:clinical_edit` | `{visible_to_guardian, reason?}` → `200`. |
| `GET /encounters/{id}/attachments/{att_id}` | `encounter:read` (+ visibilidad) | → `302` prefirmada (auditado). |

## 3. Historial — **[PP-F2-API-003] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /pets/{pet_id}/timeline?kind=&organization_id=&from=&to=&cursor` | `record:read` | `{data: TimelineItem[], next_cursor, freshness{lag_seconds}}`. |
| `GET /pets/{pet_id}/encounters?cursor` | `record:read` | consultas firmadas visibles para el actor (custodia incluida). |
| `GET /pets/{pet_id}/weight-history` | `record:read` | `{data: [{measured_at, weight_kg, source}], trend{…}}`. |
| `GET /pets/{pet_id}/summary` | `record:read` | `{alerts[], last_encounter?, next_due[], current_weight?, open_follow_ups[]}`. |
| `PUT /pets/{pet_id}/alerts` *(If-Match)* | `pet:alerts` | `{alerts: Alert[]}` → `200 Pet`. |

## 4. Carnet — **[PP-F2-API-004] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /pets/{pet_id}/card` | `record:read` | `{species, entries: CardEntry[], next_due: NextDueItem[]}` (anuladas incluidas con `status: voided`). |
| `POST /pets/{pet_id}/card/entries` *(Idempotency-Key)* | `card:record` | `{kind, product{catalog_id?, name}, administered_at, lot?, manufacturer?, expiry_date?, route?, site?, dose_label?, weight_kg?, encounter_id?, next_due?{date}}` → `201 CardEntry{next_due{date, basis}}`. `409 CARD_ENTRY_DUPLICATE`. |
| `POST /pets/{pet_id}/card/declarations` *(Idempotency-Key)* | `card:declare` (tutor) | `{kind, product, administered_at, lot?, dose_label?, evidence_object_key?}` → `201`. |
| `POST /pets/{pet_id}/card/entries/{entry_id}/verify` / `/dispute` | `card:verify` | `{note?}` → `200`. |
| `POST /pets/{pet_id}/card/entries/{entry_id}/void` | `card:void` | `{reason}` → `200`. `403 VOID_WINDOW_EXPIRED`. |
| `PUT /pets/{pet_id}/card/entries/{entry_id}/next-due` | `card:record` | `{date}` → `200` (`basis: clinic`). |
| `GET /pets/{pet_id}/card/next-due/proposal?kind=&product_catalog_id=&administered_at=` | `card:record` | `{date?, basis, explanation}` (previsualización del cálculo). |
| `GET /pets/{pet_id}/card/pdf` | `record:read` | `302`/`202` como encuentros. |

## 5. Catálogos — **[PP-F2-API-005] (MUST)**

| Método y ruta | Authz | Respuesta |
|---|---|---|
| `GET /catalogs/vaccines?species=&q=` | autenticado | `{data: Vaccine[]}`. |
| `GET /catalogs/protocols?country=&species=` | autenticado | `{data: Protocol[]}`. |
| `GET /catalogs/diagnoses?q=` | miembro clínico | `{data: [{system, code, label}]}` (COULD). |
| `POST/PUT/DELETE /admin/catalogs/vaccines[/{id}]`, `/admin/catalogs/protocols[/{id}]` | `super_admin` | con `{reason}`; auditado. |

## 6. Enlaces compartidos — **[PP-F2-API-006] (SHOULD)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /pets/{pet_id}/shares` | `share:manage` (tutor) | `{scope, expires_in_days, pin?}` → `201 {id, url, expires_at}` (el token solo se devuelve aquí). |
| `GET /pets/{pet_id}/shares` / `DELETE /pets/{pet_id}/shares/{id}` | `share:manage` | lista sin token / revocar. |
| `GET /share/{token}` (público) | — | `{pet{name, species, breed, sex, birth_date, photo_url?, microchip_masked}, card{…} \| record{…}, verified_at, pin_required}`. `401 PIN_REQUIRED`, `404`, `410 SHARE_EXPIRED`. |
| `POST /share/{token}/unlock` (público) | — | `{pin}` → `200` + cookie corta de sesión de enlace. `429` tras 5 intentos. |
| `GET /verify/encounters/{id}?hash=` (público) | — | `{valid: bool, signed_at, organization_display_name}`. |

## 7. Citas — **[PP-F2-API-007] (MUST)**

Tutor:

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `POST /appointments` *(Idempotency-Key)* | `appointment:request` | `{pet_id, organization_id, type, reason, preferences: [{start, end}] \| {date_from, date_to}}` → `201 Appointment`. `409 REQUEST_LIMIT`, `422 OUTSIDE_LEAD_TIME`. |
| `GET /appointments?pet_id=&status=&from=&to=&cursor` | `appointment:read` | mis citas. |
| `GET /appointments/{id}` | `appointment:read` | |
| `POST /appointments/{id}/accept-proposal` | `appointment:request` | → `200`. `410 PROPOSAL_EXPIRED`. |
| `POST /appointments/{id}/cancel` | `appointment:cancel` | `{reason?}` → `200 {late: bool}`. |

Clínica:

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/appointments?status=&professional_id=&date=&from=&to=&cursor` | `appointment:manage` | agenda/bandeja. |
| `POST /organizations/{org_id}/appointments` *(Idempotency-Key)* | `appointment:manage` | `{pet_id, professional_id, type, reason, slot{start, end}}` → `201` (`confirmed`). `409 SLOT_UNAVAILABLE`. |
| `POST /organizations/{org_id}/appointments/{id}/confirm` | `appointment:manage` | `{professional_id, slot}` → `200`. `409 SLOT_UNAVAILABLE`. |
| `POST /…/{id}/propose` | `appointment:manage` | `{professional_id, slot}` → `200` (`proposed`). |
| `POST /…/{id}/reject` | `appointment:manage` | `{reason}` → `200`. |
| `POST /…/{id}/reschedule` *(If-Match)* | `appointment:manage` | `{slot, professional_id?}` → `200`. |
| `POST /…/{id}/cancel` | `appointment:manage` | `{reason}` → `200`. |
| `POST /…/{id}/check-in` / `/no-show` / `/complete` | `appointment:manage` | → `200`. `409 INVALID_TRANSITION`. |

`Appointment` DTO: `{id, version, pet{…}, organization{id, display_name}, professional{id, display_name}?, type, origin, status, reason, preferences[], slot{start, end, tz}?, proposal_expires_at?, encounter_id?, cancellation{by, reason, late}?, cancellation_policy_hours, created_at, updated_at}`.

## 8. Agenda y disponibilidad — **[PP-F2-API-008] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/agendas` | `agenda:manage` (lectura: cualquier miembro) | `{data: Agenda[]}`. |
| `PUT /organizations/{org_id}/agendas/{professional_id}` *(If-Match)* | `agenda:manage` | `{weekly_template, slot_minutes, lead_time_min_hours, lead_time_max_days, cancellation_policy_hours, active}` → `200`. |
| `POST/DELETE /organizations/{org_id}/agendas/{professional_id}/exceptions[/{date}]` | `agenda:manage` | `{date, closed, ranges?}`. |
| `GET /organizations/{org_id}/availability?type=&professional_id=&from=&to=` | tutor con vínculo o miembro | `{slots: [{professional_id, start, end}]}`; tutores ven solo franjas dentro de `[lead_min, lead_max]`. |

## 9. Solicitud de vínculo — **[PP-F2-API-009] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /organizations/{org_id}/pet-lookup?microchip=\|guardian_email=` | `carelink:request` | `{data: [{pet_id, name, species, guardian_masked}]}` (máx 5; rate limit 20/h). |
| `POST /organizations/{org_id}/care-link-requests` *(Idempotency-Key)* | `carelink:request` | `{pet_id, message?}` → `201 {id, status, expires_at}`. `409 CARE_LINK_ALREADY_ACTIVE`, `409 REQUEST_PENDING`. |
| `GET /organizations/{org_id}/care-link-requests?status=` | `carelink:request` | |
| `GET /me/inbox?cursor` | autenticado | bandeja del tutor: solicitudes, propuestas, controles. |
| `POST /pets/{pet_id}/care-link-requests/{id}/approve` / `/decline` | `pet:carelink:grant` | `{reason?}` → `200 Pet`. |

## 10. Preferencias de notificación — **[PP-F2-API-010] (MUST)**

| Método y ruta | Authz | Cuerpo / respuesta |
|---|---|---|
| `GET /me/notification-preferences` | `me:read` | `{email: {appointments: true, preventive_care: true, clinical: true, account: true, security: true}, digest: {clinic_daily_agenda: false}}`. |
| `PUT /me/notification-preferences` | `me:update` | mismo objeto; `security` ignorado. |
| `POST /unsubscribe/{token}` (público) | — | baja de una categoría por enlace de correo → `200 {category}`. |

## 11. Estadísticas de clínica — **[PP-F2-API-011] (SHOULD)**

`GET /organizations/{org_id}/stats?month=YYYY-MM` → `{encounters_signed, appointments{confirmed, completed, cancelled, no_show}, new_patients, vaccinations, open_follow_ups, previous_month{…}, freshness{lag_seconds}}` (`org:stats:read`: admin y veterinarios).

## 12. Catálogo de errores (ampliación) — **[PP-F2-API-012] (MUST)**

| `code` | HTTP | Cuándo |
|---|---|---|
| `PET_NOT_ACCESSIBLE` | 403 | La org no tiene vínculo/custodia sobre la mascota. |
| `ENCOUNTER_NOT_DRAFT` / `ENCOUNTER_NOT_SIGNED` | 409 | Transición inválida. |
| `ENCOUNTER_INCOMPLETE` | 422 | Falta contenido mínimo para firmar (`errors[]` indica qué). |
| `NOT_ATTENDING_VET` | 403 | Firmante ≠ profesional responsable. |
| `ROLE_REQUIRED_VETERINARIAN` | 403 | Acción clínica por rol no veterinario. |
| `ATTACHMENT_LIMIT` / `ADDENDA_LIMIT` | 409 | |
| `APPOINTMENT_NOT_CHECKED_IN` | 409 | Abrir consulta desde cita sin check-in. |
| `CARD_ENTRY_DUPLICATE` | 409 | Misma vacuna/producto y fecha. |
| `VOID_WINDOW_EXPIRED` | 403 | > 30 días y no super_admin. |
| `ENTRY_NOT_DECLARED` | 409 | Verificar/disputar una entrada no declarada. |
| `SLOT_UNAVAILABLE` | 409 | Solapamiento o fuera de disponibilidad. |
| `OUTSIDE_LEAD_TIME` | 422 | Preferencias fuera de la antelación permitida. |
| `REQUEST_LIMIT` | 409 | > 3 solicitudes pendientes por mascota/org. |
| `PROPOSAL_EXPIRED` | 410 | |
| `INVALID_TRANSITION` | 409 | Estado de cita no admite la acción. |
| `CARE_LINK_ALREADY_ACTIVE` / `REQUEST_PENDING` / `REQUEST_EXPIRED` | 409 / 409 / 410 | |
| `LOOKUP_RATE_LIMITED` | 429 | Búsqueda de mascotas. |
| `SHARE_EXPIRED` / `PIN_REQUIRED` / `PIN_INVALID` / `SHARE_LIMIT` | 410 / 401 / 401 / 409 | |
| `DOCUMENT_PENDING` | 202 | PDF en generación (no es error; se documenta por claridad). |
| `PROJECTION_STALE` | — | Cabecera `X-PP-Freshness: lag=<s>` en vistas derivadas; no es error. |

## 13. Documentos y verificación — **[PP-F2-API-013] (MUST)**

- `GET /documents/{job_id}` → `{status: pending|ready|failed, url?, expires_at?}` (jobs de PDF encolados por `sign`, `card/pdf`).
- `GET /verify/encounters/{id}?hash=` y `GET /verify/cards/{pet_id}?h=` públicos, sin PII, con rate limit.
- Cabecera `X-PP-Freshness` en respuestas de `timeline`, `summary`, `weight-history`, `stats`, `inbox` con el retraso de la proyección en segundos.
