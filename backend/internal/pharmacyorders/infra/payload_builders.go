package infra

import (
	"fmt"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
)

// BuildQuoteRequestPayload construye el payload minimizado para farmacias al solicitar cotización (ARQ-013, SEC-005).
// Aplica lista blanca estricta: nunca comparte historial clínico ni datos de contacto del tutor.
func BuildQuoteRequestPayload(
	qr *domain.QuoteRequest,
	rx *domain.PrescriptionSnapshot,
	animal domain.AnimalSnapshot,
	publicBaseURL string,
) domain.QuoteRequestPayload {
	var rxURL *string
	if rx.Source == "issued" {
		u := fmt.Sprintf("%s/verify/prescriptions/%s", publicBaseURL, rx.ID)
		rxURL = &u
	}

	sanitizedItems := make([]domain.RequestedItem, 0, len(qr.Items))
	for _, it := range qr.Items {
		sanitizedItems = append(sanitizedItems, domain.RequestedItem{
			ItemID:              it.ItemID,
			CatalogID:           it.CatalogID,
			NameSnapshot:        it.NameSnapshot,
			Form:                it.Form,
			Concentration:       it.Concentration,
			FreeText:            it.FreeText,
			Dose:                it.Dose,
			Frequency:           it.Frequency,
			Quantity:            it.Quantity,
			Unit:                it.Unit,
			SubstitutionAllowed: it.SubstitutionAllowed,
			Controlled:          false,
		})
	}

	return domain.QuoteRequestPayload{
		QuoteRequestID:  qr.ID,
		PrescriptionID:  qr.PrescriptionID,
		PrescriptionURL: rxURL,
		Source:          rx.Source,
		Items:           sanitizedItems,
		Animal:          animal,
		Mode:            qr.Mode,
		Area: domain.Area{
			City: qr.Area.City,
			Zone: qr.Area.Zone,
		},
		ExpiresAt: qr.ExpiresAt,
	}
}

// BuildOrderPayload construye el payload para farmacias tras la adjudicación del pedido (ARQ-013, SEC-005).
// Comparte datos de contacto del tutor y dirección solo si la modalidad es delivery.
func BuildOrderPayload(
	order *domain.Order,
	rx *domain.PrescriptionSnapshot,
	guardianName string,
	publicBaseURL string,
	now time.Time,
) domain.OrderPayload {
	var rxURL *string
	if rx.Source == "issued" {
		u := fmt.Sprintf("%s/verify/prescriptions/%s", publicBaseURL, rx.ID)
		rxURL = &u
	}

	var deliveryAddr *domain.Address
	if order.Mode == domain.ModeDelivery && order.DeliveryAddress != nil {
		deliveryAddr = &domain.Address{
			Street:         order.DeliveryAddress.Street,
			City:           order.DeliveryAddress.City,
			PostalCode:     order.DeliveryAddress.PostalCode,
			Country:        order.DeliveryAddress.Country,
			AdditionalInfo: order.DeliveryAddress.AdditionalInfo,
		}
	}

	return domain.OrderPayload{
		OrderID:         order.ID,
		QuoteRequestID:  order.QuoteRequestID,
		QuoteID:         order.QuoteID,
		PrescriptionID:  order.PrescriptionID,
		PrescriptionURL: rxURL,
		Source:          rx.Source,
		GuardianName:    guardianName,
		ContactPhone:    order.ContactPhone,
		DeliveryAddress: deliveryAddr,
		Mode:            order.Mode,
		Lines:           order.Lines,
		Totals:          order.Totals,
		PlacedAt:        now,
	}
}
