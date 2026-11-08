package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

func sampleRule() *RegulatoryRule {
	return &RegulatoryRule{
		Country:                        "CO",
		DefaultValidityDays:            30,
		MaxValidityDays:                180,
		MaxRefills:                     5,
		ControlledOrderable:            false,
		RequiresCredentialVerification: true,
	}
}

func sampleItem(catID *uuid.UUID, name string, controlled bool) PrescriptionItem {
	return PrescriptionItem{
		ItemID:       uuid.New().String(),
		CatalogID:    catID,
		NameSnapshot: name,
		Dose: DoseVO{
			Value: 10,
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
		Instructions:        "Tomar con comida",
		Controlled:          controlled,
	}
}

func ptr[T any](v T) *T { return &v }

func TestPrescription_R1_VerifiedCredential(t *testing.T) {
	rx := NewPrescription()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	petID := uuid.New()
	guardianID := uuid.New()
	vetID := uuid.New()
	orgID := uuid.New()
	rules := sampleRule()

	item := sampleItem(ptr(uuid.New()), "Amoxicilina 500mg", false)

	// Sin credencial (nil): debe fallar con ErrCredentialRequired
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), petID, guardianID, vetID, orgID, nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, nil, false, now,
	)
	if err != ErrCredentialRequired {
		t.Fatalf("expected ErrCredentialRequired, got: %v", err)
	}

	// Con credencial verificada: éxito
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	draftEvt, err := rx.DraftIssuedPrescription(
		uuid.New(), petID, guardianID, vetID, orgID, nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draftEvt.Source != SourceIssued {
		t.Errorf("expected source issued")
	}

	// Al emitir (IssuePrescription), si la credencial falta debe fallar
	_, err = rx.IssuePrescription(vetID, nil, rules, "hash123", now)
	if err != ErrCredentialRequired {
		t.Fatalf("expected ErrCredentialRequired on issue without credential, got: %v", err)
	}

	// Al emitir con credencial: éxito
	issueEvt, err := rx.IssuePrescription(vetID, credRef, rules, "hash123", now)
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}
	if issueEvt.ContentHash != "hash123" {
		t.Errorf("content hash not recorded")
	}
}

func TestPrescription_R2_OrgAccess(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	item := sampleItem(ptr(uuid.New()), "Enrofloxacina", false)

	// Org sin acceso a la mascota (hasOrgAccess = false)
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, false, credRef, false, now,
	)
	if err != ErrOrgAccessDenied {
		t.Fatalf("expected ErrOrgAccessDenied, got: %v", err)
	}
}

func TestPrescription_R2Prime_ExternalGuardian(t *testing.T) {
	rx := NewPrescription()
	images := []ImageRef{
		{ObjectKey: "img1.jpg", ContentType: "image/jpeg", Page: 1, Sha256: []byte("sha1")},
	}

	_, err := rx.UploadExternalPrescription(uuid.New(), uuid.New(), uuid.New(), images, "CO")
	if err != nil {
		t.Fatalf("unexpected upload error: %v", err)
	}
	if rx.Source != SourceExternal {
		t.Errorf("expected external source, got: %s", rx.Source)
	}

	// Más de 5 imágenes debe fallar
	rx2 := NewPrescription()
	var tooMany []ImageRef
	for i := 0; i < 6; i++ {
		tooMany = append(tooMany, ImageRef{ObjectKey: "img.jpg", ContentType: "image/jpeg", Page: i + 1})
	}
	_, err = rx2.UploadExternalPrescription(uuid.New(), uuid.New(), uuid.New(), tooMany, "CO")
	if err != ErrExternalUploadLimit {
		t.Fatalf("expected ErrExternalUploadLimit, got: %v", err)
	}
}

func TestPrescription_R3_EncounterSigned(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	item := sampleItem(ptr(uuid.New()), "Meloxicam", false)
	encID := uuid.New()

	// Encounter no firmado o inconsistente (hasSignedPlan = false)
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), &encID, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	if err != ErrEncounterNotSigned {
		t.Fatalf("expected ErrEncounterNotSigned, got: %v", err)
	}

	// Encounter firmado: permitido
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), &encID, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, true, now,
	)
	if err != nil {
		t.Fatalf("expected success with signed encounter, got: %v", err)
	}
}

