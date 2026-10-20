# F2 · 01 — Especificación de producto

## 1. Personas (cambios respecto a F1)

| Persona | Novedades F2 |
|---|---|
| **Tutor** | Ve historial y carnet, solicita citas, recibe recordatorios, comparte carnet, declara vacunas antiguas, aprueba solicitudes de vínculo. |
| **Veterinario** | Atiende: agenda, consulta SOAP, firma, adendas, vacunas, control. |
| **Recepcionista** | Agenda y citas, check-in, abre borradores de consulta con constantes, registra vacunas (sin firmar consultas). |
| **Admin de clínica** | Configura agendas de profesionales, ve estadísticas, solicita vínculos. |
| **SuperAdmin** | Mantiene catálogos (vacunas, protocolos), anula entradas fuera de plazo, consulta auditoría clínica. |

## 2. Épicas

| Épica | Historias |
|---|---|
| E8 Consulta clínica (Encounter) | PRD-001 … 007 |
| E9 Historial y ficha | PRD-008, 009, 025, 026 |
| E10 Carnet y preventivos | PRD-010 … 015 |
| E11 Citas y agenda | PRD-016 … 019 |
| E12 Vínculo solicitado por clínica | PRD-020 |
| E13 Notificaciones y recordatorios | PRD-023, 024 |
| E14 Opcionales | PRD-021 (co-tutor), PRD-022 (importación) |

## 3. Historias y criterios de aceptación

### E8 — Consulta clínica

**[PP-F2-PRD-001] (MUST)** Como miembro clínico quiero abrir una consulta para una mascota vinculada, desde una cita o directamente.
- Tipos: `consultation`, `follow_up`, `emergency`, `vaccination`, `surgery`, `procedure`, `other`.
- Se crea en `draft` con fecha/hora de inicio (editable ±24 h), motivo (≤ 300) y profesional responsable (un `veterinarian` de la organización; obligatorio para firmar, opcional en borrador).
- Desde una cita `checked_in`, la consulta hereda mascota, tipo, motivo y profesional, y la cita queda enlazada.
- Solo organizaciones `active` con vínculo activo a la mascota (o solicitud aprobada) pueden abrir consultas.

**[PP-F2-PRD-002] (MUST)** Como miembro clínico quiero registrar constantes vitales.
- Peso (kg, 0.01–500, 2 decimales), temperatura (°C, 30–45), FC (lpm, 20–400), FR (rpm, 5–200), condición corporal (1–9), mucosas, TRC (s), hidratación (%); todos opcionales con rangos validados por especie (aviso, no bloqueo).
- El peso registrado alimenta el historial de peso de la mascota (PRD-025).

**[PP-F2-PRD-003] (MUST)** Como veterinario quiero documentar la consulta en formato SOAP.
- **S** anamnesis (texto ≤ 5.000), **O** examen físico por sistemas (texto libre + hallazgos marcados normal/anormal por sistema, opcional), **A** diagnósticos (1..n: texto ≤ 200, código opcional del catálogo, tipo `presumptive|confirmed|differential|ruled_out`), **P** plan: ítems de medicación estructurados (`{medication_name, presentation?, dose, dose_unit, route, frequency, duration_days?, instructions?}`), procedimientos realizados (`{name, notes?}`), recomendaciones (texto), control recomendado (PRD-007).
- Autoguardado cada 5 s en borrador; concurrencia por versión (si dos miembros editan, el segundo recibe conflicto y recarga).
- Plantillas por tipo de atención: campos sugeridos (p. ej. `vaccination` abre el panel de vacunas).

**[PP-F2-PRD-004] (SHOULD)** Como miembro clínico quiero adjuntar archivos a la consulta (resultados de laboratorio, radiografías, fotos).
- Tipos: JPEG/PNG/WebP/PDF; ≤ 10 MB; ≤ 10 por consulta; descripción opcional; se pueden eliminar solo en borrador.
- Visibles para el tutor tras la firma (opción "no visible para el tutor" por adjunto, p. ej. notas internas, con motivo).

