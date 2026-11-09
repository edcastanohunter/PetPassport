package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockCatalogEquiv struct {
	controlledMap map[uuid.UUID]bool
	equivMap      map[string]bool
}

func (m *mockCatalogEquiv) IsControlled(_ context.Context, id uuid.UUID) (bool, error) {
	return m.controlledMap[id], nil
}

func (m *mockCatalogEquiv) AreEquivalent(_ context.Context, a, b uuid.UUID) (bool, error) {
	key := a.String() + ":" + b.String()
	revKey := b.String() + ":" + a.String()
	if v, ok := m.equivMap[key]; ok {
		return v, nil
	}
	if v, ok := m.equivMap[revKey]; ok {
		return v, nil
	}
	return a == b, nil
}

type mockQuotaChecker struct {
	fail bool
}

func (m *mockQuotaChecker) CheckQuotas(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ time.Time) error {
	if m.fail {
		return ErrQuoteRequestLimit
	}
	return nil
}

type mockPartnerDir struct {
	ineligibleOrgs map[uuid.UUID]bool
}

func (m *mockPartnerDir) EligiblePharmacies(_ context.Context, _ string, _ Area, _ FulfillmentMode) ([]EligiblePharmacy, error) {
	return nil, nil
}

func (m *mockPartnerDir) IsEligible(_ context.Context, orgID uuid.UUID, _ Area, _ FulfillmentMode) (bool, error) {
	if m.ineligibleOrgs != nil && m.ineligibleOrgs[orgID] {
		return false, nil
	}
	return true, nil
}

func samplePrescription(guardianID uuid.UUID) *PrescriptionSnapshot {
	catID1 := uuid.MustParse("01930000-0000-7000-8000-000000000010")
	catID2 := uuid.MustParse("01930000-0000-7000-8000-000000000020")
	return &PrescriptionSnapshot{
		ID:             uuid.New(),
		PetID:          uuid.New(),
		GuardianID:     guardianID,
		Country:        "CO",
		Status:         "issued",
		Source:         "issued",
		ValidUntil:     time.Now().Add(48 * time.Hour),
		RefillsAllowed: 1,
		RefillsUsed:    0,
		Items: []PrescriptionItemSnapshot{
			{
				ItemID:              "item-1",
				CatalogID:           &catID1,
				Name:                "Amoxicilina 500mg",
				Quantity:            1,
				Unit:                "pack",
				SubstitutionAllowed: true,
				Controlled:          false,
			},
			{
				ItemID:              "item-2",
				CatalogID:           &catID2,
				Name:                "Ketamina 50mg",
				Quantity:            1,
				Unit:                "ampoule",
				SubstitutionAllowed: false,
				Controlled:          true,
			},
		},
	}
}

// Invariante Q1: Actor tutor de la receta
func TestQuoteRequest_Q1_GuardianOnly(t *testing.T) {
	guardianID := uuid.New()
	imposterID := uuid.New()
	rx := samplePrescription(guardianID)

	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		imposterID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrNotGuardian) {
		t.Fatalf("expected ErrNotGuardian, got %v", err)
	}
}

// Invariante Q2: Receta vigente (issued/confirmed, no expirada/agotada)
func TestQuoteRequest_Q2_PrescriptionValid(t *testing.T) {
	guardianID := uuid.New()

	// Status no permitido
	rxDraft := samplePrescription(guardianID)
	rxDraft.Status = "draft"
	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rxDraft,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrPrescriptionNotQuotable) {
		t.Fatalf("expected ErrPrescriptionNotQuotable for draft, got %v", err)
	}

	// Receta expirada
	rxExpired := samplePrescription(guardianID)
	rxExpired.ValidUntil = time.Now().Add(-2 * time.Hour)
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rxExpired,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrPrescriptionNotQuotable) {
		t.Fatalf("expected ErrPrescriptionNotQuotable for expired, got %v", err)
	}

	// Refills agotados
	rxAgotada := samplePrescription(guardianID)
	rxAgotada.RefillsUsed = 2
	rxAgotada.RefillsAllowed = 1
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rxAgotada,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrPrescriptionNotQuotable) {
		t.Fatalf("expected ErrPrescriptionNotQuotable for exhausted refills, got %v", err)
	}
}

