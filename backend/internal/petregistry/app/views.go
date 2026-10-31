package app

import (
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// PetAggregate es el agregado visto desde app (alias para no exponer el
// paquete domain en las firmas de los puertos más de lo necesario).
type PetAggregate = domain.Pet

// PetView es la mascota proyectada completa (GET /pets/{id}).
type PetView struct {
	ID                  uuid.UUID
	Version             int64
	GuardianID          uuid.UUID
	GuardianDisplayName string
	Name                string
	Species             string
	SpeciesOther        string
	Breed               string
	Sex                 string
	Neutered            string
	BirthDate           *domain.BirthDate
	Markings            string
	Microchip           string
	Corrections         int
	Photo               *domain.PhotoRef
	Status              string
	DeceasedAt          *kernel.CivilDate
	DeceasedMarkedAt    *time.Time
	ActiveLinks         []CareLinkView
	PendingTransfer     *PendingTransferView
	Alerts              []domain.PetAlert
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// PendingTransferView es la transferencia pendiente vista por el tutor
// (email enmascarado, API-004).
type PendingTransferView struct {
	ToEmailMasked string    `json:"to_email_masked"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// TransferView resuelve un token de transferencia (API-004 /pet-transfers).
type TransferView struct {
	PetID     uuid.UUID
	ExpiresAt time.Time
}

// PatientRow es una fila de la lista de pacientes de una clínica (PRD-016).
type PatientRow struct {
	PetID               uuid.UUID
	Name                string
	Species             string
	Breed               string
	Status              string
	BirthDate           *domain.BirthDate
	Photo               *domain.PhotoRef
	GuardianID          uuid.UUID
	GuardianDisplayName string
	GuardianPhone       string
	LinkedAt            time.Time
}

// PatientCursor es el cursor de la lista de pacientes: (granted_at, pet_id).
type PatientCursor struct {
	LinkedAt time.Time
	PetID    uuid.UUID
}

// CareLinkView es un vínculo proyectado.
type CareLinkView struct {
	OrganizationID uuid.UUID
	DisplayName    string
	City           string
	Status         string
	OrgSuspended   bool
	GrantedAt      time.Time
	RevokedAt      *time.Time
}

// PetListItem es la fila de la lista del tutor.
type PetListItem struct {
	ID        uuid.UUID
	Version   int64
	Name      string
	Species   string
	Breed     string
	Photo     *domain.PhotoRef
	Status    string
	BirthDate *domain.BirthDate
	Links     int
	CreatedAt time.Time
}

// PetCursor es el cursor opaco de paginación: (created_at, id) base64.
type PetCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Age calcula la edad (años, meses) desde la fecha de nacimiento (02 §9:
// derivada, nunca almacenada).
func Age(birth kernel.CivilDate, today kernel.CivilDate) (years, months int) {
	b := birth.Time()
	t := today.Time()
	if !b.Before(t) {
		return 0, 0
	}
	years = t.Year() - b.Year()
	months = int(t.Month()) - int(b.Month())
	if t.Day() < b.Day() {
		months--
	}
	if months < 0 {
		years--
		months += 12
	}
	if years < 0 {
		return 0, 0
	}
	return years, months
}
