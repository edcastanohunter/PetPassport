package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleQuoteRequestAndQuote() (*QuoteRequest, *Quote) {
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
		ModeDelivery,
		Area{City: "Bogota"},
		[]uuid.UUID{orgID},
		24,
		nil,
		nil,
		nil,
		now,
	)

	quoteID := uuid.New()
	deliveryFee := 8000.0
	subID := uuid.New()
	_ = qr.RecordQuote(
		context.Background(),
		quoteID,
		orgID,
		[]QuoteLine{
			{
				ItemID:    "item-1",
				UnitPrice: 20000,
				Qty:       2,
				Available: AvailabilityFull,
				Substitution: &QuoteSubstitution{
					CatalogID: subID,
					Name:      "Amoxicilina Premium",
					UnitPrice: 22000,
					Reason:    "Equivalente",
				},
			},
		},
		&deliveryFee,
		nil,
		nil,
		now.Add(24*time.Hour),
		nil,
		nil,
		now,
	)
	q := qr.Quotes[quoteID]
	return qr, &q
}

// Invariante O1: Una cotización -> un pedido
func TestOrder_O1_SingleOrderPerQuote(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	order := NewOrder()
	now := time.Now()

	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}
	err := order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Segundo intento sobre la misma instancia
	err = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	if !errors.Is(err, ErrOrderAlreadyPlaced) {
		t.Fatalf("expected ErrOrderAlreadyPlaced, got %v", err)
	}
}

// Invariante O2: Modalidad coherente con la cotización
func TestOrder_O2_ModeCoherent(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	// La cotización no incluye deliveryFee si es sólo recogida
	quote.DeliveryFee = nil
	qr.Mode = ModePickup

	order := NewOrder()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}
	err := order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery, // incongruente
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		time.Now(),
	)
	if !errors.Is(err, ErrModeMismatch) {
		t.Fatalf("expected ErrModeMismatch, got %v", err)
	}
}

// Invariante O3: Dirección obligatoria en delivery y teléfono obligatorio
func TestOrder_O3_DeliveryAddressAndContactRequired(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()

	// Teléfono vacío
	order1 := NewOrder()
	err := order1.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		&Address{Street: "Calle 123", City: "Bogota", Country: "CO"},
		"", // vacío
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	if !errors.Is(err, ErrContactPhoneRequired) {
		t.Fatalf("expected ErrContactPhoneRequired, got %v", err)
	}

	// Dirección nula en delivery
	order2 := NewOrder()
	err = order2.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		nil, // nulo
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	if !errors.Is(err, ErrDeliveryAddressRequired) {
		t.Fatalf("expected ErrDeliveryAddressRequired, got %v", err)
	}
}

// Invariante O4: Transiciones de la tabla
func TestOrder_O4_Transitions(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}

	order := NewOrder()
	_ = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)

	// No puede pasar a preparing directamente sin confirmed
	err := order.Prepare(now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition preparing without confirm, got %v", err)
	}

	// placed -> confirmed
	err = order.Confirm(nil, nil, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// confirmed -> preparing
	err = order.Prepare(now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// preparing -> out_for_delivery
	err = order.OutForDelivery(nil, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// out_for_delivery -> delivered
	disp := DispensationRecord{
		Lines: []DispensationLine{{ItemID: "item-1", Qty: 2}},
		At:    now,
	}
	err = order.Deliver(disp, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusDelivered {
		t.Fatalf("expected status delivered, got %s", order.Status)
	}
}

// Invariante O5: delivered exige DispensationRecord con líneas <= Lines
func TestOrder_O5_DispensationRequirements(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}

	order := NewOrder()
	_ = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	_ = order.Confirm(nil, nil, now)
	_ = order.Prepare(now)
	_ = order.OutForDelivery(nil, now)

	// Dispensación vacía
	err := order.Deliver(DispensationRecord{Lines: []DispensationLine{}}, now)
	if !errors.Is(err, ErrDispensationEmpty) {
		t.Fatalf("expected ErrDispensationEmpty, got %v", err)
	}

	// Dispensación que supera la cantidad pedida (orden: 2 unidades, entrega: 3)
	err = order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "item-1", Qty: 3}}}, now)
	if !errors.Is(err, ErrDispensationExceeds) {
		t.Fatalf("expected ErrDispensationExceeds, got %v", err)
	}
}

