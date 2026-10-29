package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

var (
	entryID  = uuid.MustParse("0192f3a1-9a10-7f0b-8c3e-0a1b2c3d4e10")
	entry2ID = uuid.MustParse("0192f3a1-9a10-7f0b-8c3e-0a1b2c3d4e11")
	catID    = uuid.MustParse("0192f3a1-5a10-7f0b-8c3e-0a1b2c3d4e20")
	org2ID   = uuid.MustParse("0192f3a1-1111-7000-8000-000000000003")
	guardID  = uuid.MustParse("7a8f0000-0000-7000-8000-0000000000d1")

	clinic  = CardActor{UserID: vetID, OrganizationID: orgID}
	clinic2 = CardActor{UserID: vet2ID, OrganizationID: org2ID}
	tutor   = CardActor{UserID: guardID, Guardian: true}
	admin0  = CardActor{UserID: adminID, SuperAdmin: true}
)

func newCard() *VaccinationCard { return &VaccinationCard{} }

func recordCmd() RecordEntry {
	return RecordEntry{
		PetID: petID, EntryID: entryID, Species: "dog", PetLinked: true,
		EntryInput: EntryInput{
			Kind: "vaccine", Product: ProductRef{CatalogID: ptr(catID), Name: "Polivalente DHPPi"},
			AdministeredAt: "2026-10-20", Lot: ptr("ab-123"), DoseLabel: ptr("1.ª dosis"),
		},
		Manufacturer: ptr("Zoetis"), Route: ptr("subcutánea"),
		Actor: clinic, Now: now,
	}
}

func recordedFields() RecordedFields {
	return RecordedFields{
		PetID: petID, EntryID: entryID, Product: ProductRef{CatalogID: ptr(catID), Name: "Polivalente DHPPi"},
		AdministeredAt: "2026-10-20", Lot: ptr("AB-123"), Manufacturer: ptr("Zoetis"), Route: ptr("subcutánea"),
		DoseLabel: ptr("1.ª dosis"), RecordedBy: EntryActor{UserID: vetID, OrganizationID: orgID},
		RecordedAt: now, Source: SourceRecorded,
	}
}

func speciesSynced() CardSpeciesSynced { return CardSpeciesSynced{PetID: petID, Species: "dog"} }

func recorded() VaccinationEntryRecorded {
	return VaccinationEntryRecorded{RecordedFields: recordedFields()}
}

func declared() VaccinationEntryDeclared {
	return VaccinationEntryDeclared{
		PetID: petID, EntryID: entry2ID, Kind: KindVaccine, Product: ProductRef{Name: "Rabia"},
		AdministeredAt: "2025-03-01", DeclaredBy: guardID,
	}
}

func given(evts ...any) []any { return append([]any{speciesSynced()}, evts...) }

// --- Registro (V1, V2, V3, V6, V7) -------------------------------------------

func TestCard_Record(t *testing.T) {
	// Primera entrada: crea el carnet (CardSpeciesSynced) y registra.
	testkit.Scenario[*VaccinationCard](t, newCard).When(recordCmd()).Then(speciesSynced(), recorded())

	// Con carnet existente no se repite la sincronización de especie.
	c := recordCmd()
	c.EntryID = entry2ID
	c.AdministeredAt = "2026-10-21"
	w := recorded()
	w.EntryID, w.AdministeredAt = entry2ID, "2026-10-21"
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).When(c).Then(w)

	// Preventivo → evento propio con kind.
	p := recordCmd()
	p.Kind, p.Product = "deworming", ProductRef{Name: "Drontal"}
	pw := PreventiveEntryRecorded{RecordedFields: recordedFields(), Kind: KindDeworming}
	pw.Product = ProductRef{Name: "Drontal"}
	testkit.Scenario[*VaccinationCard](t, newCard).When(p).Then(speciesSynced(), pw)
}

