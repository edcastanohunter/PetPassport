package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/testkit"
)

// Rehidratación y lectores: el estado derivado de los eventos.
func TestCard_Rehydrate(t *testing.T) {
	by := EntryActor{UserID: vetID, OrganizationID: orgID}
	c := newCard()
	for _, e := range given(recorded(), declared(),
		VaccinationEntryDisputed{PetID: petID, EntryID: entry2ID, By: by},
		NextDueAdjusted{PetID: petID, EntryID: entryID, Date: "2027-10-20", Basis: BasisClinic, By: by},
		VaccinationEntryVoided{PetID: petID, EntryID: entry2ID, Reason: "x", By: by}) {
		if err := c.ApplyEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	if c.PetID() != petID || c.Species() != "dog" || c.Version() != 6 {
		t.Fatalf("estado = %v %q v%d", c.PetID(), c.Species(), c.Version())
	}
	list := c.Entries()
	if len(list) != 2 || list[0].ID != entry2ID { // 2025-03-01 antes que 2026-10-20
		t.Fatalf("orden de entradas inesperado: %+v", list)
	}
	if list[0].Status != EntryVoided || list[0].Verification != Disputed || list[0].Source != SourceDeclared {
		t.Errorf("declarada = %+v", list[0])
	}
	e, ok := c.Entry(entryID)
	if !ok || e.NextDue == nil || e.NextDue.Basis != BasisClinic || e.Verification != Verified {
		t.Errorf("registrada = %+v", e)
	}
	if _, ok := c.Entry(uuid.New()); ok {
		t.Error("entrada inexistente encontrada")
	}
	if err := c.ApplyEvent(struct{}{}); err == nil {
		t.Error("evento desconocido debe fallar")
	}
	if _, err := c.HandleCommand(struct{}{}); err == nil {
		t.Error("comando desconocido debe fallar")
	}
	// Los eventos emitidos quedan pendientes hasta MarkCommitted.
	c2 := newCard()
	if _, err := c2.HandleCommand(recordCmd()); err != nil {
		t.Fatal(err)
	}
	if len(c2.UncommittedEvents()) != 2 {
		t.Fatalf("pendientes = %d", len(c2.UncommittedEvents()))
	}
	c2.MarkCommitted()
	if len(c2.UncommittedEvents()) != 0 {
		t.Error("MarkCommitted no limpia")
	}
}

func TestCard_RecordValidationEdges(t *testing.T) {
	for name, mut := range map[string]func(*RecordEntry){
		"ids":          func(c *RecordEntry) { c.PetID, c.EntryID = uuid.Nil, uuid.Nil },
		"noOrg":        func(c *RecordEntry) { c.Actor.OrganizationID = uuid.Nil },
		"expiry":       func(c *RecordEntry) { c.ExpiryDate = ptr("31/12/2027") },
		"manufacturer": func(c *RecordEntry) { c.Manufacturer = ptr(strings.Repeat("m", MaxCardTextLen+1)) },
		"nextDueFmt":   func(c *RecordEntry) { c.NextDueDate = ptr("mañana") },
		"weightDec":    func(c *RecordEntry) { c.WeightKg = ptr(12.345) },
	} {
		t.Run(name, func(t *testing.T) {
			c := recordCmd()
			mut(&c)
			testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenError(&kernel.ValidationError{})
		})
	}
	// Campos opcionales en blanco se ignoran; el lote se normaliza.
	c := recordCmd()
	c.Manufacturer, c.Route, c.Lot, c.DoseLabel = ptr("  "), nil, ptr(" "), ptr("")
	testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenAssert(func(t testing.TB, ev []any) {
		r := ev[1].(VaccinationEntryRecorded)
		if r.Manufacturer != nil || r.Lot != nil || r.DoseLabel != nil {
			t.Errorf("opcionales en blanco deben omitirse: %+v", r)
		}
	})
}

func TestCard_DeclareValidationEdges(t *testing.T) {
	base := func() DeclareEntry {
		return DeclareEntry{
			PetID: petID, EntryID: entry2ID, Species: "dog", Actor: tutor, Now: now,
			EntryInput: EntryInput{Kind: "vaccine", Product: ProductRef{Name: "Rabia"}, AdministeredAt: "2025-03-01"},
		}
	}
	for name, mut := range map[string]func(*DeclareEntry){
		"ids":      func(c *DeclareEntry) { c.PetID, c.EntryID = uuid.Nil, uuid.Nil },
		"evidence": func(c *DeclareEntry) { c.EvidenceKey = ptr(strings.Repeat("k", MaxEvidenceKeyLen+1)) },
		"future":   func(c *DeclareEntry) { c.AdministeredAt = "2027-01-01" },
	} {
		t.Run(name, func(t *testing.T) {
			c := base()
			mut(&c)
			testkit.Scenario[*VaccinationCard](t, newCard).When(c).ThenError(&kernel.ValidationError{})
		})
	}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).When(base()).ThenError(ErrCardEntryDuplicate)

	full := []any{speciesSynced()}
	for i := 0; i < MaxCardEntries; i++ {
		r := recordedFields()
		r.EntryID, r.AdministeredAt = uuid.New(), kernel.CivilDate("2020-01-01").AddDays(i)
		full = append(full, VaccinationEntryRecorded{RecordedFields: r})
	}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(full...).When(base()).ThenError(ErrCardFull)
}

