package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

// Identidades fijas para pruebas deterministas.
var (
	encID   = uuid.MustParse("0192f3a1-7a10-7f0b-8c3e-0a1b2c3d4e01")
	petID   = uuid.MustParse("0192f3a1-6a10-7f0b-8c3e-0a1b2c3d4e5f")
	orgID   = uuid.MustParse("0192f3a1-1111-7000-8000-000000000002")
	vetID   = uuid.MustParse("7a8f0000-0000-7000-8000-0000000000a1")
	vet2ID  = uuid.MustParse("7a8f0000-0000-7000-8000-0000000000a2")
	recepID = uuid.MustParse("7a8f0000-0000-7000-8000-0000000000b1")
	adminID = uuid.MustParse("7a8f0000-0000-7000-8000-0000000000c1")
	attID   = uuid.MustParse("0192f3a1-8a10-7f0b-8c3e-0a1b2c3d4e02")
	now     = time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)

	vet   = Actor{UserID: vetID, OrgRole: RoleVeterinarian}
	vet2  = Actor{UserID: vet2ID, OrgRole: RoleVeterinarian}
	recep = Actor{UserID: recepID, OrgRole: RoleReceptionist}
	admin = Actor{UserID: adminID, OrgRole: RoleClinicAdmin}
)

func newEnc() *Encounter { return &Encounter{} }

func ptr[T any](v T) *T { return &v }

func opened() EncounterOpened {
	return EncounterOpened{
		EncounterID: encID, PetID: petID, OrganizationID: orgID, Type: TypeConsultation,
		Reason: "Cojera pata trasera", StartedAt: now.Add(-time.Hour), AttendingVetID: ptr(vetID), OpenedBy: recepID,
	}
}

func openCmd() OpenEncounter {
	return OpenEncounter{
		EncounterID: encID, PetID: petID, OrganizationID: orgID, Type: "consultation",
		Reason: "Cojera pata trasera", StartedAt: now.Add(-time.Hour), AttendingVetID: ptr(vetID),
		AttendingVetEligible: true, PetLinked: true, Actor: recep, Now: now,
	}
}

func diagnoses() DiagnosesSet {
	return DiagnosesSet{EncounterID: encID, Diagnoses: []Diagnosis{{Text: "Otitis externa", Certainty: CertaintyConfirmed}}}
}

func signed() EncounterSigned {
	return EncounterSigned{
		EncounterID: encID, SignedBy: vetID, SignedAt: now, StreamVersion: 2,
		ContentHash: strings.Repeat("ab", 32), PetID: petID, OrganizationID: orgID,
	}
}

func attachment(id uuid.UUID) Attachment {
	return Attachment{ID: id, ObjectKey: AttachmentKey(petID, encID, id), ContentType: ContentPDF, SizeBytes: 1024, VisibleToGuardian: true}
}

func signCmd(version int64) SignEncounter {
	return SignEncounter{EncounterID: encID, StreamVersion: version, ContentHash: strings.Repeat("ab", 32), Actor: vet, Now: now}
}

// --- Apertura ----------------------------------------------------------------

func TestEncounter_Open(t *testing.T) {
	want := opened()
	testkit.Scenario[*Encounter](t, newEnc).When(openCmd()).Then(want)

	// Reabrir un stream existente.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(openCmd()).ThenError(ErrEncounterAlreadyOpened)

	// Sin StartedAt → ahora.
	c := openCmd()
	c.StartedAt = time.Time{}
	c.AttendingVetID = nil
	w := opened()
	w.StartedAt = now
	w.AttendingVetID = nil
	testkit.Scenario[*Encounter](t, newEnc).When(c).Then(w)
}

func TestEncounter_OpenValidation(t *testing.T) {
	for name, mut := range map[string]func(*OpenEncounter){
		"type":      func(c *OpenEncounter) { c.Type = "grooming" },
		"reason":    func(c *OpenEncounter) { c.Reason = "  " },
		"reasonLen": func(c *OpenEncounter) { c.Reason = strings.Repeat("a", 301) },
		"html":      func(c *OpenEncounter) { c.Reason = "<script>x</script>" },
		"ids":       func(c *OpenEncounter) { c.EncounterID, c.PetID, c.OrganizationID = uuid.Nil, uuid.Nil, uuid.Nil },
		"vet":       func(c *OpenEncounter) { c.AttendingVetEligible = false },
	} {
		t.Run(name, func(t *testing.T) {
			c := openCmd()
			mut(&c)
			testkit.Scenario[*Encounter](t, newEnc).When(c).ThenError(&kernel.ValidationError{})
		})
	}
}

