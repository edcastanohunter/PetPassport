package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"rsc.io/qr"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func sampleView() app.EncounterView {
	vet := uuid.MustParse("01930000-0000-7000-8000-0000000000a1")
	w, temp := 12.5, 38.4
	hr, dur := 110, 7
	seq1, seq2 := 1, 2
	subj := "Vómitos desde ayer. <script>alert(1)</script> #set text(red)"
	t0 := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	return app.EncounterView{
		ID: uuid.MustParse("01930000-0000-7000-8000-0000000000e1"), PetID: uuid.MustParse("01930000-0000-7000-8000-0000000000b1"),
		PetName: "Luna", PetSpecies: "dog", Type: "consultation", Status: "signed", StartedAt: t0,
		Reason: "Gastroenteritis", AttendingVetID: &vet, AttendingVetName: "Dra. Pérez",
		Vitals:     &domain.Vitals{WeightKg: &w, TempC: &temp, HeartRateBpm: &hr},
		Subjective: &subj,
		Objective: &domain.Objective{Narrative: "Abdomen doloroso", Systems: map[domain.BodySystem]domain.Finding{
			"digestive": {Status: domain.FindingAbnormal, Note: "dolor a la palpación"}, "cardiovascular": {Status: domain.FindingNormal},
		}},
		Diagnoses: []domain.Diagnosis{{Text: "Gastroenteritis aguda", Certainty: domain.CertaintyPresumptive}},
		Plan: domain.Plan{
			Medications: []domain.MedicationItem{{MedicationName: "Maropitant", Dose: 1, DoseUnit: "mg_per_kg", Route: "sc",
				Frequency: domain.Frequency{Every: 24, Unit: "hours"}, DurationDays: &dur}},
			Procedures:      []domain.Procedure{{Name: "Fluidoterapia"}},
			Recommendations: "Dieta blanda",
		},
		FollowUp: &domain.FollowUpRecommendation{DueDate: kernel.CivilDate("2026-10-27"), Reason: "Revisión"},
		Attachments: []app.AttachmentView{
			{ContentType: "pdf", Description: "Analítica"},
			{ContentType: "png", AddendumSeq: &seq1},
			{ContentType: "jpeg", AddendumSeq: &seq2},
		},
		Signature: &domain.Signature{SignedBy: vet, SignedAt: t0.Add(time.Hour), StreamVersion: 7, ContentHash: strings.Repeat("ab", 32)},
		Addenda: []domain.Addendum{
			{Seq: 1, Text: "Resultados normales", Reason: domain.AddendumLabResults, SignedBy: vet, SignedAt: t0.Add(48 * time.Hour)},
			{Seq: 2, Text: "Corrección de dosis", Reason: domain.AddendumCorrection, SignedBy: vet, SignedAt: t0.Add(72 * time.Hour)},
		},
	}
}

// La versión k del PDF incluye las adendas 1..k-1 (ARQ-010: v1 al firmar,
// v(seq+1) por adenda) y sus adjuntos.
func TestBuildEncounterDocument_Versions(t *testing.T) {
	v := sampleView()
	names := map[uuid.UUID]string{*v.AttendingVetID: "Dra. Pérez"}
	d1 := BuildEncounterDocument(v, 1, "Clínica Sol", names, "https://app.example/verify/encounters/")
	if len(d1.Addenda) != 0 || len(d1.Attachments) != 1 {
		t.Fatalf("v1: adendas %d adjuntos %d", len(d1.Addenda), len(d1.Attachments))
	}
	d2 := BuildEncounterDocument(v, 2, "Clínica Sol", names, "https://app.example/verify/encounters/")
	if len(d2.Addenda) != 1 || d2.Addenda[0].Reason != "Resultados de laboratorio" || len(d2.Attachments) != 2 {
		t.Fatalf("v2: %+v", d2.Addenda)
	}
	d3 := BuildEncounterDocument(v, 3, "Clínica Sol", names, "https://app.example/verify/encounters/")
	if len(d3.Addenda) != 2 {
		t.Fatalf("v3: %d adendas", len(d3.Addenda))
	}
	want := "https://app.example/verify/encounters/" + v.ID.String() + "?hash=" + v.Signature.ContentHash
	if d1.VerifyURL != want || d1.ContentHash != v.Signature.ContentHash || d1.SignedBy != "Dra. Pérez" {
		t.Fatalf("firma/verificación: %+v", d1)
	}
	if d1.Findings[0].System != "cardiovascular" { // orden estable por sistema
		t.Fatalf("findings sin ordenar: %+v", d1.Findings)
	}
	if d1.Medications[0].Frequency != "cada 24 h" || d1.Medications[0].Duration != "7 días" {
		t.Fatalf("medicación: %+v", d1.Medications[0])
	}
}

// Determinista: los mismos datos producen el mismo data.json (caché por
// versión, INF-004).
func TestBuildEncounterDocument_Deterministic(t *testing.T) {
	v := sampleView()
	a, _ := json.Marshal(BuildEncounterDocument(v, 1, "X", nil, "u/"))
	b, _ := json.Marshal(BuildEncounterDocument(v, 1, "X", nil, "u/"))
	if !bytes.Equal(a, b) {
		t.Fatal("data.json no determinista")
	}
}

func TestPDFKey(t *testing.T) {
	pet, enc := uuid.New(), uuid.New()
	if got := PDFKey(pet, enc, 3); got != "clinical/"+pet.String()+"/"+enc.String()+"/v3.pdf" {
		t.Fatal(got)
	}
}

// Render real con Typst cuando el binario está disponible (CI con la imagen
// del worker); el contenido del tutor/clínica no se interpreta como marcado.
func TestEncounterTemplate_RendersWithTypst(t *testing.T) {
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst no instalado")
	}
	v := sampleView()
	d := BuildEncounterDocument(v, 3, "Clínica Sol", map[uuid.UUID]string{*v.AttendingVetID: "Dra. Pérez"}, "https://app.example/verify/encounters/")
	raw, _ := json.Marshal(d)
	code, err := qr.Encode(d.VerifyURL, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := pdfrender.NewTypst("", 1, 30*time.Second).Render(context.Background(), encounterTemplate, raw,
		pdfrender.File{Name: "qr.png", Data: code.PNG()})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("la salida no es un PDF")
	}
}
