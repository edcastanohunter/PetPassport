package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

// DocumentLoader carga documentos desde las tablas read_* de PostgreSQL para indexación en OpenSearch (ARQ-001).
type DocumentLoader struct {
	pool   *pgxpool.Pool
	access *CachedPetAccess
}

// NewDocumentLoader crea un nuevo DocumentLoader.
func NewDocumentLoader(pool *pgxpool.Pool, access *CachedPetAccess) *DocumentLoader {
	return &DocumentLoader{
		pool:   pool,
		access: access,
	}
}

// LoadPetDocs carga una lista de PetDoc a partir de sus IDs.
func (l *DocumentLoader) LoadPetDocs(ctx context.Context, petIDs []uuid.UUID) ([]domain.PetDoc, error) {
	if len(petIDs) == 0 {
		return nil, nil
	}

	var docs []domain.PetDoc

	for _, id := range petIDs {
		var (
			name, species, sex, status, guardianName string
			breed, microchip                         *string
			birthDate                                *time.Time
			updatedAt                                time.Time
			version                                  int64
		)

		err := l.pool.QueryRow(ctx, `
			SELECT name, species, COALESCE(breed, ''), sex, status, guardian_display_name,
			       microchip, birth_date, updated_at, version
			FROM read_petregistry.pets
			WHERE id = $1
		`, id).Scan(&name, &species, &breed, &sex, &status, &guardianName, &microchip, &birthDate, &updatedAt, &version)
		if err != nil {
			continue // Mascota borrada o inexistente
		}

		acc, _ := l.access.GetPetAccess(ctx, id)
		var scope domain.AccessScope
		if acc != nil {
			scope.GuardianID = acc.GuardianID.String()
			for _, u := range acc.LinkedOrgIDs {
				scope.OrgIDsLinked = append(scope.OrgIDsLinked, u.String())
			}
			for _, u := range acc.CustodyOrgIDs {
				scope.OrgIDsCustody = append(scope.OrgIDsCustody, u.String())
			}
		}

		var lastEnc *time.Time
		_ = l.pool.QueryRow(ctx, `
			SELECT started_at FROM read_clinical.encounters
			WHERE pet_id = $1 AND status = 'signed'
			ORDER BY started_at DESC LIMIT 1
		`, id).Scan(&lastEnc)

		doc := domain.PetDoc{
			PetID:               id.String(),
			Name:                name,
			NameSuggest:         []string{name},
			Species:             species,
			Sex:                 sex,
			Status:              status,
			GuardianDisplayName: guardianName,
			Access:              scope,
			LastEncounterAt:     lastEnc,
			UpdatedAt:           updatedAt,
			Version:             version,
		}
		if breed != nil && *breed != "" {
			doc.Breed = *breed
		}
		if microchip != nil && *microchip != "" {
			doc.Microchip = *microchip
			if len(*microchip) >= 4 {
				doc.MicrochipSuffix = (*microchip)[len(*microchip)-4:]
			}
		}
		if birthDate != nil {
			bd := birthDate.Format("2006-01-02")
			doc.BirthDate = &bd
		}

		docs = append(docs, doc)
	}

	return docs, nil
}