// E1: solo con vínculo activo (custodia no basta para abrir).
func TestEncounter_E1_PetAccessRequired(t *testing.T) {
	c := openCmd()
	c.PetLinked = false
	testkit.Scenario[*Encounter](t, newEnc).When(c).ThenError(ErrPetNotAccessible)

	// Desde cita: exige check-in.
	c = openCmd()
	c.AppointmentID = ptr(uuid.MustParse("0192f3a1-9a10-7f0b-8c3e-0a1b2c3d4e03"))
	testkit.Scenario[*Encounter](t, newEnc).When(c).ThenError(ErrAppointmentNotCheckedIn)
	c.AppointmentCheckedIn = true
	w := opened()
	w.AppointmentID = c.AppointmentID
	testkit.Scenario[*Encounter](t, newEnc).When(c).Then(w)
}

// E2: StartedAt ∈ [ahora−24 h, ahora+1 h] al abrir y al editar.
func TestEncounter_E2_StartedAtWindow(t *testing.T) {
	for _, d := range []time.Duration{-25 * time.Hour, 61 * time.Minute} {
		c := openCmd()
		c.StartedAt = now.Add(d)
		testkit.Scenario[*Encounter](t, newEnc).When(c).ThenError(&kernel.ValidationError{})
	}
	for _, d := range []time.Duration{-24 * time.Hour, time.Hour} {
		c := openCmd()
		c.StartedAt = now.Add(d)
		w := opened()
		w.StartedAt = now.Add(d)
		testkit.Scenario[*Encounter](t, newEnc).When(c).Then(w)
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateEncounterDetails{EncounterID: encID, StartedAt: ptr(now.Add(-48 * time.Hour)), Actor: recep, Now: now}).
		ThenError(&kernel.ValidationError{})
}

// E3: solo draft admite edición de contenido.
func TestEncounter_E3_OnlyDraftEditable(t *testing.T) {
	for _, given := range [][]any{
		{opened(), diagnoses(), signed()},
		{opened(), EncounterDiscarded{EncounterID: encID, Reason: "duplicada", By: adminID}},
	} {
		for _, cmd := range []any{
			UpdateEncounterDetails{EncounterID: encID, Reason: ptr("Otro"), Actor: vet, Now: now},
			RecordVitals{EncounterID: encID, Vitals: Vitals{WeightKg: ptr(4.2)}, Actor: vet},
			UpdateNarrative{EncounterID: encID, Subjective: ptr("x"), Actor: vet},
			SetDiagnoses{EncounterID: encID, Actor: vet},
			SetPlan{EncounterID: encID, Actor: vet},
			RecommendFollowUp{EncounterID: encID, DueDate: "2026-11-10", Reason: "control", Actor: vet, Now: now},
			ClearFollowUp{EncounterID: encID, Actor: vet},
			AssignAttendingVet{EncounterID: encID, UserID: vet2ID, Eligible: true, Actor: vet},
			AddAttachment{EncounterID: encID, Attachment: attachment(attID), Actor: vet},
			RemoveAttachment{EncounterID: encID, AttachmentID: attID, Actor: vet},
			signCmd(3),
			DiscardEncounter{EncounterID: encID, Reason: "x", Actor: admin},
		} {
			testkit.Scenario[*Encounter](t, newEnc).Given(given...).When(cmd).ThenError(ErrEncounterNotDraft)
		}
	}
}

// E4: diagnósticos, plan, control, visibilidad, firma y adendas solo veterinario.
func TestEncounter_E4_VeterinarianOnly(t *testing.T) {
	for _, a := range []Actor{recep, admin} {
		for _, cmd := range []any{
			SetDiagnoses{EncounterID: encID, Actor: a},
			SetPlan{EncounterID: encID, Actor: a},
			RecommendFollowUp{EncounterID: encID, DueDate: "2026-11-10", Reason: "control", Actor: a, Now: now},
			ClearFollowUp{EncounterID: encID, Actor: a},
			SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: true, Actor: a},
			SignEncounter{EncounterID: encID, StreamVersion: 1, ContentHash: strings.Repeat("ab", 32), Actor: a, Now: now},
		} {
			testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(cmd).ThenError(ErrRoleRequiredVeterinarian)
		}
		testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses(), signed()).
			When(AddAddendum{EncounterID: encID, Text: "x", Reason: "other", Actor: a, Now: now}).
			ThenError(ErrRoleRequiredVeterinarian)
	}
	// La recepcionista sí edita S/O/constantes/adjuntos (RN-07).
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RecordVitals{EncounterID: encID, Vitals: Vitals{WeightKg: ptr(4.25)}, Actor: recep}).
		Then(VitalsRecorded{EncounterID: encID, Vitals: Vitals{WeightKg: ptr(4.25)}})
}

