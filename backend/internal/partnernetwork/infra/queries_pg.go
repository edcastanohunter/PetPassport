package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra/orgqueries"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Queries implementa app.Queries con sqlc sobre read_partnernetwork (ARQ-009).
type Queries struct {
	q     *orgqueries.Queries
	users app.UserLookup // enriquece miembros con email/nombre (nil = vacío)
}

var _ app.Queries = (*Queries)(nil)

// NewQueries construye las consultas sobre el pool.
func NewQueries(pool *pgxpool.Pool, users app.UserLookup) *Queries {
	return &Queries{q: orgqueries.New(pool), users: users}
}

// ErrOrgNotFound lo devuelve GetOrg cuando el id no existe.
var ErrOrgNotFound = &kernel.NotFoundError{Resource: "organization"}

// GetOrg devuelve la vista completa de la organización.
func (r *Queries) GetOrg(ctx context.Context, id uuid.UUID) (app.OrgView, error) {
	row, err := r.q.GetOrganizationByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.OrgView{}, ErrOrgNotFound
	}
	if err != nil {
		return app.OrgView{}, err
	}
	view := app.OrgView{
		ID: row.ID, Version: row.Version, Type: row.Type,
		LegalName: row.LegalName, DisplayName: row.DisplayName,
		TaxID:   domain.TaxIDPayload{Country: row.TaxCountry, Value: row.TaxValue},
		Contact: domain.ContactPayload{Email: row.ContactEmail, Phone: row.ContactPhone, Website: pgStr(row.Website)},
		City:    row.City, Country: row.Country,
		PublicCode: row.PublicCode, Status: row.Status, StatusReason: pgStr(row.StatusReason),
		FounderID: row.FounderID, MemberCount: int(row.MemberCount),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
	if err := json.Unmarshal(row.Address, &view.Address); err != nil {
		return app.OrgView{}, err
	}
	return view, nil
}

// ListMembers devuelve los miembros activos con email y nombre (UserLookup).
func (r *Queries) ListMembers(ctx context.Context, orgID uuid.UUID) ([]app.MemberView, error) {
	rows, err := r.q.ListActiveMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]app.MemberView, 0, len(rows))
	for _, row := range rows {
		m := app.MemberView{UserID: row.UserID, Role: row.Role, Status: row.Status, JoinedAt: row.JoinedAt.Time}
		if r.users != nil {
			if info, ok := r.users.Info(ctx, row.UserID); ok {
				m.Email, m.DisplayName = info.Email, info.DisplayName
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// ListInvitations devuelve las invitaciones (filtro opcional por estado
// derivado; las pendientes vencidas se informan como expired).
func (r *Queries) ListInvitations(ctx context.Context, orgID uuid.UUID, status string, now time.Time) ([]app.InvitationView, error) {
	params := orgqueries.ListInvitationsParams{OrganizationID: orgID, Now: tz(now)}
	if status != "" {
		params.Status = &status
	}
	rows, err := r.q.ListInvitations(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]app.InvitationView, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.InvitationView{
			ID: row.ID, OrganizationID: row.OrganizationID, Email: row.Email, Role: row.Role,
			Status: row.Status, ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

// InvitationByTokenHash resuelve una invitación pendiente y vigente por el
// hash del token. Cualquier otra cosa → NotFound (404 sin distinguir).
func (r *Queries) InvitationByTokenHash(ctx context.Context, hash []byte, now time.Time) (app.InvitationView, error) {
	row, err := r.q.GetPendingInvitationByToken(ctx, orgqueries.GetPendingInvitationByTokenParams{TokenHash: hash, Now: tz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return app.InvitationView{}, &kernel.NotFoundError{Resource: "invitation"}
	}
	if err != nil {
		return app.InvitationView{}, err
	}
	return app.InvitationView{
		ID: row.ID, OrganizationID: row.OrganizationID, Email: row.Email, Role: row.Role,
		Status: row.Status, ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time,
	}, nil
}

// SummaryByCode devuelve el resumen público por PublicCode. Solo clínicas
// activas; otra cosa → NotFound para no revelar (PRD-015, PRD-017).
func (r *Queries) SummaryByCode(ctx context.Context, code string) (app.OrgSummary, error) {
	if _, err := domain.NewPublicCode(code); err != nil {
		return app.OrgSummary{}, &kernel.NotFoundError{Resource: "organization"}
	}
	row, err := r.q.GetOrgSummaryByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.OrgSummary{}, &kernel.NotFoundError{Resource: "organization"}
	}
	if err != nil {
		return app.OrgSummary{}, err
	}
	if row.Status != string(domain.OrgActive) || row.Type != string(domain.OrgTypeClinic) {
		return app.OrgSummary{}, &kernel.NotFoundError{Resource: "organization"}
	}
	return app.OrgSummary{ID: row.ID, Type: row.Type, DisplayName: row.DisplayName, City: row.City, Country: row.Country, Status: row.Status}, nil
}

// ActiveClinic resuelve una clínica activa (I5) para el puerto
// OrganizationLookup de Pet Registry. Inexistente o no elegible →
// app.ErrOrgNotEligible.
func (r *Queries) ActiveClinic(ctx context.Context, orgID uuid.UUID) (app.OrgSummary, error) {
	row, err := r.q.GetOrganizationByID(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.OrgSummary{}, app.ErrOrgNotEligible
	}
	if err != nil {
		return app.OrgSummary{}, err
	}
	if row.Type != string(domain.OrgTypeClinic) || row.Status != string(domain.OrgActive) {
		return app.OrgSummary{}, app.ErrOrgNotEligible
	}
	return app.OrgSummary{ID: row.ID, Type: row.Type, DisplayName: row.DisplayName, City: row.City, Country: row.Country, Status: row.Status}, nil
}

// MembershipsOf devuelve las membresías activas de un usuario con tipo y
// estado de la organización.
func (r *Queries) MembershipsOf(ctx context.Context, userID uuid.UUID) ([]app.MembershipRow, error) {
	rows, err := r.q.ListMembershipsOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]app.MembershipRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.MembershipRow{
			OrganizationID: row.OrganizationID, DisplayName: row.DisplayName,
			OrgType: row.Type, Role: row.Role, OrgStatus: row.OrgStatus,
		})
	}
	return out, nil
}

// --------------------------------------------------------------- adaptadores

// MembershipProvider implementa auth.MembershipLoader sobre el read model
// (IAM-008). La caché LRU de 60 s vive en platform/auth.
type MembershipProvider struct {
	q *Queries
}

var _ auth.MembershipLoader = (*MembershipProvider)(nil)

// NewMembershipProvider construye el proveedor.
func NewMembershipProvider(q *Queries) *MembershipProvider { return &MembershipProvider{q: q} }

// Memberships implementa auth.MembershipLoader.
func (m *MembershipProvider) Memberships(ctx context.Context, userID uuid.UUID) ([]auth.Membership, error) {
	rows, err := m.q.MembershipsOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]auth.Membership, 0, len(rows))
	for _, r := range rows {
		out = append(out, auth.Membership{
			OrganizationID: r.OrganizationID, DisplayName: r.DisplayName,
			OrgType: r.OrgType, Role: r.Role, OrgStatus: r.OrgStatus,
		})
	}
	return out, nil
}

