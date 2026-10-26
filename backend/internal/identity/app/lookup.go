package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// AccountInfo es la vista mínima de una cuenta para otros contextos que la
// consumen por puerto (DOM-010: GuardianLookup/UserLookup). No expone más
// PII de la estrictamente necesaria: el teléfono solo lo leen las clínicas
// con vínculo activo (RN-05, pet:guardian_contact:read).
type AccountInfo struct {
	ID          uuid.UUID
	Email       kernel.Email
	DisplayName string
	Phone       string
	Suspended   bool
}

// AccountInfoById devuelve la cuenta por id (fuente: identity.user_accounts).
func (s *Service) AccountInfoById(ctx context.Context, id uuid.UUID) (AccountInfo, error) {
	acc, err := s.repo.Get(ctx, id)
	if err != nil {
		return AccountInfo{}, err
	}
	return accountInfo(acc.ID, acc.Email, acc.DisplayName, acc.Phone, acc.IsSuspended()), nil
}

// AccountInfoByEmail devuelve la cuenta por email normalizado; email
// inválido o inexistente → *kernel.NotFoundError.
func (s *Service) AccountInfoByEmail(ctx context.Context, email string) (AccountInfo, error) {
	e, err := kernel.NewEmail(email)
	if err != nil {
		return AccountInfo{}, &kernel.NotFoundError{Resource: "user_account"}
	}
	acc, err := s.repo.FindByEmail(ctx, e)
	if err != nil {
		return AccountInfo{}, err
	}
	return accountInfo(acc.ID, acc.Email, acc.DisplayName, acc.Phone, acc.IsSuspended()), nil
}

func accountInfo(id uuid.UUID, email kernel.Email, name string, phone *kernel.Phone, suspended bool) AccountInfo {
	info := AccountInfo{ID: id, Email: email, DisplayName: name, Suspended: suspended}
	if phone != nil {
		info.Phone = phone.String()
	}
	return info
}
