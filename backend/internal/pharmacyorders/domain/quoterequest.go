package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StreamTypeQuoteRequest es el identificador de stream de EventStore para solicitudes de cotización (DOM-008).
const StreamTypeQuoteRequest = "QuoteRequest"

// QuoteRequest es la raíz de agregado que orquesta la saga de cotización entre farmacias (PP-F4-DOM-008, ADR-020).
// Stream qr-<id>.
type QuoteRequest struct {
	ID              uuid.UUID
	Version         int64
	PrescriptionID  uuid.UUID
	PetID           uuid.UUID
	GuardianID      uuid.UUID
	Country         string
	Items           []RequestedItem
	Mode            FulfillmentMode
	Area            Area
	Invited         map[uuid.UUID]Invitation
	Quotes          map[uuid.UUID]Quote
	Status          QuoteRequestStatus
	ExpiresAt       time.Time
	SelectableUntil *time.Time
	SelectedQuoteID *uuid.UUID
	OrderID         *uuid.UUID
	ClosedReason    *string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	uncommitted []any
}

// NewQuoteRequest construye una instancia vacía del agregado (versión 0).
func NewQuoteRequest() *QuoteRequest {
	return &QuoteRequest{
		Invited: make(map[uuid.UUID]Invitation),
		Quotes:  make(map[uuid.UUID]Quote),
	}
}

// UncommittedEvents devuelve los eventos pendientes de persistir.
func (qr *QuoteRequest) UncommittedEvents() []any {
	return qr.uncommitted
}

// MarkCommitted limpia la cola de eventos tras persistir en el Event Store.
func (qr *QuoteRequest) MarkCommitted() {
	qr.uncommitted = nil
}

func (qr *QuoteRequest) record(evt any) {
	qr.Apply(evt)
	qr.uncommitted = append(qr.uncommitted, evt)
}

// Request inicia una nueva saga de cotización validando invariantes Q1–Q5 (DOM-008).
func (qr *QuoteRequest) Request(
	ctx context.Context,
	id uuid.UUID,
	prescription *PrescriptionSnapshot,
	guardianID uuid.UUID,
	items []RequestedItem,
	mode FulfillmentMode,
	area Area,
	orgIDs []uuid.UUID,
	expiresInHours int,
	catalogEquiv CatalogEquivalence,
	quotaChecker QuoteRequestQuotaChecker,
	dir PartnerDirectory,
	now time.Time,
) error {
	if qr.Version > 0 {
		return fmt.Errorf("quote request already initialized")
	}

	if prescription == nil {
		return ErrPrescriptionNotQuotable
	}

	// Invariante Q1: El actor debe ser el tutor legítimo de la receta
	if prescription.GuardianID != guardianID {
		return ErrNotGuardian
	}

	// Invariante Q2: Receta vigente (issued o confirmed), no expirada ni agotada
	if prescription.Status != "issued" && prescription.Status != "confirmed" {
		return ErrPrescriptionNotQuotable
	}
	if !prescription.ValidUntil.IsZero() && prescription.ValidUntil.Before(now) {
		return ErrPrescriptionNotQuotable
	}
	if prescription.RefillsUsed > prescription.RefillsAllowed {
		return ErrPrescriptionNotQuotable
	}

	// Invariante Q3: Ítems pertenecen a la receta y ninguno es controlado
	if len(items) == 0 {
		return ErrItemsEmpty
	}
	prescriptionItemMap := make(map[string]PrescriptionItemSnapshot, len(prescription.Items))
	for _, it := range prescription.Items {
		prescriptionItemMap[it.ItemID] = it
	}

	sanitizedItems := make([]RequestedItem, 0, len(items))
	for _, it := range items {
		pItem, exists := prescriptionItemMap[it.ItemID]
		if !exists {
			return ErrItemsNotSubset
		}

		// Comprobar sustancias controladas (en la receta o vía catálogo)
		if pItem.Controlled || it.Controlled {
			return ErrControlledNotOrderable
		}
		if pItem.CatalogID != nil && catalogEquiv != nil {
			isCtrl, err := catalogEquiv.IsControlled(ctx, *pItem.CatalogID)
			if err != nil {
				return fmt.Errorf("checking controlled status: %w", err)
			}
			if isCtrl {
				return ErrControlledNotOrderable
			}
		}

		it.CatalogID = pItem.CatalogID
		it.NameSnapshot = pItem.Name
		it.Form = pItem.Form
		it.Concentration = pItem.Concentration
		it.FreeText = pItem.FreeText
		it.SubstitutionAllowed = pItem.SubstitutionAllowed
		it.Controlled = false
		if it.Quantity <= 0 {
			it.Quantity = pItem.Quantity
		}
		if it.Unit == "" {
			it.Unit = pItem.Unit
		}
		sanitizedItems = append(sanitizedItems, it)
	}

	// Invariante Q4: Entre 1 y 10 farmacias aliadas elegibles para la zona y modalidad
	if len(orgIDs) < 1 || len(orgIDs) > 10 {
		return ErrInvalidOrgCount
	}
	uniqueOrgs := make(map[uuid.UUID]struct{}, len(orgIDs))
	dedupedOrgs := make([]uuid.UUID, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		if _, seen := uniqueOrgs[orgID]; !seen {
			uniqueOrgs[orgID] = struct{}{}
			dedupedOrgs = append(dedupedOrgs, orgID)
		}
	}
	if len(dedupedOrgs) == 0 {
		return ErrInvalidOrgCount
	}

	if dir != nil {
		for _, orgID := range dedupedOrgs {
			eligible, err := dir.IsEligible(ctx, orgID, area, mode)
			if err != nil {
				return fmt.Errorf("checking pharmacy eligibility: %w", err)
			}
			if !eligible {
				return ErrPharmacyNotEligible
			}
		}
	}

	// Invariante Q5: Cuotas (máx 3 abiertas por receta, máx 10 al día por tutor)
	if quotaChecker != nil {
		if err := quotaChecker.CheckQuotas(ctx, guardianID, prescription.ID, now); err != nil {
			return err
		}
	}

	if expiresInHours <= 0 {
		expiresInHours = 24
	}
	expiresAt := now.Add(time.Duration(expiresInHours) * time.Hour)

	evt := QuoteRequested{
		QuoteRequestID: id,
		PrescriptionID: prescription.ID,
		PetID:          prescription.PetID,
		GuardianID:     guardianID,
		Country:        strings.ToUpper(strings.TrimSpace(prescription.Country)),
		Items:          sanitizedItems,
		Mode:           mode,
		Area:           area,
		InvitedOrgIDs:  dedupedOrgs,
		ExpiresAt:      expiresAt,
	}
	qr.record(evt)
	return nil
}

