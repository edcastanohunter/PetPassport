package domain

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// CardStreamType es el tipo de stream del carnet (vax-<PetID>, DOM-006).
const CardStreamType = "VaccinationCard"

// VaccinationCard es la raíz de agregado del carnet (PP-F2-DOM-006): una por
// mascota. Se rehidrata por replay (zero value + ApplyEvent).
type VaccinationCard struct {
	petID       uuid.UUID
	version     int64
	species     string
	entries     map[uuid.UUID]*CardEntry
	uncommitted []any
}

// PetID devuelve la mascota dueña del carnet.
func (c *VaccinationCard) PetID() uuid.UUID { return c.petID }

// Version devuelve la versión del stream.
func (c *VaccinationCard) Version() int64 { return c.version }

// Species devuelve la especie copiada al crear el carnet.
func (c *VaccinationCard) Species() string { return c.species }

// Entries devuelve copias de las entradas ordenadas por fecha y ID (estable).
func (c *VaccinationCard) Entries() []CardEntry {
	out := make([]CardEntry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AdministeredAt != out[j].AdministeredAt {
			return out[i].AdministeredAt < out[j].AdministeredAt
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

// Entry devuelve una copia de la entrada.
func (c *VaccinationCard) Entry(id uuid.UUID) (CardEntry, bool) {
	e, ok := c.entries[id]
	if !ok {
		return CardEntry{}, false
	}
	return *e, true
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (c *VaccinationCard) UncommittedEvents() []any { return c.uncommitted }

// MarkCommitted limpia los eventos tras el Append.
func (c *VaccinationCard) MarkCommitted() { c.uncommitted = nil }

func (c *VaccinationCard) record(evt any) {
	if err := c.ApplyEvent(evt); err != nil {
		panic("clinical: aplicar evento de carnet recién emitido: " + err.Error())
	}
	c.uncommitted = append(c.uncommitted, evt)
}

// ApplyEvent rehidrata el agregado.
func (c *VaccinationCard) ApplyEvent(evt any) error {
	if c.entries == nil {
		c.entries = map[uuid.UUID]*CardEntry{}
	}
	switch ev := evt.(type) {
	case CardSpeciesSynced:
		c.petID = ev.PetID
		c.species = ev.Species
	case VaccinationEntryRecorded:
		c.petID = ev.PetID
		c.addRecorded(ev.RecordedFields, KindVaccine)
	case PreventiveEntryRecorded:
		c.petID = ev.PetID
		c.addRecorded(ev.RecordedFields, ev.Kind)
	case VaccinationEntryDeclared:
		c.petID = ev.PetID
		by := ev.DeclaredBy
		c.entries[ev.EntryID] = &CardEntry{
			ID: ev.EntryID, Kind: ev.Kind, Product: ev.Product, AdministeredAt: ev.AdministeredAt,
			Lot: ev.Lot, DoseLabel: ev.DoseLabel, Source: SourceDeclared, Verification: Unverified,
			DeclaredBy: &by, NextDue: ev.NextDue, Status: EntryActive, EvidenceKey: ev.EvidenceKey,
		}
	case VaccinationEntryVerified:
		if e := c.entries[ev.EntryID]; e != nil {
			e.Verification = Verified
		}
	case VaccinationEntryDisputed:
		if e := c.entries[ev.EntryID]; e != nil {
			e.Verification = Disputed
		}
	case VaccinationEntryVoided:
		if e := c.entries[ev.EntryID]; e != nil {
			e.Status = EntryVoided
			e.VoidReason = ev.Reason
		}
	case NextDueAdjusted:
		if e := c.entries[ev.EntryID]; e != nil {
			e.NextDue = &NextDue{Date: ev.Date, Basis: ev.Basis}
		}
	default:
		return kernel.NewDomainError("UNKNOWN_EVENT", "evento desconocido para VaccinationCard")
	}
	c.version++
	return nil
}

func (c *VaccinationCard) addRecorded(p RecordedFields, kind EntryKind) {
	by := p.RecordedBy
	c.entries[p.EntryID] = &CardEntry{
		ID: p.EntryID, Kind: kind, Product: p.Product, AdministeredAt: p.AdministeredAt,
		Lot: p.Lot, Manufacturer: p.Manufacturer, ExpiryDate: p.ExpiryDate, Route: p.Route, Site: p.Site,
		DoseLabel: p.DoseLabel, WeightKg: p.WeightKg, Source: p.Source, Verification: Verified,
		RecordedBy: &by, RecordedAt: p.RecordedAt, EncounterID: p.EncounterID, NextDue: p.NextDue,
		Status: EntryActive,
	}
}

// HandleCommand ejecuta un comando de dominio (docs/phase-2/02 §3.4).
func (c *VaccinationCard) HandleCommand(cmd any) ([]any, error) {
	switch x := cmd.(type) {
	case RecordEntry:
		return c.handleRecord(x)
	case DeclareEntry:
		return c.handleDeclare(x)
	case VerifyEntry:
		return c.handleReview(x.EntryID, x.PetLinked, x.Note, x.Actor, true)
	case DisputeEntry:
		return c.handleReview(x.EntryID, x.PetLinked, x.Note, x.Actor, false)
	case VoidEntry:
		return c.handleVoid(x)
	case AdjustNextDue:
		return c.handleAdjust(x)
	case SyncSpecies:
		return c.handleSync(x)
	}
	return nil, kernel.NewDomainError("UNKNOWN_COMMAND", "comando desconocido para VaccinationCard")
}

// entryFields son los campos de producto ya validados.
type entryFields struct {
	kind    EntryKind
	product ProductRef
	date    kernel.CivilDate
	lot     *string
	dose    *string
	nextDue *NextDue
}

// validateInput aplica V2 y V7 y normaliza los VOs. Acumula violaciones.
func validateInput(in EntryInput, birth *kernel.CivilDate, proposed *NextDue, now time.Time, ve *kernel.ValidationError) entryFields {
	var f entryFields
	kind, err := NewEntryKind(in.Kind)
	if err != nil {
		ve.Add("kind", "INVALID")
	}
	f.kind = kind
	product, err := NewProductRef(in.Product.CatalogID, in.Product.Name)
	if err != nil {
		ve.Add("product.name", "INVALID")
	}
	f.product = product
	today := kernel.CivilDateOf(now)
	date, err := kernel.NewCivilDate(in.AdministeredAt)
	switch {
	case err != nil:
		ve.Add("administered_at", "INVALID")
	case date.After(today): // V2
		ve.Add("administered_at", "IN_FUTURE")
	case birth != nil && date.Before(*birth): // V2
		ve.Add("administered_at", "BEFORE_BIRTH")
	}
	f.date = date
	if in.Lot != nil && strings.TrimSpace(*in.Lot) != "" {
		lot, err := NewLot(*in.Lot)
		if err != nil {
			ve.Add("lot", "INVALID")
		} else {
			f.lot = &lot
		}
	}
	if in.DoseLabel != nil && strings.TrimSpace(*in.DoseLabel) != "" {
		d, err := NewDoseLabel(*in.DoseLabel)
		if err != nil {
			ve.Add("dose_label", "INVALID")
		} else {
			f.dose = &d
		}
	}
	switch {
	case in.NextDueDate != nil:
		nd, err := kernel.NewCivilDate(*in.NextDueDate)
		if err != nil {
			ve.Add("next_due.date", "INVALID")
		} else {
			f.nextDue = &NextDue{Date: nd, Basis: BasisClinic}
		}
	case proposed != nil:
		p := *proposed
		f.nextDue = &p
	}
	// V7: solo es comprobable si la fecha de aplicación es válida.
	if f.nextDue != nil && date != "" && !f.nextDue.Date.After(date) {
		ve.Add("next_due.date", "NOT_AFTER_ADMINISTERED")
	}
	return f
}

func optText(p *string, field string, max int, ve *kernel.ValidationError) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	if utf8.RuneCountInString(v) > max {
		ve.Add(field, "TOO_LONG")
		return nil
	}
	return &v
}

// duplicate es V3.
func (c *VaccinationCard) duplicate(kind EntryKind, p ProductRef, date kernel.CivilDate) bool {
	for _, e := range c.entries {
		if e.Status == EntryActive && e.Kind == kind && e.Product.key() == p.key() && e.AdministeredAt == date {
			return true
		}
	}
	return false
}

func (c *VaccinationCard) ensureSpecies(petID uuid.UUID, species string, ve *kernel.ValidationError) (needsSync bool) {
	if c.version != 0 {
		return false
	}
	if strings.TrimSpace(species) == "" {
		ve.Add("species", "INVALID")
	}
	_ = petID
	return true
}

func (c *VaccinationCard) handleRecord(x RecordEntry) ([]any, error) {
	if !x.PetLinked { // V1
		return nil, ErrPetNotAccessible
	}
	if len(c.entries) >= MaxCardEntries { // V6
		return nil, ErrCardFull
	}
	ve := &kernel.ValidationError{}
	if x.PetID == uuid.Nil {
		ve.Add("pet_id", "INVALID")
	}
	if x.EntryID == uuid.Nil {
		ve.Add("entry_id", "INVALID")
	}
	if x.Actor.OrganizationID == uuid.Nil {
		ve.Add("organization_id", "INVALID")
	}
	f := validateInput(x.EntryInput, x.BirthDate, x.Proposed, x.Now, ve)
	manufacturer := optText(x.Manufacturer, "manufacturer", MaxCardTextLen, ve)
	route := optText(x.Route, "route", MaxCardTextLen, ve)
	site := optText(x.Site, "site", MaxCardTextLen, ve)
	var expiry *kernel.CivilDate
	if x.ExpiryDate != nil && strings.TrimSpace(*x.ExpiryDate) != "" {
		d, err := kernel.NewCivilDate(*x.ExpiryDate)
		if err != nil {
			ve.Add("expiry_date", "INVALID")
		} else {
			expiry = &d
		}
	}
	if x.WeightKg != nil && (*x.WeightKg < 0.01 || *x.WeightKg > 500 || !hasMaxDecimals(*x.WeightKg, 2)) {
		ve.Add("weight_kg", "OUT_OF_RANGE")
	}
	needsSync := c.ensureSpecies(x.PetID, x.Species, ve)
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if c.duplicate(f.kind, f.product, f.date) { // V3
		return nil, ErrCardEntryDuplicate
	}
	if needsSync {
		c.record(CardSpeciesSynced{PetID: x.PetID, Species: strings.TrimSpace(x.Species)})
	}
	body := RecordedFields{
		PetID: x.PetID, EntryID: x.EntryID, Product: f.product, AdministeredAt: f.date,
		Lot: f.lot, Manufacturer: manufacturer, ExpiryDate: expiry, Route: route, Site: site,
		DoseLabel: f.dose, WeightKg: x.WeightKg, RecordedBy: x.Actor.entryActor(), RecordedAt: x.Now.UTC(),
		EncounterID: cloneID(x.EncounterID), NextDue: f.nextDue, Source: SourceRecorded,
	}
	if f.kind == KindVaccine {
		c.record(VaccinationEntryRecorded{RecordedFields: body})
	} else {
		c.record(PreventiveEntryRecorded{RecordedFields: body, Kind: f.kind})
	}
	return c.uncommitted, nil
}

func (c *VaccinationCard) handleDeclare(x DeclareEntry) ([]any, error) {
	if !x.Actor.Guardian {
		return nil, ErrGuardianRequired
	}
	if len(c.entries) >= MaxCardEntries { // V6
		return nil, ErrCardFull
	}
	ve := &kernel.ValidationError{}
	if x.PetID == uuid.Nil {
		ve.Add("pet_id", "INVALID")
	}
	if x.EntryID == uuid.Nil {
		ve.Add("entry_id", "INVALID")
	}
	f := validateInput(x.EntryInput, x.BirthDate, x.Proposed, x.Now, ve)
	if f.nextDue != nil {
		f.nextDue.Basis = BasisGuardian // la declaración nunca lleva base clínica
	}
	if x.EvidenceKey != nil && utf8.RuneCountInString(*x.EvidenceKey) > MaxEvidenceKeyLen {
		ve.Add("evidence_object_key", "TOO_LONG")
	}
	needsSync := c.ensureSpecies(x.PetID, x.Species, ve)
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if c.duplicate(f.kind, f.product, f.date) { // V3
		return nil, ErrCardEntryDuplicate
	}
	if needsSync {
		c.record(CardSpeciesSynced{PetID: x.PetID, Species: strings.TrimSpace(x.Species)})
	}
	c.record(VaccinationEntryDeclared{
		PetID: x.PetID, EntryID: x.EntryID, Kind: f.kind, Product: f.product, AdministeredAt: f.date,
		Lot: f.lot, DoseLabel: f.dose, NextDue: f.nextDue, DeclaredBy: x.Actor.UserID, EvidenceKey: x.EvidenceKey,
	})
	return c.uncommitted, nil
}

func (c *VaccinationCard) requireEntry(id uuid.UUID) (*CardEntry, error) {
	e, ok := c.entries[id]
	if !ok {
		return nil, &kernel.NotFoundError{Resource: "card_entry"}
	}
	return e, nil
}

func (c *VaccinationCard) handleReview(id uuid.UUID, linked bool, note *string, actor CardActor, verify bool) ([]any, error) {
	if !linked || actor.OrganizationID == uuid.Nil { // V1
		return nil, ErrPetNotAccessible
	}
	e, err := c.requireEntry(id)
	if err != nil {
		return nil, err
	}
	if e.Status != EntryActive {
		return nil, ErrEntryNotActive
	}
	if e.Source != SourceDeclared {
		return nil, ErrEntryNotDeclared
	}
	want := Disputed
	if verify {
		want = Verified
	}
	if e.Verification == want {
		return nil, ErrNoChanges
	}
	ve := &kernel.ValidationError{}
	n := optText(note, "note", MaxNoteLen, ve)
	if err := ve.ErrOrNil(); err != nil {
		return nil, err
	}
	if verify {
		c.record(VaccinationEntryVerified{PetID: c.petID, EntryID: id, By: actor.entryActor(), Note: n})
	} else {
		c.record(VaccinationEntryDisputed{PetID: c.petID, EntryID: id, By: actor.entryActor(), Note: n})
	}
	return c.uncommitted, nil
}

// handleVoid aplica V4 y V5.
func (c *VaccinationCard) handleVoid(x VoidEntry) ([]any, error) {
	e, err := c.requireEntry(x.EntryID)
	if err != nil {
		return nil, err
	}
	if e.Status != EntryActive { // V4
		return nil, ErrEntryNotActive
	}
	reason := strings.TrimSpace(x.Reason)
	if n := utf8.RuneCountInString(reason); n < 1 || n > MaxVoidReasonLen {
		ve := &kernel.ValidationError{}
		ve.Add("reason", "INVALID")
		return nil, ve
	}
	switch {
	case x.Actor.SuperAdmin: // sin ventana
	case e.Source == SourceDeclared && e.Verification != Verified &&
		x.Actor.Guardian && e.DeclaredBy != nil && *e.DeclaredBy == x.Actor.UserID:
		// el tutor retira su propia declaración mientras nadie la verifique
	case e.RecordedBy != nil && x.Actor.OrganizationID != uuid.Nil && x.Actor.OrganizationID == e.RecordedBy.OrganizationID:
		if !x.PetLinked {
			return nil, ErrPetNotAccessible
		}
		if x.Now.UTC().After(e.RecordedAt.Add(VoidWindowDays * 24 * time.Hour)) {
			return nil, ErrVoidWindowExpired
		}
	default:
		return nil, ErrVoidNotAllowed
	}
	c.record(VaccinationEntryVoided{PetID: c.petID, EntryID: x.EntryID, Reason: reason, By: x.Actor.entryActor()})
	return c.uncommitted, nil
}

func (c *VaccinationCard) handleAdjust(x AdjustNextDue) ([]any, error) {
	if !x.PetLinked || x.Actor.OrganizationID == uuid.Nil { // V1
		return nil, ErrPetNotAccessible
	}
	e, err := c.requireEntry(x.EntryID)
	if err != nil {
		return nil, err
	}
	if e.Status != EntryActive {
		return nil, ErrEntryNotActive
	}
	d, err := kernel.NewCivilDate(x.Date)
	if err != nil {
		ve := &kernel.ValidationError{}
		ve.Add("date", "INVALID")
		return nil, ve
	}
	if !d.After(e.AdministeredAt) { // V7
		ve := &kernel.ValidationError{}
		ve.Add("date", "NOT_AFTER_ADMINISTERED")
		return nil, ve
	}
	if e.NextDue != nil && e.NextDue.Date == d && e.NextDue.Basis == BasisClinic {
		return nil, ErrNoChanges
	}
	c.record(NextDueAdjusted{PetID: c.petID, EntryID: x.EntryID, Date: d, Basis: BasisClinic, By: x.Actor.entryActor()})
	return c.uncommitted, nil
}

func (c *VaccinationCard) handleSync(x SyncSpecies) ([]any, error) {
	if c.version == 0 {
		return nil, &kernel.NotFoundError{Resource: "vaccination_card"}
	}
	s := strings.TrimSpace(x.Species)
	if s == "" {
		ve := &kernel.ValidationError{}
		ve.Add("species", "INVALID")
		return nil, ve
	}
	if s == c.species {
		return nil, ErrNoChanges
	}
	c.record(CardSpeciesSynced{PetID: c.petID, Species: s})
	return c.uncommitted, nil
}
