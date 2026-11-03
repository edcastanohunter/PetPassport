package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

type mockPetAccessLoader struct {
	records map[uuid.UUID]*domain.PetAccessRecord
}

func (m *mockPetAccessLoader) GetPetAccess(ctx context.Context, petID uuid.UUID) (*domain.PetAccessRecord, error) {
	if r, ok := m.records[petID]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockPetAccessLoader) GetPetIDsByOrg(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for petID, rec := range m.records {
		for _, o := range rec.LinkedOrgIDs {
			if o == orgID {
				ids = append(ids, petID)
				break
			}
		}
	}
	return ids, nil
}

func TestAuthorizationLeakGate_SEC001(t *testing.T) {
	mockOS := NewMockClient()
	_ = mockOS.CreateIndex(context.Background(), domain.IndexPets, nil)

	guardianA := uuid.New()
	guardianB := uuid.New()

	orgLinked := uuid.New()
	orgCustody := uuid.New()
	orgStranger := uuid.New()
	orgSuspended := uuid.New()

	accessMap := make(map[uuid.UUID]*domain.PetAccessRecord)

	// Crear 20 mascotas en varios estados y niveles de autorización
	var bulkItems []BulkOperation
	var allPetIDs []uuid.UUID

	for i := 1; i <= 20; i++ {
		pID := uuid.New()
		allPetIDs = append(allPetIDs, pID)

		var gID uuid.UUID
		if i%2 == 0 {
			gID = guardianA
		} else {
			gID = guardianB
		}

		var linked []uuid.UUID
		var custody []uuid.UUID

		if i <= 8 {
			linked = append(linked, orgLinked)
		}
		if i >= 6 && i <= 14 {
			custody = append(custody, orgCustody)
		}
		if i%3 == 0 {
			linked = append(linked, orgSuspended)
		}

		accRec := &domain.PetAccessRecord{
			PetID:         pID,
			GuardianID:    gID,
			LinkedOrgIDs:  linked,
			CustodyOrgIDs: custody,
		}
		accessMap[pID] = accRec

		var linkedStr, custodyStr []string
		for _, u := range linked {
			linkedStr = append(linkedStr, u.String())
		}
		for _, u := range custody {
			custodyStr = append(custodyStr, u.String())
		}

		bulkItems = append(bulkItems, BulkOperation{
			Index:   domain.IndexPets,
			ID:      pID.String(),
			Version: int64(i),
			Document: domain.PetDoc{
				PetID:               pID.String(),
				Name:                fmt.Sprintf("Mascota_%d", i),
				Species:             "dog",
				Status:              "active",
				GuardianDisplayName: fmt.Sprintf("Tutor_%d", i),
				Access: domain.AccessScope{
					GuardianID:    gID.String(),
					OrgIDsLinked:  linkedStr,
					OrgIDsCustody: custodyStr,
				},
				UpdatedAt: time.Now(),
			},
		})
	}

	_, err := mockOS.Bulk(context.Background(), bulkItems)
	if err != nil {
		t.Fatalf("failed seeding pets: %v", err)
	}

	accessLoader := &mockPetAccessLoader{records: accessMap}
	qs := NewQueryService(mockOS, accessLoader, nil, nil)

	// Definir escenarios de prueba (rol × nivel × org suspendida)
	scenarios := []struct {
		name      string
		principal auth.Principal
		orgID     uuid.UUID
	}{
		{
			name: "tutor A buscando lo propio",
			principal: auth.Principal{
				UserID: guardianA,
			},
			orgID: uuid.Nil,
		},
		{
			name: "tutor B buscando lo propio",
			principal: auth.Principal{
				UserID: guardianB,
			},
			orgID: uuid.Nil,
		},
		{
			name: "clínica vinculada",
			principal: auth.Principal{
				UserID: uuid.New(),
				Memberships: []auth.Membership{
					{OrganizationID: orgLinked, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
				},
			},
			orgID: orgLinked,
		},
		{
			name: "clínica con solo custodia",
			principal: auth.Principal{
				UserID: uuid.New(),
				Memberships: []auth.Membership{
					{OrganizationID: orgCustody, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
				},
			},
			orgID: orgCustody,
		},
		{
			name: "clínica extraña sin vínculos",
			principal: auth.Principal{
				UserID: uuid.New(),
				Memberships: []auth.Membership{
					{OrganizationID: orgStranger, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
				},
			},
			orgID: orgStranger,
		},
		{
			name: "clínica suspendida",
			principal: auth.Principal{
				UserID: uuid.New(),
				Memberships: []auth.Membership{
					{OrganizationID: orgSuspended, OrgType: "clinic", Role: "veterinarian", OrgStatus: "suspended"},
				},
			},
			orgID: orgSuspended,
		},
	}

	totalDiscrepancies := 0

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			res, err := qs.SearchPatients(context.Background(), PatientSearchRequest{
				OrgID: sc.orgID,
				Q:     "Mascota",
				Size:  50,
			}, sc.principal)

			if sc.orgID == orgSuspended {
				if err == nil {
					t.Errorf("organización suspendida debió ser denegada")
					totalDiscrepancies++
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected search error: %v", err)
			}

			returnedIDs := make(map[uuid.UUID]bool)
			for _, item := range res.Data {
				u, _ := uuid.Parse(item.Pet.ID)
				returnedIDs[u] = true
			}

			// Comparar cada mascota del dataset contra PetAccess en PostgreSQL
			for _, pID := range allPetIDs {
				acc := accessMap[pID]
				expectedAccess, _ := acc.HasAccess(sc.principal.UserID, sc.orgID)
				hasReturned := returnedIDs[pID]

				if expectedAccess != hasReturned {
					totalDiscrepancies++
					t.Errorf("FUGA DE AUTORIZACIÓN: mascota %s esperada=%v pero obtenida=%v para escenario %s",
						pID, expectedAccess, hasReturned, sc.name)
				}
			}
		})
	}

	if totalDiscrepancies > 0 {
		t.Fatalf("GATE FALLIDO (SEC-001): Se encontraron %d fugas de autorización en búsqueda", totalDiscrepancies)
	}
	t.Log("GATE PASADO (SEC-001): 0 fugas de autorización verificadas")
}