// E5: firma por el profesional responsable.
func TestEncounter_E5_SignedByAttendingVet(t *testing.T) {
	c := signCmd(2)
	c.Actor = vet2
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).When(c).ThenError(ErrNotAttendingVet)

	// Sin responsable asignado → incompleto con el requisito listado.
	o := opened()
	o.AttendingVetID = nil
	s := testkit.Scenario[*Encounter](t, newEnc).Given(o, diagnoses())
	_, err := runCmd(s, signCmd(2))
	var inc *IncompleteError
	if !errors.As(err, &inc) || len(inc.Missing) != 1 || inc.Missing[0] != MissingAttendingVet {
		t.Fatalf("got %v", err)
	}
	if inc.Error() != "ENCOUNTER_INCOMPLETE" {
		t.Fatal(inc.Error())
	}
}

// E6: ≥ 1 diagnóstico, o motivo + hallazgos (objetivo).
func TestEncounter_E6_MinimumContent(t *testing.T) {
	s := testkit.Scenario[*Encounter](t, newEnc).Given(opened())
	_, err := runCmd(s, signCmd(1))
	var inc *IncompleteError
	if !errors.As(err, &inc) || inc.Missing[0] != MissingContent {
		t.Fatalf("got %v", err)
	}
	// Con objetivo y motivo basta.
	obj := EncounterNarrativeUpdated{EncounterID: encID, Objective: &Objective{Narrative: "Dolor a la palpación"}}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), obj).When(signCmd(2)).Then(signed())
	// Con diagnóstico basta.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).When(signCmd(2)).Then(signed())
}

// E7: no se firma un encuentro que empieza en el futuro.
func TestEncounter_E7_NotSignedInFuture(t *testing.T) {
	o := opened()
	o.StartedAt = now.Add(30 * time.Minute)
	s := testkit.Scenario[*Encounter](t, newEnc).Given(o, diagnoses())
	_, err := runCmd(s, signCmd(2))
	var inc *IncompleteError
	if !errors.As(err, &inc) || inc.Missing[0] != MissingStartedAt {
		t.Fatalf("got %v", err)
	}
}

// E8: el hash corresponde a la versión firmada (lo calcula el repositorio; el
// agregado exige que la versión coincida y el formato sea SHA-256 hex).
func TestEncounter_E8_ContentHashMatchesVersion(t *testing.T) {
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).When(signCmd(1)).
		ThenError(kernel.NewDomainError("CONTENT_HASH_MISMATCH", ""))
	c := signCmd(2)
	c.ContentHash = "abc"
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).When(c).
		ThenError(kernel.NewDomainError("CONTENT_HASH_MISMATCH", ""))
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).When(signCmd(2)).Then(signed()).Agg()
	if agg.Status() != StatusSigned || agg.signature.StreamVersion != 2 || agg.Version() != 3 {
		t.Fatalf("estado tras firmar: %+v", agg.signature)
	}
}

// E9: tras la firma solo adendas y visibilidad.
func TestEncounter_E9_AfterSignOnlyAddendumAndVisibility(t *testing.T) {
	given := []any{opened(), AttachmentAdded{EncounterID: encID, Attachment: attachment(attID)}, diagnoses(), signed()}
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(AddAddendum{EncounterID: encID, Text: "Resultado de laboratorio: normal", Reason: "lab_results", Actor: vet2, Now: now}).
		Then(EncounterAddendumAdded{EncounterID: encID, Seq: 1, Text: "Resultado de laboratorio: normal", Reason: AddendumLabResults, SignedBy: vet2ID, SignedAt: now, Attachments: []Attachment{}})
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: false, Reason: "nota interna", Actor: vet}).
		Then(AttachmentVisibilityChanged{EncounterID: encID, AttachmentID: attID, Visible: false, Reason: "nota interna"})
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(RemoveAttachment{EncounterID: encID, AttachmentID: attID, Actor: vet}).ThenError(ErrEncounterNotDraft)
}

// --- Resto de comandos ---------------------------------------------------------

