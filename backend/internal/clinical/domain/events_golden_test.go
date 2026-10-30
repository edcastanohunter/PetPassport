package domain

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "regenera los golden files")

// TestEvents_Golden (ARQ-007): el JSON persistido de cada evento v1 es un
// contrato estable. Cualquier cambio obliga a schema_version+1 y un upcaster,
// nunca a editar el golden. Regenerar la primera vez:
// `go test ./internal/clinical/domain -run TestEvents_Golden -update`.
func TestEvents_Golden(t *testing.T) {
	events := []any{
		EncounterOpened{
			EncounterID: encID, PetID: petID, OrganizationID: orgID,
			Type: TypeConsultation, Reason: "Cojera pata trasera",
			StartedAt: now.Add(-time.Hour), AttendingVetID: ptr(vetID), OpenedBy: recepID,
		},
		EncounterDetailsUpdated{
			EncounterID: encID, Type: ptr(TypeFollowUp),
			Reason: ptr("Revisión de cojera"), StartedAt: ptr(now),
		},
		VitalsRecorded{EncounterID: encID, Vitals: Vitals{WeightKg: ptr(12.4), TempC: ptr(38.6), HeartRateBpm: ptr(96)}},
		EncounterNarrativeUpdated{
			EncounterID: encID, Subjective: ptr("El tutor refiere cojera intermitente."),
			Objective: &Objective{Narrative: "Dolor a la palpación", Systems: map[BodySystem]Finding{
				"musculoskeletal": {Status: FindingAbnormal, Note: "dolor femoral"},
			}},
		},
		DiagnosesSet{EncounterID: encID, Diagnoses: []Diagnosis{{Text: "Artrosis", Certainty: CertaintyPresumptive}}},
		PlanSet{EncounterID: encID, Plan: Plan{
			Medications: []MedicationItem{{
				MedicationName: "Meloxicam", Dose: 0.2, DoseUnit: "mg_per_kg", Route: "oral",
				Frequency: Frequency{Once: true}, Instructions: "Con alimento",
			}},
			Procedures:      []Procedure{{Name: "Radiografía", Notes: "Proyección lateral"}},
			Recommendations: "Reposo relativo",
		}},
		FollowUpRecommended{EncounterID: encID, DueDate: "2026-11-20", Reason: "Control de cojera"},
		FollowUpCleared{EncounterID: encID},
		AttendingVetAssigned{EncounterID: encID, UserID: vetID},
		AttachmentAdded{EncounterID: encID, Attachment: Attachment{
			ID: attID, ObjectKey: AttachmentKey(petID, encID, attID), ContentType: ContentPDF,
			SizeBytes: 2048, Description: "Hemograma", VisibleToGuardian: true,
		}},
		AttachmentRemoved{EncounterID: encID, AttachmentID: attID},
		AttachmentVisibilityChanged{EncounterID: encID, AttachmentID: attID, Visible: false, Reason: "nota interna"},
		EncounterSigned{
			EncounterID: encID, SignedBy: vetID, SignedAt: now, StreamVersion: 7,
			ContentHash: "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
			PetID:       petID, OrganizationID: orgID,
		},
		EncounterAddendumAdded{
			EncounterID: encID, Seq: 1, Text: "Resultados de laboratorio normales.",
			Reason: AddendumLabResults, SignedBy: vetID, SignedAt: now, Attachments: []Attachment{},
		},
		EncounterDiscarded{EncounterID: encID, Reason: "Duplicada", By: vetID},
	}
	for _, e := range events {
		name, ok := e.(interface{ EventName() string })
		if !ok {
			t.Fatalf("%T no implementa EventName", e)
		}
		t.Run(name.EventName(), func(t *testing.T) {
			raw, err := json.MarshalIndent(e, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", name.EventName()+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil { //nolint:gosec // testdata local
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, raw, 0o600); err != nil { //nolint:gosec // golden de prueba
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden) //nolint:gosec // ruta fija en testdata
			if err != nil {
				t.Fatalf("lee golden (¿ejecuta con -update la primera vez?): %v", err)
			}
			if string(raw) != string(want) {
				t.Errorf("golden %s cambió:\ngot:\n%s\nwant:\n%s\nSi el cambio es intencional, crea schema_version+1 y upcaster (ARQ-007).", golden, raw, want)
			}
		})
	}
}

// TestEvents_SchemaVersion: todos los eventos del agregado nacen en v1.
func TestEvents_SchemaVersion(t *testing.T) {
	for _, e := range []eventstoreEvent{
		EncounterOpened{}, EncounterDetailsUpdated{}, VitalsRecorded{}, EncounterNarrativeUpdated{},
		DiagnosesSet{}, PlanSet{}, FollowUpRecommended{}, FollowUpCleared{}, AttendingVetAssigned{},
		AttachmentAdded{}, AttachmentRemoved{}, AttachmentVisibilityChanged{}, EncounterSigned{},
		EncounterAddendumAdded{}, EncounterDiscarded{},
	} {
		if e.SchemaVersion() != 1 {
			t.Errorf("%s: schema_version %d, want 1", e.EventName(), e.SchemaVersion())
		}
	}
}

type eventstoreEvent interface {
	EventName() string
	SchemaVersion() int
}
