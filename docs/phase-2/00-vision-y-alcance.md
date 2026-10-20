# Fase 2 — Clínica y CQRS-ES (Medical Records)

> Estado: v1.0 en revisión · Fecha: 2026-10-20 · Requiere: Fase 1 cerrada (todos los MUST verificados)
> Tracking: [`docs/tracking/phase-2.md`](../tracking/phase-2.md)

## 1. Objetivo

Convertir PetPassport en una herramienta clínica real:

- La clínica **atiende**: abre una consulta desde una cita o directamente, registra constantes, anamnesis,
  hallazgos, diagnósticos, plan de tratamiento y adjuntos, y **firma**. Lo firmado es inmutable; se amplía con adendas.
- El **carnet de vacunación** y tratamientos preventivos vive en la mascota, lo registran las clínicas, lo
  consulta el tutor, se comparte por enlace/QR y calcula próximas dosis según protocolo.
- **Citas**: el tutor solicita, la clínica confirma/agenda, recordatorios automáticos; agenda por profesional.
- **Event Store v1**: proyecciones asíncronas con checkpoints, snapshots, upcasters, rebuild y proceso `worker`.
- **Notificaciones v0**: emails transaccionales y recordatorios programados, con preferencias.
- La clínica puede **solicitar** el vínculo con una mascota y el tutor lo aprueba (promesa de ADR-004).

Al terminar F2, F3 solo añade OpenSearch/Redis sobre eventos y read models existentes.

## 2. Documentos de la fase

| # | Documento | Área |
|---|---|---|
| 01 | [Producto](01-producto.md) | PRD |
| 02 | [Modelo DDD](02-modelo-ddd.md) | DOM |
| 03 | [Arquitectura: Event Store v1, worker, proyecciones](03-arquitectura.md) | ARQ |
| 04 | [API REST v1 (ampliación)](04-api.md) | API |
| 05 | [Datos y persistencia](05-datos-persistencia.md) | DAT |
| 06 | [Frontend](06-frontend.md) | FE |
| 07 | [Notificaciones y recordatorios](07-notificaciones.md) | NOT |
| 08 | [Seguridad, autorización y privacidad clínica](08-seguridad.md) | SEC |
| 09 | [Infraestructura, observabilidad y calidad](09-infra-observabilidad-calidad.md) | INF / QA |

Nueva área de requisitos en F2: `NOT` (notificaciones). IDs: `PP-F2-<ÁREA>-nnn`.

## 3. Alcance

### Dentro (IN)

- **Clinical Records**: `Encounter` (borrador → firmado → adendas), tipos de atención, constantes vitales, SOAP,
  diagnósticos (texto + código opcional), plan de tratamiento con ítems de medicación estructurados, procedimientos,
  adjuntos, recomendación de control, PDF firmado con hash.
- **Carnet** (`VaccinationCard`): vacunas y preventivos, lotes, protocolo por especie/país, próximas dosis, entradas
  declaradas por el tutor y verificación, anulación con motivo, PDF/QR y enlace compartido de solo lectura.
- **Scheduling**: `Appointment` (solicitud, confirmación, reprogramación, cancelación, check-in, no-show, completada),
  `ProfessionalAgenda` (disponibilidad semanal, excepciones), disponibilidad consultable, vista de agenda.
- **Pet Registry**: solicitud de vínculo por la clínica y aprobación/rechazo del tutor; búsqueda de mascota por
  microchip o email del tutor (resultados mínimos).
- **Event Store v1**: `cmd/worker`, suscripciones catch-up con checkpoints, orden total (advisory lock), snapshots,
  upcasters, rebuild, fail-stop y alertas, consumidor de outbox.
- **Notifications v0**: reglas reactivas y programadas, plantillas es/en, preferencias, dead-letter, métricas.
- **Read models derivados**: línea de tiempo clínica, próximas vacunas, historial de peso, estadísticas básicas de clínica.
- Frontend: espacio clínico completo (agenda, consulta, carnet, ficha), espacio tutor (historial, carnet, citas, recordatorios, compartir).

### Fuera (OUT) — y dónde cae

| Fuera de F2 | Fase |
|---|---|
| Búsqueda full-text en historiales, catálogo de medicamentos buscable | F3 |
| Redis (caché, rate limit distribuido, colas), particionado del worker | F3 |
| Recetas como documento legal, OCR, cotización en farmacias | F4 (los ítems de medicación del plan ya quedan estructurados) |
| SMS / WhatsApp / push | F3 (push) / F5 |
| Facturación de la consulta, inventario de vacunas, planes | F5 |
| Telemedicina, laboratorio integrado, imágenes DICOM | F6 |
| Co-tutores (`SecondaryGuardian`) | COULD en F2 (PRD-021); si no entra, F3 |
| Importación masiva desde otros sistemas | COULD en F2 (PRD-022) |

## 4. Hitos

| Hito | Nombre | Resultado observable |
|---|---|---|
| M2.0 | Event Store v1 | `worker` consume con checkpoints; proyección `medical_timeline` vacía pero operativa; rebuild probado; orden total verificado por test de concurrencia. |
| M2.1 | Encuentros | La clínica abre, edita, firma y añade adendas; PDF firmado; el tutor ve el historial. |
| M2.2 | Carnet | Registro de vacunas/preventivos, protocolos CO, próximas dosis, entradas declaradas, PDF/QR y enlace compartido. |
| M2.3 | Citas | Agenda del profesional, solicitud del tutor, confirmación, reprogramación, check-in → encuentro. |
| M2.4 | Notificaciones | Emails reactivos y recordatorios programados con preferencias; dead-letter y métricas. |
| M2.5 | Vínculo solicitado por clínica + release | Flujo completo de solicitud/aprobación; E2E verdes; criterios de salida; `v0.2.0`. |

## 5. Criterios de salida

1. Todos los `MUST` de `tracking/phase-2.md` en `VERIFICADO`.
2. E2E: cita solicitada → confirmada → check-in → consulta firmada → adenda → vacuna registrada → recordatorio programado visible en `scheduled` → tutor ve todo en su historial y carnet.
3. Rebuild completo de todas las proyecciones asíncronas desde `es.events` en staging (> 10k eventos sintéticos) sin diferencias respecto al estado en vivo (comparación por *dump*).
4. Prueba de concurrencia: 50 escritores concurrentes sobre streams distintos → `global_position` visible en orden estricto para un lector; 0 huecos.
5. Retraso de proyección asíncrona p95 < 2 s en staging bajo carga ligera; retraso de recordatorios < 60 s respecto a `due_at`.
6. Revisión clínica: al menos 2 veterinarios del piloto validan el flujo SOAP y el carnet (sesión documentada).

## 6. Riesgos específicos de F2

| Riesgo | Mitigación |
|---|---|
| Huecos en `global_position` provocan read models incompletos | Advisory lock en Append (ADR-009) + test de concurrencia como *gate*. |
| El formulario clínico no se adapta al flujo real de la consulta | Sesiones con veterinarios piloto en M2.1 antes de cerrar la UI; campos opcionales, plantillas por tipo de atención. |
| Protocolos de vacunación erróneos generan recordatorios equivocados | Catálogo revisado por veterinario; próximas dosis siempre editables por la clínica; recordatorio indica "según protocolo/clínica". |
| Worker como punto único de fallo | Fail-stop con alerta, reinicio automático, métricas de retraso; particionado en F3. |
| Volumen de adjuntos | Límites por encuentro; storage gestionado; antivirus en F3 si hay PDFs de terceros. |