func TestEncounter_UpdateDetails(t *testing.T) {
	newStart := now.Add(-2 * time.Hour)
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateEncounterDetails{EncounterID: encID, Type: ptr("emergency"), Reason: ptr(" Vómitos "), StartedAt: &newStart, Actor: recep, Now: now}).
		Then(EncounterDetailsUpdated{EncounterID: encID, Type: ptr(TypeEmergency), Reason: ptr("Vómitos"), StartedAt: &newStart})
	// Sin cambios reales → sin eventos (autoguardado idempotente).
	same := now.Add(-time.Hour)
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateEncounterDetails{EncounterID: encID, Type: ptr("consultation"), Reason: ptr("Cojera pata trasera"), StartedAt: &same, Actor: recep, Now: now}).
		ThenNoEvents()
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateEncounterDetails{EncounterID: encID, Type: ptr("x"), Reason: ptr(""), Actor: recep, Now: now}).
		ThenError(&kernel.ValidationError{})
	// Rehidratación del evento.
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(opened(), EncounterDetailsUpdated{EncounterID: encID, Type: ptr(TypeSurgery), Reason: ptr("r"), StartedAt: &newStart}).
		When(UpdateEncounterDetails{EncounterID: encID, Actor: recep, Now: now})
	agg.ThenNoEvents()
	if agg.Agg().typ != TypeSurgery || agg.Agg().reason != "r" || !agg.Agg().startedAt.Equal(newStart) {
		t.Fatal("rehidratación de EncounterDetailsUpdated")
	}
}

func TestEncounter_Vitals(t *testing.T) {
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RecordVitals{EncounterID: encID, Vitals: Vitals{}, Actor: recep}).ThenError(&kernel.ValidationError{})
	full := Vitals{
		WeightKg: ptr(12.5), TempC: ptr(38.6), HeartRateBpm: ptr(90), RespRateRpm: ptr(20), BodyCondition: ptr(5),
		Mucosa: ptr(MucosaPink), CRTSeconds: ptr(1.5), HydrationPct: ptr(5.0),
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RecordVitals{EncounterID: encID, Vitals: full, Actor: recep}).Then(VitalsRecorded{EncounterID: encID, Vitals: full})
}

func TestVitals_Ranges(t *testing.T) {
	for name, v := range map[string]Vitals{
		"weight0":   {WeightKg: ptr(0.0)},
		"weight501": {WeightKg: ptr(501.0)},
		"weightDec": {WeightKg: ptr(4.255)},
		"temp":      {TempC: ptr(29.9)},
		"tempDec":   {TempC: ptr(38.55)},
		"hr":        {HeartRateBpm: ptr(19)},
		"rr":        {RespRateRpm: ptr(201)},
		"bcs":       {BodyCondition: ptr(10)},
		"mucosa":    {Mucosa: ptr(Mucosa("blue"))},
		"crt":       {CRTSeconds: ptr(0.4)},
		"hyd":       {HydrationPct: ptr(16.0)},
	} {
		ve := &kernel.ValidationError{}
		v.Validate(ve, "")
		if !ve.Any() {
			t.Errorf("%s: se esperaba violación", name)
		}
	}
	ve := &kernel.ValidationError{}
	Vitals{WeightKg: ptr(0.01), TempC: ptr(45.0), HeartRateBpm: ptr(400), RespRateRpm: ptr(5), BodyCondition: ptr(1),
		Mucosa: ptr(MucosaIcteric), CRTSeconds: ptr(5.0), HydrationPct: ptr(0.0)}.Validate(ve, "")
	if ve.Any() {
		t.Fatalf("límites válidos rechazados: %v", ve)
	}
}

func TestVitalsWarnings(t *testing.T) {
	w := VitalsWarnings("cat", Vitals{TempC: ptr(40.5), HeartRateBpm: ptr(100), RespRateRpm: ptr(25), WeightKg: ptr(4.0)})
	if len(w) != 2 || w[0].Field != "temp_c" || w[0].Code != "ABOVE_SPECIES_RANGE" || w[1].Field != "heart_rate_bpm" || w[1].Code != "BELOW_SPECIES_RANGE" {
		t.Fatalf("warnings: %+v", w)
	}
	if got := VitalsWarnings("fish", Vitals{TempC: ptr(40.0)}); len(got) != 0 {
		t.Fatalf("especie sin tabla: %+v", got)
	}
}

