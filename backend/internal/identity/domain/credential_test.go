package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProfessionalCredential_Lifecycle(t *testing.T) {
	now := time.Now()
	issued := now.Add(-30 * 24 * time.Hour)
	expires := now.Add(365 * 24 * time.Hour)

	userID := uuid.New()
	credID := uuid.New()
	adminID := uuid.New()

	cred, err := NewProfessionalCredential(
		credID,
		userID,
		"co",
		"veterinarian",
		"TP-12345",
		"COMVEZCOL",
		issued,
		&expires,
		"credentials/doc1.pdf",
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error creating credential: %v", err)
	}

	if cred.Country != "CO" {
		t.Errorf("expected country CO, got %s", cred.Country)
	}
	if cred.Status != CredentialStatusPending {
		t.Errorf("expected pending, got %s", cred.Status)
	}
	if !cred.CanBeDeleted() {
		t.Errorf("pending credential should be deletable")
	}
	if cred.IsActive(now) {
		t.Errorf("pending credential should not be active")
	}

	// Verify
	err = cred.Verify(adminID, "Aprobado", now)
	if err != nil {
		t.Fatalf("unexpected error verifying: %v", err)
	}
	if cred.Status != CredentialStatusVerified {
		t.Errorf("expected verified, got %s", cred.Status)
	}
	if !cred.IsActive(now) {
		t.Errorf("verified credential should be active")
	}
	if cred.CanBeDeleted() {
		t.Errorf("verified credential should not be deletable by user")
	}

	// Check expired
	future := expires.Add(24 * time.Hour)
	if cred.IsActive(future) {
		t.Errorf("expired credential should not be active")
	}

	// Reject
	err = cred.Reject(adminID, "Documento ilegible", now)
	if err != nil {
		t.Fatalf("unexpected error rejecting: %v", err)
	}
	if cred.Status != CredentialStatusRejected {
		t.Errorf("expected rejected, got %s", cred.Status)
	}
	if !cred.CanBeDeleted() {
		t.Errorf("rejected credential should be deletable")
	}
}

func TestProfessionalCredential_Validation(t *testing.T) {
	now := time.Now()
	issued := now.Add(-30 * 24 * time.Hour)
	expired := now.Add(-60 * 24 * time.Hour)

	_, err := NewProfessionalCredential(uuid.New(), uuid.New(), "C", "veterinarian", "TP-123", "Issuer", issued, nil, "key", now)
	if err == nil {
		t.Error("expected error with 1-letter country")
	}

	_, err = NewProfessionalCredential(uuid.New(), uuid.New(), "CO", "nurse", "TP-123", "Issuer", issued, nil, "key", now)
	if err == nil {
		t.Error("expected error with invalid kind")
	}

	_, err = NewProfessionalCredential(uuid.New(), uuid.New(), "CO", "veterinarian", "12", "Issuer", issued, nil, "key", now)
	if err == nil {
		t.Error("expected error with short registry number")
	}

	_, err = NewProfessionalCredential(uuid.New(), uuid.New(), "CO", "veterinarian", "12345", "Issuer", issued, &expired, "key", now)
	if err == nil {
		t.Error("expected error when expires before issued")
	}
}