// V1: solo con acceso de la organización a la mascota.
func TestCard_V1_PetAccess(t *testing.T) {
	c := recordCmd()
	c.PetLinked = false
	testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenError(ErrPetNotAccessible)

	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).
		When(VerifyEntry{EntryID: entry2ID, Actor: clinic}).ThenError(ErrPetNotAccessible)
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(AdjustNextDue{EntryID: entryID, Date: "2027-10-20", Actor: clinic}).ThenError(ErrPetNotAccessible)
}

// V2: fecha ≤ hoy y ≥ nacimiento.
func TestCard_V2_AdministeredDate(t *testing.T) {
	birth := kernel.CivilDate("2026-06-01")
	for name, mut := range map[string]func(*RecordEntry){
		"future":       func(c *RecordEntry) { c.AdministeredAt = "2026-10-29" },
		"beforeBirth":  func(c *RecordEntry) { c.AdministeredAt = "2026-05-31"; c.BirthDate = &birth },
		"malformed":    func(c *RecordEntry) { c.AdministeredAt = "20/10/2026" },
		"kind":         func(c *RecordEntry) { c.Kind = "surgery" },
		"productEmpty": func(c *RecordEntry) { c.Product.Name = " " },
		"lot":          func(c *RecordEntry) { c.Lot = ptr("lote con espacios!") },
		"doseLen":      func(c *RecordEntry) { c.DoseLabel = ptr(strings.Repeat("x", 41)) },
		"weight":       func(c *RecordEntry) { c.WeightKg = ptr(0.0) },
		"species":      func(c *RecordEntry) { c.Species = "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := recordCmd()
			mut(&c)
			testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenError(&kernel.ValidationError{})
		})
	}
	// Hoy sí es válido; nacimiento el mismo día también.
	c := recordCmd()
	c.AdministeredAt, c.BirthDate = "2026-10-28", ptr(kernel.CivilDate("2026-10-28"))
	testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenAssert(func(t testing.TB, ev []any) {
		if len(ev) != 2 {
			t.Fatalf("eventos = %d, want 2", len(ev))
		}
	})
}

// V3: sin duplicados activos; una anulada libera el hueco.
func TestCard_V3_NoActiveDuplicates(t *testing.T) {
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).When(recordCmd()).
		ThenError(ErrCardEntryDuplicate)

	// Mismo producto con distinto uso de mayúsculas.
	c := recordCmd()
	c.EntryID, c.Product = entry2ID, ProductRef{Name: "POLIVALENTE dhppi"}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).When(c).ThenError(ErrCardEntryDuplicate)

	// Una declaración igual también cuenta.
	d := declared()
	dc := recordCmd()
	dc.EntryID, dc.Product, dc.AdministeredAt = entryID, d.Product, string(d.AdministeredAt)
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(d)...).When(dc).ThenError(ErrCardEntryDuplicate)

	// Anulada: se puede registrar de nuevo.
	voided := VaccinationEntryVoided{PetID: petID, EntryID: entryID, Reason: "error", By: EntryActor{UserID: vetID, OrganizationID: orgID}}
	c2 := recordCmd()
	c2.EntryID = entry2ID
	w := recorded()
	w.EntryID = entry2ID
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded(), voided)...).When(c2).Then(w)
}

// V6: máximo 500 entradas.
func TestCard_V6_MaxEntries(t *testing.T) {
	evts := []any{speciesSynced()}
	base := kernel.CivilDate("2020-01-01")
	for i := 0; i < MaxCardEntries; i++ {
		r := recordedFields()
		r.EntryID = uuid.New()
		r.AdministeredAt = base.AddDays(i)
		evts = append(evts, VaccinationEntryRecorded{RecordedFields: r})
	}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(evts...).When(recordCmd()).ThenError(ErrCardFull)
}