func TestEncounter_Narrative(t *testing.T) {
	obj := &Objective{Narrative: " Mucosas rosadas ", Systems: map[BodySystem]Finding{"ears": {Status: FindingAbnormal, Note: "eritema"}}}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateNarrative{EncounterID: encID, Subjective: ptr("Rasca la oreja\n- desde hace 3 días"), Objective: obj, Actor: recep}).
		Then(EncounterNarrativeUpdated{EncounterID: encID, Subjective: ptr("Rasca la oreja\n- desde hace 3 días"),
			Objective: &Objective{Narrative: "Mucosas rosadas", Systems: map[BodySystem]Finding{"ears": {Status: FindingAbnormal, Note: "eritema"}}}})
	// Mismo contenido → sin eventos.
	given := EncounterNarrativeUpdated{EncounterID: encID, Subjective: ptr("a"), Objective: &Objective{Narrative: "b", Systems: map[BodySystem]Finding{"ears": {Status: FindingNormal}}}}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), given).
		When(UpdateNarrative{EncounterID: encID, Subjective: ptr("a"), Objective: &Objective{Narrative: "b", Systems: map[BodySystem]Finding{"ears": {Status: FindingNormal}}}, Actor: recep}).
		ThenNoEvents()
	// Cambiar solo un hallazgo sí emite.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), given).
		When(UpdateNarrative{EncounterID: encID, Objective: &Objective{Narrative: "b", Systems: map[BodySystem]Finding{"eyes": {Status: FindingNormal}}}, Actor: recep}).
		Then(EncounterNarrativeUpdated{EncounterID: encID, Objective: &Objective{Narrative: "b", Systems: map[BodySystem]Finding{"eyes": {Status: FindingNormal}}}})
	// Objetivo vacío sobre vacío → sin eventos.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(UpdateNarrative{EncounterID: encID, Objective: &Objective{}, Actor: recep}).ThenNoEvents()
	for name, cmd := range map[string]UpdateNarrative{
		"control": {Subjective: ptr("a\x00b")},
		"long":    {Subjective: ptr(strings.Repeat("a", 5001))},
		"system":  {Objective: &Objective{Systems: map[BodySystem]Finding{"tail": {Status: FindingNormal}}}},
		"status":  {Objective: &Objective{Systems: map[BodySystem]Finding{"ears": {Status: "meh"}}}},
		"note":    {Objective: &Objective{Systems: map[BodySystem]Finding{"ears": {Status: FindingNormal, Note: strings.Repeat("a", 501)}}}},
		"html":    {Objective: &Objective{Narrative: "<img src=x>"}},
	} {
		t.Run(name, func(t *testing.T) {
			cmd.EncounterID, cmd.Actor = encID, recep
			testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(cmd).ThenError(&kernel.ValidationError{})
		})
	}
	// "T < 39" no es HTML.
	if _, err := NewNarrative("T < 39 y FC 2<3"); err != nil {
		t.Fatal(err)
	}
}

func TestEncounter_Diagnoses(t *testing.T) {
	ds := []Diagnosis{{Text: " Otitis ", Code: &DiagnosisCode{System: "venom", Value: "123"}, Certainty: CertaintyPresumptive}}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(SetDiagnoses{EncounterID: encID, Diagnoses: ds, Actor: vet}).
		Then(DiagnosesSet{EncounterID: encID, Diagnoses: []Diagnosis{{Text: "Otitis", Code: &DiagnosisCode{System: "venom", Value: "123"}, Certainty: CertaintyPresumptive}}})
	many := make([]Diagnosis, 21)
	for i := range many {
		many[i] = Diagnosis{Text: "d", Certainty: CertaintyConfirmed}
	}
	for _, bad := range [][]Diagnosis{many, {{Text: "", Certainty: CertaintyConfirmed}}, {{Text: "x", Certainty: "maybe"}}, {{Text: "x", Certainty: CertaintyRuledOut, Code: &DiagnosisCode{}}}} {
		testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
			When(SetDiagnoses{EncounterID: encID, Diagnoses: bad, Actor: vet}).ThenError(&kernel.ValidationError{})
	}
}

