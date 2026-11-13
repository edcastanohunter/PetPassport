package http

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Cuentas multi-organización (PP-F5-PRD-008): la cuenta invita a una
// organización existente por su código público y el administrador de esa
// organización acepta; aceptar vincula la organización (OrganizationAttached)
// y cierra la invitación en la misma transacción.

// Invitations es el almacén de invitaciones (infra.InvitationStore).
type Invitations interface {
	OrgByPublicCode(ctx context.Context, code string) (uuid.UUID, string, bool, error)
	Create(ctx context.Context, inv infra.Invitation) error
	Get(ctx context.Context, id uuid.UUID) (*infra.Invitation, error)
	PendingForOrg(ctx context.Context, orgID uuid.UUID) ([]infra.Invitation, error)
	PendingForAccount(ctx context.Context, accountID uuid.UUID) ([]infra.Invitation, error)
	AccountOrganizations(ctx context.Context, accountID uuid.UUID) ([]infra.AccountOrganization, error)
	Resolve(ctx context.Context, id uuid.UUID, status string, by uuid.UUID, now time.Time) error
}

func mapInvitationError(err error) error {
	switch {
	case errors.Is(err, infra.ErrInvitationExists):
		return kernel.NewDomainError("INVITATION_EXISTS", "La organización ya tiene una invitación pendiente de esta cuenta.")
	case errors.Is(err, infra.ErrInvitationNotPending):
		return kernel.NewDomainError("INVITATION_NOT_PENDING", "La invitación ya fue aceptada, rechazada o revocada.")
	default:
		return mapDomainError(err)
	}
}

func toGenInvitation(inv infra.Invitation) gen.BillingOrgInvitation {
	return gen.BillingOrgInvitation{
		Id:               inv.ID,
		AccountId:        inv.AccountID,
		AccountName:      inv.AccountLegalName,
		OrganizationId:   inv.OrganizationID,
		OrganizationName: inv.OrganizationName,
		SeatPool:         gen.BillingOrgInvitationSeatPool(inv.SeatPool),
		Status:           gen.BillingOrgInvitationStatus(inv.Status),
		CreatedAt:        inv.CreatedAt,
	}
}

func toGenInvitations(in []infra.Invitation) []gen.BillingOrgInvitation {
	out := make([]gen.BillingOrgInvitation, 0, len(in))
	for _, inv := range in {
		out = append(out, toGenInvitation(inv))
	}
	return out
}

// ListBillingAccountOrganizations lista las organizaciones de la cuenta y sus invitaciones pendientes.
func (h *BillingHandlers) ListBillingAccountOrganizations(ctx context.Context, req gen.ListBillingAccountOrganizationsRequestObject) (gen.ListBillingAccountOrganizationsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingRead, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	out := gen.ListBillingAccountOrganizations200JSONResponse{
		Data: []gen.BillingAccountOrganization{}, Invitations: []gen.BillingOrgInvitation{},
	}
	if h.invitations == nil {
		return out, nil
	}
	orgs, err := h.invitations.AccountOrganizations(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	for _, o := range orgs {
		out.Data = append(out.Data, gen.BillingAccountOrganization{
			OrganizationId: o.OrganizationID, Name: o.Name,
			SeatPool: gen.BillingAccountOrganizationSeatPool(o.SeatPool), JoinedAt: o.JoinedAt,
		})
	}
	pending, err := h.invitations.PendingForAccount(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out.Invitations = toGenInvitations(pending)
	return out, nil
}

// InviteOrganizationToAccount invita una organización existente a la cuenta.
func (h *BillingHandlers) InviteOrganizationToAccount(ctx context.Context, req gen.InviteOrganizationToAccountRequestObject) (gen.InviteOrganizationToAccountResponseObject, error) {
	p, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id))
	if err != nil {
		return nil, err
	}
	code := ""
	if req.Body != nil {
		code = strings.TrimSpace(req.Body.PublicCode)
	}
	if len(code) < 3 {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "public_code", Code: "REQUIRED"}}}
	}
	seatPool := "own"
	if req.Body.SeatPool != nil {
		seatPool = string(*req.Body.SeatPool)
	}
	if h.invitations == nil {
		return nil, &kernel.NotFoundError{Resource: "Organization"}
	}
	acc, err := h.queries.GetAccount(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.Status != "active" {
		return nil, &kernel.NotFoundError{Resource: "Cuenta de facturación"}
	}
	orgID, orgName, found, err := h.invitations.OrgByPublicCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &kernel.NotFoundError{Resource: "Organization"}
	}
	if billed, err := h.queries.GetAccountByOrg(ctx, orgID); err != nil {
		return nil, err
	} else if billed != nil {
		return nil, kernel.NewDomainError("ORGANIZATION_ALREADY_BILLED", "La organización ya factura con una cuenta.")
	}
	inv := infra.Invitation{
		ID: uuid.New(), AccountID: req.Id, AccountLegalName: acc.LegalEntity.LegalName,
		OrganizationID: orgID, OrganizationName: orgName, SeatPool: seatPool,
		Status: "pending", InvitedBy: p.UserID, CreatedAt: time.Now().UTC(),
	}
	if err := h.invitations.Create(ctx, inv); err != nil {
		return nil, mapInvitationError(err)
	}
	return gen.InviteOrganizationToAccount201JSONResponse(toGenInvitation(inv)), nil
}