func TestPrescription_R4_ValidityWithinRules(t *testing.T) {
	rx := NewPrescription()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	item := sampleItem(ptr(uuid.New()), "Cefalexina", false)

	// Refills mayor al máximo permitido (5)
	invalidValidity := &Validity{
		ValidUntil:     kernel.CivilDateOf(now.AddDate(0, 1, 0)),
		RefillsAllowed: 6,
	}
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, invalidValidity, nil, rules, true, credRef, false, now,
	)
	if err != ErrValidityOutOfRules {
		t.Fatalf("expected ErrValidityOutOfRules for refills=6, got: %v", err)
	}

	// Fecha posterior al máximo permitido (180 días)
	tooFar := &Validity{
		ValidUntil:     kernel.CivilDateOf(now.AddDate(0, 0, 200)),
		RefillsAllowed: 1,
	}
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, tooFar, nil, rules, true, credRef, false, now,
	)
	if err != ErrValidityOutOfRules {
		t.Fatalf("expected ErrValidityOutOfRules for validity > 180 days, got: %v", err)
	}
}

func TestPrescription_R5_ValidItems(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()

	// Lista vacía
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{}, nil, nil, rules, true, credRef, false, now,
	)
	if err != ErrPrescriptionIncomplete {
		t.Fatalf("expected ErrPrescriptionIncomplete, got: %v", err)
	}

	// Duplicado de catalog_id
	catID := uuid.New()
	it1 := sampleItem(&catID, "Producto A", false)
	it2 := sampleItem(&catID, "Producto A repetido", false)
	it2.ItemID = uuid.New().String()
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{it1, it2}, nil, nil, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Fatalf("expected error for duplicate catalog_id")
	}

	// Free text válido
	freeText := "Fórmula magistral especial"
	validFree := PrescriptionItem{
		ItemID:              uuid.New().String(),
		FreeText:            &freeText,
		NameSnapshot:        freeText,
		Dose:                DoseVO{Value: 5, Unit: "ml"},
		Route:               "oral",
		Frequency:           "every_8_hours",
		QuantityToDispense:  QuantityVO{Value: 1, Unit: "unit"},
		SubstitutionAllowed: false,
	}
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil, "CO",
		[]PrescriptionItem{validFree}, nil, nil, rules, true, credRef, false, now,
	)
	if err != nil {
		t.Fatalf("unexpected error with free text: %v", err)
	}
}

func TestPrescription_R6_RevokeNoDispensations(t *testing.T) {
	rx := NewPrescription()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Prednisolona", false)

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	_, _ = rx.IssuePrescription(vetID, credRef, rules, "hash999", now)

	// Revocar sin dispensaciones: permitido
	revEvt, err := rx.RevokePrescription("Error en dosis", vetID, false)
	if err != nil {
		t.Fatalf("unexpected revoke error: %v", err)
	}
	if revEvt.Reason != "Error en dosis" || rx.Status != StatusRevoked {
		t.Fatalf("revocation state mismatch")
	}

	// Nueva receta con dispensación
	rx2 := NewPrescription()
	item2 := sampleItem(ptr(uuid.New()), "Prednisolona", false)
	item2.QuantityToDispense.Value = 2
	_, _ = rx2.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item2}, nil, nil, rules, true, credRef, false, now,
	)
	_, _ = rx2.IssuePrescription(vetID, credRef, rules, "hash999", now)

	// Registrar dispensación parcial (1 de 2)
	_, err = rx2.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item2.ItemID, Qty: 1},
	}, now)
	if err != nil {
		t.Fatalf("dispense failed: %v", err)
	}

	// Veterinario intenta revocar tras dispensación: debe fallar con ErrPrescriptionDispensed
	_, err = rx2.RevokePrescription("Revocar por vet", vetID, false)
	if err != ErrPrescriptionDispensed {
		t.Fatalf("expected ErrPrescriptionDispensed, got: %v", err)
	}

	// SuperAdmin sí puede revocar tras dispensación
	adminID := uuid.New()
	adminRev, err := rx2.RevokePrescription("Orden sanitaria", adminID, true)
	if err != nil {
		t.Fatalf("super_admin revoke failed: %v", err)
	}
	if adminRev.By != adminID || rx2.Status != StatusRevoked {
		t.Fatalf("admin revocation failed")
	}
}