// MarkInvitationSent registra el despacho de la solicitud a una farmacia.
func (qr *QuoteRequest) MarkInvitationSent(orgID uuid.UUID, channel string, sentAt time.Time) {
	if qr.Status != QuoteRequestStatusOpen {
		return
	}
	if _, invited := qr.Invited[orgID]; !invited {
		return
	}
	qr.record(QuoteInvitationSent{
		QuoteRequestID: qr.ID,
		OrganizationID: orgID,
		Channel:        channel,
		SentAt:         sentAt,
	})
}

// MarkInvitationFailed registra el fallo del despacho de invitación a una farmacia.
func (qr *QuoteRequest) MarkInvitationFailed(orgID uuid.UUID, lastErr string, at time.Time) {
	if qr.Status != QuoteRequestStatusOpen {
		return
	}
	if _, invited := qr.Invited[orgID]; !invited {
		return
	}
	qr.record(QuoteInvitationFailed{
		QuoteRequestID: qr.ID,
		OrganizationID: orgID,
		LastError:      lastErr,
		At:             at,
	})
}

// RecordQuote procesa una cotización recibida de una farmacia (Q6, Q7).
func (qr *QuoteRequest) RecordQuote(
	ctx context.Context,
	quoteID uuid.UUID,
	orgID uuid.UUID,
	lines []QuoteLine,
	deliveryFee *float64,
	eta *string,
	pickup *PickupRef,
	validUntil time.Time,
	notes *string,
	catalogEquiv CatalogEquivalence,
	now time.Time,
) error {
	if qr.Status != QuoteRequestStatusOpen {
		return ErrQuoteRequestClosed
	}

	// Comprobar que la farmacia esté invitada
	if _, invited := qr.Invited[orgID]; !invited {
		return ErrOrgNotInvited
	}

	// Invariante Q7: Precios >= 0 y validación estricta de sustituciones
	reqItemMap := make(map[string]RequestedItem, len(qr.Items))
	for _, it := range qr.Items {
		reqItemMap[it.ItemID] = it
	}

	var subtotal float64
	allFull := true
	hasAny := false

	sanitizedLines := make([]QuoteLine, 0, len(lines))
	for _, l := range lines {
		reqItem, exists := reqItemMap[l.ItemID]
		if !exists {
			continue
		}
		if l.UnitPrice < 0 {
			return ErrPriceNegative
		}
		if l.Available == AvailabilityNone {
			allFull = false
		} else {
			hasAny = true
			if l.Available == AvailabilityPartial {
				allFull = false
			}
			subtotal += l.UnitPrice * l.Qty
		}

		if l.Substitution != nil {
			if !reqItem.SubstitutionAllowed {
				return ErrSubstitutionNotAllowed
			}
			if l.Substitution.UnitPrice < 0 {
				return ErrPriceNegative
			}
			if reqItem.CatalogID != nil && catalogEquiv != nil {
				equiv, err := catalogEquiv.AreEquivalent(ctx, *reqItem.CatalogID, l.Substitution.CatalogID)
				if err != nil {
					return fmt.Errorf("checking equivalence: %w", err)
				}
				if !equiv {
					return ErrSubstitutionNotEquivalent
				}
			}
		}
		sanitizedLines = append(sanitizedLines, l)
	}

	var completeness Availability
	if allFull && len(sanitizedLines) == len(qr.Items) {
		completeness = AvailabilityFull
	} else if hasAny {
		completeness = AvailabilityPartial
	} else {
		completeness = AvailabilityNone
	}

	total := subtotal
	if deliveryFee != nil && *deliveryFee > 0 {
		total += *deliveryFee
	}

	if validUntil.IsZero() {
		validUntil = qr.ExpiresAt
	}

	q := Quote{
		ID:           quoteID,
		OrgID:        orgID,
		Lines:        sanitizedLines,
		Subtotal:     subtotal,
		DeliveryFee:  deliveryFee,
		Total:        total,
		Currency:     "COP", // Default ISO para CO según país
		ETA:          eta,
		Pickup:       pickup,
		ValidUntil:   validUntil,
		Notes:        notes,
		Completeness: completeness,
		ReceivedAt:   now,
		Superseded:   false,
	}

	qr.record(QuoteReceived{
		QuoteRequestID: qr.ID,
		OrganizationID: orgID,
		Quote:          q,
	})
	return nil
}

