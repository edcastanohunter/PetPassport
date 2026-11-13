package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// BillingAccount representa la raíz de agregado para el titular fiscal y de pago (PP-F5-DOM-003).
// Stream ID: bacct-<id>.
type BillingAccount struct {
	id            uuid.UUID
	version       int64
	ownerUserID   uuid.UUID
	legalEntity   LegalEntity
	country       string
	currency      string
	organizations map[uuid.UUID]AccountOrgMembership
	paymentMethod *PaymentMethodRef
	provider      PaymentProviderRef
	status        string // "active" | "closed"

	changes []any
}

// NewBillingAccount inicializa una instancia vacía para rehidratación.
func NewBillingAccount() *BillingAccount {
	return &BillingAccount{
		organizations: make(map[uuid.UUID]AccountOrgMembership),
		status:        "active",
	}
}

// ID devuelve el identificador de la cuenta.
func (a *BillingAccount) ID() uuid.UUID { return a.id }

// Version devuelve la versión actual del agregado.
func (a *BillingAccount) Version() int64 { return a.version }

// OwnerUserID devuelve el usuario titular de la cuenta.
func (a *BillingAccount) OwnerUserID() uuid.UUID { return a.ownerUserID }

// LegalEntity devuelve los datos fiscales de la cuenta.
func (a *BillingAccount) LegalEntity() LegalEntity { return a.legalEntity }

// Country devuelve el país fiscal.
func (a *BillingAccount) Country() string { return a.country }

// Currency devuelve la moneda configurada.
func (a *BillingAccount) Currency() string { return a.currency }

// Organizations devuelve las organizaciones asociadas.
func (a *BillingAccount) Organizations() map[uuid.UUID]AccountOrgMembership {
	res := make(map[uuid.UUID]AccountOrgMembership, len(a.organizations))
	for k, v := range a.organizations {
		res[k] = v
	}
	return res
}

// PaymentMethod devuelve la referencia del método de pago, si existe.
func (a *BillingAccount) PaymentMethod() *PaymentMethodRef { return a.paymentMethod }

// Provider devuelve la referencia del cliente en la pasarela.
func (a *BillingAccount) Provider() PaymentProviderRef { return a.provider }

// Status devuelve el estado de la cuenta ("active" | "closed").
func (a *BillingAccount) Status() string { return a.status }

// Changes devuelve y limpia los eventos no confirmados.
func (a *BillingAccount) Changes() []any {
	c := a.changes
	a.changes = nil
	return c
}

func (a *BillingAccount) recordThat(event any) {
	a.apply(event)
	a.changes = append(a.changes, event)
}