func TestPrescription_R7_R8_DispensationLimitsAndRefills(t *testing.T) {
	rx := NewPrescription()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Gabapentina", false)
	item.QuantityToDispense.Value = 2 // 2 unidades por ciclo

	validity := &Validity{
		ValidUntil:     kernel.CivilDateOf(now.AddDate(0, 1, 0)),
		RefillsAllowed: 1, // 1 refill (2 ciclos totales de 2 unidades)
	}

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, validity, nil, rules, true, credRef, false, now,
	)
	_, _ = rx.IssuePrescription(vetID, credRef, rules, "hash1", now)

	// R7: Intentar dispensar más de lo prescrito (3 > 2)
	_, err := rx.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item.ItemID, Qty: 3},
	}, now)
	if err != ErrDispensationExceeds {
		t.Fatalf("expected ErrDispensationExceeds for qty=3 > 2, got: %v", err)
	}

	// Dispensar 1 (parcial)
	evts, err := rx.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item.ItemID, Qty: 1},
	}, now)
	if err != nil {
		t.Fatalf("partial dispensation failed: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if rx.ItemsRemaining[item.ItemID] != 1 {
		t.Fatalf("expected 1 remaining, got %f", rx.ItemsRemaining[item.ItemID])
	}
	if rx.RefillsUsed != 0 {
		t.Fatalf("refills_used should still be 0")
	}

	// Dispensar el 1 restante del ciclo 1 -> completa ciclo 1, pasa a refill 1
	_, err = rx.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item.ItemID, Qty: 1},
	}, now)
	if err != nil {
		t.Fatalf("second dispensation failed: %v", err)
	}
	if rx.RefillsUsed != 1 {
		t.Fatalf("expected refills_used=1, got %d", rx.RefillsUsed)
	}
	// Saldo reiniciado para el refill 1
	if rx.ItemsRemaining[item.ItemID] != 2 {
		t.Fatalf("expected remaining reset to 2 for refill, got %f", rx.ItemsRemaining[item.ItemID])
	}

	// Dispensar las 2 unidades del refill 1 -> agota completamente la receta
	evts, err = rx.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item.ItemID, Qty: 2},
	}, now)
	if err != nil {
		t.Fatalf("third dispensation failed: %v", err)
	}
	// Debe emitir PrescriptionDispensed y PrescriptionExhausted
	if len(evts) != 2 {
		t.Fatalf("expected 2 events (dispensed + exhausted), got %d", len(evts))
	}
	if rx.Status != StatusExhausted {
		t.Fatalf("expected status exhausted, got: %s", rx.Status)
	}

	// Intentar dispensar nuevamente cuando ya se agotó: debe fallar con ErrPrescriptionNotQuotable
	_, err = rx.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{
		{ItemID: item.ItemID, Qty: 1},
	}, now)
	if err != ErrPrescriptionNotQuotable {
		t.Fatalf("expected ErrPrescriptionNotQuotable on exhausted, got: %v", err)
	}
}

func TestPrescription_R9_Immutability(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Omeprazol", false)

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	_, _ = rx.IssuePrescription(vetID, credRef, rules, "hash99", now)

	// Intento de UpdateDraft tras emitir: debe fallar con ErrPrescriptionNotDraft
	_, err := rx.UpdateDraft([]PrescriptionItem{item}, nil, nil, rules, now)
	if err != ErrPrescriptionNotDraft {
		t.Fatalf("expected ErrPrescriptionNotDraft after issue, got: %v", err)
	}

	// Intento de IssuePrescription nuevamente
	_, err = rx.IssuePrescription(vetID, credRef, rules, "hash99", now)
	if err != ErrPrescriptionNotDraft {
		t.Fatalf("expected ErrPrescriptionNotDraft for second issue, got: %v", err)
	}
}

