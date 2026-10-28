package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// MinAdminReason es la longitud mínima del motivo de una acción admin (API-008).
const MinAdminReason = 10

// ErrReasonRequired: acción admin sin motivo suficiente (422 REASON_REQUIRED).
var ErrReasonRequired = kernel.NewDomainError("REASON_REQUIRED", "La acción requiere un motivo de al menos 10 caracteres.")

// AdminReason normaliza el motivo: obligatorio (≥ 10) o, si es opcional,
// vacío o ≥ 10 (verify, API-008).
func AdminReason(reason string, required bool) (string, error) {
	r := strings.TrimSpace(reason)
	if r == "" && !required {
		return "", nil
	}
	if len([]rune(r)) < MinAdminReason {
		return "", ErrReasonRequired
	}
	return r, nil
}

type adminAuditKey struct{}

// WithAdminAudit adjunta la entrada de auditoría que el comando debe
// escribir en su misma transacción (TargetID lo completa el servicio).
func WithAdminAudit(ctx context.Context, e audit.Entry) context.Context {
	return context.WithValue(ctx, adminAuditKey{}, e)
}

func (s *Service) recordAdminAudit(ctx context.Context, orgID uuid.UUID) error {
	e, ok := ctx.Value(adminAuditKey{}).(audit.Entry)
	if !ok {
		return nil
	}
	if s.auditor == nil {
		return errors.New("partnernetwork: comando admin sin Auditor configurado")
	}
	e.TargetType, e.TargetID = "organization", &orgID
	return s.auditor.Record(ctx, e)
}

// AdminTransitionCommand construye el comando de dominio de una transición
// admin a partir de la acción (verify/reject/suspend/reactivate).
func AdminTransitionCommand(action string, orgID uuid.UUID, reason string) (bus.Command, error) {
	switch action {
	case "verify":
		return domain.VerifyOrganization{OrganizationID: orgID, Reason: reason}, nil
	case "reject":
		return domain.RejectOrganization{OrganizationID: orgID, Reason: reason}, nil
	case "suspend":
		return domain.SuspendOrganization{OrganizationID: orgID, Reason: reason}, nil
	case "reactivate":
		return domain.ReactivateOrganization{OrganizationID: orgID, Reason: reason}, nil
	}
	return nil, errors.New("partnernetwork: transición admin desconocida " + action)
}

// AdminOrgView es una organización con su fundador (vista admin).
type AdminOrgView struct {
	OrgView
	Founder *UserInfo
}

// AdminListOrganizations lista la cola admin enriquecida con el fundador
// (email y nombre vía UserLookup, ≤ 100 filas por página).
func (s *Service) AdminListOrganizations(ctx context.Context, f AdminOrgFilter) ([]AdminOrgView, *OrgCursor, error) {
	if f.Status == "" {
		f.Status = string(domain.OrgPendingVerification)
	}
	f.Query = strings.TrimSpace(f.Query)
	orgs, next, err := s.queries.AdminListOrgs(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	out := make([]AdminOrgView, len(orgs))
	for i, o := range orgs {
		out[i] = s.AdminOrg(ctx, o)
	}
	return out, next, nil
}

// AdminOrg enriquece una organización con su fundador.
func (s *Service) AdminOrg(ctx context.Context, o OrgView) AdminOrgView {
	v := AdminOrgView{OrgView: o}
	if s.users != nil {
		if info, ok := s.users.Info(ctx, o.FounderID); ok {
			v.Founder = &info
		}
	}
	return v
}
