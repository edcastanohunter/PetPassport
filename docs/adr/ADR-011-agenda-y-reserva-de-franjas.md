# ADR-011: `Appointment` como agregado; agenda del profesional como agregado de disponibilidad; no-solapamiento por restricción de exclusión transaccional

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F2

## Contexto

Dos citas del mismo profesional no pueden solaparse. Esa invariante cruza agregados `Appointment`
distintos; en Event Sourcing no se puede garantizar solo con el stream de cada cita. Es el mismo problema que
la unicidad del microchip (ADR-002) con rangos temporales en lugar de valores.

## Decisión

1. **`Appointment`** es raíz de agregado por cita (stream `appt-<id>`): mascota, organización, profesional
   (opcional hasta confirmar), franja `[start, end)`, tipo, origen (tutor o clínica), estado y trazas.
2. **`ProfessionalAgenda`** es raíz de agregado por `(organización, profesional)` (stream `agenda-<orgId>-<userId>`):
   plantilla semanal de disponibilidad, duración de franja por tipo, excepciones (vacaciones, bloqueos) y
   configuración de antelación mínima/máxima. Solo define *cuándo se puede* reservar; no contiene citas.
3. **No-solapamiento** se aplica con la tabla de reserva `read_scheduling.slot_reservations` y una restricción
   de exclusión PostgreSQL:
   ```sql
   EXCLUDE USING gist (professional_id WITH =, during WITH &&) WHERE (active)
   ```
   El handler de `ConfirmAppointment`/`RescheduleAppointment` inserta/actualiza la reserva **en la misma
   transacción** que el `Append`; si la restricción falla → `SLOT_UNAVAILABLE` y la transacción se revierte.
4. La **disponibilidad** (agenda + reservas existentes) se calcula en una consulta (`GET /availability`) sobre el
   read model; es informativa. La autoridad es la restricción de exclusión en el momento de confirmar.
5. Las citas solicitadas por el tutor nacen en `requested` **sin reserva** (no bloquean agenda) hasta que la
   clínica las confirma asignando profesional y franja. La clínica puede crear citas directamente `confirmed`.

## Consecuencias

- Se evita un agregado "Agenda del día" que serializaría todas las citas de la clínica.
- La extensión `btree_gist` pasa a ser requisito de la base de datos.
- Reglas de negocio de la cita (cancelación tardía, no-show) viven en `Appointment`; la política de
  recordatorios vive en Notifications (ADR-012).

## Alternativas consideradas

- Agregado `DailySchedule` por profesional y día conteniendo las citas: coherente, pero obliga a mover citas
  entre agregados al reprogramar y complica la vista del tutor; se descarta.
- Verificar solapamiento leyendo el read model antes de confirmar: condición de carrera; se descarta como
  única medida (se usa solo para la UX de disponibilidad).