**[PP-F2-PRD-005] (MUST)** Como veterinario quiero firmar la consulta para que quede inmutable.
- Precondiciones: profesional responsable asignado = firmante; al menos un diagnóstico o motivo + hallazgos; fecha de inicio ≤ ahora.
- Al firmar se registra hora, firmante, versión del stream y *hash* del contenido; se genera PDF descargable con sello "Firmado electrónicamente por … el …" y hash.
- Tras la firma: campos bloqueados; adjuntos no eliminables; el tutor recibe notificación "Nueva consulta registrada" (según preferencias).
- Un borrador con > 7 días sin actividad se marca "abandonado" (visible en una lista para limpieza; se puede retomar o descartar con motivo).

**[PP-F2-PRD-006] (MUST)** Como veterinario quiero añadir adendas a una consulta firmada.
- Texto ≤ 5.000 + motivo (`correction|additional_findings|lab_results|other`); firmada por un veterinario de la misma organización; visible como "Adenda n.º k" bajo la consulta; el PDF se regenera con las adendas.

**[PP-F2-PRD-007] (SHOULD)** Como veterinario quiero recomendar un control en N días para que el tutor reciba recordatorio y pueda pedir cita.
- `{due_date, reason}`; genera recordatorio T-3d y T-0 (PRD-024) con enlace a "Solicitar cita"; visible en el panel del tutor y en la lista "Controles pendientes" de la clínica.

### E9 — Historial y ficha

**[PP-F2-PRD-008] (MUST)** Como tutor quiero ver el historial clínico de mi mascota en orden cronológico y el detalle de cada consulta.
- Línea de tiempo con consultas firmadas (no borradores), vacunas/preventivos, citas completadas, cambios de tutela/vínculo; filtro por tipo y clínica; paginación.
- Detalle de consulta: todo lo firmado + adendas + adjuntos visibles + PDF. Indica clínica y profesional.

**[PP-F2-PRD-009] (MUST)** Como miembro clínico quiero la ficha completa del paciente.
- Pestañas: resumen (alertas: alergias/condiciones crónicas declaradas en el perfil ampliado de la mascota, última visita, próximas vacunas, peso actual), historial (todas las consultas visibles para la organización: propias y, mientras el vínculo esté activo, las de otras clínicas), carnet, citas, peso.
- Tras revocación del vínculo, la organización solo ve las consultas que ella misma firmó (ADR-010).

**[PP-F2-PRD-025] (SHOULD)** Historial de peso con gráfico y tendencia; alerta de variación > 10% en 90 días.

**[PP-F2-PRD-026] (SHOULD)** Estadísticas básicas de clínica (mes actual y anterior): consultas firmadas, citas confirmadas/completadas/canceladas/no-show, nuevos pacientes, vacunas aplicadas, controles pendientes.

### E10 — Carnet y preventivos

**[PP-F2-PRD-010] (MUST)** Como miembro clínico quiero registrar una vacuna aplicada.
- `{vaccine (catálogo), administered_at (≤ hoy), manufacturer?, lot, expiry_date?, route, site?, dose_label (p. ej. "1.ª dosis", "refuerzo anual"), professional, encounter_id?, next_due_at (propuesta por protocolo, editable), notes?}`.
- Si hay consulta abierta de tipo `vaccination`, se enlaza; si no, se crea la entrada sin consulta (atención rápida).
- Registrar produce entrada `recorded` (verificada) y actualiza próximas dosis; el tutor recibe notificación.

**[PP-F2-PRD-011] (SHOULD)** Registrar tratamiento preventivo: desparasitación interna/externa, antiparasitario; `{product, active_ingredient?, lot?, administered_at, next_due_at?, weight_at_application?}`.

**[PP-F2-PRD-012] (MUST)** Como miembro clínico quiero anular una entrada registrada por error.
- Solo la organización que la registró, dentro de 30 días, con motivo; después solo SuperAdmin. La entrada queda visible como "Anulada" (tachada) con motivo; se puede registrar una nueva.

