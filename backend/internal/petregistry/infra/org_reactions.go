package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// OrgReactionsProjector reacciona a eventos de Partner Network sobre
// read_petregistry.pet_care_links (DOM-011, misma tx en F1):
//   - OrganizationSuspended / OrganizationReactivated → org_suspended.
//     No se emiten eventos en Pet: el vínculo sigue existiendo.
//   - OrganizationProfileUpdated → nombre y ciudad desnormalizados.
//
// Lee solo los campos que necesita del JSON del evento (ADR-001: Pet
// Registry no importa el dominio de Partner Network). Idempotente.
type OrgReactionsProjector struct{}

var _ projection.Projector = OrgReactionsProjector{}

// Name implementa Projector.
func (OrgReactionsProjector) Name() string { return "petregistry.org_reactions" }

// Handles implementa Projector.
func (OrgReactionsProjector) Handles() []string {
	return []string{"OrganizationSuspended", "OrganizationReactivated", "OrganizationProfileUpdated"}
}

// orgEvent es la parte del payload que interesa (v1).
type orgEvent struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Changes        struct {
		DisplayName *string `json:"display_name"`
		Address     *struct {
			City string `json:"city"`
		} `json:"address"`
	} `json:"changes"`
}

// Project implementa Projector.
func (OrgReactionsProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	if e.SchemaVersion != 1 {
		return fmt.Errorf("org reactions: %s@%d no soportado", e.Type, e.SchemaVersion)
	}
	var ev orgEvent
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return fmt.Errorf("org reactions: %w", err)
	}
	switch e.Type {
	case "OrganizationSuspended", "OrganizationReactivated":
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_care_links SET org_suspended = $2 WHERE organization_id = $1`,
			ev.OrganizationID, e.Type == "OrganizationSuspended")
		return err
	case "OrganizationProfileUpdated":
		if ev.Changes.DisplayName == nil && ev.Changes.Address == nil {
			return nil
		}
		var city *string
		if ev.Changes.Address != nil {
			city = &ev.Changes.Address.City
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_petregistry.pet_care_links SET
			  org_display_name = COALESCE($2, org_display_name),
			  org_city = COALESCE($3, org_city)
			 WHERE organization_id = $1`, ev.OrganizationID, ev.Changes.DisplayName, city)
		return err
	}
	return nil
}
