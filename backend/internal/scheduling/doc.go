// Package scheduling es el contexto Scheduling (PP-F2-DOM-002,
// docs/phase-2/02 §4): Appointment y ProfessionalAgenda (event-sourced) y la
// reserva de franjas con exclusión (ADR-011).
//
// Mapa de contextos (reglas de dependencia en .golangci.yml, ADR-001):
//   - Pet Registry → Scheduling: puerto PetAccess.
//   - Partner Network → Scheduling: puerto Membership.
//   - Scheduling → Clinical: AppointmentCheckedIn habilita abrir consulta.
//   - Clinical → Scheduling: EncounterSigned → PM scheduling.complete-on-sign.
//   - Scheduling → Notifications: Appointment* vía outbox.
//
// El módulo se implementa en M2.3; M2.0 deja el límite del contexto.
package scheduling