// Invariante Q3: Ítems pertenecen a la receta y no son controlados
func TestQuoteRequest_Q3_ItemsSubsetAndNotControlled(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)

	// Ítem ajeno
	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "non-existent-item"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrItemsNotSubset) {
		t.Fatalf("expected ErrItemsNotSubset, got %v", err)
	}

	// Ítem controlado en receta
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-2"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrControlledNotOrderable) {
		t.Fatalf("expected ErrControlledNotOrderable, got %v", err)
	}

	// Ítem controlado en catálogo
	catEquiv := &mockCatalogEquiv{
		controlledMap: map[uuid.UUID]bool{
			*rx.Items[0].CatalogID: true,
		},
	}
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		catEquiv,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrControlledNotOrderable) {
		t.Fatalf("expected ErrControlledNotOrderable from catalog, got %v", err)
	}
}

// Invariante Q4: 1–10 farmacias aliadas elegibles
func TestQuoteRequest_Q4_PharmacyCountAndEligibility(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)

	// 0 orgs
	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{},
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrInvalidOrgCount) {
		t.Fatalf("expected ErrInvalidOrgCount for 0, got %v", err)
	}

	// 11 orgs
	elevenOrgs := make([]uuid.UUID, 11)
	for i := range elevenOrgs {
		elevenOrgs[i] = uuid.New()
	}
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		elevenOrgs,
		24,
		nil,
		nil,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrInvalidOrgCount) {
		t.Fatalf("expected ErrInvalidOrgCount for 11, got %v", err)
	}

	// Org inelegible
	badOrg := uuid.New()
	dir := &mockPartnerDir{
		ineligibleOrgs: map[uuid.UUID]bool{badOrg: true},
	}
	err = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{badOrg},
		24,
		nil,
		nil,
		dir,
		time.Now(),
	)
	if !errors.Is(err, ErrPharmacyNotEligible) {
		t.Fatalf("expected ErrPharmacyNotEligible, got %v", err)
	}
}

// Invariante Q5: Cuotas de solicitudes
func TestQuoteRequest_Q5_QuotaLimits(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)

	quotaChecker := &mockQuotaChecker{fail: true}
	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{uuid.New()},
		24,
		nil,
		quotaChecker,
		nil,
		time.Now(),
	)
	if !errors.Is(err, ErrQuoteRequestLimit) {
		t.Fatalf("expected ErrQuoteRequestLimit, got %v", err)
	}
}