// OpenBillingAccount abre una nueva cuenta de facturación.
func OpenBillingAccount(
	id uuid.UUID,
	ownerUserID uuid.UUID,
	legalEntity LegalEntity,
	country string,
	currency string,
	orgID *uuid.UUID,
	now time.Time,
) (*BillingAccount, error) {
	if id == uuid.Nil {
		return nil, ErrLegalEntityIncomplete
	}
	if ownerUserID == uuid.Nil {
		return nil, ErrLegalEntityIncomplete
	}
	if strings.TrimSpace(legalEntity.LegalName) == "" || strings.TrimSpace(legalEntity.TaxID.Value) == "" {
		return nil, ErrLegalEntityIncomplete
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	if len(country) != 2 {
		return nil, ErrInvalidTaxID
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		currency = "COP"
	}

	acc := NewBillingAccount()
	acc.recordThat(BillingAccountOpened{
		AccountID:   id,
		OwnerUserID: ownerUserID,
		LegalEntity: legalEntity,
		Country:     country,
		Currency:    currency,
		At:          now.UTC(),
	})

	if orgID != nil && *orgID != uuid.Nil {
		acc.recordThat(OrganizationAttached{
			AccountID:      id,
			OrganizationID: *orgID,
			SeatPool:       "own",
			At:             now.UTC(),
		})
	}

	return acc, nil
}

// UpdateLegalEntity modifica la información fiscal de la cuenta.
func (a *BillingAccount) UpdateLegalEntity(entity LegalEntity, now time.Time) error {
	if a.status == "closed" {
		return ErrAccountClosed
	}
	if strings.TrimSpace(entity.LegalName) == "" || strings.TrimSpace(entity.TaxID.Value) == "" {
		return ErrLegalEntityIncomplete
	}
	a.recordThat(LegalEntityUpdated{
		AccountID:   a.id,
		LegalEntity: entity,
		At:          now.UTC(),
	})
	return nil
}

// AttachOrganization vincula una organización a la cuenta.
func (a *BillingAccount) AttachOrganization(orgID uuid.UUID, seatPool string, now time.Time) error {
	if a.status == "closed" {
		return ErrAccountClosed
	}
	if orgID == uuid.Nil {
		return ErrOrgNotAttached
	}
	if _, exists := a.organizations[orgID]; exists {
		return nil // Ya vinculada, idempotente
	}
	if seatPool == "" {
		seatPool = "own"
	}
	a.recordThat(OrganizationAttached{
		AccountID:      a.id,
		OrganizationID: orgID,
		SeatPool:       seatPool,
		At:             now.UTC(),
	})
	return nil
}

// DetachOrganization retira una organización de la cuenta.
func (a *BillingAccount) DetachOrganization(orgID uuid.UUID, now time.Time) error {
	if a.status == "closed" {
		return ErrAccountClosed
	}
	if _, exists := a.organizations[orgID]; !exists {
		return ErrOrgNotAttached
	}
	if len(a.organizations) == 1 {
		// Una cuenta sin organizaciones queda huérfana: se cierra en su lugar (PRD-008).
		return ErrLastOrganization
	}
	a.recordThat(OrganizationDetached{
		AccountID:      a.id,
		OrganizationID: orgID,
		At:             now.UTC(),
	})
	return nil
}

// SetPaymentMethod vincula un nuevo método de pago a la cuenta.
func (a *BillingAccount) SetPaymentMethod(ref PaymentMethodRef, now time.Time) error {
	if a.status == "closed" {
		return ErrAccountClosed
	}
	if strings.TrimSpace(ref.Token) == "" && ref.Kind != "manual" {
		return ErrPaymentMethodRequired
	}
	a.recordThat(PaymentMethodSet{
		AccountID:     a.id,
		PaymentMethod: ref,
		At:            now.UTC(),
	})
	return nil
}

// RemovePaymentMethod retira el método de pago. Solo si ninguna suscripción
// vigente se cobra con él (todas canceladas o de facturación manual): quitarlo
// dejaría la próxima renovación sin forma de cobro. Idempotente sin método.
func (a *BillingAccount) RemovePaymentMethod(backsLiveSubscription bool, now time.Time) error {
	if a.status == "closed" {
		return ErrAccountClosed
	}
	if a.paymentMethod == nil {
		return nil
	}
	if backsLiveSubscription {
		return ErrPaymentMethodInUse
	}
	a.recordThat(PaymentMethodRemoved{AccountID: a.id, At: now.UTC()})
	return nil
}

// CloseAccount cierra la cuenta siempre que no tenga suscripciones activas.
func (a *BillingAccount) CloseAccount(hasActiveSubscription bool, now time.Time) error {
	if a.status == "closed" {
		return nil
	}
	if hasActiveSubscription {
		return ErrActiveSubscription
	}
	a.recordThat(BillingAccountClosed{
		AccountID: a.id,
		At:        now.UTC(),
	})
	return nil
}

// Apply rehidrata el estado a partir de un evento.
func (a *BillingAccount) Apply(event any) {
	a.apply(event)
}

func (a *BillingAccount) apply(event any) {
	switch e := event.(type) {
	case BillingAccountOpened:
		a.id = e.AccountID
		a.ownerUserID = e.OwnerUserID
		a.legalEntity = e.LegalEntity
		a.country = e.Country
		a.currency = e.Currency
		a.status = "active"
	case LegalEntityUpdated:
		a.legalEntity = e.LegalEntity
	case OrganizationAttached:
		a.organizations[e.OrganizationID] = AccountOrgMembership{
			JoinedAt: e.At,
			SeatPool: e.SeatPool,
		}
	case OrganizationDetached:
		delete(a.organizations, e.OrganizationID)
	case PaymentMethodSet:
		ref := e.PaymentMethod
		a.paymentMethod = &ref
	case PaymentMethodRemoved:
		a.paymentMethod = nil
	case BillingAccountClosed:
		a.status = "closed"
	}
	a.version++
}