// V7: la próxima dosis es posterior a la aplicación.
func TestCard_V7_NextDueAfterAdministered(t *testing.T) {
	for _, d := range []string{"2026-10-20", "2026-10-19"} {
		c := recordCmd()
		c.NextDueDate = ptr(d)
		testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenError(&kernel.ValidationError{})
	}
	// Dada por el actor → basis clinic.
	c := recordCmd()
	c.NextDueDate = ptr("2026-11-10")
	w := recorded()
	w.NextDue = &NextDue{Date: "2026-11-10", Basis: BasisClinic}
	testkit.Scenario[*VaccinationCard](t, newCard).When(c).Then(speciesSynced(), w)

	// Propuesta del protocolo cuando el actor no da fecha; la explícita gana.
	c2 := recordCmd()
	c2.Proposed = &NextDue{Date: "2026-11-10", Basis: BasisProtocol}
	w2 := recorded()
	w2.NextDue = &NextDue{Date: "2026-11-10", Basis: BasisProtocol}
	testkit.Scenario[*VaccinationCard](t, newCard).When(c2).Then(speciesSynced(), w2)

	// AdjustNextDue también valida V7.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(AdjustNextDue{EntryID: entryID, Date: "2026-10-20", PetLinked: true, Actor: clinic}).
		ThenError(&kernel.ValidationError{})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(AdjustNextDue{EntryID: entryID, Date: "2027-10-20", PetLinked: true, Actor: clinic}).
		Then(NextDueAdjusted{PetID: petID, EntryID: entryID, Date: "2027-10-20", Basis: BasisClinic, By: EntryActor{UserID: vetID, OrganizationID: orgID}})
}

// --- Declarar / verificar / disputar -----------------------------------------

func TestCard_Declare(t *testing.T) {
	cmd := DeclareEntry{
		PetID: petID, EntryID: entry2ID, Species: "dog",
		EntryInput: EntryInput{Kind: "vaccine", Product: ProductRef{Name: "Rabia"}, AdministeredAt: "2025-03-01"},
		Actor:      tutor, Now: now,
	}
	testkit.Scenario[*VaccinationCard](t, newCard).When(cmd).Then(speciesSynced(), declared())

	// Solo el tutor declara.
	cmd.Actor = clinic
	testkit.Scenario[*VaccinationCard](t, newCard).When(cmd).ThenError(ErrGuardianRequired)

	// La declaración nunca lleva basis clinic, aunque el tutor dé la fecha.
	cmd.Actor = tutor
	cmd.NextDueDate = ptr("2026-03-01")
	w := declared()
	w.NextDue = &NextDue{Date: "2026-03-01", Basis: BasisGuardian}
	testkit.Scenario[*VaccinationCard](t, newCard).When(cmd).Then(speciesSynced(), w)
}

func TestCard_VerifyAndDispute(t *testing.T) {
	by := EntryActor{UserID: vetID, OrganizationID: orgID}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).
		When(VerifyEntry{EntryID: entry2ID, PetLinked: true, Actor: clinic}).
		Then(VaccinationEntryVerified{PetID: petID, EntryID: entry2ID, By: by})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).
		When(DisputeEntry{EntryID: entry2ID, PetLinked: true, Note: ptr("lote ilegible"), Actor: clinic}).
		Then(VaccinationEntryDisputed{PetID: petID, EntryID: entry2ID, By: by, Note: ptr("lote ilegible")})

	// ENTRY_NOT_DECLARED sobre una registrada por clínica.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(VerifyEntry{EntryID: entryID, PetLinked: true, Actor: clinic}).ThenError(ErrEntryNotDeclared)

	// Repetir no emite nada.
	testkit.Scenario[*VaccinationCard](t, newCard).
		Given(given(declared(), VaccinationEntryVerified{PetID: petID, EntryID: entry2ID, By: by})...).
		When(VerifyEntry{EntryID: entry2ID, PetLinked: true, Actor: clinic}).ThenError(ErrNoChanges)

	// Inexistente → no encontrada (404).
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).
		When(VerifyEntry{EntryID: uuid.New(), PetLinked: true, Actor: clinic}).ThenError(&kernel.NotFoundError{})

	// Anulada: V4.
	testkit.Scenario[*VaccinationCard](t, newCard).
		Given(given(declared(), VaccinationEntryVoided{PetID: petID, EntryID: entry2ID, Reason: "x", By: by})...).
		When(VerifyEntry{EntryID: entry2ID, PetLinked: true, Actor: clinic}).ThenError(ErrEntryNotActive)
}

