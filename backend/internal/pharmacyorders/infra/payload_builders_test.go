package infra

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
)

// TestPayloadBuilders_Whitelist verifica que solo los campos explícitamente permitidos viajan a partners (SEC-005, ARQ-013).
func TestPayloadBuilders_Whitelist(t *testing.T) {
	qrID := uuid.New()
	rxID := uuid.New()
	petID := uuid.New()
	guardianID := uuid.New()
	orgID := uuid.New()
	now := time.Now()

	rx := &domain.PrescriptionSnapshot{
		ID:         rxID,
		PetID:      petID,
		GuardianID: guardianID,
		Country:    "CO",
		Status:     "issued",
		Source:     "issued",
		Items: []domain.PrescriptionItemSnapshot{
			{
				ItemID:              "item-1",
				Name:                "Amoxicilina 500mg",
				Quantity:            1,
				Unit:                "pack",
				SubstitutionAllowed: true,
			},
		},
	}

	qr := &domain.QuoteRequest{
		ID:             qrID,
		PrescriptionID: rxID,
		PetID:          petID,
		GuardianID:     guardianID,
		Country:        "CO",
		Items: []domain.RequestedItem{
			{
				ItemID:              "item-1",
				NameSnapshot:        "Amoxicilina 500mg",
				Quantity:            1,
				Unit:                "pack",
				SubstitutionAllowed: true,
			},
		},
		Mode:      domain.ModeDelivery,
		Area:      domain.Area{City: "Bogota"},
		ExpiresAt: now.Add(24 * time.Hour),
	}

	animal := domain.AnimalSnapshot{
		Species:  "canine",
		WeightKg: ptrFloat(12.5),
	}

	// 1. Validar QuoteRequestPayload
	qrPayload := BuildQuoteRequestPayload(qr, rx, animal, "https://petpassport.dev")
	qrJSON, err := json.Marshal(qrPayload)
	if err != nil {
		t.Fatalf("marshal qr payload: %v", err)
	}

	var qrMap map[string]any
	if err := json.Unmarshal(qrJSON, &qrMap); err != nil {
		t.Fatalf("unmarshal qr map: %v", err)
	}

	allowedQRKeys := map[string]bool{
		"quote_request_id": true,
		"prescription_id":  true,
		"prescription_url": true,
		"source":           true,
		"items":            true,
		"animal":           true,
		"mode":             true,
		"area":             true,
		"expires_at":       true,
	}

	for k := range qrMap {
		if !allowedQRKeys[k] {
			t.Errorf("campo no permitido en QuoteRequestPayload: %s", k)
		}
	}

	// Comprobar que NO existan datos de contacto del tutor ni historial
	prohibitedKeys := []string{"guardian_id", "guardian_name", "email", "phone", "medical_record", "diagnoses"}
	for _, pk := range prohibitedKeys {
		if _, exists := qrMap[pk]; exists {
			t.Errorf("campo prohibido presente en QuoteRequestPayload: %s", pk)
		}
	}

	// 2. Validar OrderPayload
	order := &domain.Order{
		ID:             uuid.New(),
		QuoteRequestID: qrID,
		QuoteID:        uuid.New(),
		PrescriptionID: rxID,
		PetID:          petID,
		GuardianID:     guardianID,
		PharmacyOrgID:  orgID,
		Mode:           domain.ModeDelivery,
		DeliveryAddress: &domain.Address{
			Street:  "Calle 123 # 45-67",
			City:    "Bogota",
			Country: "CO",
		},
		ContactPhone: "+573001234567",
		Lines: []domain.OrderLine{
			{ItemID: "item-1", Qty: 1, UnitPrice: 15000},
		},
		Totals: domain.OrderTotals{
			Subtotal:    15000,
			DeliveryFee: 5000,
			Total:       20000,
			Currency:    "COP",
		},
		Status: domain.OrderStatusPlaced,
	}

	orderPayload := BuildOrderPayload(order, rx, "Juan Pérez", "https://petpassport.dev", now)
	orderJSON, err := json.Marshal(orderPayload)
	if err != nil {
		t.Fatalf("marshal order payload: %v", err)
	}

	var orderMap map[string]any
	if err := json.Unmarshal(orderJSON, &orderMap); err != nil {
		t.Fatalf("unmarshal order map: %v", err)
	}

	allowedOrderKeys := map[string]bool{
		"order_id":         true,
		"quote_request_id": true,
		"quote_id":         true,
		"prescription_id":  true,
		"prescription_url": true,
		"source":           true,
		"guardian_name":    true,
		"contact_phone":    true,
		"delivery_address": true,
		"mode":             true,
		"lines":            true,
		"totals":           true,
		"placed_at":        true,
	}

	for k := range orderMap {
		if !allowedOrderKeys[k] {
			t.Errorf("campo no permitido en OrderPayload: %s", k)
		}
	}
}

func ptrFloat(f float64) *float64 { return &f }