// DeclineQuote registra cuando una farmacia rechaza explícitamente cotizar.
func (qr *QuoteRequest) DeclineQuote(orgID uuid.UUID, reason string, now time.Time) error {
	if qr.Status != QuoteRequestStatusOpen {
		return ErrQuoteRequestClosed
	}
	if _, invited := qr.Invited[orgID]; !invited {
		return ErrOrgNotInvited
	}
	qr.record(QuoteDeclined{
		QuoteRequestID: qr.ID,
		OrganizationID: orgID,
		Reason:         reason,
		At:             now,
	})
	return nil
}

// SelectQuote valida y adjudica la cotización ganadora (Invariante Q8).
func (qr *QuoteRequest) SelectQuote(
	quoteID uuid.UUID,
	decisions []SubstitutionDecision,
	now time.Time,
) (*Quote, error) {
	if qr.Status != QuoteRequestStatusOpen {
		return nil, ErrQuoteRequestClosed
	}

	q, exists := qr.Quotes[quoteID]
	if !exists || q.Superseded {
		return nil, ErrQuoteNotFound
	}

	// Invariante Q8: Cotización vigente (ValidUntil y plazo de saga)
	if now.After(q.ValidUntil) {
		return nil, ErrQuoteExpired
	}
	if now.After(qr.ExpiresAt) {
		if qr.SelectableUntil == nil || now.After(*qr.SelectableUntil) {
			return nil, ErrQuoteExpired
		}
	}

	// Invariante Q8: Se requiere decisión explícita para cada sustitución propuesta en la cotización
	decisionMap := make(map[string]bool, len(decisions))
	for _, d := range decisions {
		decisionMap[d.ItemID] = d.Accept
	}

	for _, line := range q.Lines {
		if line.Substitution != nil {
			if _, answered := decisionMap[line.ItemID]; !answered {
				return nil, ErrSubstitutionDecisionRequired
			}
		}
	}

	qr.record(QuoteSelected{
		QuoteRequestID:        qr.ID,
		QuoteID:               quoteID,
		OrganizationID:        q.OrgID,
		SubstitutionDecisions: decisions,
		At:                    now,
	})
	return &q, nil
}

// Cancel cancela la solicitud por solicitud del tutor.
func (qr *QuoteRequest) Cancel(reason string, guardianID uuid.UUID, now time.Time) error {
	if qr.Status != QuoteRequestStatusOpen {
		return ErrQuoteRequestClosed
	}
	if qr.GuardianID != guardianID {
		return ErrNotGuardian
	}
	qr.record(QuoteRequestCancelled{
		QuoteRequestID: qr.ID,
		Reason:         reason,
		At:             now,
	})
	return nil
}

// Expire expira las invitaciones pendientes al vencer el plazo (DOM-008).
func (qr *QuoteRequest) Expire(now time.Time) error {
	if qr.Status != QuoteRequestStatusOpen {
		return nil
	}
	if now.Before(qr.ExpiresAt) {
		return nil
	}

	hasQuotes := false
	for _, q := range qr.Quotes {
		if !q.Superseded && q.ValidUntil.After(now) {
			hasQuotes = true
			break
		}
	}

	var selectableUntil *time.Time
	if hasQuotes {
		// Sigue seleccionable 24 horas más
		ext := qr.ExpiresAt.Add(24 * time.Hour)
		selectableUntil = &ext
	}

	qr.record(QuoteRequestExpired{
		QuoteRequestID:  qr.ID,
		SelectableUntil: selectableUntil,
		At:              now,
	})
	return nil
}

