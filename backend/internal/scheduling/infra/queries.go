package infra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	"github.com/edcastanohunter/petpassport/backend/internal/scheduling/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries implementa app.AppointmentQueries y app.AgendaReader sobre
// read_scheduling, con los nombres de mascota, clínica y profesional
// resueltos por JOIN a los read models de los otros contextos.
type Queries struct{ pool *pgxpool.Pool }

var (
	_ app.AppointmentQueries = (*Queries)(nil)
	_ app.AgendaReader       = (*Queries)(nil)
)

// NewQueries construye las consultas.
func NewQueries(pool *pgxpool.Pool) *Queries { return &Queries{pool: pool} }

const apptSelect = `
	SELECT a.id, a.version, a.pet_id, COALESCE(p.name, ''), COALESCE(p.species, ''), a.guardian_id,
	       a.organization_id, COALESCE(o.display_name, ''), a.professional_id, COALESCE(u.display_name, ''),
	       a.type, a.origin, a.status, a.reason, a.preferences, a.slot_start, a.slot_end, COALESCE(a.tz, ''),
	       a.proposal_expires_at, a.encounter_id, a.cancellation, COALESCE(a.cancellation_policy_hours, 0),
	       a.history, a.created_at, a.updated_at
	  FROM read_scheduling.appointments a
	  LEFT JOIN read_petregistry.pets p ON p.id = a.pet_id
	  LEFT JOIN read_partnernetwork.organizations o ON o.id = a.organization_id
	  LEFT JOIN identity.user_accounts u ON u.id = a.professional_id`