**[PP-F2-PRD-013] (SHOULD)** Como tutor quiero declarar vacunas de un carnet anterior y que una clínica pueda verificarlas.
- Entrada `declared` con foto del carnet opcional; marcada "No verificada" en todas las vistas; cuenta para próximas dosis con aviso.
- La clínica vinculada puede marcar `verified` (tras ver el carnet físico) o `disputed` con nota.

**[PP-F2-PRD-014] (MUST)** Como SuperAdmin quiero mantener el catálogo de vacunas y protocolos por especie/país para que el sistema proponga próximas dosis.
- Vacuna: nombre, especie(s), enfermedades, tipo (`core|non_core`), rutas válidas. Protocolo: país, especie, vacuna, esquema inicial (edad en semanas de cada dosis, intervalo mínimo), refuerzo (meses), validez legal (p. ej. rabia 12 meses).
- Semilla CO: perro (rabia, polivalente DHPPi+L, tos de las perreras), gato (triple/cuádruple felina, rabia, leucemia).
- `next_due_at` propuesto = f(protocolo, edad, dosis previas); siempre editable por la clínica con etiqueta "ajustado por la clínica".

**[PP-F2-PRD-015] (SHOULD)** Como tutor quiero descargar el carnet en PDF y compartirlo por enlace/QR de solo lectura.
- PDF con datos de la mascota, foto, microchip, tabla de vacunas (verificadas y declaradas diferenciadas), preventivos, próximas dosis, QR al enlace de verificación.
- Enlace compartido: alcance `vaccination_card` o `full_record`, caducidad 1–90 días, PIN opcional de 4–6 dígitos, revocable; la página pública muestra "Verificado por PetPassport" y fecha; registra accesos (contador, no IP en claro).

### E11 — Citas y agenda

**[PP-F2-PRD-016] (MUST)** Como admin de clínica quiero configurar la agenda de cada profesional.
- Plantilla semanal (día, franjas horarias), duración por tipo de cita (p. ej. consulta 30 min, vacunación 15, cirugía 90), excepciones (días bloqueados, vacaciones, horario especial), antelación mínima (p. ej. 2 h) y máxima (p. ej. 60 días) para solicitudes de tutores, zona horaria de la organización.

**[PP-F2-PRD-017] (MUST)** Como tutor quiero solicitar una cita en una clínica vinculada y cancelarla.
- Elijo mascota, clínica (vinculada y activa), tipo, motivo (≤ 300), y hasta 3 preferencias de franja a partir de la disponibilidad publicada (o "cualquier hora" en un rango de fechas). La solicitud queda `requested`.
- Recibo confirmación con fecha/hora y profesional cuando la clínica confirma; puedo cancelar hasta X horas antes (configurable por clínica, por defecto 4 h); después, la cancelación queda marcada "tardía".
- Puedo ver próximas y pasadas citas de todas mis mascotas.

**[PP-F2-PRD-018] (MUST)** Como miembro clínico quiero gestionar las citas.
- Bandeja de solicitudes; confirmar asignando profesional y franja (la franja debe estar libre: disponibilidad + sin solapamiento); proponer alternativa (la solicitud vuelve al tutor con nueva franja a aceptar); rechazar con motivo.
- Crear cita directa (`confirmed`) para una mascota vinculada o para un paciente sin cuenta (COULD: "paciente provisional" sin tutor registrado, diferido a F3 salvo capacidad).
- Reprogramar (nueva franja, notifica al tutor), cancelar (motivo), vista de agenda por día/semana/profesional con estados por color.

**[PP-F2-PRD-019] (MUST)** Check-in, no-show y completar.
- `check_in` al llegar (habilita "Iniciar consulta" → PRD-001); `no_show` tras la hora de fin si no hubo check-in (manual o automático a las 24 h); `completed` al firmar la consulta enlazada o manualmente.

### E12 — Vínculo solicitado por clínica