// Invariante Q6: Una cotización por org (reemplaza si se envía nueva oferta)
func TestQuoteRequest_Q6_OneQuotePerOrg(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	_ = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID},
		24,
		nil,
		nil,
		nil,
		now,
	)

	// Primera cotización
	q1ID := uuid.New()
	err := qr.RecordQuote(
		context.Background(),
		q1ID,
		orgID,
		[]QuoteLine{{ItemID: "item-1", UnitPrice: 15000, Qty: 1, Available: AvailabilityFull}},
		nil,
		nil,
		nil,
		now.Add(24*time.Hour),
		nil,
		nil,
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(qr.Quotes) != 1 {
		t.Fatalf("expected 1 quote, got %d", len(qr.Quotes))
	}

	// Segunda cotización de la misma farmacia (reemplaza a la primera como superseded)
	q2ID := uuid.New()
	err = qr.RecordQuote(
		context.Background(),
		q2ID,
		orgID,
		[]QuoteLine{{ItemID: "item-1", UnitPrice: 14000, Qty: 1, Available: AvailabilityFull}},
		nil,
		nil,
		nil,
		now.Add(24*time.Hour),
		nil,
		nil,
		now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !qr.Quotes[q1ID].Superseded {
		t.Fatalf("expected first quote to be superseded")
	}
	if qr.Quotes[q2ID].Superseded {
		t.Fatalf("expected second quote not to be superseded")
	}
}

// Invariante Q7: Sustituciones válidas y precios >= 0
func TestQuoteRequest_Q7_SubstitutionEquivalenceAndNonNegative(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	_ = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID},
		24,
		nil,
		nil,
		nil,
		now,
	)

	// Precio negativo
	err := qr.RecordQuote(
		context.Background(),
		uuid.New(),
		orgID,
		[]QuoteLine{{ItemID: "item-1", UnitPrice: -100, Qty: 1, Available: AvailabilityFull}},
		nil,
		nil,
		nil,
		now.Add(24*time.Hour),
		nil,
		nil,
		now,
	)
	if !errors.Is(err, ErrPriceNegative) {
		t.Fatalf("expected ErrPriceNegative, got %v", err)
	}

	// Sustitución no equivalente
	subID := uuid.New()
	catEquiv := &mockCatalogEquiv{
		equivMap: map[string]bool{}, // not equivalent
	}
	err = qr.RecordQuote(
		context.Background(),
		uuid.New(),
		orgID,
		[]QuoteLine{
			{
				ItemID:    "item-1",
				UnitPrice: 12000,
				Qty:       1,
				Available: AvailabilityFull,
				Substitution: &QuoteSubstitution{
					CatalogID: subID,
					Name:      "Amoxicilina Genérica",
					UnitPrice: 10000,
					Reason:    "Disponibilidad",
				},
			},
		},
		nil,
		nil,
		nil,
		now.Add(24*time.Hour),
		nil,
		catEquiv,
		now,
	)
	if !errors.Is(err, ErrSubstitutionNotEquivalent) {
		t.Fatalf("expected ErrSubstitutionNotEquivalent, got %v", err)
	}
}

// Invariante Q8: Cotización vigente y decisiones de sustitución requeridas
func TestQuoteRequest_Q8_QuoteValidityAndSubstitutionDecisions(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	_ = qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID},
		24,
		nil,
		nil,
		nil,
		now,
	)

	subID := uuid.New()
	catEquiv := &mockCatalogEquiv{
		equivMap: map[string]bool{
			rx.Items[0].CatalogID.String() + ":" + subID.String(): true,
		},
	}

	quoteID := uuid.New()
	_ = qr.RecordQuote(
		context.Background(),
		quoteID,
		orgID,
		[]QuoteLine{
			{
				ItemID:    "item-1",
				UnitPrice: 15000,
				Qty:       1,
				Available: AvailabilityFull,
				Substitution: &QuoteSubstitution{
					CatalogID: subID,
					Name:      "Amoxicilina Clavulanato",
					UnitPrice: 18000,
					Reason:    "Sustitución recomendada",
				},
			},
		},
		nil,
		nil,
		nil,
		now.Add(12*time.Hour),
		nil,
		catEquiv,
		now,
	)

	// Falta decisión para la sustitución
	_, err := qr.SelectQuote(quoteID, []SubstitutionDecision{}, now)
	if !errors.Is(err, ErrSubstitutionDecisionRequired) {
		t.Fatalf("expected ErrSubstitutionDecisionRequired, got %v", err)
	}

	// Oferta expirada
	_, err = qr.SelectQuote(quoteID, []SubstitutionDecision{{ItemID: "item-1", Accept: true}}, now.Add(13*time.Hour))
	if !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("expected ErrQuoteExpired, got %v", err)
	}

	// Selección válida
	selected, err := qr.SelectQuote(quoteID, []SubstitutionDecision{{ItemID: "item-1", Accept: true}}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected == nil || qr.Status != QuoteRequestStatusSelected {
		t.Fatalf("expected status selected, got %s", qr.Status)
	}
}

