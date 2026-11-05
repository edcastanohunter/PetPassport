package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
)

// CatalogService coordina las operaciones del catálogo de medicamentos.
type CatalogService struct {
	repo MedicationRepository
}

// NewCatalogService construye una nueva instancia del servicio.
func NewCatalogService(repo MedicationRepository) *CatalogService {
	return &CatalogService{repo: repo}
}

// CreateMedication crea un medicamento en el catálogo.
func (s *CatalogService) CreateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error) {
	return s.repo.CreateMedication(ctx, actor, m, reason)
}

// UpdateMedication actualiza un medicamento en el catálogo.
func (s *CatalogService) UpdateMedication(ctx context.Context, actor uuid.UUID, m domain.Medication, reason string) (*domain.Medication, error) {
	return s.repo.UpdateMedication(ctx, actor, m, reason)
}

// DiscontinueMedication descontinúa un medicamento.
func (s *CatalogService) DiscontinueMedication(ctx context.Context, actor uuid.UUID, id uuid.UUID, reason string) error {
	return s.repo.DiscontinueMedication(ctx, actor, id, reason)
}

// MergeMedication fusiona un medicamento en otro.
func (s *CatalogService) MergeMedication(ctx context.Context, actor uuid.UUID, sourceID, targetID uuid.UUID, reason string) error {
	return s.repo.MergeMedication(ctx, actor, sourceID, targetID, reason)
}

// GetMedication obtiene un medicamento por ID resolviendo redirección si fue fusionado.
func (s *CatalogService) GetMedication(ctx context.Context, id uuid.UUID) (*domain.Medication, *uuid.UUID, error) {
	return s.repo.GetMedication(ctx, id)
}

// ResolveActiveMedication implementa clinicalapp.MedicationCatalogPort (PP-F3-DOM-006).
func (s *CatalogService) ResolveActiveMedication(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	return s.repo.ResolveActiveMedication(ctx, id)
}

// ListMedications consulta medicamentos aplicando filtros.
func (s *CatalogService) ListMedications(ctx context.Context, filter MedicationFilter) ([]domain.Medication, error) {
	return s.repo.ListMedications(ctx, filter)
}

// CreateSuggestion registra una nueva sugerencia propuesta por un veterinario.
func (s *CatalogService) CreateSuggestion(ctx context.Context, sug domain.MedicationSuggestion) (*domain.MedicationSuggestion, error) {
	return s.repo.CreateSuggestion(ctx, sug)
}

// ListSuggestions lista sugerencias.
func (s *CatalogService) ListSuggestions(ctx context.Context, orgID *uuid.UUID, status *string) ([]domain.MedicationSuggestion, error) {
	return s.repo.ListSuggestions(ctx, orgID, status)
}

// ApproveSuggestion aprueba una sugerencia creando el medicamento.
func (s *CatalogService) ApproveSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, m domain.Medication) (*domain.Medication, error) {
	return s.repo.ApproveSuggestion(ctx, actor, suggestionID, m)
}

// RejectSuggestion rechaza una sugerencia con motivo.
func (s *CatalogService) RejectSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, reason string) error {
	return s.repo.RejectSuggestion(ctx, actor, suggestionID, reason)
}

// SaveImportBatch persiste un lote de importación.
func (s *CatalogService) SaveImportBatch(ctx context.Context, batch domain.ImportBatch, rows []domain.ImportRow) error {
	return s.repo.SaveImportBatch(ctx, batch, rows)
}

// GetImportBatch consulta un lote por id.
func (s *CatalogService) GetImportBatch(ctx context.Context, id uuid.UUID) (*domain.ImportBatch, error) {
	return s.repo.GetImportBatch(ctx, id)
}

// RevertImportBatch revierte un lote de importación.
func (s *CatalogService) RevertImportBatch(ctx context.Context, actor uuid.UUID, batchID uuid.UUID) error {
	return s.repo.RevertImportBatch(ctx, actor, batchID)
}

// UsageStats calcula las métricas de uso del catálogo.
func (s *CatalogService) UsageStats(ctx context.Context, from, to *time.Time) (*UsageStats, error) {
	return s.repo.UsageStats(ctx, from, to)
}
