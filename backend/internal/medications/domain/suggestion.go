package domain

import (
	"time"

	"github.com/google/uuid"
)

// SuggestionStatus es el estado de una sugerencia de medicamento propuesta por un veterinario.
type SuggestionStatus string

const (
	SuggestionPending  SuggestionStatus = "pending"
	SuggestionApproved SuggestionStatus = "approved"
	SuggestionRejected SuggestionStatus = "rejected"
)

// MedicationSuggestion es una sugerencia de alta de medicamento propuesta por un veterinario.
type MedicationSuggestion struct {
	ID             uuid.UUID        `json:"id"`
	ProposedBy     uuid.UUID        `json:"proposed_by"`
	OrganizationID uuid.UUID        `json:"organization_id"`
	Name           string           `json:"name"`
	Notes          *string          `json:"notes,omitempty"`
	PhotoObjectKey *string          `json:"photo_object_key,omitempty"`
	Status         SuggestionStatus `json:"status"`
	ReviewedBy     *uuid.UUID       `json:"reviewed_by,omitempty"`
	Reason         *string          `json:"reason,omitempty"`
	MedicationID   *uuid.UUID       `json:"medication_id,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	ReviewedAt     *time.Time       `json:"reviewed_at,omitempty"`
}

// ImportBatch representa un lote de importación masiva de medicamentos.
type ImportBatch struct {
	ID         uuid.UUID  `json:"id"`
	Source     string     `json:"source"`
	Status     string     `json:"status"` // "completed", "reverted", "failed"
	DryRun     bool       `json:"dry_run"`
	Created    int        `json:"created"`
	Updated    int        `json:"updated"`
	Skipped    int        `json:"skipped"`
	Errors     []string   `json:"errors,omitempty"`
	CreatedBy  uuid.UUID  `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	RevertedAt *time.Time `json:"reverted_at,omitempty"`
}

// ImportRow registra la acción aplicada a un medicamento dentro de un lote para reversibilidad.
type ImportRow struct {
	BatchID      uuid.UUID `json:"batch_id"`
	MedicationID uuid.UUID `json:"medication_id"`
	Action       string    `json:"action"` // "created", "updated"
	Previous     []byte    `json:"previous,omitempty"`
}
