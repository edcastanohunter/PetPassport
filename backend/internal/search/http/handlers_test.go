package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/app"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/search/infra"
)

func setupTestRouter() (*chi.Mux, uuid.UUID, uuid.UUID, auth.Principal) {
	mockClient := app.NewMockClient()
	_ = mockClient.CreateIndex(context.Background(), domain.IndexPets, nil)
	_ = mockClient.CreateIndex(context.Background(), domain.IndexClinical, nil)

	guardianID := uuid.New()
	orgID := uuid.New()

	// Seed pet
	_, _ = mockClient.Bulk(context.Background(), []app.BulkOperation{
		{
			Index:   domain.IndexPets,
			ID:      uuid.New().String(),
			Version: 1,
			Document: domain.PetDoc{
				PetID:               uuid.New().String(),
				Name:                "Toby",
				Species:             "dog",
				Breed:               "Golden Retriever",
				Status:              "active",
				GuardianDisplayName: "Carlos",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
				UpdatedAt: time.Now(),
			},
		},
	})

	freshness := &infra.MockFreshnessProvider{Lag: 2}
	qs := app.NewQueryService(mockClient, nil, freshness, nil)
	cb := app.NewCircuitBreaker(5, 30*time.Second, 60*time.Second)
	resilient := app.NewResilientSearchService(qs, nil, cb)
	admin := app.NewAdminService(mockClient, nil, freshness, nil)

	handler := NewSearchHandler(resilient, admin, nil, nil)
	r := chi.NewRouter()
	handler.MountRoutes(r)

	principal := auth.Principal{
		UserID: guardianID,
		Memberships: []auth.Membership{
			{OrganizationID: orgID, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
		},
	}

	return r, orgID, guardianID, principal
}

func TestSearchPatients_HTTP(t *testing.T) {
	r, orgID, _, principal := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/organizations/"+orgID.String()+"/search/patients?q=Toby", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if mode := w.Header().Get("X-PP-Search-Mode"); mode != "full" {
		t.Errorf("expected X-PP-Search-Mode full, got %s", mode)
	}
	if freshness := w.Header().Get("X-PP-Freshness"); freshness != "2" {
		t.Errorf("expected X-PP-Freshness 2, got %s", freshness)
	}
}

func TestSearchPatients_ForbiddenOrg(t *testing.T) {
	r, _, _, principal := setupTestRouter()
	otherOrg := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/organizations/"+otherOrg.String()+"/search/patients?q=Toby", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAdminSearch_ReasonRequired(t *testing.T) {
	r, _, _, _ := setupTestRouter()
	adminPrincipal := auth.Principal{
		UserID:      uuid.New(),
		GlobalRoles: []string{"super_admin"},
	}

	// Without reason -> 422 ADMIN_REASON_REQUIRED
	req := httptest.NewRequest(http.MethodGet, "/admin/search/clinical?q=test", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), adminPrincipal))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestAdminSearch_ForbiddenForNonAdmin(t *testing.T) {
	r, _, _, principal := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/admin/search/clinical?q=test&reason=audit", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
