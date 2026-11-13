package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
)

// Invitaciones de organizaciones a una cuenta de facturación (PP-F5-PRD-008).
// La invitación es un proceso fuera del agregado: aceptarla ejecuta
// AttachOrganization sobre la cuenta en la misma transacción.

var (
	// ErrInvitationExists: ya hay una invitación pendiente para esa organización.
	ErrInvitationExists = errors.New("billing: ya hay una invitación pendiente")
	// ErrInvitationNotPending: la invitación ya se aceptó, rechazó o revocó.
	ErrInvitationNotPending = errors.New("billing: la invitación ya no está pendiente")
)

// Invitation es una fila de billing.org_invitations.
type Invitation struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	AccountLegalName string
	OrganizationID   uuid.UUID
	OrganizationName string
	SeatPool         string
	Status           string
	InvitedBy        uuid.UUID
	CreatedAt        time.Time
}

// InvitationStore persiste las invitaciones.
type InvitationStore struct{ pool *pgxpool.Pool }

// NewInvitationStore construye el almacén.
func NewInvitationStore(pool *pgxpool.Pool) *InvitationStore { return &InvitationStore{pool: pool} }

func (s *InvitationStore) q(ctx context.Context) querier {
	if tx, err := db.TxFrom(ctx); err == nil {
		return tx
	}
	return s.pool
}

// OrgByPublicCode resuelve una organización activa por su código público.
func (s *InvitationStore) OrgByPublicCode(ctx context.Context, code string) (uuid.UUID, string, bool, error) {
	if s == nil || s.pool == nil {
		return uuid.Nil, "", false, nil
	}
	var id uuid.UUID
	var name string
	err := s.q(ctx).QueryRow(ctx,
		`SELECT id, display_name FROM read_partnernetwork.organizations WHERE public_code = upper($1) AND status = 'active'`,
		code).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", false, fmt.Errorf("org by public code: %w", err)
	}
	return id, name, true, nil
}

// Create registra una invitación pendiente.
func (s *InvitationStore) Create(ctx context.Context, inv Invitation) error {
	if s == nil || s.pool == nil {
		return nil
	}
	_, err := s.q(ctx).Exec(ctx,
		`INSERT INTO billing.org_invitations (id, account_id, organization_id, seat_pool, status, invited_by, created_at)
		 VALUES ($1, $2, $3, $4, 'pending', $5, $6)`,
		inv.ID, inv.AccountID, inv.OrganizationID, inv.SeatPool, inv.InvitedBy, inv.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrInvitationExists
	}
	if err != nil {
		return fmt.Errorf("create invitation: %w", err)
	}
	return nil
}

const invitationSelect = `
SELECT i.id, i.account_id, coalesce(a.legal_entity->>'legal_name', ''), i.organization_id,
       coalesce(o.display_name, ''), i.seat_pool, i.status, i.invited_by, i.created_at
  FROM billing.org_invitations i
  JOIN billing.accounts a ON a.id = i.account_id
  LEFT JOIN read_partnernetwork.organizations o ON o.id = i.organization_id`

const (
	pendingForOrgSQL     = invitationSelect + ` WHERE i.organization_id = $1 AND i.status = 'pending' ORDER BY i.created_at`
	pendingForAccountSQL = invitationSelect + ` WHERE i.account_id = $1 AND i.status = 'pending' ORDER BY i.created_at`
	invitationByIDSQL    = invitationSelect + ` WHERE i.id = $1`
)

func scanInvitation(row pgx.Row) (Invitation, error) {
	var inv Invitation
	err := row.Scan(&inv.ID, &inv.AccountID, &inv.AccountLegalName, &inv.OrganizationID,
		&inv.OrganizationName, &inv.SeatPool, &inv.Status, &inv.InvitedBy, &inv.CreatedAt)
	return inv, err
}

func (s *InvitationStore) list(ctx context.Context, sql string, arg uuid.UUID) ([]Invitation, error) {
	rows, err := s.q(ctx).Query(ctx, sql, arg)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// PendingForOrg lista las invitaciones pendientes que recibió una organización.
func (s *InvitationStore) PendingForOrg(ctx context.Context, orgID uuid.UUID) ([]Invitation, error) {
	if s == nil || s.pool == nil {
		return []Invitation{}, nil
	}
	return s.list(ctx, pendingForOrgSQL, orgID)
}

// PendingForAccount lista las invitaciones pendientes que envió una cuenta.
func (s *InvitationStore) PendingForAccount(ctx context.Context, accountID uuid.UUID) ([]Invitation, error) {
	if s == nil || s.pool == nil {
		return []Invitation{}, nil
	}
	return s.list(ctx, pendingForAccountSQL, accountID)
}

// AccountOrganization es una organización vinculada a la cuenta.
type AccountOrganization struct {
	OrganizationID uuid.UUID
	Name           string
	SeatPool       string
	JoinedAt       time.Time
}

// AccountOrganizations lista las organizaciones vinculadas a la cuenta.
func (s *InvitationStore) AccountOrganizations(ctx context.Context, accountID uuid.UUID) ([]AccountOrganization, error) {
	if s == nil || s.pool == nil {
		return []AccountOrganization{}, nil
	}
	rows, err := s.q(ctx).Query(ctx,
		`SELECT ao.organization_id, coalesce(o.display_name, ''), ao.seat_pool, ao.joined_at
		   FROM billing.account_organizations ao
		   LEFT JOIN read_partnernetwork.organizations o ON o.id = ao.organization_id
		  WHERE ao.account_id = $1 ORDER BY ao.joined_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("account organizations: %w", err)
	}
	defer rows.Close()
	out := []AccountOrganization{}
	for rows.Next() {
		var o AccountOrganization
		if err := rows.Scan(&o.OrganizationID, &o.Name, &o.SeatPool, &o.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Get devuelve una invitación en cualquier estado (nil si no existe).
func (s *InvitationStore) Get(ctx context.Context, id uuid.UUID) (*Invitation, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	inv, err := scanInvitation(s.q(ctx).QueryRow(ctx, invitationByIDSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invitation: %w", err)
	}
	return &inv, nil
}

// Resolve cierra una invitación pendiente (accepted | declined | revoked).
func (s *InvitationStore) Resolve(ctx context.Context, id uuid.UUID, status string, by uuid.UUID, now time.Time) error {
	if s == nil || s.pool == nil {
		return nil
	}
	tag, err := s.q(ctx).Exec(ctx,
		`UPDATE billing.org_invitations SET status = $2, resolved_by = $3, resolved_at = $4
		  WHERE id = $1 AND status = 'pending'`, id, status, by, now)
	if err != nil {
		return fmt.Errorf("resolve invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvitationNotPending
	}
	return nil
}