// --- Anulación (V4, V5) ---------------------------------------------------------

func TestCard_V4_V5_Void(t *testing.T) {
	voidBy := func(a CardActor, linked bool, at time.Time) VoidEntry {
		return VoidEntry{EntryID: entryID, Reason: "Registrada por error", PetLinked: linked, Actor: a, Now: at}
	}
	want := func(a CardActor) VaccinationEntryVoided {
		return VaccinationEntryVoided{PetID: petID, EntryID: entryID, Reason: "Registrada por error", By: a.entryActor()}
	}

	// Misma organización dentro de 30 días (borde inclusive).
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(clinic, true, now.Add(30*24*time.Hour))).Then(want(clinic))
	// Pasados 30 días → VOID_WINDOW_EXPIRED.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(clinic, true, now.Add(30*24*time.Hour+time.Second))).ThenError(ErrVoidWindowExpired)
	// super_admin sin ventana.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(admin0, false, now.Add(400*24*time.Hour))).Then(want(admin0))
	// Otra organización.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(clinic2, true, now)).ThenError(ErrVoidNotAllowed)
	// La organización perdió el vínculo.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(clinic, false, now)).ThenError(ErrPetNotAccessible)
	// El tutor no anula una entrada registrada por la clínica.
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(voidBy(tutor, false, now)).ThenError(ErrVoidNotAllowed)

	// El tutor sí retira su declaración, mientras no esté verificada.
	dv := VoidEntry{EntryID: entry2ID, Reason: "Me equivoqué", Actor: tutor, Now: now}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).When(dv).
		Then(VaccinationEntryVoided{PetID: petID, EntryID: entry2ID, Reason: "Me equivoqué", By: tutor.entryActor()})
	by := EntryActor{UserID: vetID, OrganizationID: orgID}
	testkit.Scenario[*VaccinationCard](t, newCard).
		Given(given(declared(), VaccinationEntryVerified{PetID: petID, EntryID: entry2ID, By: by})...).
		When(dv).ThenError(ErrVoidNotAllowed)
	// Otro tutor no puede.
	other := CardActor{UserID: uuid.New(), Guardian: true}
	dv.Actor = other
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).When(dv).ThenError(ErrVoidNotAllowed)

	// V4: solo activas.
	voided := VaccinationEntryVoided{PetID: petID, EntryID: entryID, Reason: "x", By: by}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded(), voided)...).
		When(voidBy(clinic, true, now)).ThenError(ErrEntryNotActive)

	// Motivo obligatorio.
	bad := voidBy(clinic, true, now)
	bad.Reason = "  "
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).When(bad).ThenError(&kernel.ValidationError{})
}

func TestCard_SyncSpecies(t *testing.T) {
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(SyncSpecies{PetID: petID, Species: "cat"}).Then(CardSpeciesSynced{PetID: petID, Species: "cat"})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(SyncSpecies{PetID: petID, Species: "dog"}).ThenError(ErrNoChanges)
	testkit.Scenario[*VaccinationCard](t, newCard).When(SyncSpecies{PetID: petID, Species: "dog"}).
		ThenError(&kernel.NotFoundError{})
}

// --- NextDueCalculator ------------------------------------------------------------