// LoadClinicalDocs carga una lista de ClinicalDoc a partir de sus IDs (encuentros o carnet).
func (l *DocumentLoader) LoadClinicalDocs(ctx context.Context, docIDs []uuid.UUID) ([]domain.ClinicalDoc, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}

	var docs []domain.ClinicalDoc

	for _, id := range docIDs {
		// 1. Intentar cargar como encuentro
		var (
			petID, orgID    uuid.UUID
			encType, status string
			startedAt       time.Time
			reason          string
			attendingVetID  *uuid.UUID
			subjective      *string
			objectiveRaw    []byte
			diagnosesRaw    []byte
			planRaw         []byte
			version         int64
		)

		err := l.pool.QueryRow(ctx, `
			SELECT pet_id, organization_id, type, status, started_at, reason,
			       attending_vet_id, subjective, objective, diagnoses, plan, version
			FROM read_clinical.encounters
			WHERE id = $1 AND status = 'signed'
		`, id).Scan(&petID, &orgID, &encType, &status, &startedAt, &reason, &attendingVetID, &subjective, &objectiveRaw, &diagnosesRaw, &planRaw, &version)

		if err == nil {
			// Es un encuentro firmado
			var petName, species, orgName string
			_ = l.pool.QueryRow(ctx, "SELECT name, species FROM read_petregistry.pets WHERE id = $1", petID).Scan(&petName, &species)
			_ = l.pool.QueryRow(ctx, "SELECT display_name FROM read_partnernetwork.organizations WHERE id = $1", orgID).Scan(&orgName)

			acc, _ := l.access.GetPetAccess(ctx, petID)
			var scope domain.AccessScope
			if acc != nil {
				scope.GuardianID = acc.GuardianID.String()
				for _, u := range acc.LinkedOrgIDs {
					scope.OrgIDsLinked = append(scope.OrgIDsLinked, u.String())
				}
				for _, u := range acc.CustodyOrgIDs {
					scope.OrgIDsCustody = append(scope.OrgIDsCustody, u.String())
				}
			}

			// Construir texto libre concatenado (anamnesis + hallazgos + plan)
			var textParts []string
			if reason != "" {
				textParts = append(textParts, reason)
			}
			if subjective != nil && *subjective != "" {
				textParts = append(textParts, *subjective)
			}
			if len(objectiveRaw) > 0 {
				var obj map[string]any
				if err := json.Unmarshal(objectiveRaw, &obj); err == nil {
					if n, ok := obj["narrative"].(string); ok && n != "" {
						textParts = append(textParts, n)
					}
				}
			}

			var diagnoses []domain.DiagnosisItem
			if len(diagnosesRaw) > 0 {
				_ = json.Unmarshal(diagnosesRaw, &diagnoses)
				for _, d := range diagnoses {
					if d.Text != "" {
						textParts = append(textParts, d.Text)
					}
				}
			}

			var medications []domain.MedicationSubDoc
			if len(planRaw) > 0 {
				var planObj struct {
					Medications []struct {
						Name      string  `json:"name"`
						CatalogID *string `json:"catalog_id"`
					} `json:"medications"`
					Procedures      []string `json:"procedures"`
					Recommendations string   `json:"recommendations"`
				}
				if err := json.Unmarshal(planRaw, &planObj); err == nil {
					for _, m := range planObj.Medications {
						medications = append(medications, domain.MedicationSubDoc{
							Name:      m.Name,
							CatalogID: m.CatalogID,
						})
						textParts = append(textParts, m.Name)
					}
					if planObj.Recommendations != "" {
						textParts = append(textParts, planObj.Recommendations)
					}
				}
			}

			doc := domain.ClinicalDoc{
				DocKind:          "encounter",
				PetID:            petID.String(),
				PetName:          petName,
				Species:          species,
				OrganizationID:   orgID.String(),
				OrganizationName: orgName,
				OccurredAt:       startedAt,
				EncounterType:    encType,
				Diagnoses:        diagnoses,
				Text:             strings.Join(textParts, " "),
				Medications:      medications,
				Locale:           "es",
				Year:             startedAt.Year(),
				Access:           scope,
				Position:         version,
			}
			if attendingVetID != nil {
				doc.ProfessionalID = attendingVetID.String()
			}
			docs = append(docs, doc)
			continue
		}

		// 2. Intentar cargar como entrada de carnet
		var (
			cardPetID, cardOrgID uuid.UUID
			kind, prodName       string
			admDate              time.Time
			cardVersion          int64
		)
		err = l.pool.QueryRow(ctx, `
			SELECT pet_id, recorded_by_org, kind, product_name, administered_at, version
			FROM read_clinical.card_entries
			WHERE id = $1 AND status = 'active'
		`, id).Scan(&cardPetID, &cardOrgID, &kind, &prodName, &admDate, &cardVersion)
		if err == nil {
			var petName, species, orgName string
			_ = l.pool.QueryRow(ctx, "SELECT name, species FROM read_petregistry.pets WHERE id = $1", cardPetID).Scan(&petName, &species)
			if cardOrgID != uuid.Nil {
				_ = l.pool.QueryRow(ctx, "SELECT display_name FROM read_partnernetwork.organizations WHERE id = $1", cardOrgID).Scan(&orgName)
			}

			acc, _ := l.access.GetPetAccess(ctx, cardPetID)
			var scope domain.AccessScope
			if acc != nil {
				scope.GuardianID = acc.GuardianID.String()
				for _, u := range acc.LinkedOrgIDs {
					scope.OrgIDsLinked = append(scope.OrgIDsLinked, u.String())
				}
				for _, u := range acc.CustodyOrgIDs {
					scope.OrgIDsCustody = append(scope.OrgIDsCustody, u.String())
				}
			}

			doc := domain.ClinicalDoc{
				DocKind:          "card_entry",
				PetID:            cardPetID.String(),
				PetName:          petName,
				Species:          species,
				OrganizationID:   cardOrgID.String(),
				OrganizationName: orgName,
				OccurredAt:       admDate,
				EncounterType:    kind,
				Text:             fmt.Sprintf("%s: %s", kind, prodName),
				VaccineProduct:   &prodName,
				Locale:           "es",
				Year:             admDate.Year(),
				Access:           scope,
				Position:         cardVersion,
			}
			docs = append(docs, doc)
		}
	}

	return docs, nil
}

// LoadMedicationDocs carga documentos de medicamentos desde ref.medications para indexación en OpenSearch (ARQ-001).
func (l *DocumentLoader) LoadMedicationDocs(ctx context.Context, catalogIDs []uuid.UUID) ([]domain.MedicationDoc, error) {
	if len(catalogIDs) == 0 {
		return nil, nil
	}

	rows, err := l.pool.Query(ctx, `
		SELECT id, name, active_ingredients, form, concentration, presentation,
		       routes, species, manufacturer, atcvet_code, controlled,
		       prescription_required, status, synonyms, usage_count
		FROM ref.medications
		WHERE id = ANY($1)
	`, catalogIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []domain.MedicationDoc
	for rows.Next() {
		var (
			id                   uuid.UUID
			name, form, status   string
			conc, pres, manu     *string
			atcvet               *string
			routes, species, syn []string
			actJSON              []byte
			controlled, prescReq bool
			usage                int
		)

		if err := rows.Scan(
			&id, &name, &actJSON, &form, &conc, &pres,
			&routes, &species, &manu, &atcvet, &controlled,
			&prescReq, &status, &syn, &usage,
		); err != nil {
			return nil, err
		}

		var ingredients []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(actJSON, &ingredients)
		var ingNames []string
		for _, ing := range ingredients {
			if ing.Name != "" {
				ingNames = append(ingNames, ing.Name)
			}
		}

		nameSuggest := append([]string{name}, syn...)

		docs = append(docs, domain.MedicationDoc{
			CatalogID:            id.String(),
			Name:                 name,
			NameSuggest:          nameSuggest,
			ActiveIngredients:    ingNames,
			Form:                 form,
			Concentration:        conc,
			Presentation:         pres,
			Manufacturer:         manu,
			Species:              species,
			Routes:               routes,
			PrescriptionRequired: prescReq,
			Controlled:           controlled,
			Status:               status,
			Synonyms:             syn,
			ATCVetCode:           atcvet,
			UsageCount:           usage,
		})
	}

	return docs, rows.Err()
}
