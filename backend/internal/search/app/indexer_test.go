package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

type dummySource struct {
	pets     []domain.PetDoc
	clinical []domain.ClinicalDoc
}

func (d *dummySource) LoadPetDocs(ctx context.Context, petIDs []uuid.UUID) ([]domain.PetDoc, error) {
	var out []domain.PetDoc
	for _, id := range petIDs {
		for _, p := range d.pets {
			if p.PetID == id.String() {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (d *dummySource) LoadClinicalDocs(ctx context.Context, docIDs []uuid.UUID) ([]domain.ClinicalDoc, error) {
	var out []domain.ClinicalDoc
	for _, id := range docIDs {
		for _, c := range d.clinical {
			if c.PetID == id.String() {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func TestIndexer_IdempotencyAndExternalGTE_QA001(t *testing.T) {
	mockOS := NewMockClient()
	_ = mockOS.CreateIndex(context.Background(), "pets-v1", nil)
	_ = mockOS.UpdateAliases(context.Background(), []AliasAction{
		{Type: "add", Index: "pets-v1", Alias: domain.AliasPetsWrite},
		{Type: "add", Index: "pets-v1", Alias: domain.IndexPets},
	})

	petID := uuid.New()

	source := &dummySource{
		pets: []domain.PetDoc{
			{
				PetID:     petID.String(),
				Name:      "Luna Version 2",
				Species:   "dog",
				Status:    "active",
				Version:   2,
				UpdatedAt: time.Now(),
			},
		},
	}

	idx := NewIndexer(mockOS, source, nil, nil)

	// 1. Indexar versión 2
	err := idx.ProcessBulkPets(context.Background(), []string{petID.String()})
	if err != nil {
		t.Fatalf("failed indexing v2: %v", err)
	}

	// Comprobar que en mockOS está versión 2
	res, err := mockOS.Search(context.Background(), domain.IndexPets, map[string]any{
		"query": map[string]any{"bool": map[string]any{}},
	})
	if err != nil || len(res.Hits.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(res.Hits.Hits))
	}
	var doc domain.PetDoc
	_ = json.Unmarshal(res.Hits.Hits[0].Source, &doc)
	if doc.Name != "Luna Version 2" {
		t.Fatalf("expected 'Luna Version 2', got %q", doc.Name)
	}

	// 2. Intentar indexar versión 1 antigua (idempotencia external_gte)
	source.pets = []domain.PetDoc{
		{
			PetID:     petID.String(),
			Name:      "Luna Version 1 (STALE)",
			Species:   "dog",
			Status:    "active",
			Version:   1, // Versión inferior a 2
			UpdatedAt: time.Now(),
		},
	}

	err = idx.ProcessBulkPets(context.Background(), []string{petID.String()})
	if err != nil {
		t.Fatalf("bulk execution failed: %v", err)
	}

	// Comprobar que no se sobrescribió
	res, _ = mockOS.Search(context.Background(), domain.IndexPets, map[string]any{
		"query": map[string]any{"bool": map[string]any{}},
	})
	_ = json.Unmarshal(res.Hits.Hits[0].Source, &doc)
	if doc.Name != "Luna Version 2" {
		t.Fatalf("FALLO DE IDEMPOTENCIA: versión antigua sobrescribió la versión más reciente! Doc: %q", doc.Name)
	}
	t.Log("QA-001 IDEMPOTENCIA VERIFICADA: versión anterior ignorada por external_gte")
}

func TestAccessProjector_UpdateByQuery_QA001(t *testing.T) {
	mockOS := NewMockClient()
	_ = mockOS.CreateIndex(context.Background(), domain.IndexClinical, nil)
	_ = mockOS.CreateIndex(context.Background(), domain.IndexPets, nil)

	petID := uuid.New()
	guardianID := uuid.New()
	orgA := uuid.New()
	orgB := uuid.New()

	// Documento clínico inicial vinculado solo a OrgA
	_, _ = mockOS.Bulk(context.Background(), []BulkOperation{
		{
			Index:   domain.IndexClinical,
			ID:      "enc-01",
			Version: 1,
			Document: domain.ClinicalDoc{
				PetID: petID.String(),
				Text:  "Chequeo de rutina",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgA.String()},
				},
			},
		},
	})

	accessLoader := &mockPetAccessLoader{
		records: map[uuid.UUID]*domain.PetAccessRecord{
			petID: {
				PetID:        petID,
				GuardianID:   guardianID,
				LinkedOrgIDs: []uuid.UUID{orgA, orgB}, // OrgB ahora tiene vínculo
			},
		},
	}

	source := &dummySource{
		pets: []domain.PetDoc{
			{PetID: petID.String(), Name: "Luna", Version: 1},
		},
	}
	indexer := NewIndexer(mockOS, source, nil, nil)
	projector := NewAccessProjector(mockOS, indexer, accessLoader, nil, nil)

	// Ejecutar actualización de acceso
	err := projector.UpdatePetAccess(context.Background(), petID)
	if err != nil {
		t.Fatalf("failed updating pet access: %v", err)
	}

	// Comprobar que OrgB ahora puede ver el historial clínico
	qs := NewQueryService(mockOS, accessLoader, nil, nil)
	res, err := qs.SearchClinical(context.Background(), ClinicalSearchRequest{
		OrgID: orgB,
		Q:     "Chequeo",
	}, auth.Principal{
		UserID: uuid.New(),
		Memberships: []auth.Membership{
			{OrganizationID: orgB, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
		},
	})

	if err != nil || len(res.Data) != 1 {
		t.Fatalf("expected OrgB to find the encounter after update_by_query access projection, got %d", len(res.Data))
	}
	t.Log("QA-001 ACCESO ACTUALIZADO POR UPDATE_BY_QUERY VERIFICADO")
}