func TestNextDueCalculator_Propose(t *testing.T) {
	months := func(n int) *int { return &n }
	birth := kernel.CivilDate("2026-08-01") // ~10 semanas el 2026-10-10
	// Polivalente CO: [{6 sem}, {+21 d}, {+21 d}], refuerzo 12 m.
	polivalente := &Protocol{
		VaccineID:             catID,
		InitialSeries:         []SeriesDose{{MinAgeWeeks: 6}, {MinIntervalDays: 21}, {MinIntervalDays: 21}},
		BoosterIntervalMonths: months(12),
	}
	// Rabia CO: primera dosis ≥ 12 sem, refuerzo 12 m.
	rabia := &Protocol{VaccineID: catID, InitialSeries: []SeriesDose{{MinAgeWeeks: 12}}, BoosterIntervalMonths: months(12)}
	// FeLV CO: 2 dosis (+21 d) y refuerzo 12 m.
	felv := &Protocol{VaccineID: catID, InitialSeries: []SeriesDose{{MinAgeWeeks: 8}, {MinIntervalDays: 21}}, BoosterIntervalMonths: months(12)}
	noBooster := &Protocol{VaccineID: catID, InitialSeries: []SeriesDose{{MinAgeWeeks: 6}}}

	prior := func(date string, v VerificationStatus, st EntryStatus) CardEntry {
		return CardEntry{
			ID: uuid.New(), Kind: KindVaccine, Product: ProductRef{CatalogID: ptr(catID), Name: "X"},
			AdministeredAt: kernel.CivilDate(date), Verification: v, Status: st,
		}
	}
	other := CardEntry{ID: uuid.New(), Kind: KindVaccine, Product: ProductRef{CatalogID: ptr(uuid.New()), Name: "Y"},
		AdministeredAt: "2026-09-01", Verification: Verified, Status: EntryActive}

	tests := []struct {
		name    string
		entries []CardEntry
		date    kernel.CivilDate
		birth   *kernel.CivilDate
		p       *Protocol
		want    *NextDue
	}{
		{"sin protocolo", nil, "2026-10-10", &birth, nil, nil},
		{"protocolo de otra vacuna", nil, "2026-10-10", &birth, &Protocol{VaccineID: uuid.New()}, nil},
		{"1.ª de 3: +21 d", nil, "2026-10-10", &birth, polivalente, &NextDue{"2026-10-31", BasisProtocol}},
		{"2.ª de 3: +21 d", []CardEntry{prior("2026-10-10", Verified, EntryActive)}, "2026-10-31", &birth,
			polivalente, &NextDue{"2026-11-21", BasisProtocol}},
		{"3.ª de 3 completa la serie: refuerzo 12 m",
			[]CardEntry{prior("2026-10-10", Verified, EntryActive), prior("2026-10-31", Verified, EntryActive)},
			"2026-11-21", &birth, polivalente, &NextDue{"2027-11-21", BasisProtocol}},
		{"refuerzo tras la serie", []CardEntry{
			prior("2026-10-10", Verified, EntryActive), prior("2026-10-31", Verified, EntryActive), prior("2026-11-21", Verified, EntryActive)},
			"2027-11-21", &birth, polivalente, &NextDue{"2028-11-21", BasisProtocol}},
		{"edad mínima manda sobre el intervalo (rabia 12 sem)", nil, "2026-08-10", &birth,
			&Protocol{VaccineID: catID, InitialSeries: []SeriesDose{{MinAgeWeeks: 6}, {MinAgeWeeks: 12}}, BoosterIntervalMonths: months(12)},
			&NextDue{"2026-10-24", BasisProtocol}},
		{"rabia: serie de 1 dosis → refuerzo 12 m", nil, "2026-10-24", &birth, rabia, &NextDue{"2027-10-24", BasisProtocol}},
		{"FeLV 1.ª → +21 d", nil, "2026-10-01", &birth, felv, &NextDue{"2026-10-22", BasisProtocol}},
		{"FeLV 2.ª → refuerzo 12 m", []CardEntry{prior("2026-10-01", Verified, EntryActive)}, "2026-10-22", &birth,
			felv, &NextDue{"2027-10-22", BasisProtocol}},
		{"sin fecha de nacimiento ignora la edad", nil, "2026-10-10", nil, polivalente, &NextDue{"2026-10-31", BasisProtocol}},
		{"declarada sin verificar cuenta con basis guardian",
			[]CardEntry{prior("2026-10-10", Unverified, EntryActive)}, "2026-10-31", &birth, polivalente,
			&NextDue{"2026-11-21", BasisGuardian}},
		{"disputada no cuenta", []CardEntry{prior("2026-10-10", Disputed, EntryActive)}, "2026-10-31", &birth,
			polivalente, &NextDue{"2026-11-21", BasisProtocol}},
		{"anulada no cuenta", []CardEntry{prior("2026-10-10", Verified, EntryVoided)}, "2026-10-10", &birth,
			polivalente, &NextDue{"2026-10-31", BasisProtocol}},
		{"otra vacuna no cuenta", []CardEntry{other}, "2026-10-10", &birth, polivalente, &NextDue{"2026-10-31", BasisProtocol}},
		{"dosis posterior (retroactivo) no cuenta", []CardEntry{prior("2026-12-01", Verified, EntryActive)}, "2026-10-10",
			&birth, polivalente, &NextDue{"2026-10-31", BasisProtocol}},
		{"serie completa sin refuerzo definido", nil, "2026-10-10", &birth, noBooster, nil},
		{"refuerzo fin de mes (29 feb)", nil, "2027-02-28", nil, &Protocol{VaccineID: catID, BoosterIntervalMonths: months(12)},
			&NextDue{"2028-02-28", BasisProtocol}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextDueCalculator{}.Propose(tt.entries, catID, tt.date, tt.birth, tt.p)
			switch {
			case tt.want == nil && got.NextDue != nil:
				t.Fatalf("NextDue = %+v, want nil", *got.NextDue)
			case tt.want != nil && (got.NextDue == nil || *got.NextDue != *tt.want):
				t.Fatalf("NextDue = %+v, want %+v", got.NextDue, *tt.want)
			}
			if got.Explanation == "" {
				t.Error("la propuesta debe explicarse")
			}
		})
	}
}