func scanAppt(row pgx.Row) (app.AppointmentView, error) {
	var (
		v                      app.AppointmentView
		prefs, cancel, history []byte
		slotStart, slotEnd     *time.Time
	)
	err := row.Scan(&v.ID, &v.Version, &v.PetID, &v.PetName, &v.PetSpecies, &v.GuardianID,
		&v.OrganizationID, &v.OrganizationName, &v.ProfessionalID, &v.ProfessionalName,
		&v.Type, &v.Origin, &v.Status, &v.Reason, &prefs, &slotStart, &slotEnd, &v.TZ,
		&v.ProposalExpiresAt, &v.EncounterID, &cancel, &v.PolicyHours, &history, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if slotStart != nil && slotEnd != nil {
		v.Slot = &domain.Slot{Start: *slotStart, End: *slotEnd}
	}
	if len(prefs) > 0 {
		_ = json.Unmarshal(prefs, &v.Preferences)
	}
	if len(cancel) > 0 && string(cancel) != "null" {
		var c domain.Cancellation
		if json.Unmarshal(cancel, &c) == nil {
			v.Cancellation = &c
		}
	}
	if len(history) > 0 {
		_ = json.Unmarshal(history, &v.History)
	}
	return v, nil
}

// Get devuelve la cita (NotFoundError si no existe).
func (q *Queries) Get(ctx context.Context, id uuid.UUID) (app.AppointmentView, error) {
	v, err := scanAppt(q.pool.QueryRow(ctx, apptSelect+` WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &kernel.NotFoundError{Resource: "appointment"}
	}
	return v, err
}

// PendingRequests cuenta las solicitudes `requested` de la mascota en la organización.
func (q *Queries) PendingRequests(ctx context.Context, petID, orgID uuid.UUID) (int, error) {
	var n int
	err := q.pool.QueryRow(ctx, `
		SELECT count(*) FROM read_scheduling.appointments
		 WHERE pet_id = $1 AND organization_id = $2 AND status = 'requested'`, petID, orgID).Scan(&n)
	return n, err
}

// cursor codifica (instante de orden, id) para la paginación por claves.
type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		ve := &kernel.ValidationError{}
		ve.Add("cursor", "INVALID")
		return c, ve
	}
	return c, nil
}

const (
	defaultLimit = 50
	maxLimit     = 200
	agendaLimit  = 500
)

// List devuelve citas. Con From/To es una agenda: franjas en orden
// ascendente, sin cursor (hasta 500). Sin ellos es una bandeja: orden
// descendente por franja (o creación si aún no hay), con cursor.
func (q *Queries) List(ctx context.Context, f app.ListFilter) ([]app.AppointmentView, string, error) {
	where := " WHERE true"
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if f.OrganizationID != nil {
		where += " AND a.organization_id = " + arg(*f.OrganizationID)
	}
	if f.GuardianID != nil {
		where += " AND a.guardian_id = " + arg(*f.GuardianID)
	}
	if f.PetID != nil {
		where += " AND a.pet_id = " + arg(*f.PetID)
	}
	if f.ProfessionalID != nil {
		where += " AND a.professional_id = " + arg(*f.ProfessionalID)
	}
	if len(f.Statuses) > 0 {
		where += " AND a.status = ANY(" + arg(f.Statuses) + ")"
	}
	agenda := f.From != nil || f.To != nil
	if agenda {
		if f.From != nil {
			where += " AND a.slot_end > " + arg(*f.From)
		}
		if f.To != nil {
			where += " AND a.slot_start < " + arg(*f.To)
		}
		rows, err := q.pool.Query(ctx, apptSelect+where+" ORDER BY a.slot_start ASC, a.id ASC LIMIT "+arg(agendaLimit), args...)
		if err != nil {
			return nil, "", err
		}
		out, err := scanAll(rows)
		return out, "", err
	}
	limit := f.Limit
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	const sortKey = "COALESCE(a.slot_start, a.created_at)"
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		where += fmt.Sprintf(" AND (%s, a.id) < (%s, %s)", sortKey, arg(c.T), arg(c.ID))
	}
	rows, err := q.pool.Query(ctx, apptSelect+where+" ORDER BY "+sortKey+" DESC, a.id DESC LIMIT "+arg(limit+1), args...)
	if err != nil {
		return nil, "", err
	}
	out, err := scanAll(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		t := last.CreatedAt
		if last.Slot != nil {
			t = last.Slot.Start
		}
		next = encodeCursor(cursor{T: t, ID: last.ID})
	}
	return out, next, nil
}

func scanAll(rows pgx.Rows) ([]app.AppointmentView, error) {
	defer rows.Close()
	out := []app.AppointmentView{}
	for rows.Next() {
		v, err := scanAppt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- agendas

const agendaSelect = `
	SELECT organization_id, professional_id, version, tz, weekly_template, slot_minutes, exceptions,
	       lead_time_min_hours, lead_time_max_days, cancellation_policy_hours, active
	  FROM read_scheduling.agendas`

func scanAgenda(row pgx.Row) (*domain.ProfessionalAgenda, error) {
	var (
		org, prof     uuid.UUID
		version       int64
		cfg           domain.AgendaConfig
		tpl, dur, exc []byte
		active        bool
	)
	if err := row.Scan(&org, &prof, &version, &cfg.TimeZone, &tpl, &dur, &exc,
		&cfg.LeadMinHours, &cfg.LeadMaxDays, &cfg.CancelPolicyHrs, &active); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(tpl, &cfg.WeeklyTemplate)
	_ = json.Unmarshal(dur, &cfg.SlotMinutes)
	exceptions := map[string]domain.DayException{}
	_ = json.Unmarshal(exc, &exceptions)
	return domain.RestoreAgenda(org, prof, version, active, cfg, exceptions), nil
}

// Agenda devuelve la agenda proyectada (NotFoundError si no existe).
func (q *Queries) Agenda(ctx context.Context, orgID, professionalID uuid.UUID) (*domain.ProfessionalAgenda, error) {
	a, err := scanAgenda(q.pool.QueryRow(ctx, agendaSelect+` WHERE organization_id = $1 AND professional_id = $2`, orgID, professionalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &kernel.NotFoundError{Resource: "agenda"}
	}
	return a, err
}

// Agendas devuelve todas las agendas de la organización.
func (q *Queries) Agendas(ctx context.Context, orgID uuid.UUID) ([]*domain.ProfessionalAgenda, error) {
	rows, err := q.pool.Query(ctx, agendaSelect+` WHERE organization_id = $1 ORDER BY professional_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.ProfessionalAgenda{}
	for rows.Next() {
		a, err := scanAgenda(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ProfessionalNames resuelve los display_name de los profesionales de las agendas.
func (q *Queries) ProfessionalNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := q.pool.Query(ctx, `SELECT id, display_name FROM identity.user_accounts WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
