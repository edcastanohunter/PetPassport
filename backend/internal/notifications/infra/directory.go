// Package infra adapta Notifications a PostgreSQL y SMTP: lectura de los read
// models para resolver destinatarios, escritura de notifications.*, el process
// manager de reglas, los runners de despacho y envío y el canal SMTP.
package infra

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
)

// Querier es lo que comparten *pgxpool.Pool y pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Directory implementa app.Directory sobre los read models. Dentro del
// outbox se construye con la transacción del evento; los read models que
// necesita son proyecciones síncronas, ya escritas en la tx del comando.
type Directory struct{ q Querier }

var _ app.Directory = (*Directory)(nil)

// NewDirectory construye el directorio.
func NewDirectory(q Querier) *Directory { return &Directory{q: q} }

func one[T any](err error, v T) (T, bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		var zero T
		return zero, false, nil
	}
	if err != nil {
		var zero T
		return zero, false, err
	}
	return v, true, nil
}

// User resuelve una cuenta de identity.user_accounts.
func (d *Directory) User(ctx context.Context, id uuid.UUID) (app.User, bool, error) {
	var u app.User
	err := d.q.QueryRow(ctx,
		`SELECT id, email::text, locale, display_name, coalesce(country,'') FROM identity.user_accounts WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Locale, &u.DisplayName, &u.Country)
	return one(err, u)
}

// Pet resuelve nombre, tutor y estado de la mascota.
func (d *Directory) Pet(ctx context.Context, id uuid.UUID) (app.Pet, bool, error) {
	var p app.Pet
	var status string
	err := d.q.QueryRow(ctx,
		`SELECT id, name, guardian_id, status FROM read_petregistry.pets WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.GuardianID, &status)
	p.Deceased = status == "deceased"
	return one(err, p)
}

// Org resuelve nombre, país y contacto de la organización.
func (d *Directory) Org(ctx context.Context, id uuid.UUID) (app.Org, bool, error) {
	var o app.Org
	err := d.q.QueryRow(ctx,
		`SELECT id, display_name, country, contact_email::text, status FROM read_partnernetwork.organizations WHERE id = $1`, id,
	).Scan(&o.ID, &o.Name, &o.Country, &o.ContactEmail, &o.Status)
	return one(err, o)
}

// Appointment resuelve la cita desde el read model de Scheduling.
func (d *Directory) Appointment(ctx context.Context, id uuid.UUID) (app.Appointment, bool, error) {
	var a app.Appointment
	var tz *string
	err := d.q.QueryRow(ctx,
		`SELECT id, organization_id, pet_id, guardian_id, status, slot_start, tz FROM read_scheduling.appointments WHERE id = $1`, id,
	).Scan(&a.ID, &a.OrgID, &a.PetID, &a.GuardianID, &a.Status, &a.Start, &tz)
	if tz != nil {
		a.TZ = *tz
	}
	return one(err, a)
}

// Encounter resuelve la consulta; el control recomendado solo cuenta si está firmada.
func (d *Directory) Encounter(ctx context.Context, id uuid.UUID) (app.Encounter, bool, error) {
	var e app.Encounter
	var due *string
	err := d.q.QueryRow(ctx,
		`SELECT id, pet_id, organization_id, status,
		        CASE WHEN status = 'signed' THEN follow_up->>'due_date' END
		   FROM read_clinical.encounters WHERE id = $1`, id,
	).Scan(&e.ID, &e.PetID, &e.OrgID, &e.Status, &due)
	if due != nil && *due != "" {
		e.FollowUp = &app.FollowUp{DueDate: *due}
	}
	return one(err, e)
}

// CardEntry resuelve la entrada del carnet y su próxima dosis.
func (d *Directory) CardEntry(ctx context.Context, id uuid.UUID) (app.CardEntry, bool, error) {
	var c app.CardEntry
	var due *string
	err := d.q.QueryRow(ctx,
		`SELECT id, pet_id, status, next_due_date::text FROM read_clinical.card_entries WHERE id = $1`, id,
	).Scan(&c.ID, &c.PetID, &c.Status, &due)
	if due != nil {
		c.NextDue = *due
	}
	return one(err, c)
}

// OrgMembers lista los miembros activos con alguno de los roles.
func (d *Directory) OrgMembers(ctx context.Context, orgID uuid.UUID, roles ...string) ([]uuid.UUID, error) {
	return d.ids(ctx,
		`SELECT user_id FROM read_partnernetwork.org_members
		  WHERE organization_id = $1 AND status = 'active' AND role = ANY($2) ORDER BY user_id`, orgID, roles)
}

// FutureConfirmedGuardians lista los tutores con citas confirmadas futuras.
func (d *Directory) FutureConfirmedGuardians(ctx context.Context, orgID uuid.UUID, now time.Time) ([]uuid.UUID, error) {
	return d.ids(ctx,
		`SELECT DISTINCT guardian_id FROM read_scheduling.appointments
		  WHERE organization_id = $1 AND status = 'confirmed' AND slot_start > $2 ORDER BY guardian_id`, orgID, now)
}

func (d *Directory) ids(ctx context.Context, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := d.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
