// Package clinical es el contexto Clinical Records (PP-F2-DOM-002,
// docs/phase-2/02 §2–3): Encounter y VaccinationCard (event-sourced),
// RecordShare y catálogos (CRUD), y los read models derivados
// (medical_timeline, weight_history, vaccination_due, encounter_custody).
//
// Mapa de contextos (reglas de dependencia en .golangci.yml, ADR-001):
//   - Pet Registry → Clinical: puerto PetAccess (app/ports.go) sobre
//     read_petregistry; eventos CareLink* y PetGuardianshipTransferred se
//     consumen como JSON publicado, nunca importando petregistry/domain.
//   - Partner Network → Clinical: puerto Membership.
//   - Clinical → Scheduling: EncounterSigned{appointment_id} lo consume el
//     process manager scheduling.complete-on-sign (outbox).
//   - Clinical → Notifications: eventos vía outbox (07-notificaciones).
//
// M2.0 crea el módulo con la proyección asíncrona clinical.medical_timeline;
// los agregados llegan en M2.1 (Encounter) y M2.2 (VaccinationCard).
package clinical