func TestEncounter_Plan(t *testing.T) {
	plan := Plan{
		Medications: []MedicationItem{
			{MedicationName: "Amoxicilina", Dose: 12.5, DoseUnit: "mg_per_kg", Route: "oral", Frequency: Frequency{Every: 12, Unit: "hours"}, DurationDays: ptr(7)},
			{MedicationName: "Meloxicam", Dose: 0.1, DoseUnit: "mg_per_kg", Route: "sc", Frequency: Frequency{Once: true}},
		},
		Procedures:      []Procedure{{Name: "Limpieza ótica", Notes: "bilateral"}},
		Recommendations: "Reposo",
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(SetPlan{EncounterID: encID, Plan: plan, Actor: vet}).Then(PlanSet{EncounterID: encID, Plan: plan})
	med := func(f func(*MedicationItem)) Plan {
		m := plan.Medications[0]
		f(&m)
		return Plan{Medications: []MedicationItem{m}}
	}
	tooMany := Plan{Medications: make([]MedicationItem, 31), Procedures: make([]Procedure, 21)}
	for name, p := range map[string]Plan{
		"name":      med(func(m *MedicationItem) { m.MedicationName = "" }),
		"pres":      med(func(m *MedicationItem) { m.Presentation = strings.Repeat("p", 121) }),
		"dose":      med(func(m *MedicationItem) { m.Dose = 0 }),
		"doseDec":   med(func(m *MedicationItem) { m.Dose = 1.0005 }),
		"unit":      med(func(m *MedicationItem) { m.DoseUnit = "kg" }),
		"route":     med(func(m *MedicationItem) { m.Route = "nasal" }),
		"freq":      med(func(m *MedicationItem) { m.Frequency = Frequency{Every: 0, Unit: "hours"} }),
		"freqOnce":  med(func(m *MedicationItem) { m.Frequency = Frequency{Once: true, Every: 2} }),
		"freqDays":  med(func(m *MedicationItem) { m.Frequency = Frequency{Every: 91, Unit: "days"} }),
		"duration":  med(func(m *MedicationItem) { m.DurationDays = ptr(366) }),
		"instr":     med(func(m *MedicationItem) { m.Instructions = strings.Repeat("i", 301) }),
		"tooMany":   tooMany,
		"procName":  {Procedures: []Procedure{{Name: ""}}},
		"procNotes": {Procedures: []Procedure{{Name: "x", Notes: strings.Repeat("n", 1001)}}},
		"rec":       {Recommendations: "<b>x</b>"},
	} {
		t.Run(name, func(t *testing.T) {
			testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
				When(SetPlan{EncounterID: encID, Plan: p, Actor: vet}).ThenError(&kernel.ValidationError{})
		})
	}
	// Frecuencia en días válida.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(SetPlan{EncounterID: encID, Plan: med(func(m *MedicationItem) { m.Frequency = Frequency{Every: 1, Unit: "days"} }), Actor: vet}).
		ThenAssert(func(t testing.TB, evs []any) {
			if len(evs) != 1 {
				t.Fatal(evs)
			}
		})
}

func TestEncounter_FollowUp(t *testing.T) {
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RecommendFollowUp{EncounterID: encID, DueDate: "2026-11-11", Reason: "Revisión", Actor: vet, Now: now}).
		Then(FollowUpRecommended{EncounterID: encID, DueDate: "2026-11-11", Reason: "Revisión"})
	for _, due := range []string{"2026-10-28", "2027-10-29", "11/11/2026"} {
		testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
			When(RecommendFollowUp{EncounterID: encID, DueDate: due, Reason: "Revisión", Actor: vet, Now: now}).
			ThenError(&kernel.ValidationError{})
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RecommendFollowUp{EncounterID: encID, DueDate: "2026-11-11", Reason: "", Actor: vet, Now: now}).
		ThenError(&kernel.ValidationError{})
	fu := FollowUpRecommended{EncounterID: encID, DueDate: "2026-11-11", Reason: "Revisión"}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), fu).
		When(RecommendFollowUp{EncounterID: encID, DueDate: "2026-11-11", Reason: "Revisión", Actor: vet, Now: now}).ThenNoEvents()
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), fu).
		When(ClearFollowUp{EncounterID: encID, Actor: vet}).Then(FollowUpCleared{EncounterID: encID})
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), fu, FollowUpCleared{EncounterID: encID}).
		When(ClearFollowUp{EncounterID: encID, Actor: vet}).ThenNoEvents()
}

func TestEncounter_AssignAttendingVet(t *testing.T) {
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(AssignAttendingVet{EncounterID: encID, UserID: vet2ID, Eligible: true, Actor: recep}).
		Then(AttendingVetAssigned{EncounterID: encID, UserID: vet2ID})
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(AssignAttendingVet{EncounterID: encID, UserID: vetID, Eligible: true, Actor: recep}).ThenNoEvents()
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(AssignAttendingVet{EncounterID: encID, UserID: recepID, Eligible: false, Actor: recep}).ThenError(ErrAttendingVetNotEligible)
	// Tras reasignar, firma el nuevo responsable.
	c := signCmd(3)
	c.Actor = vet2
	s := signed()
	s.SignedBy, s.StreamVersion = vet2ID, 3
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), AttendingVetAssigned{EncounterID: encID, UserID: vet2ID}, diagnoses()).When(c).Then(s)
}

