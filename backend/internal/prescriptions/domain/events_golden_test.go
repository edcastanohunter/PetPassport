package domain

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var update = flag.Bool("update", false, "regenera los golden files")

func TestEvents_Golden(t *testing.T) {
	fixedUUID := uuid.MustParse("01930000-0000-7000-8000-000000000001")
	fixedTime := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	fixedDate := kernel.CivilDate("2026-12-06")

	item := PrescriptionItem{
		ItemID:       "item-001",
		CatalogID:    &fixedUUID,
		NameSnapshot: "Amoxicilina 500mg",
		Dose: DoseVO{
			Value: 500,
			Unit:  "mg",
		},
		Route:        "oral",
		Frequency:    "every_12_hours",
		DurationDays: ptr(7),
		QuantityToDispense: QuantityVO{
			Value: 1,
			Unit:  "pack",
		},
		SubstitutionAllowed: true,
		Instructions:        "Con alimento",
		Controlled:          false,
	}

	cred := CredentialRef{
		Country:        "CO",
		RegistryNumber: "TP-12345",
		VerifiedAt:     fixedTime,
	}

	events := []struct {
		name string
		data any
	}{
		{
			name: "PrescriptionDrafted",
			data: PrescriptionDrafted{
				PrescriptionID: fixedUUID,
				PetID:          fixedUUID,
				GuardianID:     fixedUUID,
				Source:         SourceIssued,
				IssuerID:       &fixedUUID,
				IssuerOrgID:    &fixedUUID,
				Country:        "CO",
				Items:          []PrescriptionItem{item},
				Validity:       Validity{ValidUntil: fixedDate, RefillsAllowed: 0},
				Notes:          ptr("Instrucciones especiales para farmacia"),
			},
		},
		{
			name: "PrescriptionDraftUpdated",
			data: PrescriptionDraftUpdated{
				PrescriptionID: fixedUUID,
				Items:          []PrescriptionItem{item},
				Validity:       Validity{ValidUntil: fixedDate, RefillsAllowed: 1},
				Notes:          ptr("Notas actualizadas"),
			},
		},
		{
			name: "PrescriptionIssued",
			data: PrescriptionIssued{
				PrescriptionID: fixedUUID,
				IssuerID:       fixedUUID,
				Credential:     cred,
				IssuedAt:       fixedTime,
				ValidUntil:     fixedDate,
				RefillsAllowed: 1,
				Items:          []PrescriptionItem{item},
				StreamVersion:  2,
				ContentHash:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "PrescriptionRevoked",
			data: PrescriptionRevoked{
				PrescriptionID: fixedUUID,
				Reason:         "Error en dosificación",
				By:             fixedUUID,
			},
		},
		{
			name: "ExternalPrescriptionUploaded",
			data: ExternalPrescriptionUploaded{
				PrescriptionID: fixedUUID,
				PetID:          fixedUUID,
				GuardianID:     fixedUUID,
				Images: []ImageRef{
					{ObjectKey: "rx/page1.jpg", ContentType: "image/jpeg", Page: 1, Sha256: []byte("sha256")},
				},
				Country: "CO",
			},
		},
		{
			name: "PrescriptionExtractionRecorded",
			data: PrescriptionExtractionRecorded{
				PrescriptionID: fixedUUID,
				Extraction: Extraction{
					Version:     1,
					ExtractorID: "claude-opus-5-5",
					Legibility:  0.95,
					Warnings:    []string{},
					Header:      ExtractedHeader{VetName: ptr("Dr. Perez")},
					Items:       []ExtractedItem{},
					CostUSD:     0.02,
					LatencyMs:   1200,
				},
			},
		},
		{
			name: "PrescriptionReuploadRequested",
			data: PrescriptionReuploadRequested{
				PrescriptionID: fixedUUID,
				Reason:         "Imagen borrosa",
			},
		},
		{
			name: "ExternalPrescriptionConfirmed",
			data: ExternalPrescriptionConfirmed{
				PrescriptionID: fixedUUID,
				Items:          []PrescriptionItem{item},
				Validity:       Validity{ValidUntil: fixedDate, RefillsAllowed: 0},
				ExternalMeta:   ExternalMeta{VetName: ptr("Dr. Perez"), ClinicName: ptr("Clínica Norte")},
				Diff:           map[string]any{"edited": true},
			},
		},
		{
			name: "ExternalPrescriptionReopened",
			data: ExternalPrescriptionReopened{
				PrescriptionID: fixedUUID,
			},
		},
		{
			name: "PrescriptionPharmacistValidated",
			data: PrescriptionPharmacistValidated{
				PrescriptionID: fixedUUID,
				By:             fixedUUID,
				OrgID:          fixedUUID,
				Status:         "validated",
				Note:           "Receta externa cotejada conforme",
			},
		},
		{
			name: "PrescriptionDispensed",
			data: PrescriptionDispensed{
				PrescriptionID: fixedUUID,
				OrderID:        fixedUUID,
				PharmacyOrgID:  fixedUUID,
				Lines: []DispensationLine{
					{ItemID: "item-001", Qty: 1},
				},
				Partial:     false,
				RefillsUsed: 0,
			},
		},
		{
			name: "PrescriptionExhausted",
			data: PrescriptionExhausted{
				PrescriptionID: fixedUUID,
			},
		},
		{
			name: "PrescriptionExpired",
			data: PrescriptionExpired{
				PrescriptionID: fixedUUID,
			},
		},
	}

	goldenDir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(goldenDir, 0o750); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
	}

	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.MarshalIndent(tc.data, "", "  ")
			if err != nil {
				t.Fatalf("marshal %s: %v", tc.name, err)
			}
			goldenPath := filepath.Clean(filepath.Join(goldenDir, fmt.Sprintf("%s.json", tc.name)))
			if *update {
				if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath) // #nosec G304
			if err != nil {
				t.Fatalf("read golden %s: %v (ejecuta con -update si es nuevo)", goldenPath, err)
			}
			if string(got) != string(want) {
				t.Errorf("%s mismatch (-got +want):\nGOT:\n%s\nWANT:\n%s", tc.name, string(got), string(want))
			}
		})
	}
}