func TestPrescription_ControlledSubstance_Flag(t *testing.T) {
	// Requisito PP-F4-PRD-020, PP-F4-SEC-006, PP-F4-SEC-007
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()

	controlledMed := sampleItem(ptr(uuid.New()), "Fenobarbital 100mg", true)
	if !controlledMed.Controlled {
		t.Fatalf("expected controlled med to have flag set")
	}

	draft, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{controlledMed}, nil, nil, rules, true, credRef, false, now,
	)
	if err != nil {
		t.Fatalf("controlled medication should be draftable: %v", err)
	}
	if !draft.Items[0].Controlled {
		t.Fatalf("controlled flag must be preserved in draft")
	}

	issued, err := rx.IssuePrescription(vetID, credRef, rules, "controlledhash", now)
	if err != nil {
		t.Fatalf("controlled medication should be issuable: %v", err)
	}
	if !issued.Items[0].Controlled {
		t.Fatalf("controlled flag must be preserved in issued event")
	}
}

func TestPrescription_ExpirePrescription(t *testing.T) {
	rx := NewPrescription()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Antibiótico", false)

	validity := &Validity{
		ValidUntil:     kernel.CivilDateOf(now.AddDate(0, 0, 10)),
		RefillsAllowed: 0,
	}

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, validity, nil, rules, true, credRef, false, now,
	)
	_, _ = rx.IssuePrescription(vetID, credRef, rules, "hashExp", now)

	// Aún no vence a día 5
	_, err := rx.ExpirePrescription(now.AddDate(0, 0, 5))
	if err == nil {
		t.Fatalf("expected error expiring before valid_until")
	}

	// Vence a día 11
	expEvt, err := rx.ExpirePrescription(now.AddDate(0, 0, 11))
	if err != nil {
		t.Fatalf("unexpected error expiring after valid_until: %v", err)
	}
	if expEvt.PrescriptionID != rx.ID || rx.Status != StatusExpired {
		t.Fatalf("status should be expired")
	}
}

func TestPrescription_UpdateDraft(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	rules := sampleRule()
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Antiinflamatorio", false)

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)

	newItem := sampleItem(ptr(uuid.New()), "Antiinflamatorio Forte", false)
	notes := "Notas actualizadas"
	newVal := &Validity{
		ValidUntil:     kernel.CivilDateOf(now.AddDate(0, 0, 45)),
		RefillsAllowed: 2,
	}

	upd, err := rx.UpdateDraft([]PrescriptionItem{newItem}, newVal, &notes, rules, now)
	if err != nil {
		t.Fatalf("update draft failed: %v", err)
	}
	if len(upd.Items) != 1 || upd.Items[0].NameSnapshot != "Antiinflamatorio Forte" {
		t.Fatalf("item name not updated")
	}
	if *upd.Notes != "Notas actualizadas" {
		t.Fatalf("notes not updated")
	}
}

func TestPrescription_ApplyAllEvents(t *testing.T) {
	rx := NewPrescription()
	id := uuid.New()
	petID := uuid.New()
	guardianID := uuid.New()

	rx.Apply(&ExternalPrescriptionUploaded{
		PrescriptionID: id,
		PetID:          petID,
		GuardianID:     guardianID,
		Images:         []ImageRef{{ObjectKey: "k", Page: 1}},
		Country:        "CO",
	})
	if rx.Status != StatusDraft || rx.Source != SourceExternal {
		t.Fatalf("expected external draft")
	}

	rx.Apply(&PrescriptionExtractionRecorded{
		PrescriptionID: id,
		Extraction:     Extraction{Version: 1, ExtractorID: "claude"},
	})
	if rx.Extraction == nil || rx.Extraction.ExtractorID != "claude" {
		t.Fatalf("extraction not applied")
	}

	rx.Apply(&PrescriptionReuploadRequested{PrescriptionID: id, Reason: "Borroso"})

	item := sampleItem(ptr(uuid.New()), "Med", false)
	rx.Apply(&ExternalPrescriptionConfirmed{
		PrescriptionID: id,
		Items:          []PrescriptionItem{item},
		Validity:       Validity{ValidUntil: "2026-12-01", RefillsAllowed: 0},
		ExternalMeta:   ExternalMeta{VetName: ptr("Vet")},
	})
	if rx.Status != StatusConfirmed || rx.ExternalMeta == nil || *rx.ExternalMeta.VetName != "Vet" {
		t.Fatalf("confirmed not applied")
	}

	rx.Apply(&ExternalPrescriptionReopened{PrescriptionID: id})
	if rx.Status != StatusDraft {
		t.Fatalf("reopened not applied")
	}

	rx.Apply(&PrescriptionPharmacistValidated{
		PrescriptionID: id,
		By:             uuid.New(),
		OrgID:          uuid.New(),
		Status:         "validated",
		Note:           "Valid",
	})
	if rx.Validation == nil || rx.Validation.Status != "validated" {
		t.Fatalf("validation not applied")
	}
}

