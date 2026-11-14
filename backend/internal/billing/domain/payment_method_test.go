package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// PP-F5-API-004: el método de pago solo se retira si no respalda una suscripción
// vigente; sin método la operación no emite nada.
func TestPPF5API004_RemovePaymentMethodOnlyWhenNotBackingASubscription(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	org := uuid.New()
	acc, err := OpenBillingAccount(uuid.New(), uuid.New(), LegalEntity{
		Kind: "company", LegalName: "Clínica Veterinaria Central", Address: "Av. Caracas # 45-67", Email: "billing@central.co",
		TaxID: TaxID{Country: "CO", Value: "900123456-7", Kind: "NIT"},
	}, "CO", "COP", &org, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.RemovePaymentMethod(false, now); err != nil || acc.PaymentMethod() != nil {
		t.Fatalf("sin método: err=%v", err)
	}
	last4 := "4242"
	if err := acc.SetPaymentMethod(PaymentMethodRef{Provider: "fake", Token: "tok", Kind: "card", Last4: &last4}, now); err != nil {
		t.Fatal(err)
	}
	if err := acc.RemovePaymentMethod(true, now); !errors.Is(err, ErrPaymentMethodInUse) {
		t.Fatalf("con suscripción vigente: esperaba ErrPaymentMethodInUse, obtuvo %v", err)
	}
	if acc.PaymentMethod() == nil {
		t.Fatal("el método no debía retirarse")
	}
	if err := acc.RemovePaymentMethod(false, now); err != nil {
		t.Fatal(err)
	}
	if acc.PaymentMethod() != nil {
		t.Fatal("el método debía quedar retirado")
	}
}

// PP-F5-PRD-004: PSE no deja token reutilizable (cada cargo se paga con su
// redirect); una tarjeta sin token sigue siendo inválida.
func TestPPF5PRD004_PSEMethodNeedsNoToken(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	org := uuid.New()
	acc, err := OpenBillingAccount(uuid.New(), uuid.New(), LegalEntity{
		Kind: "company", LegalName: "Clínica Veterinaria Central", Address: "Av. Caracas # 45-67", Email: "billing@central.co",
		TaxID: TaxID{Country: "CO", Value: "900123456-7", Kind: "NIT"},
	}, "CO", "COP", &org, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.SetPaymentMethod(PaymentMethodRef{Provider: "wompi", Kind: "card"}, now); !errors.Is(err, ErrPaymentMethodRequired) {
		t.Fatalf("tarjeta sin token: esperaba ErrPaymentMethodRequired, obtuvo %v", err)
	}
	if err := acc.SetPaymentMethod(PaymentMethodRef{Provider: "wompi", Kind: "pse"}, now); err != nil {
		t.Fatal(err)
	}
	if pm := acc.PaymentMethod(); pm == nil || pm.Kind != "pse" || pm.Reusable() {
		t.Fatalf("método = %+v", pm)
	}
}
