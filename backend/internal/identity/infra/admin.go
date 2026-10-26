package infra

import (
	"context"
	"fmt"
	"strings"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

var _ app.AccountDirectory = (*PostgresRepository)(nil)

// likeEscaper escapa los comodines de ILIKE en la búsqueda del usuario.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// maxSearchLimit acota la página de la búsqueda admin.
const maxSearchLimit = 100

// SearchAccounts implementa la búsqueda admin por email o nombre (PRD-018):
// subcadena sin distinguir mayúsculas (índices trigram de 000003/000007),
// orden (created_at, id) descendente con cursor. Consulta parametrizada.
func (r *PostgresRepository) SearchAccounts(ctx context.Context, f app.AccountSearch) ([]*domain.UserAccount, *app.AccountCursor, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = 20
	}
	var q, status *string
	if s := strings.TrimSpace(f.Query); s != "" {
		escaped := likeEscaper.Replace(s)
		q = &escaped
	}
	if f.Status != "" {
		status = &f.Status
	}
	var curAt, curID any
	if f.Cursor != nil {
		curAt, curID = f.Cursor.CreatedAt, f.Cursor.ID
	}
	rows, err := r.pool.Query(ctx, selectAccount+`
		WHERE ($1::text IS NULL OR email::text ILIKE '%' || $1::text || '%' OR display_name ILIKE '%' || $1::text || '%')
		  AND ($2::text IS NULL OR status = $2::text)
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3::timestamptz, $4::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $5`, q, status, curAt, curID, limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("identity search: %w", err)
	}
	defer rows.Close()
	var out []*domain.UserAccount
	for rows.Next() {
		acc, err := scanAccount(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("identity search scan: %w", err)
		}
		out = append(out, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *app.AccountCursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = &app.AccountCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out, next, nil
}

// AuditLog implementa app.AuditLog sobre platform.audit_log.
type AuditLog struct {
	pool *db.Pool
}

var _ app.AuditLog = (*AuditLog)(nil)

// NewAuditLog construye el adaptador.
func NewAuditLog(pool *db.Pool) *AuditLog { return &AuditLog{pool: pool} }

// List delega en platform/audit.
func (a *AuditLog) List(ctx context.Context, f audit.Filter) ([]audit.Row, *audit.Cursor, error) {
	return audit.List(ctx, a.pool, f)
}