type eventWithMeta interface {
	EventName() string
	SchemaVersion() int
}

func TestEvents_EventNameAndVersion(t *testing.T) {
	evts := []eventWithMeta{
		PrescriptionDrafted{},
		PrescriptionDraftUpdated{},
		PrescriptionIssued{},
		PrescriptionRevoked{},
		ExternalPrescriptionUploaded{},
		PrescriptionExtractionRecorded{},
		PrescriptionReuploadRequested{},
		ExternalPrescriptionConfirmed{},
		ExternalPrescriptionReopened{},
		PrescriptionPharmacistValidated{},
		PrescriptionDispensed{},
		PrescriptionExhausted{},
		PrescriptionExpired{},
	}

	for _, e := range evts {
		if e.EventName() == "" {
			t.Errorf("empty event name")
		}
		if e.SchemaVersion() != 1 {
			t.Errorf("schema version want 1, got %d", e.SchemaVersion())
		}
	}
}

func TestPrescription_MoreValidationEdgeCases(t *testing.T) {
	now := time.Now()
	rules := sampleRule()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	vetID := uuid.New()

	rx := NewPrescription()
	// Invalid country length
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "COLOMBIA",
		[]PrescriptionItem{sampleItem(ptr(uuid.New()), "A", false)}, nil, nil, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Error("expected error with country > 2 letters")
	}

	// Notes too long (> 500)
	longNotes := string(make([]byte, 501))
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{sampleItem(ptr(uuid.New()), "A", false)}, nil, &longNotes, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Error("expected error with notes > 500 chars")
	}

	// Item instructions too long (> 300)
	itBadInstructions := sampleItem(ptr(uuid.New()), "A", false)
	itBadInstructions.Instructions = string(make([]byte, 301))
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{itBadInstructions}, nil, nil, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Error("expected error with instructions > 300 chars")
	}

	// Item empty name
	itBadName := sampleItem(ptr(uuid.New()), "   ", false)
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{itBadName}, nil, nil, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Error("expected error with empty name")
	}

	// Item empty item ID
	itBadID := sampleItem(ptr(uuid.New()), "A", false)
	itBadID.ItemID = ""
	_, err = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{itBadID}, nil, nil, rules, true, credRef, false, now,
	)
	if err == nil {
		t.Error("expected error with empty item id")
	}

	// Signer not issuer
	rxValid := NewPrescription()
	_, _ = rxValid.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{sampleItem(ptr(uuid.New()), "A", false)}, nil, nil, rules, true, credRef, false, now,
	)
	_, err = rxValid.IssuePrescription(uuid.New(), credRef, rules, "hash", now)
	if err == nil {
		t.Error("expected error when signer != issuer")
	}

	// Empty content hash
	_, err = rxValid.IssuePrescription(vetID, credRef, rules, "   ", now)
	if err == nil {
		t.Error("expected error with empty content hash")
	}

	// Revoke with empty reason
	_, _ = rxValid.IssuePrescription(vetID, credRef, rules, "hash123", now)
	_, err = rxValid.RevokePrescription("  ", vetID, false)
	if err == nil {
		t.Error("expected error with empty revoke reason")
	}
}

