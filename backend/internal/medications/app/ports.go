// Package app define los casos de uso y puertos del catálogo de medicamentos (PP-F3-DOM-005, ADR-016).
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
)

// MedicationFilter contiene los criterios de búsqueda y paginación del catálogo.
type MedicationFilter struct {
	Q                    string
	Species              string
	Form                 string
	PrescriptionRequired *bool
	Status               string
	Cursor               string
	Limit                int
}

// UsageStats representa el informe de adopción del catálogo en prescripciones SOAP (PP-F3-PRD-005).
type UsageStats struct {
	ItemsTotal    int               `json:"items_total"`
	WithCatalogID int               `json:"with_catalog_id"`
	Ratio         float64           `json:"ratio"`
	Top           []MedicationUsage `json:"top"`
}

// MedicationUsage representa la métrica individual de uso de un medicamento.
type MedicationUsage struct {
	CatalogID uuid.UUID `json:"catalog_id"`
	Name      string    `json:"name"`
	Count     int       `json:"count"`
}

// MedicationRepository define las operaciones de persistencia del catálogo (puerto).
type MedicationRepository interface {
	CreateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error)
	UpdateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error)
	DiscontinueMedication(ctx context.Context, actor uuid.UUID, id uuid.UUID, reason string) error
	MergeMedication(ctx context.Context, actor uuid.UUID, sourceID, targetID uuid.UUID, reason string) error
	GetMedication(ctx context.Context, id uuid.UUID) (*domain.Medication, *uuid.UUID, error)
	ResolveActiveMedication(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error)
	ListMedications(ctx context.Context, filter MedicationFilter) ([]domain.Medication, error)
	CreateSuggestion(ctx context.Context, sug domain.MedicationSuggestion) (*domain.MedicationSuggestion, error)
	ListSuggestions(ctx context.Context, orgID *uuid.UUID, status *string) ([]domain.MedicationSuggestion, error)
	ApproveSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, m domain.Medication) (*domain.Medication, error)
	RejectSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, reason string) error
	SaveImportBatch(ctx context.Context, batch domain.ImportBatch, rows []domain.ImportRow) error
	GetImportBatch(ctx context.Context, id uuid.UUID) (*domain.ImportBatch, error)
	RevertImportBatch(ctx context.Context, actor uuid.UUID, batchID uuid.UUID) error
	UsageStats(ctx context.Context, from, to *time.Time) (*UsageStats, error)
}
