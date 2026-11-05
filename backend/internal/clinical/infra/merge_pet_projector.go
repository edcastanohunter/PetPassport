package infra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

// MergePetProjectorName es la clave del checkpoint (PP-F3-DOM-007, PP-F3-DOM-008).
const MergePetProjectorName = app.ProjectionMergePet

// MergePetProjector actualiza el pet_id en los read models clínicos cuando una mascota provisional se fusiona.
type MergePetProjector struct{}

var _ projection.AsyncProjector = MergePetProjector{}

// NewMergePetProjector construye el proyector de fusión de mascotas.
func NewMergePetProjector() MergePetProjector { return MergePetProjector{} }

// Name implementa projection.Projector.
func (MergePetProjector) Name() string { return MergePetProjectorName }

// Reset implementa projection.AsyncProjector (no-op para re-enlace).
func (MergePetProjector) Reset(_ context.Context, _ pgx.Tx) error { return nil }

// Handles implementa projection.Projector.
func (MergePetProjector) Handles() []string { return []string{"ProvisionalPetMerged"} }

// Project implementa projection.Projector.
func (MergePetProjector) Project(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent) error {
	var p struct {
		PetID    uuid.UUID `json:"pet_id"`
		TargetID uuid.UUID `json:"target_id"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("merge-pet: payload: %w", err)
	}

	queries := []string{
		`UPDATE read_clinical.encounters SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.encounter_attachments SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.weight_history SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.encounter_custody SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.medical_timeline SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.card_entries SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.cards SET pet_id = $1 WHERE pet_id = $2`,
		`UPDATE read_clinical.vaccination_due SET pet_id = $1 WHERE pet_id = $2`,
	}

	for _, q := range queries {
		if _, err := tx.Exec(ctx, q, p.TargetID, p.PetID); err != nil {
			return fmt.Errorf("merge-pet: exec %s: %w", q, err)
		}
	}

	return nil
}