func TestPrescription_CoveragePushTo90(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	rules := sampleRule()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Med1", false)

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)

	// UpdateDraft with bad validity
	badVal := &Validity{RefillsAllowed: 10}
	_, err := rx.UpdateDraft(nil, badVal, nil, rules, now)
	if err == nil {
		t.Error("expected error updating draft with bad refills")
	}

	// UpdateDraft with bad items
	_, err = rx.UpdateDraft([]PrescriptionItem{}, nil, nil, rules, now)
	if err == nil {
		t.Error("expected error updating draft with empty items")
	}

	// UploadExternal with bad country
	rxExt := NewPrescription()
	_, err = rxExt.UploadExternalPrescription(uuid.New(), uuid.New(), uuid.New(), []ImageRef{{Page: 1}}, "COL")
	if err == nil {
		t.Error("expected error with country length 3")
	}

	// RecordDispensation error cases
	rxDisp := NewPrescription()
	_, _ = rxDisp.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	_, _ = rxDisp.IssuePrescription(vetID, credRef, rules, "hashX", now)

	// Empty lines
	_, err = rxDisp.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{}, now)
	if err == nil {
		t.Error("expected error with empty dispensation lines")
	}

	// Unknown item
	_, err = rxDisp.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{{ItemID: "unknown", Qty: 1}}, now)
	if err == nil {
		t.Error("expected error with unknown item in dispensation")
	}

	// Qty <= 0
	_, err = rxDisp.RecordDispensation(uuid.New(), uuid.New(), []DispensationLine{{ItemID: item.ItemID, Qty: 0}}, now)
	if err == nil {
		t.Error("expected error with qty <= 0")
	}
}

func TestPrescription_ImmutableVersionCheck(t *testing.T) {
	rx := NewPrescription()
	now := time.Now()
	rules := sampleRule()
	credRef := &CredentialRef{Country: "CO", RegistryNumber: "TP-12345", VerifiedAt: now}
	vetID := uuid.New()
	item := sampleItem(ptr(uuid.New()), "Med1", false)

	_, _ = rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)

	// Draft again when version > 0
	_, err := rx.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	if err != ErrPrescriptionImmutable {
		t.Errorf("expected ErrPrescriptionImmutable when drafting over existing")
	}

	// Upload external when version > 0
	_, err = rx.UploadExternalPrescription(uuid.New(), uuid.New(), uuid.New(), []ImageRef{{Page: 1}}, "CO")
	if err != ErrPrescriptionImmutable {
		t.Errorf("expected ErrPrescriptionImmutable when uploading over existing")
	}

	// Revoke on draft
	rxDraft := NewPrescription()
	_, _ = rxDraft.DraftIssuedPrescription(
		uuid.New(), uuid.New(), uuid.New(), vetID, uuid.New(), nil, "CO",
		[]PrescriptionItem{item}, nil, nil, rules, true, credRef, false, now,
	)
	_, err = rxDraft.RevokePrescription("reason", vetID, false)
	if err != ErrPrescriptionImmutable {
		t.Errorf("expected ErrPrescriptionImmutable when revoking draft")
	}

	// ConfirmExternalPrescription on external prescription
	extRx := NewPrescription()
	extID := uuid.New()
	guardianID := uuid.New()
	_, err = extRx.UploadExternalPrescription(extID, uuid.New(), guardianID, []ImageRef{{Page: 1}}, "CO")
	if err != nil {
		t.Fatalf("upload external failed: %v", err)
	}

	val := Validity{ValidUntil: kernel.CivilDateOf(now).AddDays(30), RefillsAllowed: 1}
	extMeta := ExternalMeta{ClinicName: ptr("Clinica ABC")}
	confirmEvt, err := extRx.ConfirmExternalPrescription([]PrescriptionItem{item}, val, extMeta, map[string]any{"note": "ok"}, rules, now)
	if err != nil {
		t.Fatalf("confirm external failed: %v", err)
	}
	if confirmEvt.PrescriptionID != extID {
		t.Errorf("wrong prescription id on confirm")
	}

	// Error cases for ConfirmExternalPrescription
	_, err = extRx.ConfirmExternalPrescription([]PrescriptionItem{}, val, extMeta, nil, rules, now)
	if err == nil {
		t.Errorf("expected error confirming empty items")
	}

	valOutOfBound := Validity{ValidUntil: kernel.CivilDateOf(now).AddDays(500), RefillsAllowed: 10}
	_, err = extRx.ConfirmExternalPrescription([]PrescriptionItem{item}, valOutOfBound, extMeta, nil, rules, now)
	if err == nil {
		t.Errorf("expected error confirming out of bounds validity")
	}

	// Confirm on issued rx (wrong source)
	_, err = rxDraft.ConfirmExternalPrescription([]PrescriptionItem{item}, val, extMeta, nil, rules, now)
	if err == nil {
		t.Errorf("expected error confirming non-external prescription")
	}
}