func TestQuoteRequest_LifecycleAndEvents(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1"}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID},
		24,
		nil,
		nil,
		nil,
		now,
	)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}

	// Double request error
	if err := qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now); err == nil {
		t.Fatalf("expected error on re-init")
	}

	// Mark invitation sent and failed
	qr.MarkInvitationSent(orgID, "portal", now)
	if inv := qr.Invited[orgID]; inv.Status != InvitationSent || inv.Channel != "portal" {
		t.Fatalf("expected invitation sent, got %+v", inv)
	}
	qr.MarkInvitationFailed(orgID, "network err", now)
	if inv := qr.Invited[orgID]; inv.Status != InvitationFailed || inv.Attempts != 1 {
		t.Fatalf("expected invitation failed, got %+v", inv)
	}

	// Ignored if org not invited
	uninvited := uuid.New()
	qr.MarkInvitationSent(uninvited, "portal", now)
	qr.MarkInvitationFailed(uninvited, "err", now)

	// Decline quote
	err = qr.DeclineQuote(uninvited, "reason", now)
	if !errors.Is(err, ErrOrgNotInvited) {
		t.Fatalf("expected ErrOrgNotInvited, got %v", err)
	}
	err = qr.DeclineQuote(orgID, "No stock", now)
	if err != nil {
		t.Fatalf("unexpected decline err: %v", err)
	}
	if inv := qr.Invited[orgID]; inv.Status != InvitationDeclined {
		t.Fatalf("expected status declined, got %v", inv.Status)
	}

	// Record quote error if org not invited
	err = qr.RecordQuote(context.Background(), uuid.New(), uninvited, nil, nil, nil, nil, now, nil, nil, now)
	if !errors.Is(err, ErrOrgNotInvited) {
		t.Fatalf("expected ErrOrgNotInvited, got %v", err)
	}

	// Expire with and without quotes
	qrWithoutQuotes := NewQuoteRequest()
	_ = qrWithoutQuotes.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)
	// Before expires at -> noop
	_ = qrWithoutQuotes.Expire(now.Add(10 * time.Hour))
	if qrWithoutQuotes.Status != QuoteRequestStatusOpen {
		t.Fatalf("expected open before expires at")
	}
	// After expires at -> closed
	_ = qrWithoutQuotes.Expire(now.Add(25 * time.Hour))
	if qrWithoutQuotes.Status != QuoteRequestStatusClosed {
		t.Fatalf("expected closed, got %s", qrWithoutQuotes.Status)
	}

	// Cancel
	qrToCancel := NewQuoteRequest()
	_ = qrToCancel.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)
	// Cancel by wrong user
	err = qrToCancel.Cancel("reason", uuid.New(), now)
	if !errors.Is(err, ErrNotGuardian) {
		t.Fatalf("expected ErrNotGuardian, got %v", err)
	}
	// Cancel by guardian
	err = qrToCancel.Cancel("user cancelled", guardianID, now)
	if err != nil {
		t.Fatalf("unexpected cancel error: %v", err)
	}
	if qrToCancel.Status != QuoteRequestStatusClosed {
		t.Fatalf("expected closed after cancel, got %s", qrToCancel.Status)
	}

	// Close and Reopen
	orderID := uuid.New()
	err = qr.Close(&orderID, "order created", now)
	if err != nil {
		t.Fatalf("close err: %v", err)
	}
	if qr.Status != QuoteRequestStatusClosed {
		t.Fatalf("expected closed")
	}
	err = qr.Reopen("order rejected", now)
	if err != nil {
		t.Fatalf("reopen err: %v", err)
	}
	if qr.Status != QuoteRequestStatusOpen {
		t.Fatalf("expected open after reopen")
	}

	// Select on non-existent quote
	_, err = qr.SelectQuote(uuid.New(), nil, now)
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("expected ErrQuoteNotFound, got %v", err)
	}

	// Uncommitted events and MarkCommitted
	events := qr.UncommittedEvents()
	if len(events) == 0 {
		t.Fatalf("expected uncommitted events")
	}
	qr.MarkCommitted()
	if len(qr.UncommittedEvents()) != 0 {
		t.Fatalf("expected 0 uncommitted events")
	}
}