// DetachOrganizationFromAccount retira una organización de la cuenta.
func (h *BillingHandlers) DetachOrganizationFromAccount(ctx context.Context, req gen.DetachOrganizationFromAccountRequestObject) (gen.DetachOrganizationFromAccountResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	if h.svc == nil {
		return gen.DetachOrganizationFromAccount204Response{}, nil
	}
	if err := h.svc.DetachOrganization(ctx, req.Id, req.OrgId); err != nil {
		return nil, mapDomainError(err)
	}
	return gen.DetachOrganizationFromAccount204Response{}, nil
}

// ListOrgBillingInvitations lista las invitaciones pendientes que recibió la organización.
func (h *BillingHandlers) ListOrgBillingInvitations(ctx context.Context, req gen.ListOrgBillingInvitationsRequestObject) (gen.ListOrgBillingInvitationsResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingRead, authz.OrgResource{ID: req.OrgId}); err != nil {
		return nil, err
	}
	if h.invitations == nil {
		return gen.ListOrgBillingInvitations200JSONResponse{Data: []gen.BillingOrgInvitation{}}, nil
	}
	pending, err := h.invitations.PendingForOrg(ctx, req.OrgId)
	if err != nil {
		return nil, err
	}
	return gen.ListOrgBillingInvitations200JSONResponse{Data: toGenInvitations(pending)}, nil
}

// pendingInvitation carga la invitación; inexistente → 404.
func (h *BillingHandlers) pendingInvitation(ctx context.Context, id uuid.UUID) (*infra.Invitation, error) {
	if h.invitations == nil {
		return nil, &kernel.NotFoundError{Resource: "Invitation"}
	}
	inv, err := h.invitations.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, &kernel.NotFoundError{Resource: "Invitation"}
	}
	return inv, nil
}

// AcceptBillingInvitation: el administrador de la organización invitada acepta.
func (h *BillingHandlers) AcceptBillingInvitation(ctx context.Context, req gen.AcceptBillingInvitationRequestObject) (gen.AcceptBillingInvitationResponseObject, error) {
	inv, err := h.pendingInvitation(ctx, req.InvitationId)
	if err != nil {
		return nil, err
	}
	// Solo la organización invitada acepta: ser admin de la cuenta que invita no basta.
	p, err := h.authorize(ctx, authz.BillingManage, authz.OrgResource{ID: inv.OrganizationID})
	if err != nil {
		return nil, err
	}
	if inv.Status != "pending" {
		return nil, mapInvitationError(infra.ErrInvitationNotPending)
	}
	if billed, err := h.queries.GetAccountByOrg(ctx, inv.OrganizationID); err != nil {
		return nil, err
	} else if billed != nil {
		return nil, kernel.NewDomainError("ORGANIZATION_ALREADY_BILLED", "La organización ya factura con una cuenta.")
	}
	if h.svc == nil {
		inv.Status = "accepted"
		return gen.AcceptBillingInvitation200JSONResponse(toGenInvitation(*inv)), nil
	}
	now := time.Now().UTC()
	err = h.svc.AttachOrganizationWith(ctx, inv.AccountID, inv.OrganizationID, inv.SeatPool, func(txCtx context.Context) error {
		return h.invitations.Resolve(txCtx, inv.ID, "accepted", p.UserID, now)
	})
	if err != nil {
		return nil, mapInvitationError(err)
	}
	inv.Status = "accepted"
	return gen.AcceptBillingInvitation200JSONResponse(toGenInvitation(*inv)), nil
}

// DeclineBillingInvitation: la organización invitada rechaza o la cuenta revoca.
func (h *BillingHandlers) DeclineBillingInvitation(ctx context.Context, req gen.DeclineBillingInvitationRequestObject) (gen.DeclineBillingInvitationResponseObject, error) {
	inv, err := h.pendingInvitation(ctx, req.InvitationId)
	if err != nil {
		return nil, err
	}
	status := "declined"
	p, err := h.authorize(ctx, authz.BillingManage, authz.OrgResource{ID: inv.OrganizationID})
	if err != nil {
		// No es admin de la organización invitada: puede revocarla la cuenta que invitó.
		if p, err = h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, inv.AccountID)); err != nil {
			return nil, err
		}
		status = "revoked"
	}
	if err := h.invitations.Resolve(ctx, inv.ID, status, p.UserID, time.Now().UTC()); err != nil {
		return nil, mapInvitationError(err)
	}
	inv.Status = status
	return gen.DeclineBillingInvitation200JSONResponse(toGenInvitation(*inv)), nil
}
