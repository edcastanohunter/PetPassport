package infra

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// rawQuerier lo cumplen *pgxpool.Pool y pgx.Tx.
type rawQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// RequestQueries implementa app.CareLinkRequestQueries sobre read_petregistry
// (PRD-020, API-009). Consultas escritas a mano: sqlc no cubre las tablas de M2.5.
type RequestQueries struct{ q rawQuerier }

var _ app.CareLinkRequestQueries = (*RequestQueries)(nil)

// NewRequestQueries construye las consultas.
func NewRequestQueries(q rawQuerier) *RequestQueries { return &RequestQueries{q: q} }

func (r *RequestQueries) lookup(ctx context.Context, where string, arg any, n int) ([]app.PetLookupResult, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, name, species, guardian_display_name
		  FROM read_petregistry.pets
		 WHERE `+where+` AND status = 'active'
		 ORDER BY name, id LIMIT $2`, arg, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.PetLookupResult
	for rows.Next() {
		var p app.PetLookupResult
		if err := rows.Scan(&p.PetID, &p.Name, &p.Species, &p.GuardianDisplayName); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PetsByMicrochip implementa el puerto (coincidencia exacta).
func (r *RequestQueries) PetsByMicrochip(ctx context.Context, microchip string, n int) ([]app.PetLookupResult, error) {
	return r.lookup(ctx, `microchip = $1`, microchip, n)
}

// PetsByGuardian implementa el puerto.
func (r *RequestQueries) PetsByGuardian(ctx context.Context, guardianID uuid.UUID, n int) ([]app.PetLookupResult, error) {
	return r.lookup(ctx, `guardian_id = $1`, guardianID, n)
}

const requestColumns = `c.id, c.pet_id, p.name, c.organization_id, c.org_display_name, c.requested_by,
	coalesce(c.message, ''), c.status, c.expires_at, c.created_at, c.resolved_at`

func scanRequest(row pgx.Row) (app.CareLinkRequestView, error) {
	var v app.CareLinkRequestView
	err := row.Scan(&v.ID, &v.PetID, &v.PetName, &v.OrganizationID, &v.OrgName, &v.RequestedBy,
		&v.Message, &v.Status, &v.ExpiresAt, &v.CreatedAt, &v.ResolvedAt)
	return v, err
}

// GetRequest devuelve una solicitud de una mascota (404 si no existe).
func (r *RequestQueries) GetRequest(ctx context.Context, petID, requestID uuid.UUID) (app.CareLinkRequestView, error) {
	v, err := scanRequest(r.q.QueryRow(ctx, `
		SELECT `+requestColumns+`
		  FROM read_petregistry.care_link_requests c JOIN read_petregistry.pets p ON p.id = c.pet_id
		 WHERE c.id = $1 AND c.pet_id = $2`, requestID, petID))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &kernel.NotFoundError{Resource: "care_link_request"}
	}
	return v, err
}

// GetOrgRequest devuelve una solicitud hecha por una organización.
func (r *RequestQueries) GetOrgRequest(ctx context.Context, orgID, requestID uuid.UUID) (app.CareLinkRequestView, error) {
	v, err := scanRequest(r.q.QueryRow(ctx, `
		SELECT `+requestColumns+`
		  FROM read_petregistry.care_link_requests c JOIN read_petregistry.pets p ON p.id = c.pet_id
		 WHERE c.id = $1 AND c.organization_id = $2`, requestID, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &kernel.NotFoundError{Resource: "care_link_request"}
	}
	return v, err
}

// ListOrgRequests lista las solicitudes de la organización, las más recientes
// primero. Una pendiente vencida por reloj se muestra como expirada aunque el
// scheduler aún no la haya cerrado.
func (r *RequestQueries) ListOrgRequests(ctx context.Context, orgID uuid.UUID, status string, limit int) ([]app.CareLinkRequestView, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.q.Query(ctx, `
		SELECT `+requestColumns+`
		  FROM read_petregistry.care_link_requests c JOIN read_petregistry.pets p ON p.id = c.pet_id
		 WHERE c.organization_id = $1
		   AND ($2 = '' OR c.status = $2 OR ($2 = 'expired' AND c.status = 'pending' AND c.expires_at <= now()))
		 ORDER BY c.created_at DESC, c.id DESC LIMIT $3`, orgID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.CareLinkRequestView{}
	for rows.Next() {
		v, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		if v.Status == "pending" && !v.ExpiresAt.After(time.Now()) {
			v.Status = "expired"
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListInbox devuelve las solicitudes pendientes y vigentes del tutor.
func (r *RequestQueries) ListInbox(ctx context.Context, guardianID uuid.UUID) ([]app.InboxItem, error) {
	rows, err := r.q.Query(ctx, `
		SELECT i.id, i.kind, i.ref_id, i.pet_id, coalesce(p.name, ''), i.organization_id, i.title,
		       coalesce(i.body, ''), i.status, i.created_at, i.expires_at
		  FROM read_petregistry.guardian_inbox i LEFT JOIN read_petregistry.pets p ON p.id = i.pet_id
		 WHERE i.guardian_id = $1 AND i.status = 'pending' AND (i.expires_at IS NULL OR i.expires_at > now())
		 ORDER BY i.created_at DESC, i.id DESC LIMIT 100`, guardianID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.InboxItem{}
	for rows.Next() {
		var it app.InboxItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.RefID, &it.PetID, &it.PetName, &it.OrganizationID, &it.OrgName,
			&it.Body, &it.Status, &it.CreatedAt, &it.ExpiresAt); err != nil {
			return nil, err
		}
		it.Title = it.OrgName
		out = append(out, it)
	}
	return out, rows.Err()
}

// LookupLimiter implementa app.LookupLimiter con una tabla operativa: el
// límite vale entre instancias del api.
type LookupLimiter struct{ q rawQuerier }

var _ app.LookupLimiter = (*LookupLimiter)(nil)

// NewLookupLimiter construye el limitador.
func NewLookupLimiter(q rawQuerier) *LookupLimiter { return &LookupLimiter{q: q} }

// Allow registra el intento (cuenta aunque no haya resultados) y comprueba el
// límite en la ventana. Purga los intentos antiguos del usuario.
func (l *LookupLimiter) Allow(ctx context.Context, userID uuid.UUID, limit int, window time.Duration) (bool, error) {
	if _, err := l.q.Exec(ctx, `
		DELETE FROM read_petregistry.pet_lookup_attempts
		 WHERE user_id = $1 AND attempted_at < now() - interval '1 day'`, userID); err != nil {
		return false, err
	}
	if _, err := l.q.Exec(ctx, `
		INSERT INTO read_petregistry.pet_lookup_attempts (user_id) VALUES ($1)`, userID); err != nil {
		return false, err
	}
	var n int
	if err := l.q.QueryRow(ctx, `
		SELECT count(*) FROM read_petregistry.pet_lookup_attempts
		 WHERE user_id = $1 AND attempted_at > now() - $2::interval`, userID, window.String()).Scan(&n); err != nil {
		return false, err
	}
	return n <= limit, nil
}