func TestCard_AdjustAndSyncEdges(t *testing.T) {
	adj := func(id uuid.UUID, date string) AdjustNextDue {
		return AdjustNextDue{EntryID: id, Date: date, PetLinked: true, Actor: clinic}
	}
	by := EntryActor{UserID: vetID, OrganizationID: orgID}
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(adj(uuid.New(), "2027-01-01")).ThenError(&kernel.NotFoundError{})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(adj(entryID, "pronto")).ThenError(&kernel.ValidationError{})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded(), VaccinationEntryVoided{PetID: petID, EntryID: entryID, Reason: "x", By: by})...).
		When(adj(entryID, "2027-01-01")).ThenError(ErrEntryNotActive)
	// Misma fecha con basis clinic: sin cambios.
	testkit.Scenario[*VaccinationCard](t, newCard).
		Given(given(recorded(), NextDueAdjusted{PetID: petID, EntryID: entryID, Date: "2027-01-01", Basis: BasisClinic, By: by})...).
		When(adj(entryID, "2027-01-01")).ThenError(ErrNoChanges)
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(recorded())...).
		When(SyncSpecies{PetID: petID, Species: " "}).ThenError(&kernel.ValidationError{})
	testkit.Scenario[*VaccinationCard](t, newCard).Given(given(declared())...).
		When(DisputeEntry{EntryID: entry2ID, PetLinked: true, Note: ptr(strings.Repeat("n", MaxNoteLen+1)), Actor: clinic}).
		ThenError(&kernel.ValidationError{})
}

func TestCardVO(t *testing.T) {
	for _, k := range []string{"vaccine", " deworming ", "ectoparasite"} {
		if _, err := NewEntryKind(k); err != nil {
			t.Errorf("kind %q: %v", k, err)
		}
	}
	if _, err := NewEntryKind("x"); err == nil {
		t.Error("kind inválido aceptado")
	}
	if l, err := NewLot(" ab-1/2.3 "); err != nil || l != "AB-1/2.3" {
		t.Errorf("lot = %q, %v", l, err)
	}
	for _, bad := range []string{"", "a b", strings.Repeat("A", 41), "ñ"} {
		if _, err := NewLot(bad); err == nil {
			t.Errorf("lot %q aceptado", bad)
		}
	}
	if _, err := NewProductRef(nil, strings.Repeat("p", 121)); err == nil {
		t.Error("producto largo aceptado")
	}
	if _, err := NewDoseLabel(strings.Repeat("d", 41)); err == nil {
		t.Error("dose label largo aceptado")
	}
}