// --- Golden de eventos (ARQ-007) ------------------------------------------------------

func TestCardEvents_Golden(t *testing.T) {
	by := EntryActor{UserID: vetID, OrganizationID: orgID}
	rec := recordedFields()
	rec.ExpiryDate, rec.Site, rec.WeightKg = ptr(kernel.CivilDate("2027-05-01")), ptr("flanco derecho"), ptr(12.4)
	rec.EncounterID, rec.NextDue = ptr(encID), &NextDue{Date: "2026-11-10", Basis: BasisProtocol}
	dec := declared()
	dec.Lot, dec.DoseLabel, dec.NextDue, dec.EvidenceKey = ptr("R-99"), ptr("refuerzo"),
		&NextDue{Date: "2026-03-01", Basis: BasisGuardian}, ptr("pets/p/card/e.jpg")
	events := []any{
		VaccinationEntryRecorded{RecordedFields: rec},
		PreventiveEntryRecorded{RecordedFields: recordedFields(), Kind: KindDeworming},
		dec,
		VaccinationEntryVerified{PetID: petID, EntryID: entry2ID, By: by, Note: ptr("carnet físico revisado")},
		VaccinationEntryDisputed{PetID: petID, EntryID: entry2ID, By: by, Note: ptr("lote ilegible")},
		VaccinationEntryVoided{PetID: petID, EntryID: entryID, Reason: "Registrada por error", By: by},
		NextDueAdjusted{PetID: petID, EntryID: entryID, Date: "2027-10-20", Basis: BasisClinic, By: by},
		CardSpeciesSynced{PetID: petID, Species: "dog"},
	}
	for _, e := range events {
		ev, ok := e.(interface {
			EventName() string
			SchemaVersion() int
		})
		if !ok {
			t.Fatalf("%T no implementa eventstore.Event", e)
		}
		if ev.SchemaVersion() != 1 {
			t.Errorf("%s: schema_version %d, want 1", ev.EventName(), ev.SchemaVersion())
		}
		t.Run(ev.EventName(), func(t *testing.T) {
			raw, err := json.MarshalIndent(e, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "golden", ev.EventName()+".json")
			if *update {
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
				t.Errorf("golden %s cambió:\ngot:\n%s\nwant:\n%s\nSi es intencional: schema_version+1 y upcaster (ARQ-007).", golden, raw, want)
			}
		})
	}
}
