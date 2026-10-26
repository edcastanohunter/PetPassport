package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Querier es lo mínimo que necesita List (pgxpool.Pool o pgx.Tx).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Filter filtra el listado (GET /admin/audit-log, API-008). From incluido,
// To excluido; campos nil = sin filtro.
type Filter struct {
	ActorID  *uuid.UUID
	TargetID *uuid.UUID
	From     *time.Time
	To       *time.Time
	Cursor   *Cursor
	Limit    int
}

// Cursor es la posición (occurred_at, id) de la última entrada devuelta.
type Cursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

// Row es una entrada leída. No incluye ip_hash (no se expone).
type Row struct {
	ID            uuid.UUID
	OccurredAt    time.Time
	ActorID       *uuid.UUID
	ActorRoles    []string
	Action        string
	TargetType    *string
	TargetID      *uuid.UUID
	Reason        *string
	Details       map[string]any
	CorrelationID *uuid.UUID
}

// MaxLimit acota el tamaño de página.
const MaxLimit = 100

// List devuelve una página de entradas, más recientes primero, y el cursor
// de la siguiente (nil si no hay más). Consulta parametrizada: los filtros
// nil se anulan con `IS NULL`.
func List(ctx context.Context, q Querier, f Filter) ([]Row, *Cursor, error) {
	limit := f.Limit
	if limit <= 0 || limit > MaxLimit {
		limit = 20
	}
	var curAt *time.Time
	var curID *uuid.UUID
	if f.Cursor != nil {
		curAt, curID = &f.Cursor.OccurredAt, &f.Cursor.ID
	}
	rows, err := q.Query(ctx, `
		SELECT id, occurred_at, actor_id, actor_roles, action, target_type, target_id, reason, details, correlation_id
		  FROM platform.audit_log
		 WHERE ($1::uuid IS NULL OR actor_id = $1)
		   AND ($2::uuid IS NULL OR target_id = $2)
		   AND ($3::timestamptz IS NULL OR occurred_at >= $3)
		   AND ($4::timestamptz IS NULL OR occurred_at < $4)
		   AND ($5::timestamptz IS NULL OR (occurred_at, id) < ($5, $6::uuid))
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $7`,
		f.ActorID, f.TargetID, f.From, f.To, curAt, curID, limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("audit list: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var details []byte
		if err := rows.Scan(&r.ID, &r.OccurredAt, &r.ActorID, &r.ActorRoles, &r.Action, &r.TargetType,
			&r.TargetID, &r.Reason, &details, &r.CorrelationID); err != nil {
			return nil, nil, fmt.Errorf("audit scan: %w", err)
		}
		if len(details) > 0 {
			if err := json.Unmarshal(details, &r.Details); err != nil {
				return nil, nil, fmt.Errorf("audit details: %w", err)
			}
		}
		if r.ActorRoles == nil {
			r.ActorRoles = []string{}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	return out, next, nil
}
