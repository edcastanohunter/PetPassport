package infra

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

var (
	_ projection.SyncResetter = (*PetsProjector)(nil)
	_ projection.SyncResetter = OrgReactionsProjector{}
)

var petTransferColumns = []string{"token_hash", "pet_id", "to_email_hash", "expires_at", "status"}

// ResetForRebuild implementa projection.SyncResetter (rebuild --sync,
// PP-F2-ARQ-006). Datos operativos que no derivan de eventos:
//   - pet_transfers: el hash del token no viaja en los eventos y el replay de
//     cancelaciones antiguas cancelaría la transferencia pendiente actual
//     (UPDATE … WHERE pet_id AND pending): se guarda, se vacía durante el
//     replay y se restaura tal cual.
//   - microchip_reservations.reserved_at (DEFAULT now()): se restaura el
//     instante original de cada reserva.
func (p *PetsProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	rows, err := tx.Query(ctx, `
		SELECT token_hash, pet_id, to_email_hash, expires_at, status FROM read_petregistry.pet_transfers`)
	if err != nil {
		return nil, err
	}
	transfers, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([]any, error) { return r.Values() })
	if err != nil {
		return nil, err
	}
	var (
		chips      []string
		reservedAt []time.Time
	)
	rows, err = tx.Query(ctx, `SELECT microchip, reserved_at FROM read_petregistry.microchip_reservations`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var chip string
		var at time.Time
		if err := rows.Scan(&chip, &at); err != nil {
			rows.Close()
			return nil, err
		}
		chips, reservedAt = append(chips, chip), append(reservedAt, at)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Orden por claves foráneas: pet_care_links referencia pets.
	for _, table := range []string{"pet_transfers", "microchip_reservations", "pet_care_links", "pets"} {
		if _, err := tx.Exec(ctx, `DELETE FROM read_petregistry.`+table); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, tx pgx.Tx) error {
		if len(chips) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE read_petregistry.microchip_reservations m SET reserved_at = k.reserved_at
				  FROM unnest($1::text[], $2::timestamptz[]) AS k(microchip, reserved_at)
				 WHERE m.microchip = k.microchip`, chips, reservedAt); err != nil {
				return err
			}
		}
		if len(transfers) == 0 {
			return nil
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{"read_petregistry", "pet_transfers"},
			petTransferColumns, pgx.CopyFromRows(transfers))
		return err
	}, nil
}

// ResetForRebuild implementa projection.SyncResetter: este proyector solo
// actualiza pet_care_links, cuyo dueño (y reset) es petregistry.pets.
func (OrgReactionsProjector) ResetForRebuild(context.Context, pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	return nil, nil
}

// RegisterSyncProjectors registra los proyectores primarios (síncronos) de
// Pet Registry. Lo usan el api (proyección en la tx del comando) y el worker
// (`rebuild --sync`), para que ambos tengan exactamente el mismo conjunto.
// Registrar después de Partner Network: org_reactions actualiza vínculos.
func RegisterSyncProjectors(r *projection.Runner, reg *eventstore.Registry, guardian app.GuardianLookup, orgs app.OrganizationLookup) {
	r.Register(NewPetsProjector(reg, guardian).WithOrganizations(orgs))
	r.Register(OrgReactionsProjector{})
}
