package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// AccessProjector actualiza la denormalización de permisos en OpenSearch (DOM-004, ARQ-001).
type AccessProjector struct {
	client  Client
	indexer *Indexer
	access  PetAccessLoader
	pool    *pgxpool.Pool
	logger  *slog.Logger
}

// NewAccessProjector crea un nuevo AccessProjector.
func NewAccessProjector(client Client, indexer *Indexer, access PetAccessLoader, pool *pgxpool.Pool, logger *slog.Logger) *AccessProjector {
	if logger == nil {
		logger = slog.Default()
	}
	return &AccessProjector{
		client:  client,
		indexer: indexer,
		access:  access,
		pool:    pool,
		logger:  logger,
	}
}

// UpdatePetAccess recalcula y actualiza la autorización denormalizada para una mascota en todos los índices.
func (p *AccessProjector) UpdatePetAccess(ctx context.Context, petID uuid.UUID) error {
	acc, err := p.access.GetPetAccess(ctx, petID)
	if err != nil {
		return fmt.Errorf("loading pet access for %s: %w", petID, err)
	}
	if acc == nil {
		return nil
	}

	scope := domain.AccessScope{
		GuardianID: acc.GuardianID.String(),
	}
	for _, u := range acc.LinkedOrgIDs {
		scope.OrgIDsLinked = append(scope.OrgIDsLinked, u.String())
	}
	for _, u := range acc.CustodyOrgIDs {
		scope.OrgIDsCustody = append(scope.OrgIDsCustody, u.String())
	}

	// 1. Reindexar documento de mascota en pets
	if err := p.indexer.ProcessBulkPets(ctx, []string{petID.String()}); err != nil {
		p.logger.WarnContext(ctx, "failed to reindex pet during access update", "pet_id", petID, "error", err)
	}

	// 2. update_by_query en clinical (DOM-004)
	query := map[string]any{
		"term": map[string]any{
			"pet_id": petID.String(),
		},
	}
	script := map[string]any{
		"source": "ctx._source.access = params.access",
		"lang":   "painless",
		"params": map[string]any{
			"access": scope,
		},
	}

	updated, err := p.client.UpdateByQuery(ctx, domain.IndexClinical, query, script)
	if err != nil {
		return fmt.Errorf("update_by_query clinical for pet %s: %w", petID, err)
	}

	p.logger.InfoContext(ctx, "updated access in clinical docs", "pet_id", petID, "updated_count", updated)
	return nil
}

// UpdateOrganizationAccess actualiza el acceso para todas las mascotas de una organización suspendida/reactivada.
func (p *AccessProjector) UpdateOrganizationAccess(ctx context.Context, orgID uuid.UUID) error {
	rows, err := p.pool.Query(ctx, `
		SELECT pet_id FROM read_petregistry.pet_care_links
		WHERE organization_id = $1
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var petIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			petIDs = append(petIDs, id)
		}
	}

	for _, pid := range petIDs {
		_ = p.UpdatePetAccess(ctx, pid)
	}
	return nil
}