func TestEncounter_Attachments(t *testing.T) {
	a := attachment(attID)
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(AddAttachment{EncounterID: encID, Attachment: a, Actor: recep}).
		Then(AttachmentAdded{EncounterID: encID, Attachment: a}).Agg()
	if got, ok := agg.Attachment(attID); !ok || got.SizeBytes != 1024 || agg.AttachmentCount() != 1 {
		t.Fatal("adjunto no aplicado")
	}
	given := []any{opened(), AttachmentAdded{EncounterID: encID, Attachment: a}}
	// Reintento idempotente.
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).When(AddAttachment{EncounterID: encID, Attachment: a, Actor: recep}).ThenNoEvents()
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(RemoveAttachment{EncounterID: encID, AttachmentID: attID, Actor: recep}).
		Then(AttachmentRemoved{EncounterID: encID, AttachmentID: attID})
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(RemoveAttachment{EncounterID: encID, AttachmentID: attID, Actor: recep}).ThenError(ErrAttachmentNotFound)
	// Límite de 10.
	full := []any{opened()}
	for i := 0; i < MaxAttachments; i++ {
		full = append(full, AttachmentAdded{EncounterID: encID, Attachment: attachment(uuid.New())})
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(full...).When(AddAttachment{EncounterID: encID, Attachment: a, Actor: recep}).ThenError(ErrAttachmentLimit)
	// Validación.
	bad := a
	bad.ObjectKey, bad.ContentType, bad.SizeBytes, bad.Description = "otra/clave", "gif", MaxAttachmentBytes+1, strings.Repeat("d", 201)
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(AddAttachment{EncounterID: encID, Attachment: bad, Actor: recep}).ThenError(&kernel.ValidationError{})
	bad = Attachment{ObjectKey: AttachmentKey(petID, encID, uuid.Nil), ContentType: ContentJPEG, SizeBytes: 1}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(AddAttachment{EncounterID: encID, Attachment: bad, Actor: recep}).ThenError(&kernel.ValidationError{})
}

func TestEncounter_Visibility(t *testing.T) {
	given := []any{opened(), AttachmentAdded{EncounterID: encID, Attachment: attachment(attID)}}
	// Ocultar exige motivo.
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: false, Actor: vet}).ThenError(&kernel.ValidationError{})
	// Mostrar lo ya visible → sin eventos.
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: true, Actor: vet}).ThenNoEvents()
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: true, Actor: vet}).ThenError(ErrAttachmentNotFound)
	hidden := AttachmentVisibilityChanged{EncounterID: encID, AttachmentID: attID, Visible: false, Reason: "interna"}
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(append(given, hidden)...).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: true, Actor: vet}).
		Then(AttachmentVisibilityChanged{EncounterID: encID, AttachmentID: attID, Visible: true}).Agg()
	if a, _ := agg.Attachment(attID); !a.VisibleToGuardian {
		t.Fatal("visibilidad no aplicada")
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), EncounterDiscarded{EncounterID: encID, Reason: "x", By: adminID}).
		When(SetTutorVisibility{EncounterID: encID, AttachmentID: attID, Visible: true, Actor: vet}).ThenError(ErrEncounterNotDraft)
}

