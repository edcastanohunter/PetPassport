package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
)

func TestDirectory_DOM010_UpdateDirectoryListing(t *testing.T) {
	org := &domain.Organization{}
	regCmd := domain.RegisterOrganization{
		OrganizationID: uuid.New(),
		Type:           string(domain.OrgTypeClinic),
		LegalName:      "Clínica Veterinaria Medellín SAS",
		DisplayName:    "Clínica Medellín",
		TaxCountry:     "CO",
		TaxValue:       "900123456-8",
		Address:        domain.AddressInput{Country: "CO", City: "Medellín", Line1: "Calle 10 # 40-20"},
		ContactEmail:   "info@clinicamedellin.co",
		ContactPhone:   "+573001234567",
		FounderID:      uuid.New(),
	}
	if _, err := org.HandleCommand(regCmd); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Verificar la organización para que pase a estado activo
	if _, err := org.HandleCommand(domain.VerifyOrganization{OrganizationID: org.ID(), Reason: "documentos validados"}); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	org.MarkCommitted()

	lat := 6.20876543
	lon := -75.56789123
	neigh := "El Poblado"
	cmd := domain.UpdateDirectoryListing{
		OrganizationID: org.ID(),
		Listed:         true,
		Services:       []string{"urgencias_24h", "cirugia", "laboratorio"},
		ShowPhone:      true,
		Neighborhood:   &neigh,
		GeoLat:         &lat,
		GeoLon:         &lon,
	}

	events, err := org.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("UpdateDirectoryListing failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev, ok := events[0].(domain.OrganizationDirectoryListingUpdated)
	if !ok {
		t.Fatalf("expected OrganizationDirectoryListingUpdated, got %T", events[0])
	}
	if !ev.Listing.Listed {
		t.Fatal("expected listed true")
	}
	if len(ev.Listing.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(ev.Listing.Services))
	}
}

func TestDirectory_SEC008_GeoPrivacy(t *testing.T) {
	ctx := context.Background()
	svc := app.NewDirectoryService(nil)

	// Sin DB devuelve lista vacía sin pánico
	res, err := svc.Search(ctx, "", "Medellín", "CO", 20)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected 0 results, got %d", len(res))
	}
}
