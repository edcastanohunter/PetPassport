package infra

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
)

var _ projection.SyncResetter = (*OrgsProjector)(nil)

// ResetForRebuild implementa projection.SyncResetter (rebuild --sync,
// PP-F2-ARQ-006). El token de las invitaciones y sus contadores de reenvío
// no viajan en los eventos (el replay pone un hash aleatorio): se guardan y
// se restauran tras el replay para que las invitaciones pendientes sigan
// funcionando sin reenviarlas.
func (p *OrgsProjector) ResetForRebuild(ctx context.Context, tx pgx.Tx) (func(context.Context, pgx.Tx) error, error) {
	var (
		ids          []uuid.UUID
		hashes       [][]byte
		resends      []int32
		lastSent     []*time.Time
		windowStarts []*time.Time
	)
	rows, err := tx.Query(ctx, `
		SELECT id, token_hash, resend_count, last_sent_at, resend_window_start
		  FROM read_partnernetwork.invitations`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id     uuid.UUID
			hash   []byte
			resend int32
			last   *time.Time
			window *time.Time
		)
		if err := rows.Scan(&id, &hash, &resend, &last, &window); err != nil {
			rows.Close()
			return nil, err
		}
		ids, hashes, resends = append(ids, id), append(hashes, hash), append(resends, resend)
		lastSent, windowStarts = append(lastSent, last), append(windowStarts, window)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Orden por claves foráneas: las tablas hijas antes que organizations.
	for _, table := range []string{"invitations", "org_members", "public_code_reservations", "organizations"} {
		if _, err := tx.Exec(ctx, `DELETE FROM read_partnernetwork.`+table); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, tx pgx.Tx) error {
		if len(ids) == 0 {
			return nil
		}
		_, err := tx.Exec(ctx, `
			UPDATE read_partnernetwork.invitations i
			   SET token_hash = k.token_hash, resend_count = k.resend_count,
			       last_sent_at = k.last_sent_at, resend_window_start = k.resend_window_start
			  FROM unnest($1::uuid[], $2::bytea[], $3::int[], $4::timestamptz[], $5::timestamptz[])
			       AS k(id, token_hash, resend_count, last_sent_at, resend_window_start)
			 WHERE i.id = k.id`, ids, hashes, resends, lastSent, windowStarts)
		return err
	}, nil
}

// RegisterSyncProjectors registra los proyectores primarios (síncronos) de
// Partner Network. Lo usan el api (proyección en la tx del comando) y el
// worker (`rebuild --sync`), para que ambos tengan exactamente el mismo
// conjunto.
func RegisterSyncProjectors(r *projection.Runner, reg *eventstore.Registry) {
	r.Register(NewOrgsProjector(reg))
}
