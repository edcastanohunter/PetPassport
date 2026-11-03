package app

import (
	"context"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

type goldenTestCase struct {
	Q           string   `yaml:"q"`
	Index       string   `yaml:"index"`
	ExpectedTop []string `yaml:"expected_top"`
	Description string   `yaml:"description"`
}

type goldenTestSuite struct {
	Queries []goldenTestCase `yaml:"queries"`
}

func TestGoldenQueries_MRR(t *testing.T) {
	data, err := os.ReadFile("../../../test/search/golden/golden_queries.yaml")
	if err != nil {
		t.Fatalf("failed reading golden queries file: %v", err)
	}

	var suite goldenTestSuite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		t.Fatalf("failed unmarshaling golden queries: %v", err)
	}

	mock := NewMockClient()
	_ = mock.CreateIndex(context.Background(), domain.IndexPets, nil)
	_ = mock.CreateIndex(context.Background(), domain.IndexClinical, nil)

	guardianID := uuid.New()
	orgID := uuid.New()

	// Sembrar dataset sintético para golden queries
	pets := []BulkOperation{
		{
			Index:   domain.IndexPets,
			ID:      "pet-luna-01",
			Version: 1,
			Document: domain.PetDoc{
				PetID:               "pet-luna-01",
				Name:                "Luna",
				Species:             "dog",
				Breed:               "Beagle",
				Microchip:           "941000012345678",
				MicrochipSuffix:     "5678",
				Status:              "active",
				GuardianDisplayName: "Ana Gomez",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
				UpdatedAt: time.Now(),
			},
		},
		{
			Index:   domain.IndexPets,
			ID:      "pet-max-02",
			Version: 1,
			Document: domain.PetDoc{
				PetID:               "pet-max-02",
				Name:                "Max",
				Species:             "dog",
				Breed:               "Labrador",
				Status:              "active",
				GuardianDisplayName: "Carlos Perez",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
				UpdatedAt: time.Now(),
			},
		},
		{
			Index:   domain.IndexPets,
			ID:      "pet-mickey-03",
			Version: 1,
			Document: domain.PetDoc{
				PetID:               "pet-mickey-03",
				Name:                "Mickey",
				Species:             "cat",
				Breed:               "Siames",
				Status:              "active",
				GuardianDisplayName: "Maria Lopez",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
				UpdatedAt: time.Now(),
			},
		},
	}
	_, _ = mock.Bulk(context.Background(), pets)

	clinical := []BulkOperation{
		{
			Index:   domain.IndexClinical,
			ID:      "enc-vomito-01",
			Version: 1,
			Document: domain.ClinicalDoc{
				DocKind:          "encounter",
				PetID:            "pet-luna-01",
				PetName:          "Luna",
				Species:          "dog",
				OrganizationID:   orgID.String(),
				OrganizationName: "Clinica Central",
				OccurredAt:       time.Now(),
				EncounterType:    "consultation",
				Text:             "Paciente presenta vómito agudo tras ingerir alimento extraño.",
				Diagnoses: []domain.DiagnosisItem{
					{Text: "Gastroenteritis con vómito", Certainty: "confirmed"},
				},
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
			},
		},
		{
			Index:   domain.IndexClinical,
			ID:      "enc-picazon-02",
			Version: 1,
			Document: domain.ClinicalDoc{
				DocKind:          "encounter",
				PetID:            "pet-max-02",
				PetName:          "Max",
				Species:          "dog",
				OrganizationID:   orgID.String(),
				OrganizationName: "Clinica Central",
				OccurredAt:       time.Now(),
				EncounterType:    "consultation",
				Text:             "Tutor refiere picazón persistente y rascado en orejas.",
				Diagnoses: []domain.DiagnosisItem{
					{Text: "Dermatitis por pulgas con picazón", Certainty: "confirmed"},
				},
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
			},
		},
		{
			Index:   domain.IndexClinical,
			ID:      "enc-rabia-03",
			Version: 1,
			Document: domain.ClinicalDoc{
				DocKind:          "card_entry",
				PetID:            "pet-luna-01",
				PetName:          "Luna",
				Species:          "dog",
				OrganizationID:   orgID.String(),
				OrganizationName: "Clinica Central",
				OccurredAt:       time.Now(),
				EncounterType:    "vaccine",
				Text:             "Vacunación: vacuna antirrábica anual.",
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
			},
		},
		{
			Index:   domain.IndexClinical,
			ID:      "enc-otitis-04",
			Version: 1,
			Document: domain.ClinicalDoc{
				DocKind:          "encounter",
				PetID:            "pet-mickey-03",
				PetName:          "Mickey",
				Species:          "cat",
				OrganizationID:   orgID.String(),
				OrganizationName: "Clinica Central",
				OccurredAt:       time.Now(),
				EncounterType:    "consultation",
				Text:             "Exudado ceruminoso bilateral en oídos.",
				Diagnoses: []domain.DiagnosisItem{
					{Text: "Otitis externa bacteriana", Certainty: "confirmed"},
				},
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
			},
		},
		{
			Index:   domain.IndexClinical,
			ID:      "enc-diarrea-05",
			Version: 1,
			Document: domain.ClinicalDoc{
				DocKind:          "encounter",
				PetID:            "pet-luna-01",
				PetName:          "Luna",
				Species:          "dog",
				OrganizationID:   orgID.String(),
				OrganizationName: "Clinica Central",
				OccurredAt:       time.Now(),
				EncounterType:    "consultation",
				Text:             "Heces blandas y líquidas de 24 horas de evolución.",
				Diagnoses: []domain.DiagnosisItem{
					{Text: "Diarrea bacteriana", Certainty: "confirmed"},
				},
				Access: domain.AccessScope{
					GuardianID:   guardianID.String(),
					OrgIDsLinked: []string{orgID.String()},
				},
			},
		},
	}
	_, _ = mock.Bulk(context.Background(), clinical)

	qs := NewQueryService(mock, nil, nil, nil)
	principal := auth.Principal{
		UserID: guardianID,
		Memberships: []auth.Membership{
			{OrganizationID: orgID, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
		},
	}

	var totalReciprocalRank float64
	queriesCount := len(suite.Queries)

	for _, tc := range suite.Queries {
		var hitIDs []string
		if tc.Index == domain.IndexPets {
			res, err := qs.SearchPatients(context.Background(), PatientSearchRequest{
				OrgID: orgID,
				Q:     tc.Q,
			}, principal)
			if err != nil {
				t.Errorf("query %q failed: %v", tc.Q, err)
				continue
			}
			for _, item := range res.Data {
				hitIDs = append(hitIDs, item.Pet.ID)
			}
		} else {
			res, err := qs.SearchClinical(context.Background(), ClinicalSearchRequest{
				OrgID: orgID,
				Q:     tc.Q,
			}, principal)
			if err != nil {
				t.Errorf("query %q failed: %v", tc.Q, err)
				continue
			}
			for _, group := range res.Data {
				for _, h := range group.Hits {
					hitIDs = append(hitIDs, h.RefID)
				}
			}
		}

		// Calcular Reciprocal Rank para este caso
		rr := 0.0
		found := false
		for rank, hitID := range hitIDs {
			for _, exp := range tc.ExpectedTop {
				if hitID == exp {
					rr = 1.0 / float64(rank+1)
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		totalReciprocalRank += rr
		if !found {
			t.Logf("Golden query %q (%s) no encontró coincidencias esperadas en top-3. Hits: %v", tc.Q, tc.Description, hitIDs)
		}
	}

	mrr := totalReciprocalRank / float64(queriesCount)
	t.Logf("Relevancia Golden Queries: MRR = %.3f (umbral QA-002: >= 0.8)", mrr)

	if mrr < 0.8 {
		t.Fatalf("MRR insuficiente: %.3f < 0.8", mrr)
	}
}