func TestQuoteRequest_EdgeCasesForCoverage(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	// Prescription nil
	qr := NewQuoteRequest()
	err := qr.Request(context.Background(), uuid.New(), nil, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)
	if !errors.Is(err, ErrPrescriptionNotQuotable) {
		t.Fatalf("expected ErrPrescriptionNotQuotable for nil prescription")
	}

	// Empty items
	err = qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)
	if !errors.Is(err, ErrItemsEmpty) {
		t.Fatalf("expected ErrItemsEmpty for empty items")
	}

	// Request valid
	_ = qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)

	// Record quote when item does not exist or substitution has negative price
	err = qr.RecordQuote(context.Background(), uuid.New(), orgID, []QuoteLine{
		{ItemID: "non-existent-item", UnitPrice: 100, Qty: 1, Available: AvailabilityFull},
		{ItemID: "item-1", UnitPrice: 1000, Qty: 1, Available: AvailabilityNone},
	}, nil, nil, nil, now.Add(24*time.Hour), nil, nil, now)
	if err != nil {
		t.Fatalf("unexpected error for none available: %v", err)
	}

	// Substitution with negative price
	subID := uuid.New()
	err = qr.RecordQuote(context.Background(), uuid.New(), orgID, []QuoteLine{
		{
			ItemID:    "item-1",
			UnitPrice: 1000,
			Qty:       1,
			Available: AvailabilityFull,
			Substitution: &QuoteSubstitution{
				CatalogID: subID,
				UnitPrice: -50,
			},
		},
	}, nil, nil, nil, now.Add(24*time.Hour), nil, nil, now)
	if !errors.Is(err, ErrPriceNegative) {
		t.Fatalf("expected ErrPriceNegative for negative sub price")
	}

	// Close when already closed
	_ = qr.Close(nil, "closed", now)
	_ = qr.Close(nil, "closed again", now) // no-op

	// Reopen when already open
	_ = qr.Reopen("reopened", now)
	_ = qr.Reopen("already open", now) // no-op

	// Mark invitation sent/failed when closed
	_ = qr.Close(nil, "closed", now)
	qr.MarkInvitationSent(orgID, "portal", now)
	qr.MarkInvitationFailed(orgID, "err", now)
	_ = qr.DeclineQuote(orgID, "decline", now)
	_ = qr.Cancel("cancel", guardianID, now)
	_ = qr.Expire(now)
}

func TestCoverage_QuoteRequestRemaining(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	// Deduplicated orgIDs
	qr := NewQuoteRequest()
	err := qr.Request(
		context.Background(),
		uuid.New(),
		rx,
		guardianID,
		[]RequestedItem{{ItemID: "item-1", Quantity: 0, Unit: ""}},
		ModePickup,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID, orgID}, // duplicate
		0,                         // default expiresInHours (24)
		nil,
		nil,
		nil,
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error with dup orgs: %v", err)
	}

	// Selection within 24h grace period after expires_at
	qID := uuid.New()
	_ = qr.RecordQuote(
		context.Background(),
		qID,
		orgID,
		[]QuoteLine{{ItemID: "item-1", UnitPrice: 1000, Qty: 1, Available: AvailabilityFull}},
		nil,
		nil,
		nil,
		now.Add(48*time.Hour),
		nil,
		nil,
		now,
	)
	// Expire sets SelectableUntil
	_ = qr.Expire(now.Add(25 * time.Hour))
	if qr.SelectableUntil == nil {
		t.Fatalf("expected selectable until to be set")
	}
	// Select within grace period
	_, err = qr.SelectQuote(qID, nil, now.Add(26*time.Hour))
	if err != nil {
		t.Fatalf("expected selection within grace period to succeed: %v", err)
	}

	// Completeness: AvailabilityNone
	qrNone := NewQuoteRequest()
	_ = qrNone.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, nil, now)
	_ = qrNone.RecordQuote(context.Background(), uuid.New(), orgID, []QuoteLine{
		{ItemID: "item-1", UnitPrice: 0, Qty: 0, Available: AvailabilityNone},
	}, nil, nil, nil, time.Time{}, nil, nil, now) // validUntil is zero -> sets to expiresAt
	for _, q := range qrNone.Quotes {
		if q.Completeness != AvailabilityNone {
			t.Fatalf("expected Completeness none, got %s", q.Completeness)
		}
	}
}

