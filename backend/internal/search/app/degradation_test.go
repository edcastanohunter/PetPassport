package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

type mockFallbackBackend struct {
	called bool
}

func (f *mockFallbackBackend) SearchPatientsPG(ctx context.Context, req PatientSearchRequest, principal auth.Principal) (*PatientSearchResponse, error) {
	f.called = true
	res := &PatientSearchResponse{
		Data: []PatientSearchResultItem{
			{
				GuardianDisplayName: "Carlos Fallback",
				AccessLevel:         "linked",
			},
		},
		Mode: "degraded",
	}
	res.Data[0].Pet.ID = "pet-fallback-01"
	res.Data[0].Pet.Name = "Luna PG"
	return res, nil
}

func (f *mockFallbackBackend) SearchClinicalPG(ctx context.Context, req ClinicalSearchRequest, principal auth.Principal) (*ClinicalSearchResponse, error) {
	f.called = true
	return &ClinicalSearchResponse{
		Data: []ClinicalSearchResultItem{},
		Mode: "degraded",
	}, nil
}

func (f *mockFallbackBackend) SuggestPatientsPG(ctx context.Context, orgID uuid.UUID, q string, principal auth.Principal) ([]SuggestPatientItem, error) {
	f.called = true
	return []SuggestPatientItem{
		{PetID: "pet-fallback-01", Name: "Luna PG"},
	}, nil
}

type failingOSClient struct {
	Client
}

func (f *failingOSClient) Search(ctx context.Context, index string, query map[string]any) (*SearchRawResult, error) {
	return nil, errors.New("connection refused: opensearch is down")
}

func TestDegradationToPostgreSQL_ARQ004(t *testing.T) {
	failingClient := &failingOSClient{}
	queryService := NewQueryService(failingClient, nil, nil, nil)
	fallback := &mockFallbackBackend{}
	breaker := NewCircuitBreaker(3, 10*time.Second, 30*time.Second)

	resilientService := NewResilientSearchService(queryService, fallback, breaker)

	principal := auth.Principal{
		UserID: uuid.New(),
		Memberships: []auth.Membership{
			{OrganizationID: uuid.New(), OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
		},
	}

	// Ejecutar consulta cuando OpenSearch está caído
	res, err := resilientService.SearchPatients(context.Background(), PatientSearchRequest{
		OrgID: principal.Memberships[0].OrganizationID,
		Q:     "Luna",
	}, principal)

	if err != nil {
		t.Fatalf("search failed instead of degrading gracefully: %v", err)
	}

	if !fallback.called {
		t.Errorf("expected fallback to be called when OpenSearch is down")
	}

	if res.Mode != "degraded" {
		t.Errorf("expected Mode='degraded', got %q", res.Mode)
	}

	if len(res.Data) != 1 || res.Data[0].Pet.Name != "Luna PG" {
		t.Errorf("unexpected fallback data: %+v", res.Data)
	}

	t.Log("ARQ-004 DEGRADACIÓN A POSTGRESQL CON CIRCUIT BREAKER VERIFICADA")
}