// Close finaliza la solicitud de cotización por creación de pedido o revocación.
func (qr *QuoteRequest) Close(orderID *uuid.UUID, reason string, now time.Time) error {
	if qr.Status == QuoteRequestStatusClosed {
		return nil
	}
	qr.record(QuoteRequestClosed{
		QuoteRequestID: qr.ID,
		OrderID:        orderID,
		Reason:         reason,
		At:             now,
	})
	return nil
}

// Reopen reactiva la solicitud si el pedido generado fue rechazado o cancelado por farmacia.
func (qr *QuoteRequest) Reopen(reason string, now time.Time) error {
	if qr.Status == QuoteRequestStatusOpen {
		return nil
	}
	qr.record(QuoteRequestReopened{
		QuoteRequestID: qr.ID,
		Reason:         reason,
		At:             now,
	})
	return nil
}

// Apply reconstruye el estado del agregado a partir de los eventos del stream.
func (qr *QuoteRequest) Apply(evt any) {
	qr.Version++
	qr.UpdatedAt = time.Now()

	switch e := evt.(type) {
	case QuoteRequested:
		qr.ID = e.QuoteRequestID
		qr.PrescriptionID = e.PrescriptionID
		qr.PetID = e.PetID
		qr.GuardianID = e.GuardianID
		qr.Country = e.Country
		qr.Items = e.Items
		qr.Mode = e.Mode
		qr.Area = e.Area
		qr.Status = QuoteRequestStatusOpen
		qr.ExpiresAt = e.ExpiresAt
		qr.CreatedAt = e.ExpiresAt.Add(-24 * time.Hour)
		qr.Invited = make(map[uuid.UUID]Invitation, len(e.InvitedOrgIDs))
		for _, orgID := range e.InvitedOrgIDs {
			qr.Invited[orgID] = Invitation{
				OrgID:  orgID,
				Status: InvitationPending,
			}
		}

	case QuoteInvitationSent:
		if inv, ok := qr.Invited[e.OrganizationID]; ok {
			inv.Status = InvitationSent
			inv.Channel = e.Channel
			inv.SentAt = &e.SentAt
			qr.Invited[e.OrganizationID] = inv
		}

	case QuoteInvitationFailed:
		if inv, ok := qr.Invited[e.OrganizationID]; ok {
			inv.Status = InvitationFailed
			inv.Attempts++
			inv.LastError = &e.LastError
			qr.Invited[e.OrganizationID] = inv
		}

	case QuoteReceived:
		// Marca anteriores de esta farmacia como superseded (Invariante Q6)
		for k, existing := range qr.Quotes {
			if existing.OrgID == e.OrganizationID {
				existing.Superseded = true
				qr.Quotes[k] = existing
			}
		}
		qr.Quotes[e.Quote.ID] = e.Quote
		if inv, ok := qr.Invited[e.OrganizationID]; ok {
			inv.Status = InvitationQuoted
			inv.RespondedAt = &e.Quote.ReceivedAt
			qr.Invited[e.OrganizationID] = inv
		}

	case QuoteDeclined:
		if inv, ok := qr.Invited[e.OrganizationID]; ok {
			inv.Status = InvitationDeclined
			inv.LastError = &e.Reason
			inv.RespondedAt = &e.At
			qr.Invited[e.OrganizationID] = inv
		}

	case QuoteSelected:
		qr.Status = QuoteRequestStatusSelected
		qr.SelectedQuoteID = &e.QuoteID

	case QuoteRequestCancelled:
		qr.Status = QuoteRequestStatusClosed
		qr.ClosedReason = &e.Reason

	case QuoteRequestExpired:
		qr.SelectableUntil = e.SelectableUntil
		if e.SelectableUntil == nil {
			qr.Status = QuoteRequestStatusClosed
			reason := "expired_without_quotes"
			qr.ClosedReason = &reason
		}
		// Marca invitaciones pendientes como expiradas
		for orgID, inv := range qr.Invited {
			if inv.Status == InvitationPending || inv.Status == InvitationSent {
				inv.Status = InvitationExpired
				qr.Invited[orgID] = inv
			}
		}

	case QuoteRequestClosed:
		qr.Status = QuoteRequestStatusClosed
		qr.OrderID = e.OrderID
		qr.ClosedReason = &e.Reason

	case QuoteRequestReopened:
		qr.Status = QuoteRequestStatusOpen
		qr.SelectedQuoteID = nil
		qr.OrderID = nil
	}
}
