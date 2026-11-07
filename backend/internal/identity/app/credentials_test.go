package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/identity/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

type memoryCredentialStore struct {
	creds map[uuid.UUID]*domain.ProfessionalCredential
}

func newMemoryCredentialStore() *memoryCredentialStore {
	return &memoryCredentialStore{creds: make(map[uuid.UUID]*domain.ProfessionalCredential)}
}

func (m *memoryCredentialStore) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domain.ProfessionalCredential, error) {
	var res []domain.ProfessionalCredential
	for _, c := range m.creds {
		if c.UserID == userID {
			res = append(res, *c)
		}
	}
	return res, nil
}

func (m *memoryCredentialStore) GetByID(ctx context.Context, id uuid.UUID) (*domain.ProfessionalCredential, error) {
	c, ok := m.creds[id]
	if !ok {
		return nil, domain.ErrCredentialNotFound
	}
	copy := *c
	return &copy, nil
}

func (m *memoryCredentialStore) GetVerifiedByUserAndCountry(ctx context.Context, userID uuid.UUID, country string) (*domain.ProfessionalCredential, error) {
	for _, c := range m.creds {
		if c.UserID == userID && c.Country == country && c.Status == domain.CredentialStatusVerified {
			copy := *c
			return &copy, nil
		}
	}
	return nil, &kernel.NotFoundError{Resource: "verified_credential"}
}

func (m *memoryCredentialStore) Create(ctx context.Context, cred *domain.ProfessionalCredential) error {
	for _, c := range m.creds {
		if c.UserID == cred.UserID && c.Country == cred.Country &&
			(c.Status == domain.CredentialStatusPending || c.Status == domain.CredentialStatusVerified) {
			return domain.ErrCredentialAlreadyActive
		}
	}
	copy := *cred
	m.creds[cred.ID] = &copy
	return nil
}

func (m *memoryCredentialStore) Update(ctx context.Context, cred *domain.ProfessionalCredential) error {
	if _, ok := m.creds[cred.ID]; !ok {
		return domain.ErrCredentialNotFound
	}
	copy := *cred
	m.creds[cred.ID] = &copy
	return nil
}

func (m *memoryCredentialStore) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	c, ok := m.creds[id]
	if !ok || c.UserID != userID {
		return domain.ErrCredentialNotFound
	}
	delete(m.creds, id)
	return nil
}

func (m *memoryCredentialStore) ListByOrgMembers(ctx context.Context, memberUserIDs []uuid.UUID) ([]MemberCredentialSummary, error) {
	var res []MemberCredentialSummary
	set := make(map[uuid.UUID]bool)
	for _, id := range memberUserIDs {
		set[id] = true
	}
	for _, c := range m.creds {
		if set[c.UserID] {
			res = append(res, MemberCredentialSummary{
				UserID:         c.UserID,
				Country:        c.Country,
				Kind:           c.Kind,
				RegistryNumber: c.RegistryNumber,
				Issuer:         c.Issuer,
				Status:         string(c.Status),
				ExpiresAt:      c.ExpiresAt,
			})
		}
	}
	return res, nil
}

func (m *memoryCredentialStore) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]MemberCredentialSummary, error) {
	return nil, nil
}

func (m *memoryCredentialStore) ListByStatus(ctx context.Context, status string) ([]domain.ProfessionalCredential, error) {
	var res []domain.ProfessionalCredential
	for _, c := range m.creds {
		if status == "" || string(c.Status) == status {
			res = append(res, *c)
		}
	}
	return res, nil
}

func TestCredentialService_SubmitAndVerify(t *testing.T) {
	ctx := context.Background()
	store := newMemoryCredentialStore()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	svc := NewCredentialService(store, nil).WithClock(func() time.Time { return now })

	vetID := uuid.New()
	vetPrincipal := auth.Principal{
		UserID:      vetID,
		GlobalRoles: []string{"pet_owner"},
	}

	adminID := uuid.New()
	adminPrincipal := auth.Principal{
		UserID:      adminID,
		GlobalRoles: []string{"super_admin"},
	}

	// 1. Submit
	cred, err := svc.SubmitCredential(ctx, vetPrincipal, SubmitCredentialCmd{
		Country:           "CO",
		Kind:              "veterinarian",
		RegistryNumber:    "TP-9876",
		Issuer:            "COMVEZCOL",
		IssuedAt:          now.Add(-30 * 24 * time.Hour),
		EvidenceObjectKey: "evidence/vet1.pdf",
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if cred.Status != domain.CredentialStatusPending {
		t.Fatalf("expected pending, got %s", cred.Status)
	}

	// 2. Port CredentialCheck: not verified yet
	ref, verified, err := svc.VerifiedCredential(ctx, vetID, "CO")
	if err != nil {
		t.Fatalf("port error: %v", err)
	}
	if verified || ref != nil {
		t.Fatalf("pending credential must not return verified=true")
	}

	// 3. Admin Verify
	verifiedCred, err := svc.AdminVerifyCredential(ctx, adminPrincipal, cred.ID, "Documento verificado en COMVEZCOL")
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if verifiedCred.Status != domain.CredentialStatusVerified {
		t.Fatalf("expected verified, got %s", verifiedCred.Status)
	}

	// 4. Port CredentialCheck: now verified
	ref, verified, err = svc.VerifiedCredential(ctx, vetID, "CO")
	if err != nil {
		t.Fatalf("port error: %v", err)
	}
	if !verified || ref == nil {
		t.Fatalf("expected verified=true")
	}
	if ref.RegistryNumber != "TP-9876" || ref.Country != "CO" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	// 5. Cannot delete verified credential
	err = svc.DeleteCredential(ctx, vetPrincipal, cred.ID)
	if err == nil {
		t.Fatalf("expected error deleting verified credential")
	}

	// 6. Admin Reject
	rejectedCred, err := svc.AdminRejectCredential(ctx, adminPrincipal, cred.ID, "Suspendido")
	if err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if rejectedCred.Status != domain.CredentialStatusRejected {
		t.Fatalf("expected rejected, got %s", rejectedCred.Status)
	}

	// 7. Rejected can be deleted
	err = svc.DeleteCredential(ctx, vetPrincipal, cred.ID)
	if err != nil {
		t.Fatalf("delete rejected failed: %v", err)
	}
}