func TestPrescription_DOM007_ExternalLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	rules := sampleRule()
	item := sampleItem(ptr(uuid.New()), "Amoxicilina 500mg", false)

	// 1. Upload external
	rx := NewPrescription()
	rxID := uuid.New()
	petID := uuid.New()
	guardianID := uuid.New()
	images := []ImageRef{
		{ObjectKey: "rx/1/page-1.webp", ContentType: "image/webp", Page: 1, Sha256: []byte("sha")},
	}
	_, err := rx.UploadExternalPrescription(rxID, petID, guardianID, images, "CO")
	if err != nil {
		t.Fatalf("upload external failed: %v", err)
	}

	// 2. RecordExtraction
	ext := Extraction{
		Version:     1,
		ExtractorID: "claude:claude-opus-5-5:prompt-v1",
		Legibility:  0.92,
		Warnings:    []string{},
		Header: ExtractedHeader{
			VetName: ptr("Dr. Juan Pérez"),
		},
		Items: []ExtractedItem{
			{Index: 0, RawText: "Amoxicilina 500mg", Product: ExtractedProduct{Text: "Amoxicilina 500mg"}},
		},
		CostUSD:   0.025,
		LatencyMs: 1200,
	}
	recEvt, err := rx.RecordExtraction(ext)
	if err != nil {
		t.Fatalf("record extraction failed: %v", err)
	}
	if recEvt.Extraction.ExtractorID != ext.ExtractorID {
		t.Errorf("expected extractor id %s, got %s", ext.ExtractorID, recEvt.Extraction.ExtractorID)
	}
	if rx.Extraction == nil || rx.Extraction.Legibility != 0.92 {
		t.Errorf("extraction not applied to aggregate")
	}

	// 3. ConfirmExternalItems
	val := Validity{ValidUntil: kernel.CivilDateOf(now).AddDays(30), RefillsAllowed: 1}
	extMeta := ExternalMeta{VetName: ptr("Dr. Juan Pérez"), ClinicName: ptr("Clínica Veterinaria Central")}
	diff := map[string]any{"items": []any{map[string]any{"action": "accepted", "item_id": item.ItemID}}}
	confirmEvt, err := rx.ConfirmExternalItems([]PrescriptionItem{item}, val, extMeta, diff, rules, now)
	if err != nil {
		t.Fatalf("confirm external items failed: %v", err)
	}
	if confirmEvt.PrescriptionID != rxID {
		t.Errorf("wrong rx id on confirmation")
	}
	if rx.Status != StatusConfirmed {
		t.Errorf("expected status confirmed, got %s", rx.Status)
	}

	// 4. ValidateByPharmacist (pharmacist validation)
	pharmID := uuid.New()
	pharmOrgID := uuid.New()
	valEvt, err := rx.ValidateByPharmacist(pharmID, pharmOrgID, "validated", "Receta verificada con éxito")
	if err != nil {
		t.Fatalf("pharmacist validate failed: %v", err)
	}
	if valEvt.Status != "validated" {
		t.Errorf("expected validated status, got %s", valEvt.Status)
	}
	if rx.Validation == nil || rx.Validation.Status != "validated" {
		t.Errorf("validation not applied to aggregate")
	}

	// Invalid validation status
	_, err = rx.ValidateByPharmacist(pharmID, pharmOrgID, "unknown_status", "")
	if err == nil {
		t.Errorf("expected error for invalid validation status")
	}

	// 5. ReopenExternal
	reopenEvt, err := rx.ReopenExternal()
	if err != nil {
		t.Fatalf("reopen external failed: %v", err)
	}
	if reopenEvt.PrescriptionID != rxID {
		t.Errorf("wrong rx id on reopen")
	}
	if rx.Status != StatusDraft {
		t.Errorf("expected status draft after reopen, got %s", rx.Status)
	}

	// 6. RequestReupload in draft
	reuploadEvt, err := rx.RequestReupload("Imagen demasiado borrosa")
	if err != nil {
		t.Fatalf("request reupload failed: %v", err)
	}
	if reuploadEvt.Reason != "Imagen demasiado borrosa" {
		t.Errorf("expected reason 'Imagen demasiado borrosa', got '%s'", reuploadEvt.Reason)
	}
}