// PublicCodes gestiona la reserva de códigos públicos (O5, DAT-006) en la tx
// del comando.
type PublicCodes struct{}

var _ app.PublicCodeReservations = PublicCodes{}

// Reserve inserta la reserva. Idempotente para la misma org;
// app.ErrPublicCodeTaken si la tiene otra.
func (PublicCodes) Reserve(ctx context.Context, code string, orgID uuid.UUID) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	q := orgqueries.New(tx)
	n, err := q.ReservePublicCode(ctx, orgqueries.ReservePublicCodeParams{Code: code, OrganizationID: orgID})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	owner, err := q.GetPublicCodeOwner(ctx, code)
	if err != nil {
		return err
	}
	if owner != orgID {
		return app.ErrPublicCodeTaken
	}
	return nil
}

// InvitationTokens implementa app.InvitationTokens sobre el read model.
type InvitationTokens struct {
	q *orgqueries.Queries
}

var _ app.InvitationTokens = (*InvitationTokens)(nil)

// NewInvitationTokens construye el adaptador.
func NewInvitationTokens(pool *pgxpool.Pool) *InvitationTokens {
	return &InvitationTokens{q: orgqueries.New(pool)}
}

// Rotate sustituye el token y cuenta el reenvío de forma atómica (una sola
// sentencia UPDATE … RETURNING).
func (t *InvitationTokens) Rotate(ctx context.Context, orgID, invitationID uuid.UUID, hash []byte, now time.Time) (string, string, error) {
	row, err := t.q.RotateInvitationToken(ctx, orgqueries.RotateInvitationTokenParams{
		TokenHash: hash, Now: tz(now), ID: invitationID, OrganizationID: orgID, MaxResends: domain.MaxResends24h,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		pending, perr := t.q.InvitationIsPending(ctx, orgqueries.InvitationIsPendingParams{ID: invitationID, OrganizationID: orgID, Now: tz(now)})
		if perr != nil {
			return "", "", perr
		}
		if pending {
			return "", "", app.ErrResendLimit
		}
		return "", "", &kernel.NotFoundError{Resource: "invitation"}
	}
	if err != nil {
		return "", "", err
	}
	return row.Email, row.Role, nil
}

// UserAdapter adapta identity.app.Service al puerto app.UserLookup (DOM-010).
type UserAdapter struct {
	svc *identityapp.Service
}

var _ app.UserLookup = (*UserAdapter)(nil)

// NewUserAdapter construye el adaptador.
func NewUserAdapter(svc *identityapp.Service) *UserAdapter { return &UserAdapter{svc: svc} }

// FindByEmail implementa el puerto.
func (a *UserAdapter) FindByEmail(ctx context.Context, email string) (uuid.UUID, bool) {
	info, err := a.svc.AccountInfoByEmail(ctx, email)
	if err != nil {
		return uuid.Nil, false
	}
	return info.ID, true
}

// Info implementa el puerto.
func (a *UserAdapter) Info(ctx context.Context, id uuid.UUID) (app.UserInfo, bool) {
	info, err := a.svc.AccountInfoById(ctx, id)
	if err != nil {
		return app.UserInfo{}, false
	}
	return app.UserInfo{Email: info.Email.String(), DisplayName: info.DisplayName}, true
}

// helpers --------------------------------------------------------------------

func pgStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func tz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