**[PP-F2-PRD-020] (MUST)** Como miembro clínico quiero solicitar el vínculo con una mascota que atiendo y que el tutor lo apruebe desde su cuenta.
- Búsqueda por microchip exacto o email del tutor; el resultado muestra solo nombre de la mascota, especie y nombre del tutor enmascarado (no datos de contacto) para evitar enumeración; límite 20 búsquedas/hora/usuario.
- La solicitud notifica al tutor (email + bandeja en la app); el tutor aprueba (equivale a conceder vínculo) o rechaza; caduca a los 14 días. Máximo 1 solicitud pendiente por (mascota, organización).
- Mientras está pendiente la clínica **no** ve la ficha.

### E13 — Notificaciones

**[PP-F2-PRD-023] (MUST)** Como usuario quiero configurar qué notificaciones recibo.
- Categorías: `appointments`, `preventive_care`, `clinical`, `account` (desactivables), `security` (no desactivable). Canal email en F2. Enlace de "gestionar preferencias" en todos los correos; "darse de baja" de una categoría con un clic (token).

**[PP-F2-PRD-024] (MUST)** Como tutor quiero recibir recordatorios.
- Cita: T-24 h y T-2 h (confirmadas); cambios (confirmación, reprogramación, cancelación) inmediatos.
- Vacuna/preventivo: T-14 d y T-0 respecto a `next_due_at`; si la dosis se registra antes, se cancelan.
- Control recomendado: T-3 d y T-0.
- La clínica recibe: nueva solicitud de cita, cancelación por tutor, solicitud de vínculo aprobada/rechazada, resumen diario de la agenda (opcional, `clinic_admin`).

### E14 — Opcionales

**[PP-F2-PRD-021] (COULD)** Co-tutor: el tutor principal invita a otro usuario como `secondary_guardian` con permisos de lectura y solicitud de citas; no puede transferir ni revocar vínculos.

**[PP-F2-PRD-022] (COULD)** Importación CSV para clínicas piloto: pacientes (crea mascotas "provisionales" asociadas a un email de tutor que recibe invitación para reclamarlas) y vacunas (entradas `recorded` por la clínica importadora con marca `imported`). Plantilla CSV y validación previa con informe de errores.

## 4. Reglas de negocio F2

| Regla | Detalle |
|---|---|
| RN-07 | Solo `veterinarian` firma consultas y adendas; `receptionist` registra constantes, vacunas y preventivos pero no diagnósticos ni plan. |
| RN-08 | Un encuentro firmado no cambia; las correcciones son adendas. |
| RN-09 | Una entrada de carnet no se edita; se anula y se crea otra. |
| RN-10 | La organización que firmó un encuentro conserva acceso de lectura a él aunque el vínculo se revoque. |
| RN-11 | Las citas solicitadas por el tutor no bloquean agenda hasta confirmarse. |
| RN-12 | Un profesional no tiene dos citas confirmadas solapadas. |
| RN-13 | Las entradas `declared` no cuentan como verificación legal (p. ej. rabia para viaje) y se muestran como tales en PDF/enlace. |
| RN-14 | Los datos de contacto del tutor nunca aparecen en enlaces compartidos públicos. |
| RN-15 | Todo acceso a una ficha clínica por un miembro de clínica queda auditado (quién, cuándo, qué mascota). |

## 5. Métricas F2 (piloto)

- Consultas firmadas por clínica activa/semana (objetivo ≥ 15 en clínicas piloto a 8 semanas).
- % de citas confirmadas que terminan en consulta firmada (≥ 70%).
- % de recordatorios entregados antes de `due_at + 60 s` (≥ 99%).
- Retraso p95 de proyección asíncrona (< 2 s).
- % de vacunas con `next_due_at` sin ajuste manual (indicador de calidad del protocolo).

## 6. Fuera de alcance explícito

Facturación, inventario de vacunas/medicamentos, recetas como documento (F4), telemedicina, laboratorio integrado, SMS/WhatsApp, pacientes sin cuenta de tutor (salvo PRD-022).
