package domain

import (
	"time"

	"github.com/google/uuid"
)

// Comandos del agregado Encounter (docs/phase-2/02 §3.1). Cada comando toca un
// solo agregado (ARQ-003). Los datos que dependen de otros contextos (acceso a
// la mascota, rol del actor en la organización, estado de la cita, hash del
// stream) los resuelve app con sus puertos y llegan aquí como valores, para
// que el dominio sea puro y determinista en pruebas.

// Roles de organización relevantes para Clinical (Partner Network).
const (
	RoleVeterinarian = "veterinarian"
	RoleClinicAdmin  = "clinic_admin"
	RoleReceptionist = "receptionist"
)

// Actor es quien ejecuta el comando: su rol es el que tiene en la
// organización del encuentro (SEC-001: no cualquier membresía). E4 se afirma
// aquí además de en authz.
type Actor struct {
	UserID     uuid.UUID
	OrgRole    string
	SuperAdmin bool
}

func (a Actor) isVet() bool { return a.OrgRole == RoleVeterinarian }

// OpenEncounter crea el encuentro en draft (E1, E2). PetLinked es el
// resultado de PetAccess (vínculo activo; custodia no basta para abrir).
// AppointmentCheckedIn solo se evalúa si AppointmentID != nil.
type OpenEncounter struct {
	EncounterID          uuid.UUID
	PetID                uuid.UUID
	OrganizationID       uuid.UUID
	Type                 string
	Reason               string
	StartedAt            time.Time
	AttendingVetID       *uuid.UUID
	AttendingVetEligible bool // Membership: veterinario activo de la org
	AppointmentID        *uuid.UUID
	AppointmentCheckedIn bool
	PetLinked            bool
	Actor                Actor
	Now                  time.Time
}

// CommandName implementa bus.Command.
func (OpenEncounter) CommandName() string { return "clinical.OpenEncounter" }

// UpdateEncounterDetails cambia tipo, motivo o fecha de inicio (E2, E3).
// Puntero nil = sin cambio.
type UpdateEncounterDetails struct {
	EncounterID uuid.UUID
	Type        *string
	Reason      *string
	StartedAt   *time.Time
	Actor       Actor
	Now         time.Time
}

// CommandName implementa bus.Command.
func (UpdateEncounterDetails) CommandName() string { return "clinical.UpdateEncounterDetails" }

// RecordVitals reemplaza las constantes (E3).
type RecordVitals struct {
	EncounterID uuid.UUID
	Vitals      Vitals
	Actor       Actor
}

// CommandName implementa bus.Command.
func (RecordVitals) CommandName() string { return "clinical.RecordVitals" }

// UpdateNarrative cambia S y/o O (E3). Puntero nil = sin cambio.
type UpdateNarrative struct {
	EncounterID uuid.UUID
	Subjective  *string
	Objective   *Objective
	Actor       Actor
}

// CommandName implementa bus.Command.
func (UpdateNarrative) CommandName() string { return "clinical.UpdateNarrative" }

// SetDiagnoses reemplaza la lista de diagnósticos (E3, E4).
type SetDiagnoses struct {
	EncounterID uuid.UUID
	Diagnoses   []Diagnosis
	Actor       Actor
}

// CommandName implementa bus.Command.
func (SetDiagnoses) CommandName() string { return "clinical.SetDiagnoses" }

// SetPlan reemplaza el plan (E3, E4).
type SetPlan struct {
	EncounterID uuid.UUID
	Plan        Plan
	Actor       Actor
}

// CommandName implementa bus.Command.
func (SetPlan) CommandName() string { return "clinical.SetPlan" }

// RecommendFollowUp fija el control recomendado (E3, E4; DueDate ∈ (hoy, hoy+365]).
type RecommendFollowUp struct {
	EncounterID uuid.UUID
	DueDate     string
	Reason      string
	Actor       Actor
	Now         time.Time
}

// CommandName implementa bus.Command.
func (RecommendFollowUp) CommandName() string { return "clinical.RecommendFollowUp" }

// ClearFollowUp retira el control recomendado (E3, E4).
type ClearFollowUp struct {
	EncounterID uuid.UUID
	Actor       Actor
}

// CommandName implementa bus.Command.
func (ClearFollowUp) CommandName() string { return "clinical.ClearFollowUp" }

// AssignAttendingVet asigna el responsable (E3). Eligible = Membership dice
// que es veterinario activo de la organización.
type AssignAttendingVet struct {
	EncounterID uuid.UUID
	UserID      uuid.UUID
	Eligible    bool
	Actor       Actor
}

// CommandName implementa bus.Command.
func (AssignAttendingVet) CommandName() string { return "clinical.AssignAttendingVet" }

// AddAttachment añade un adjunto ya subido a storage (E3, ≤ 10).
type AddAttachment struct {
	EncounterID uuid.UUID
	Attachment  Attachment
	Actor       Actor
}

// CommandName implementa bus.Command.
func (AddAttachment) CommandName() string { return "clinical.AddAttachment" }

// RemoveAttachment quita un adjunto en borrador (E3).
type RemoveAttachment struct {
	EncounterID  uuid.UUID
	AttachmentID uuid.UUID
	Actor        Actor
}

// CommandName implementa bus.Command.
func (RemoveAttachment) CommandName() string { return "clinical.RemoveAttachment" }

// SetTutorVisibility cambia la visibilidad de un adjunto para el tutor en
// cualquier estado (E4, E9). Ocultar exige motivo (SEC-003).
type SetTutorVisibility struct {
	EncounterID  uuid.UUID
	AttachmentID uuid.UUID
	Visible      bool
	Reason       string
	Actor        Actor
}

// CommandName implementa bus.Command.
func (SetTutorVisibility) CommandName() string { return "clinical.SetTutorVisibility" }

// SignEncounter firma (E3, E5, E6, E7). StreamVersion y ContentHash los
// calcula el repositorio sobre los eventos persistidos hasta la versión
// actual (E8); el agregado exige que StreamVersion coincida con la suya.
type SignEncounter struct {
	EncounterID   uuid.UUID
	StreamVersion int64
	ContentHash   string
	Actor         Actor
	Now           time.Time
}

// CommandName implementa bus.Command.
func (SignEncounter) CommandName() string { return "clinical.SignEncounter" }

// AddAddendum añade una adenda firmada (E9; E4 veterinario de la org).
type AddAddendum struct {
	EncounterID uuid.UUID
	Text        string
	Reason      string
	Attachments []Attachment
	Actor       Actor
	Now         time.Time
}

// CommandName implementa bus.Command.
func (AddAddendum) CommandName() string { return "clinical.AddAddendum" }

// DiscardEncounter descarta un borrador nunca firmado (E3) por clinic_admin,
// el responsable o super_admin (SEC-001).
type DiscardEncounter struct {
	EncounterID uuid.UUID
	Reason      string
	Actor       Actor
}

// CommandName implementa bus.Command.
func (DiscardEncounter) CommandName() string { return "clinical.DiscardEncounter" }