// Invariante O6: Cancelación limitada para el tutor
func TestOrder_O6_CancellationRules(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}

	order := NewOrder()
	_ = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	_ = order.Confirm(nil, nil, now)
	_ = order.Prepare(now)

	// Cancelación válida dentro de 1h
	err := order.Cancel("guardian", "Arrepentimiento", now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("unexpected cancel error within 1h: %v", err)
	}

	// Orden nueva para probar timeout de cancelación
	order2 := NewOrder()
	_ = order2.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	_ = order2.Confirm(nil, nil, now)
	_ = order2.Prepare(now)

	// Tutor intenta cancelar tras 1h 10m en preparing
	err = order2.Cancel("guardian", "Tarde", now.Add(70*time.Minute))
	if !errors.Is(err, ErrCancellationTimeout) {
		t.Fatalf("expected ErrCancellationTimeout, got %v", err)
	}

	// La farmacia sí puede cancelar en cualquier momento con motivo
	err = order2.Cancel("pharmacy", "Sin stock de emergencia", now.Add(70*time.Minute))
	if err != nil {
		t.Fatalf("expected pharmacy to be able to cancel, got %v", err)
	}
}

// Invariante O7: rejected libera la QuoteRequest
func TestOrder_O7_ReopenOnRejection(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}

	// Seleccionar cotización en QuoteRequest
	_, _ = qr.SelectQuote(quote.ID, []SubstitutionDecision{{ItemID: "item-1", Accept: false}}, now)
	if qr.Status != QuoteRequestStatusSelected {
		t.Fatalf("expected QR to be selected")
	}

	order := NewOrder()
	_ = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)

	// Farmacia rechaza el pedido
	err := order.Reject("No hay stock disponible", now)
	if err != nil {
		t.Fatalf("unexpected rejection error: %v", err)
	}
	if order.Status != OrderStatusRejected {
		t.Fatalf("expected order rejected, got %s", order.Status)
	}

	// Reacción en la saga: Reopen
	err = qr.Reopen("Pedido rechazado por la farmacia", now)
	if err != nil {
		t.Fatalf("unexpected reopen error: %v", err)
	}
	if qr.Status != QuoteRequestStatusOpen {
		t.Fatalf("expected QR to reopen to open status, got %s", qr.Status)
	}
}

// Invariante O8: Totales recalculados en el agregado
func TestOrder_O8_RecalculatedTotals(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 123", City: "Bogota", Country: "CO"}

	// Caso 1: Sustitución aceptada (precio original 20000 -> sustituto 22000; cant 2 = 44000 + 8000 envío = 52000)
	orderSubAccepted := NewOrder()
	err := orderSubAccepted.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: true}},
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orderSubAccepted.Totals.Subtotal != 44000 {
		t.Fatalf("expected subtotal 44000, got %f", orderSubAccepted.Totals.Subtotal)
	}
	if orderSubAccepted.Totals.DeliveryFee != 8000 {
		t.Fatalf("expected delivery fee 8000, got %f", orderSubAccepted.Totals.DeliveryFee)
	}
	if orderSubAccepted.Totals.Total != 52000 {
		t.Fatalf("expected total 52000, got %f", orderSubAccepted.Totals.Total)
	}

	// Caso 2: Sustitución rechazada (se mantiene precio original 20000; cant 2 = 40000 + 8000 envío = 48000)
	orderSubRejected := NewOrder()
	err = orderSubRejected.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		addr,
		"+573001234567",
		[]SubstitutionDecision{{ItemID: "item-1", Accept: false}},
		now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orderSubRejected.Totals.Subtotal != 40000 {
		t.Fatalf("expected subtotal 40000, got %f", orderSubRejected.Totals.Subtotal)
	}
	if orderSubRejected.Totals.Total != 48000 {
		t.Fatalf("expected total 48000, got %f", orderSubRejected.Totals.Total)
	}
}

