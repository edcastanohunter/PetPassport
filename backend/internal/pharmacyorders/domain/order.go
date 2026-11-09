package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StreamTypeOrder es el identificador de stream de EventStore para pedidos a farmacias (DOM-009).
const StreamTypeOrder = "Order"

// Order es la raíz de agregado del pedido formal acordado entre el tutor y la farmacia (PP-F4-DOM-009, ADR-020).
// Stream ord-<id>.
type Order struct {
	ID              uuid.UUID
	Version         int64
	QuoteRequestID  uuid.UUID
	QuoteID         uuid.UUID
	PrescriptionID  uuid.UUID
	PetID           uuid.UUID
	GuardianID      uuid.UUID
	PharmacyOrgID   uuid.UUID
	Mode            FulfillmentMode
	DeliveryAddress *Address
	ContactPhone    string
	Lines           []OrderLine
	Totals          OrderTotals
	Status          OrderStatus
	Timeline        []OrderTimelineEntry
	Dispensation    *DispensationRecord
	PartnerRef      *string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	uncommitted []any
}

// NewOrder crea una instancia vacía del agregado Order (versión 0).
func NewOrder() *Order {
	return &Order{
		Timeline: make([]OrderTimelineEntry, 0),
		Lines:    make([]OrderLine, 0),
	}
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (o *Order) UncommittedEvents() []any {
	return o.uncommitted
}

// MarkCommitted limpia los eventos tras persistir en el Event Store.
func (o *Order) MarkCommitted() {
	o.uncommitted = nil
}

func (o *Order) record(evt any) {
	o.Apply(evt)
	o.uncommitted = append(o.uncommitted, evt)
}

// Place inicializa el agregado Order a partir de una cotización seleccionada (Invariantes O1, O2, O3, O8).
func (o *Order) Place(
	id uuid.UUID,
	qr *QuoteRequest,
	quote *Quote,
	mode FulfillmentMode,
	deliveryAddress *Address,
	contactPhone string,
	decisions []SubstitutionDecision,
	now time.Time,
) error {
	// Invariante O1: Una cotización -> un pedido
	if o.Version > 0 {
		return ErrOrderAlreadyPlaced
	}

	if qr == nil || quote == nil {
		return fmt.Errorf("quote request and quote required")
	}

	// Invariante O2: Modalidad coherente con la cotización
	if mode == ModeDelivery && (quote.DeliveryFee == nil && qr.Mode != ModeDelivery) {
		return ErrModeMismatch
	}

	// Invariante O3: Datos de contacto y entrega
	contactPhone = strings.TrimSpace(contactPhone)
	if contactPhone == "" {
		return ErrContactPhoneRequired
	}
	if mode == ModeDelivery {
		if deliveryAddress == nil || strings.TrimSpace(deliveryAddress.Street) == "" || strings.TrimSpace(deliveryAddress.City) == "" {
			return ErrDeliveryAddressRequired
		}
	}

	decisionMap := make(map[string]bool, len(decisions))
	for _, d := range decisions {
		decisionMap[d.ItemID] = d.Accept
	}

	reqItemMap := make(map[string]RequestedItem, len(qr.Items))
	for _, it := range qr.Items {
		reqItemMap[it.ItemID] = it
	}

	// Invariante O8: Totales recalculados en el agregado
	lines := make([]OrderLine, 0, len(quote.Lines))
	var subtotal float64

	for _, ql := range quote.Lines {
		if ql.Available == AvailabilityNone {
			continue
		}

		reqItem, exists := reqItemMap[ql.ItemID]
		if !exists {
			continue
		}

		ol := OrderLine{
			ItemID:    ql.ItemID,
			CatalogID: reqItem.CatalogID,
			FreeText:  reqItem.FreeText,
			Qty:       ql.Qty,
			UnitPrice: ql.UnitPrice,
		}

		// Aplicar sustitución si fue propuesta y aceptada
		if ql.Substitution != nil {
			if accepted, ok := decisionMap[ql.ItemID]; ok && accepted {
				subID := ql.Substitution.CatalogID
				subName := ql.Substitution.Name
				ol.Substituted = &subID
				ol.SubstitutedName = &subName
				ol.UnitPrice = ql.Substitution.UnitPrice
			}
		}

		subtotal += ol.UnitPrice * ol.Qty
		lines = append(lines, ol)
	}

	var deliveryFee float64
	if mode == ModeDelivery && quote.DeliveryFee != nil {
		deliveryFee = *quote.DeliveryFee
	}
	total := subtotal + deliveryFee

	totals := OrderTotals{
		Subtotal:    subtotal,
		DeliveryFee: deliveryFee,
		Total:       total,
		Currency:    quote.Currency,
	}

	evt := OrderPlaced{
		OrderID:         id,
		QuoteRequestID:  qr.ID,
		QuoteID:         quote.ID,
		PrescriptionID:  qr.PrescriptionID,
		PetID:           qr.PetID,
		GuardianID:      qr.GuardianID,
		PharmacyOrgID:   quote.OrgID,
		Mode:            mode,
		Lines:           lines,
		Totals:          totals,
		DeliveryAddress: deliveryAddress,
		ContactPhone:    contactPhone,
		At:              now,
	}
	o.record(evt)
	return nil
}

// Confirm transiciona el pedido a confirmado por la farmacia (O4).
func (o *Order) Confirm(partnerRef *string, eta *string, now time.Time) error {
	if o.Status != OrderStatusPlaced {
		return ErrInvalidTransition
	}
	o.record(OrderConfirmed{
		OrderID:    o.ID,
		PartnerRef: partnerRef,
		ETA:        eta,
		At:         now,
	})
	return nil
}

// Reject rechaza el pedido por parte de la farmacia (O4, O7).
func (o *Order) Reject(reason string, now time.Time) error {
	if o.Status != OrderStatusPlaced {
		return ErrInvalidTransition
	}
	o.record(OrderRejected{
		OrderID: o.ID,
		Reason:  reason,
		At:      now,
	})
	return nil
}

// Prepare pasa el pedido al estado preparing (O4).
func (o *Order) Prepare(now time.Time) error {
	if o.Status != OrderStatusConfirmed {
		return ErrInvalidTransition
	}
	o.record(OrderPreparing{
		OrderID: o.ID,
		At:      now,
	})
	return nil
}

// Ready marca el pedido de recogida como listo en sede (O4).
func (o *Order) Ready(now time.Time) error {
	if o.Status != OrderStatusPreparing || o.Mode != ModePickup {
		return ErrInvalidTransition
	}
	o.record(OrderReady{
		OrderID: o.ID,
		At:      now,
	})
	return nil
}

// OutForDelivery marca el pedido de entrega a domicilio como despachado en camino (O4).
func (o *Order) OutForDelivery(eta *string, now time.Time) error {
	if o.Status != OrderStatusPreparing || o.Mode != ModeDelivery {
		return ErrInvalidTransition
	}
	o.record(OrderOutForDelivery{
		OrderID: o.ID,
		ETA:     eta,
		At:      now,
	})
	return nil
}

// Deliver finaliza el pedido como entregado y registra la dispensación (Invariantes O4, O5).
func (o *Order) Deliver(dispensation DispensationRecord, now time.Time) error {
	if o.Mode == ModePickup && o.Status != OrderStatusReady {
		return ErrInvalidTransition
	}
	if o.Mode == ModeDelivery && o.Status != OrderStatusOutForDelivery {
		return ErrInvalidTransition
	}

	// Invariante O5: El registro de dispensación es obligatorio y sus cantidades no pueden superar las pedidas
	if len(dispensation.Lines) == 0 {
		return ErrDispensationEmpty
	}

	orderLineMap := make(map[string]float64, len(o.Lines))
	for _, l := range o.Lines {
		orderLineMap[l.ItemID] = l.Qty
	}

	for _, dl := range dispensation.Lines {
		orderedQty, exists := orderLineMap[dl.ItemID]
		if !exists {
			return ErrDispensationExceeds
		}
		if dl.Qty <= 0 || dl.Qty > orderedQty {
			return ErrDispensationExceeds
		}
	}

	if dispensation.At.IsZero() {
		dispensation.At = now
	}

	o.record(OrderDelivered{
		OrderID:      o.ID,
		Dispensation: dispensation,
		At:           now,
	})
	return nil
}

// Cancel cancela el pedido según el actor (Invariantes O4, O6).
func (o *Order) Cancel(by string, reason string, now time.Time) error {
	by = strings.ToLower(strings.TrimSpace(by))
	if by != "guardian" && by != "pharmacy" {
		by = "pharmacy"
	}

	if o.Status != OrderStatusPlaced && o.Status != OrderStatusConfirmed && o.Status != OrderStatusPreparing {
		return ErrOrderNotCancellable
	}

	// Invariante O6: Limitación para el tutor (máximo 1h en preparing)
	if by == "guardian" && o.Status == OrderStatusPreparing {
		var preparingAt time.Time
		for _, t := range o.Timeline {
			if t.Status == OrderStatusPreparing {
				preparingAt = t.At
			}
		}
		if !preparingAt.IsZero() && now.Sub(preparingAt) > time.Hour {
			return ErrCancellationTimeout
		}
	}

	o.record(OrderCancelled{
		OrderID: o.ID,
		By:      by,
		Reason:  reason,
		At:      now,
	})
	return nil
}

// Apply rehidrata el agregado Order según los eventos recibidos.
func (o *Order) Apply(evt any) {
	o.Version++
	o.UpdatedAt = time.Now()

	switch e := evt.(type) {
	case OrderPlaced:
		o.ID = e.OrderID
		o.QuoteRequestID = e.QuoteRequestID
		o.QuoteID = e.QuoteID
		o.PrescriptionID = e.PrescriptionID
		o.PetID = e.PetID
		o.GuardianID = e.GuardianID
		o.PharmacyOrgID = e.PharmacyOrgID
		o.Mode = e.Mode
		o.Lines = e.Lines
		o.Totals = e.Totals
		o.DeliveryAddress = e.DeliveryAddress
		o.ContactPhone = e.ContactPhone
		o.Status = OrderStatusPlaced
		o.CreatedAt = e.At
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusPlaced, At: e.At})

	case OrderConfirmed:
		o.Status = OrderStatusConfirmed
		o.PartnerRef = e.PartnerRef
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusConfirmed, At: e.At})

	case OrderRejected:
		o.Status = OrderStatusRejected
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusRejected, At: e.At, Reason: &e.Reason})

	case OrderPreparing:
		o.Status = OrderStatusPreparing
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusPreparing, At: e.At})

	case OrderReady:
		o.Status = OrderStatusReady
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusReady, At: e.At})

	case OrderOutForDelivery:
		o.Status = OrderStatusOutForDelivery
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusOutForDelivery, At: e.At})

	case OrderDelivered:
		o.Status = OrderStatusDelivered
		o.Dispensation = &e.Dispensation
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusDelivered, At: e.At})

	case OrderCancelled:
		o.Status = OrderStatusCancelled
		o.Timeline = append(o.Timeline, OrderTimelineEntry{Status: OrderStatusCancelled, At: e.At, Reason: &e.Reason})
	}
}
