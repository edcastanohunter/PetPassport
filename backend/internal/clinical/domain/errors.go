package domain

import "github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"

// Errores de dominio de Encounter (catálogo PP-F2-API-012). Centinelas: se
// comparan con errors.Is y mapean a problem+json vía kernel.DomainError.
var (
	// ErrPetNotAccessible: E1 — la organización no tiene vínculo activo.
	ErrPetNotAccessible = kernel.NewDomainError("PET_NOT_ACCESSIBLE", "La organización no tiene acceso a la mascota.")

	// ErrEncounterNotDraft: E3/E9 — el encuentro no admite edición.
	ErrEncounterNotDraft = kernel.NewDomainError("ENCOUNTER_NOT_DRAFT", "La consulta no está en borrador.")

	// ErrEncounterNotSigned: adenda sobre un encuentro sin firmar.
	ErrEncounterNotSigned = kernel.NewDomainError("ENCOUNTER_NOT_SIGNED", "La consulta no está firmada.")

	// ErrNotAttendingVet: E5 — el firmante no es el profesional responsable.
	ErrNotAttendingVet = kernel.NewDomainError("NOT_ATTENDING_VET", "Solo el profesional responsable puede firmar.")

	// ErrRoleRequiredVeterinarian: E4 — acción clínica por rol no veterinario.
	ErrRoleRequiredVeterinarian = kernel.NewDomainError("ROLE_REQUIRED_VETERINARIAN", "La acción requiere rol veterinario.")

	// ErrAttachmentLimit: ≤ 10 adjuntos por consulta, ≤ 5 por adenda.
	ErrAttachmentLimit = kernel.NewDomainError("ATTACHMENT_LIMIT", "Se alcanzó el máximo de adjuntos.")

	// ErrAddendaLimit: ≤ 20 adendas.
	ErrAddendaLimit = kernel.NewDomainError("ADDENDA_LIMIT", "Se alcanzó el máximo de adendas.")

	// ErrAppointmentNotCheckedIn: abrir desde una cita sin check-in.
	ErrAppointmentNotCheckedIn = kernel.NewDomainError("APPOINTMENT_NOT_CHECKED_IN", "La cita no tiene check-in.")

	// ErrEncounterAlreadyOpened: el stream ya existe.
	ErrEncounterAlreadyOpened = kernel.NewDomainError("ENCOUNTER_ALREADY_OPENED", "La consulta ya existe.")

	// ErrDiscardNotAllowed: descartar sin ser clinic_admin ni el responsable.
	ErrDiscardNotAllowed = kernel.NewDomainError("DISCARD_NOT_ALLOWED", "Solo el administrador o el profesional responsable pueden descartar.")

	// ErrAttachmentNotFound: el adjunto no pertenece al encuentro.
	ErrAttachmentNotFound = kernel.NewDomainError("ATTACHMENT_NOT_FOUND", "El adjunto no existe.")

	// ErrAttendingVetNotEligible: el usuario no es veterinario activo de la org.
	ErrAttendingVetNotEligible = kernel.NewDomainError("ATTENDING_VET_NOT_ELIGIBLE", "El profesional no es veterinario activo de la organización.")

	// ErrNoChanges: el comando no cambia nada; no se emite evento.
	ErrNoChanges = kernel.NewDomainError("NO_CHANGES", "No hay cambios que aplicar.")
)

// IncompleteError es E6/E7 al firmar (422 ENCOUNTER_INCOMPLETE): Missing
// enumera los requisitos pendientes para que la UI marque la checklist
// (FE-004). Valores: attending_vet, content, started_at.
type IncompleteError struct {
	Missing []string
}

func (e *IncompleteError) Error() string { return "ENCOUNTER_INCOMPLETE" }

// Requisitos de firma que puede listar IncompleteError.
const (
	MissingAttendingVet = "attending_vet"
	MissingContent      = "content"
	MissingStartedAt    = "started_at"
)
