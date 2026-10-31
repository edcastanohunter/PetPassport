package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
)

func jsonMarshal(v any) (json.RawMessage, error) { return json.Marshal(v) }

// pgExclusionViolation es el SQLSTATE de una restricción EXCLUDE.
const pgExclusionViolation = "23P01"

// Reservations implementa app.Reservations sobre read_scheduling.slot_reservations,
// cuya restricción de exclusión (ADR-011, DAT-004) es la AUTORIDAD de
// no-solapamiento. Reserve y Release exigen la tx del Unit of Work.
type Reservations struct{ pool *pgxpool.Pool }

var _ app.Reservations = (*Reservations)(nil)

// NewReservations construye el puerto.
func NewReservations(pool *pgxpool.Pool) *Reservations { return &Reservations{pool: pool} }

// Reserve crea o reemplaza (UPSERT por cita) la reserva. Las provisionales
// vencidas del mismo profesional se purgan antes: la restricción no conoce
// expires_at. SLOT_UNAVAILABLE si se solapa con otra cita.
func (r *Reservations) Reserve(ctx context.Context, res app.Reservation) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM read_scheduling.slot_reservations
		 WHERE professional_id = $1 AND kind = 'provisional' AND expires_at <= now() AND appointment_id <> $2`,
		res.ProfessionalID, res.AppointmentID); err != nil {
		return err
	}
	kind := "confirmed"
	if res.Provisional {
		kind = "provisional"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO read_scheduling.slot_reservations (appointment_id, professional_id, during, kind, expires_at)
		VALUES ($1, $2, tstzrange($3, $4, '[)'), $5, $6)
		ON CONFLICT (appointment_id) DO UPDATE
		   SET professional_id = EXCLUDED.professional_id, during = EXCLUDED.during,
		       kind = EXCLUDED.kind, expires_at = EXCLUDED.expires_at`,
		res.AppointmentID, res.ProfessionalID, res.Slot.Start, res.Slot.End, kind, res.ExpiresAt)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == pgExclusionViolation {
		return domain.ErrSlotUnavailable
	}
	return err
}

// Release elimina la reserva de la cita (no-op si no existe).
func (r *Reservations) Release(ctx context.Context, appointmentID uuid.UUID) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM read_scheduling.slot_reservations WHERE appointment_id = $1`, appointmentID)
	return err
}

// Reserved devuelve las reservas vigentes del profesional que tocan [from, to).
func (r *Reservations) Reserved(ctx context.Context, professionalID uuid.UUID, from, to time.Time) ([]domain.Reservation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT lower(during), upper(during) FROM read_scheduling.slot_reservations
		 WHERE professional_id = $1 AND during && tstzrange($2, $3, '[)')
		   AND NOT (kind = 'provisional' AND expires_at <= now())
		 ORDER BY lower(during)`, professionalID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Reservation{}
	for rows.Next() {
		var r domain.Reservation
		if err := rows.Scan(&r.Start, &r.End); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
