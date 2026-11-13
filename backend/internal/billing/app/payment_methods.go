package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// ErrAccountNotFound indica que la cuenta de facturación no existe.
var ErrAccountNotFound = errors.New("billing: la cuenta de facturación no existe")

// AttachPaymentMethodCmd lleva el token de un solo uso del widget del proveedor
// y las aceptaciones que el usuario vio en él. Nunca datos de tarjeta (SEC-001).
type AttachPaymentMethodCmd struct {
	ProviderToken   string
	AcceptanceToken string
	PersonalAuth    string
}

// SetupPaymentMethod devuelve la configuración pública del widget del proveedor
// de pagos del país de la cuenta (PP-F5-API-004).
func (s *Service) SetupPaymentMethod(ctx context.Context, accountID uuid.UUID, kind string) (providers.SetupConfig, error) {
	acc, err := s.queries.GetAccount(ctx, accountID)
	if err != nil {
		return providers.SetupConfig{}, err
	}
	if acc == nil {
		return providers.SetupConfig{}, ErrAccountNotFound
	}
	prov, err := s.router.Payment(ctx, acc.Country, false)
	if err != nil {
		return providers.SetupConfig{}, err
	}
	return prov.SetupPaymentMethod(ctx, providers.SetupRequest{AccountID: accountID, Kind: kind, Email: acc.LegalEntity.Email})
}

// AttachPaymentMethod cambia el token del widget por un método reutilizable en
// el proveedor y lo deja como método por defecto de la cuenta (PP-F5-API-004).
// La llamada al proveedor ocurre fuera de la transacción; si guardar falla, el
// método queda huérfano en el proveedor pero nunca se cobra (no está referenciado).
func (s *Service) AttachPaymentMethod(ctx context.Context, accountID uuid.UUID, cmd AttachPaymentMethodCmd) (domain.PaymentMethodRef, error) {
	acc, err := s.queries.GetAccount(ctx, accountID)
	if err != nil {
		return domain.PaymentMethodRef{}, err
	}
	if acc == nil {
		return domain.PaymentMethodRef{}, ErrAccountNotFound
	}
	prov, err := s.router.Payment(ctx, acc.Country, false)
	if err != nil {
		return domain.PaymentMethodRef{}, err
	}
	pm, err := prov.AttachPaymentMethod(ctx, providers.AttachRequest{
		AccountID: accountID, Email: acc.LegalEntity.Email, ProviderToken: cmd.ProviderToken,
		AcceptanceToken: cmd.AcceptanceToken, PersonalAuth: cmd.PersonalAuth,
	})
	if err != nil {
		return domain.PaymentMethodRef{}, fmt.Errorf("billing: registrar método de pago: %w", err)
	}
	ref := domain.PaymentMethodRef{Provider: prov.Name(), Token: pm.Token, Kind: pm.Kind, ExpiresAt: pm.ExpiresAt}
	if pm.Last4 != "" {
		ref.Last4 = &pm.Last4
	}
	if err := s.SetPaymentMethod(ctx, accountID, ref); err != nil {
		return domain.PaymentMethodRef{}, err
	}
	return ref, nil
}

// RemovePaymentMethod retira el método de pago de la cuenta (PP-F5-API-004):
// solo si ninguna suscripción vigente se cobra con él. El token queda sin
// referencia en PetPassport; revocarlo en el proveedor es responsabilidad de este.
func (s *Service) RemovePaymentMethod(ctx context.Context, accountID uuid.UUID) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		acc, err := s.accountRepo.Load(txCtx, accountID)
		if err != nil {
			return err
		}
		subs, err := s.queries.ListAccountSubscriptions(txCtx, accountID)
		if err != nil {
			return err
		}
		inUse := false
		for _, sub := range subs {
			live := sub.Status != domain.StatusCancelled && sub.Status != domain.StatusExpired
			if live && !sub.ManualBilling {
				inUse = true
			}
		}
		if err := acc.RemovePaymentMethod(inUse, now); err != nil {
			return err
		}
		events, err := s.accountRepo.Save(txCtx, acc)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}