func TestOrder_PickupFlowAndCoverage(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	qr.Mode = ModePickup
	quote.DeliveryFee = nil
	now := time.Now()

	// Nil arguments error
	orderNil := NewOrder()
	if err := orderNil.Place(uuid.New(), nil, nil, ModePickup, nil, "123", nil, now); err == nil {
		t.Fatalf("expected error for nil qr/quote")
	}

	order := NewOrder()
	err := order.Place(
		uuid.New(),
		qr,
		quote,
		ModePickup,
		nil, // dirección no necesaria en pickup
		"+573001234567",
		nil,
		now,
	)
	if err != nil {
		t.Fatalf("unexpected place error: %v", err)
	}

	// Confirm
	if err := order.Confirm(nil, nil, now); err != nil {
		t.Fatalf("confirm error: %v", err)
	}
	// Prepare
	if err := order.Prepare(now); err != nil {
		t.Fatalf("prepare error: %v", err)
	}
	// Ready
	if err := order.Ready(now); err != nil {
		t.Fatalf("ready error: %v", err)
	}
	// Cannot out_for_delivery if pickup
	if err := order.OutForDelivery(nil, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for out for delivery in pickup, got %v", err)
	}

	// Deliver
	disp := DispensationRecord{
		Lines: []DispensationLine{{ItemID: "item-1", Qty: 2}},
		At:    now,
	}
	if err := order.Deliver(disp, now); err != nil {
		t.Fatalf("deliver error: %v", err)
	}
	if order.Status != OrderStatusDelivered {
		t.Fatalf("expected delivered, got %s", order.Status)
	}

	// Cannot cancel after delivered
	if err := order.Cancel("guardian", "tarde", now); !errors.Is(err, ErrOrderNotCancellable) {
		t.Fatalf("expected ErrOrderNotCancellable, got %v", err)
	}

	// Uncommitted events
	if len(order.UncommittedEvents()) == 0 {
		t.Fatalf("expected uncommitted events")
	}
	order.MarkCommitted()
	if len(order.UncommittedEvents()) != 0 {
		t.Fatalf("expected 0 events after commit")
	}
}

func TestOrder_RejectAndInvalidTransitions(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()

	order := NewOrder()
	_ = order.Place(
		uuid.New(),
		qr,
		quote,
		ModeDelivery,
		&Address{Street: "Calle 10", City: "Bogota", Country: "CO"},
		"+573001234567",
		nil,
		now,
	)

	// Ready not allowed when placed
	if err := order.Ready(now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}

	// OutForDelivery not allowed when placed
	if err := order.OutForDelivery(nil, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}

	// Deliver not allowed when placed
	if err := order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "item-1", Qty: 1}}}, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}

	// Reject when placed -> ok
	if err := order.Reject("Rechazado", now); err != nil {
		t.Fatalf("reject error: %v", err)
	}

	// Cannot confirm when rejected
	if err := order.Confirm(nil, nil, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition confirming rejected, got %v", err)
	}

	// Cannot prepare when rejected
	if err := order.Prepare(now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition preparing rejected, got %v", err)
	}
}

