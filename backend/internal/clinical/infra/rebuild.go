package infra

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

var _ projection.SyncResetter = (*EncountersProjector)(nil)

// ResetForRebuild implementa projection.SyncResetter (rebuild --sync,
// PP-F2-ARQ-006). Vacía encounters y encounter_attachments; no toca los
// datos operativos que no derivan de eventos:
//   - document_jobs: estado y objeto de los PDF ya generados; el replay
//     re-encola con ON CONFLICT (kind, ref_id, version) DO NOTHING, así que
//     las versiones existentes se conservan tal cual.
//   - attachment_uploads: adjuntos pendientes de adenda.
func (p *EncountersProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	// Orden por claves foráneas: encounter_attachments referencia encounters.
	for _, table := range []string{"encounter_attachments", "encounters"} {
		if _, err := tx.Exec(ctx, `DELETE FROM read_clinical.`+table); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