func TestEncounter_Addenda(t *testing.T) {
	given := []any{opened(), diagnoses(), signed()}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened(), diagnoses()).
		When(AddAddendum{EncounterID: encID, Text: "x", Reason: "other", Actor: vet, Now: now}).ThenError(ErrEncounterNotSigned)
	a := attachment(attID)
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(AddAddendum{EncounterID: encID, Text: "Hemograma normal", Reason: "lab_results", Attachments: []Attachment{a}, Actor: vet, Now: now}).
		Then(EncounterAddendumAdded{EncounterID: encID, Seq: 1, Text: "Hemograma normal", Reason: AddendumLabResults, SignedBy: vetID, SignedAt: now, Attachments: []Attachment{a}}).Agg()
	if _, ok := agg.Attachment(attID); !ok || agg.AttachmentCount() != 0 {
		t.Fatal("los adjuntos de adenda no cuentan en el cuerpo")
	}
	withOne := append(append([]any{}, given...), EncounterAddendumAdded{EncounterID: encID, Seq: 1, Text: "a", Reason: AddendumOther, SignedBy: vetID, SignedAt: now, Attachments: []Attachment{a}})
	testkit.Scenario[*Encounter](t, newEnc).Given(withOne...).
		When(AddAddendum{EncounterID: encID, Text: "b", Reason: "correction", Attachments: []Attachment{a}, Actor: vet, Now: now}).
		ThenError(&kernel.ValidationError{})
	for _, cmd := range []AddAddendum{
		{Text: "", Reason: "other"},
		{Text: "x", Reason: "because"},
		{Text: strings.Repeat("x", 5001), Reason: "other"},
	} {
		cmd.EncounterID, cmd.Actor, cmd.Now = encID, vet, now
		testkit.Scenario[*Encounter](t, newEnc).Given(given...).When(cmd).ThenError(&kernel.ValidationError{})
	}
	six := make([]Attachment, 6)
	testkit.Scenario[*Encounter](t, newEnc).Given(given...).
		When(AddAddendum{EncounterID: encID, Text: "x", Reason: "other", Attachments: six, Actor: vet, Now: now}).ThenError(ErrAttachmentLimit)
	full := append([]any{}, given...)
	for i := 1; i <= MaxAddenda; i++ {
		full = append(full, EncounterAddendumAdded{EncounterID: encID, Seq: i, Text: "a", Reason: AddendumOther, SignedBy: vetID, SignedAt: now, Attachments: []Attachment{}})
	}
	testkit.Scenario[*Encounter](t, newEnc).Given(full...).
		When(AddAddendum{EncounterID: encID, Text: "x", Reason: "other", Actor: vet, Now: now}).ThenError(ErrAddendaLimit)
}

func TestEncounter_Discard(t *testing.T) {
	want := EncounterDiscarded{EncounterID: encID, Reason: "Duplicada", By: adminID}
	agg := testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(DiscardEncounter{EncounterID: encID, Reason: " Duplicada ", Actor: admin}).Then(want).Agg()
	if agg.Status() != StatusDiscarded {
		t.Fatal(agg.Status())
	}
	// El responsable puede; otro veterinario o la recepcionista no.
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(DiscardEncounter{EncounterID: encID, Reason: "x", Actor: vet}).Then(EncounterDiscarded{EncounterID: encID, Reason: "x", By: vetID})
	for _, a := range []Actor{vet2, recep} {
		testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
			When(DiscardEncounter{EncounterID: encID, Reason: "x", Actor: a}).ThenError(ErrDiscardNotAllowed)
	}
	sa := Actor{UserID: uuid.MustParse("7a8f0000-0000-7000-8000-0000000000d1"), SuperAdmin: true}
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(DiscardEncounter{EncounterID: encID, Reason: "x", Actor: sa}).Then(EncounterDiscarded{EncounterID: encID, Reason: "x", By: sa.UserID})
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).
		When(DiscardEncounter{EncounterID: encID, Reason: "", Actor: admin}).ThenError(&kernel.ValidationError{})
}

func TestEncounter_UnknownAndMissing(t *testing.T) {
	testkit.Scenario[*Encounter](t, newEnc).When(RecordVitals{EncounterID: encID}).ThenError(&kernel.NotFoundError{})
	testkit.Scenario[*Encounter](t, newEnc).Given(opened()).When(struct{}{}).ThenError(&kernel.DomainError{Code: "UNKNOWN_COMMAND"})
	if err := newEnc().ApplyEvent(struct{}{}); err == nil {
		t.Fatal("evento desconocido aceptado")
	}
	e := newEnc()
	for _, ev := range []any{opened(), VitalsRecorded{EncounterID: encID, Vitals: Vitals{WeightKg: ptr(3.0)}}, PlanSet{EncounterID: encID, Plan: Plan{}}} {
		if err := e.ApplyEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	if e.ID() != encID || e.PetID() != petID || e.OrganizationID() != orgID || *e.AttendingVetID() != vetID || e.vitals == nil {
		t.Fatal("getters")
	}
	e.uncommitted = []any{1}
	if len(e.UncommittedEvents()) != 1 {
		t.Fatal("uncommitted")
	}
	e.MarkCommitted()
	if e.UncommittedEvents() != nil {
		t.Fatal("MarkCommitted")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("record de evento inválido debe hacer pánico")
		}
	}()
	e.record(struct{}{})
}

func TestEncounterType(t *testing.T) {
	for _, s := range []string{"consultation", "follow_up", "emergency", "vaccination", "surgery", "procedure", "other"} {
		if _, err := NewEncounterType(s); err != nil {
			t.Error(s)
		}
	}
}

// runCmd ejecuta el escenario y devuelve los eventos y el error sin aserción.
func runCmd(s *testkit.ScenarioCtx[*Encounter], cmd any) ([]any, error) {
	return s.When(cmd).Capture()
}
