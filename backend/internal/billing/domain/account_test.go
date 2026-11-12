package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBillingAccount_Lifecycle(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	ownerID := uuid.New()
	orgID := uuid.New()

	validEntity := LegalEntity{
		Kind:      "company",
		LegalName: "Clínica Veterinaria Central",
		TaxID: TaxID{
			Country: "CO",
			Value:   "900123456-7",
			Kind:    "NIT",
		},
		Address: "Av. Caracas # 45-67",
		Email:   "billing@central.co",
	}

	// 1. Validation errors on creation
	_, err := OpenBillingAccount(uuid.Nil, ownerID, validEntity, "CO", "COP", nil, now)
	if err == nil {
		t.Fatal("expected error with nil account id")
	}

	_, err = OpenBillingAccount(accountID, uuid.Nil, validEntity, "CO", "COP", nil, now)
	if err == nil {
		t.Fatal("expected error with nil owner user id")
	}

	invalidEntity := validEntity
	invalidEntity.LegalName = ""
	_, err = OpenBillingAccount(accountID, ownerID, invalidEntity, "CO", "COP", nil, now)
	if err == nil {
		t.Fatal("expected error with empty legal name")
	}

	_, err = OpenBillingAccount(accountID, ownerID, validEntity, "COLOMBIA", "COP", nil, now)
	if err == nil {
		t.Fatal("expected error with invalid country code")
	}

	// 2. Successful creation
	acc, err := OpenBillingAccount(accountID, ownerID, validEntity, "co", "cop", &orgID, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if acc.ID() != accountID || acc.OwnerUserID() != ownerID || acc.Country() != "CO" || acc.Currency() != "COP" {
		t.Fatalf("unexpected account state: %+v", acc)
	}
	if acc.Provider().Name != "" {
		t.Fatalf("expected empty provider name")
	}
	if acc.Status() != "active" {
		t.Fatalf("expected status active, got %s", acc.Status())
	}
	if len(acc.Organizations()) != 1 {
		t.Fatalf("expected 1 organization attached, got %d", len(acc.Organizations()))
	}

	changes := acc.Changes()
	if len(changes) != 2 { // Opened + Attached
		t.Fatalf("expected 2 uncommitted events, got %d", len(changes))
	}

	// 3. Update legal entity
	updatedEntity := validEntity
	updatedEntity.Address = "Nueva dirección # 1-2"
	if err := acc.UpdateLegalEntity(updatedEntity, now); err != nil {
		t.Fatalf("failed to update legal entity: %v", err)
	}
	if acc.LegalEntity().Address != "Nueva dirección # 1-2" {
		t.Fatalf("address not updated")
	}

	invalidEntity2 := validEntity
	invalidEntity2.TaxID.Value = ""
	if err := acc.UpdateLegalEntity(invalidEntity2, now); err == nil {
		t.Fatal("expected error on empty tax id")
	}

	// 4. Attach & Detach organization
	org2 := uuid.New()
	if err := acc.AttachOrganization(org2, "shared", now); err != nil {
		t.Fatalf("failed to attach org2: %v", err)
	}
	if len(acc.Organizations()) != 2 {
		t.Fatalf("expected 2 orgs, got %d", len(acc.Organizations()))
	}
	// Idempotent attach
	if err := acc.AttachOrganization(org2, "shared", now); err != nil {
		t.Fatalf("idempotent attach failed: %v", err)
	}

	if err := acc.DetachOrganization(org2, now); err != nil {
		t.Fatalf("failed to detach org2: %v", err)
	}
	if len(acc.Organizations()) != 1 {
		t.Fatalf("expected 1 org after detach, got %d", len(acc.Organizations()))
	}
	if err := acc.DetachOrganization(org2, now); err == nil {
		t.Fatal("expected error detaching non-attached org")
	}

	// 5. Set payment method
	pm := PaymentMethodRef{
		Provider: "wompi",
		Token:    "tok_123",
		Kind:     "card",
	}
	if err := acc.SetPaymentMethod(pm, now); err != nil {
		t.Fatalf("failed to set payment method: %v", err)
	}
	if acc.PaymentMethod() == nil || acc.PaymentMethod().Token != "tok_123" {
		t.Fatalf("payment method not set")
	}

	// Missing token for card
	if err := acc.SetPaymentMethod(PaymentMethodRef{Kind: "card", Token: ""}, now); err == nil {
		t.Fatal("expected error setting empty token for card")
	}

	// 6. Close account
	if err := acc.CloseAccount(true, now); err != ErrActiveSubscription {
		t.Fatalf("expected ErrActiveSubscription, got %v", err)
	}

	if err := acc.CloseAccount(false, now); err != nil {
		t.Fatalf("failed to close account: %v", err)
	}
	if acc.Status() != "closed" {
		t.Fatalf("expected status closed, got %s", acc.Status())
	}

	// Operations on closed account should fail
	if err := acc.UpdateLegalEntity(validEntity, now); err != ErrAccountClosed {
		t.Fatalf("expected ErrAccountClosed, got %v", err)
	}
	if err := acc.AttachOrganization(uuid.New(), "own", now); err != ErrAccountClosed {
		t.Fatalf("expected ErrAccountClosed, got %v", err)
	}
	if err := acc.DetachOrganization(orgID, now); err != ErrAccountClosed {
		t.Fatalf("expected ErrAccountClosed, got %v", err)
	}
	if err := acc.SetPaymentMethod(pm, now); err != ErrAccountClosed {
		t.Fatalf("expected ErrAccountClosed, got %v", err)
	}

	// 7. Test rehydration via Apply
	rehydrated := NewBillingAccount()
	allEvents := []any{
		BillingAccountOpened{AccountID: accountID, OwnerUserID: ownerID, LegalEntity: validEntity, Country: "CO", Currency: "COP"},
		OrganizationAttached{AccountID: accountID, OrganizationID: orgID, SeatPool: "own"},
		PaymentMethodSet{AccountID: accountID, PaymentMethod: pm},
		BillingAccountClosed{AccountID: accountID},
	}
	for _, e := range allEvents {
		rehydrated.Apply(e)
	}
	if rehydrated.ID() != accountID || rehydrated.Status() != "closed" || rehydrated.Version() != 4 {
		t.Fatalf("rehydration mismatch: ID=%v, Status=%s, Version=%d", rehydrated.ID(), rehydrated.Status(), rehydrated.Version())
	}
}