func TestOrder_EdgeCasesForCoverage(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 10", City: "Bogota", Country: "CO"}

	// Item in quote not in quote request items
	quoteWithExtra := *quote
	quoteWithExtra.Lines = append([]QuoteLine{{ItemID: "unknown-item", UnitPrice: 100, Qty: 1, Available: AvailabilityFull}}, quote.Lines...)
	order := NewOrder()
	err := order.Place(uuid.New(), qr, &quoteWithExtra, ModeDelivery, addr, "+573001234567", nil, now)
	if err != nil {
		t.Fatalf("unexpected place error: %v", err)
	}

	_ = order.Confirm(nil, nil, now)
	_ = order.Prepare(now)
	_ = order.OutForDelivery(nil, now)

	// Dispensation line for unknown item
	err = order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "unknown-item", Qty: 1}}}, now)
	if !errors.Is(err, ErrDispensationExceeds) {
		t.Fatalf("expected ErrDispensationExceeds for unknown item in dispensation")
	}

	// Dispensation line with 0 or negative qty
	err = order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "item-1", Qty: 0}}}, now)
	if !errors.Is(err, ErrDispensationExceeds) {
		t.Fatalf("expected ErrDispensationExceeds for 0 qty")
	}

	// Valid deliver without setting time beforehand
	err = order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "item-1", Qty: 1}}}, now)
	if err != nil {
		t.Fatalf("unexpected deliver error: %v", err)
	}

	// Double deliver
	err = order.Deliver(DispensationRecord{Lines: []DispensationLine{{ItemID: "item-1", Qty: 1}}}, now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition on double deliver")
	}
}

func TestCoverage_OrderRemaining(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()

	// Empty street address in delivery
	order1 := NewOrder()
	err := order1.Place(uuid.New(), qr, quote, ModeDelivery, &Address{Street: "   ", City: "Bogota"}, "+573001234567", nil, now)
	if !errors.Is(err, ErrDeliveryAddressRequired) {
		t.Fatalf("expected ErrDeliveryAddressRequired for empty street")
	}

	// Empty city in delivery
	order2 := NewOrder()
	err = order2.Place(uuid.New(), qr, quote, ModeDelivery, &Address{Street: "Calle 1", City: "   "}, "+573001234567", nil, now)
	if !errors.Is(err, ErrDeliveryAddressRequired) {
		t.Fatalf("expected ErrDeliveryAddressRequired for empty city")
	}

	// Cancel when placed
	order3 := NewOrder()
	_ = order3.Place(uuid.New(), qr, quote, ModeDelivery, &Address{Street: "Calle 1", City: "Bogota"}, "+573001234567", nil, now)
	err = order3.Cancel("guardian", "Changed mind early", now)
	if err != nil {
		t.Fatalf("cancel placed error: %v", err)
	}

	// Cancel when confirmed
	order4 := NewOrder()
	_ = order4.Place(uuid.New(), qr, quote, ModeDelivery, &Address{Street: "Calle 1", City: "Bogota"}, "+573001234567", nil, now)
	_ = order4.Confirm(nil, nil, now)
	err = order4.Cancel("guardian", "Changed mind confirmed", now)
	if err != nil {
		t.Fatalf("cancel confirmed error: %v", err)
	}
}

func TestCoverage_OrderCancelDefaultActor(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 1", City: "Bogota"}
	order := NewOrder()
	_ = order.Place(uuid.New(), qr, quote, ModeDelivery, addr, "+573001234567", nil, now)

	// Cancel with unknown actor string defaults to "pharmacy"
	err := order.Cancel("custom_role", "Stock problem", now)
	if err != nil {
		t.Fatalf("cancel error: %v", err)
	}
	if order.Status != OrderStatusCancelled {
		t.Fatalf("expected cancelled")
	}

	// Preparing without timeline entry (preparingAt is zero)
	orderPrep := NewOrder()
	_ = orderPrep.Place(uuid.New(), qr, quote, ModeDelivery, addr, "+573001234567", nil, now)
	orderPrep.Status = OrderStatusPreparing
	orderPrep.Timeline = nil // clear timeline so preparingAt is zero
	err = orderPrep.Cancel("guardian", "Fast cancel", now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("expected cancel to succeed when timeline has no preparing timestamp: %v", err)
	}
}

