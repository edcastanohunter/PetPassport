package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AccountInfo es la vista mínima de una cuenta para otros contextos que la
// consumen por puerto (DOM-010: GuardianLookup/UserLookup). No expone más
// PII de la estrictamente necesaria.
type AccountInfo struct {
	ID          uuid.UUID
	Email       kernel.Email
	DisplayName string
	Suspended   bool
}

// AccountInfoById devuelve la cuenta por id (fuente: identity.user_accounts).
func (s *Service) AccountInfoById(ctx context.Context, id uuid.UUID) (AccountInfo, error) {
	acc, err := s.repo.Get(ctx, id)
	if err != nil {
		return AccountInfo{}, err
	}
	return AccountInfo{ID: acc.ID, Email: acc.Email, DisplayName: acc.DisplayName, Suspended: acc.IsSuspended()}, nil
}