type errCatalogEquiv struct{}

func (errCatalogEquiv) IsControlled(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, errors.New("catalog db failure")
}
func (errCatalogEquiv) AreEquivalent(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, errors.New("catalog equiv failure")
}

type errPartnerDir struct{}

func (errPartnerDir) EligiblePharmacies(_ context.Context, _ string, _ Area, _ FulfillmentMode) ([]EligiblePharmacy, error) {
	return nil, nil
}
func (errPartnerDir) IsEligible(_ context.Context, _ uuid.UUID, _ Area, _ FulfillmentMode) (bool, error) {
	return false, errors.New("directory error")
}

func TestCoverage_ErrorsPropagation(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	orgID := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	// Directory error
	err := qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, errPartnerDir{}, now)
	if err == nil || err.Error() != "checking pharmacy eligibility: directory error" {
		t.Fatalf("expected directory error, got %v", err)
	}

	// Catalog IsControlled error
	err = qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, errCatalogEquiv{}, nil, nil, now)
	if err == nil || err.Error() != "checking controlled status: catalog db failure" {
		t.Fatalf("expected catalog isControlled error, got %v", err)
	}

	// Successful request with partner directory
	okDir := &mockPartnerDir{}
	err = qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{orgID}, 24, nil, nil, okDir, now)
	if err != nil {
		t.Fatalf("expected ok with dir, got %v", err)
	}

	// Catalog AreEquivalent error
	subID := uuid.New()
	err = qr.RecordQuote(context.Background(), uuid.New(), orgID, []QuoteLine{
		{
			ItemID:    "item-1",
			UnitPrice: 1000,
			Qty:       1,
			Available: AvailabilityFull,
			Substitution: &QuoteSubstitution{
				CatalogID: subID,
				UnitPrice: 1200,
			},
		},
	}, nil, nil, nil, now.Add(24*time.Hour), nil, errCatalogEquiv{}, now)
	if err == nil || err.Error() != "checking equivalence: catalog equiv failure" {
		t.Fatalf("expected equivalence error, got %v", err)
	}
}

func TestCoverage_QuoteRequestExpireInvitations(t *testing.T) {
	guardianID := uuid.New()
	rx := samplePrescription(guardianID)
	org1 := uuid.New()
	org2 := uuid.New()
	now := time.Now()

	qr := NewQuoteRequest()
	_ = qr.Request(context.Background(), uuid.New(), rx, guardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{org1, org2}, 24, nil, nil, nil, now)

	// org1 is pending, org2 is sent
	qr.MarkInvitationSent(org2, "portal", now)

	// Expire with no quotes
	_ = qr.Expire(now.Add(25 * time.Hour))

	if qr.Invited[org1].Status != InvitationExpired {
		t.Fatalf("expected org1 invitation expired, got %s", qr.Invited[org1].Status)
	}
	if qr.Invited[org2].Status != InvitationExpired {
		t.Fatalf("expected org2 invitation expired, got %s", qr.Invited[org2].Status)
	}
}