func TestCoverage_OrderModesAndReadyTransitions(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 1", City: "Bogota"}

	// Delivery order: Ready should fail
	orderDelivery := NewOrder()
	_ = orderDelivery.Place(uuid.New(), qr, quote, ModeDelivery, addr, "+573001234567", nil, now)
	_ = orderDelivery.Confirm(nil, nil, now)
	_ = orderDelivery.Prepare(now)
	err := orderDelivery.Ready(now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for Ready on delivery mode, got %v", err)
	}

	// Pickup order: OutForDelivery should fail
	qrPickup := samplePrescription(uuid.New())
	qrP := NewQuoteRequest()
	_ = qrP.Request(context.Background(), uuid.New(), qrPickup, qrPickup.GuardianID, []RequestedItem{{ItemID: "item-1"}}, ModePickup, Area{City: "Bogota"}, []uuid.UUID{uuid.New()}, 24, nil, nil, nil, now)
	quotePickup := *quote
	quotePickup.DeliveryFee = nil
	orderPickup := NewOrder()
	_ = orderPickup.Place(uuid.New(), qrP, &quotePickup, ModePickup, nil, "+573001234567", nil, now)
	_ = orderPickup.Confirm(nil, nil, now)
	_ = orderPickup.Prepare(now)
	err = orderPickup.OutForDelivery(nil, now)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for OutForDelivery on pickup mode, got %v", err)
	}
}

func TestOrder_PRD022_Rating(t *testing.T) {
	qr, quote := sampleQuoteRequestAndQuote()
	now := time.Now()
	addr := &Address{Street: "Calle 1", City: "Bogota"}

	order := NewOrder()
	orderID := uuid.New()
	_ = order.Place(orderID, qr, quote, ModeDelivery, addr, "+573001234567", nil, now)

	// Intento de valorar antes de estar entregado -> ErrOrderNotDelivered
	if err := order.Rate(5, "Excelente servicio", now); !errors.Is(err, ErrOrderNotDelivered) {
		t.Fatalf("expected ErrOrderNotDelivered, got %v", err)
	}

	_ = order.Confirm(nil, nil, now)
	_ = order.Prepare(now)
	_ = order.OutForDelivery(nil, now)

	// Deliver
	disp := DispensationRecord{
		Lines: []DispensationLine{
			{ItemID: "item-1", Qty: 2},
		},
		At: now,
	}
	if err := order.Deliver(disp, now); err != nil {
		t.Fatalf("unexpected deliver error: %v", err)
	}

	// Score inválido (< 1 o > 5)
	if err := order.Rate(0, "Malo", now); !errors.Is(err, ErrInvalidRatingScore) {
		t.Fatalf("expected ErrInvalidRatingScore for 0, got %v", err)
	}
	if err := order.Rate(6, "Genial", now); !errors.Is(err, ErrInvalidRatingScore) {
		t.Fatalf("expected ErrInvalidRatingScore for 6, got %v", err)
	}

	// Comentario demasiado largo (> 500 caracteres)
	longComment := strings.Repeat("A", 501)
	if err := order.Rate(4, longComment, now); !errors.Is(err, ErrRatingCommentTooLong) {
		t.Fatalf("expected ErrRatingCommentTooLong, got %v", err)
	}

	// Valoración válida
	if err := order.Rate(5, "Muy rápida la entrega y medicamentos en perfecto estado.", now); err != nil {
		t.Fatalf("unexpected rate error: %v", err)
	}

	if order.Rating == nil || order.Rating.Score != 5 {
		t.Fatalf("expected order.Rating to be set with score 5, got %v", order.Rating)
	}

	// Intento de valorar dos veces -> ErrOrderAlreadyRated
	if err := order.Rate(4, "Otro comentario", now); !errors.Is(err, ErrOrderAlreadyRated) {
		t.Fatalf("expected ErrOrderAlreadyRated, got %v", err)
	}

	// Verificar evento emitido
	uncommitted := order.UncommittedEvents()
	var ratedEvent *OrderRated
	for _, ev := range uncommitted {
		if re, ok := ev.(OrderRated); ok {
			ratedEvent = &re
			break
		}
	}
	if ratedEvent == nil {
		t.Fatalf("expected OrderRated event in uncommitted events")
	}
	if ratedEvent.Score != 5 || ratedEvent.OrderID != orderID {
		t.Fatalf("invalid OrderRated event data: %+v", ratedEvent)
	}
}
