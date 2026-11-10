package domain

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

var update = flag.Bool("update", false, "regenera los golden files")

func TestEvents_Golden(t *testing.T) {
	fixedUUID := uuid.MustParse("01930000-0000-7000-8000-000000000001")
	fixedTime := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	reqItem := RequestedItem{
		ItemID:              "item-001",
		CatalogID:           &fixedUUID,
		NameSnapshot:        "Amoxicilina 500mg",
		Form:                "tableta",
		Concentration:       "500mg",
		Dose:                "1 tableta",
		Frequency:           "every_12_hours",
		Quantity:            2,
		Unit:                "pack",
		SubstitutionAllowed: true,
		Controlled:          false,
	}

	qLine := QuoteLine{
		ItemID:    "item-001",
		UnitPrice: 15000,
		Qty:       2,
		Available: AvailabilityFull,
		Substitution: &QuoteSubstitution{
			CatalogID: fixedUUID,
			Name:      "Amoxicilina Bioequivalente",
			UnitPrice: 14000,
			Reason:    "Disponibilidad de laboratorio",
		},
	}

	quote := Quote{
		ID:           fixedUUID,
		OrgID:        fixedUUID,
		Lines:        []QuoteLine{qLine},
		Subtotal:     30000,
		DeliveryFee:  ptrFloat(5000),
		Total:        35000,
		Currency:     "COP",
		ETA:          ptrString("2 horas"),
		Pickup:       &PickupRef{Address: "Carrera 7 # 12-34", Hours: "8:00 - 18:00"},
		ValidUntil:   fixedTime.Add(24 * time.Hour),
		Notes:        ptrString("Oferta preferencial"),
		Completeness: AvailabilityFull,
		ReceivedAt:   fixedTime,
		Superseded:   false,
	}

	events := []struct {
		name string
		data any
	}{
		{
			name: "QuoteRequested",
			data: QuoteRequested{
				QuoteRequestID: fixedUUID,
				PrescriptionID: fixedUUID,
				PetID:          fixedUUID,
				GuardianID:     fixedUUID,
				Country:        "CO",
				Items:          []RequestedItem{reqItem},
				Mode:           ModeDelivery,
				Area:           Area{City: "Bogota", Zone: ptrString("Chapinero")},
				InvitedOrgIDs:  []uuid.UUID{fixedUUID},
				ExpiresAt:      fixedTime.Add(24 * time.Hour),
			},
		},
		{
			name: "QuoteInvitationSent",
			data: QuoteInvitationSent{
				QuoteRequestID: fixedUUID,
				OrganizationID: fixedUUID,
				Channel:        "portal",
				SentAt:         fixedTime,
			},
		},
		{
			name: "QuoteInvitationFailed",
			data: QuoteInvitationFailed{
				QuoteRequestID: fixedUUID,
				OrganizationID: fixedUUID,
				LastError:      "connection timeout",
				At:             fixedTime,
			},
		},
		{
			name: "QuoteReceived",
			data: QuoteReceived{
				QuoteRequestID: fixedUUID,
				OrganizationID: fixedUUID,
				Quote:          quote,
			},
		},
		{
			name: "QuoteDeclined",
			data: QuoteDeclined{
				QuoteRequestID: fixedUUID,
				OrganizationID: fixedUUID,
				Reason:         "Fuera de zona de entrega",
				At:             fixedTime,
			},
		},
		{
			name: "QuoteSelected",
			data: QuoteSelected{
				QuoteRequestID: fixedUUID,
				QuoteID:        fixedUUID,
				OrganizationID: fixedUUID,
				SubstitutionDecisions: []SubstitutionDecision{
					{ItemID: "item-001", Accept: true},
				},
				At: fixedTime,
			},
		},
		{
			name: "QuoteRequestCancelled",
			data: QuoteRequestCancelled{
				QuoteRequestID: fixedUUID,
				Reason:         "Ya no requiero el medicamento",
				At:             fixedTime,
			},
		},
		{
			name: "QuoteRequestExpired",
			data: QuoteRequestExpired{
				QuoteRequestID:  fixedUUID,
				SelectableUntil: ptrTime(fixedTime.Add(24 * time.Hour)),
				At:              fixedTime,
			},
		},
		{
			name: "QuoteRequestClosed",
			data: QuoteRequestClosed{
				QuoteRequestID: fixedUUID,
				OrderID:        &fixedUUID,
				Reason:         "Pedido adjudicado exitosamente",
				At:             fixedTime,
			},
		},
		{
			name: "QuoteRequestReopened",
			data: QuoteRequestReopened{
				QuoteRequestID: fixedUUID,
				Reason:         "Pedido previo rechazado por farmacia",
				At:             fixedTime,
			},
		},
		{
			name: "OrderPlaced",
			data: OrderPlaced{
				OrderID:        fixedUUID,
				QuoteRequestID: fixedUUID,
				QuoteID:        fixedUUID,
				PrescriptionID: fixedUUID,
				PetID:          fixedUUID,
				GuardianID:     fixedUUID,
				PharmacyOrgID:  fixedUUID,
				Mode:           ModeDelivery,
				Lines: []OrderLine{
					{
						ItemID:          "item-001",
						CatalogID:       &fixedUUID,
						Qty:             2,
						UnitPrice:       14000,
						Substituted:     &fixedUUID,
						SubstitutedName: ptrString("Amoxicilina Bioequivalente"),
					},
				},
				Totals: OrderTotals{
					Subtotal:    28000,
					DeliveryFee: 5000,
					Total:       33000,
					Currency:    "COP",
				},
				DeliveryAddress: &Address{
					Street:     "Calle 100 # 15-20",
					City:       "Bogota",
					Country:    "CO",
					PostalCode: "110111",
				},
				ContactPhone: "+573001234567",
				At:           fixedTime,
			},
		},
		{
			name: "OrderConfirmed",
			data: OrderConfirmed{
				OrderID:    fixedUUID,
				PartnerRef: ptrString("EXT-ORD-999"),
				ETA:        ptrString("45 minutos"),
				At:         fixedTime,
			},
		},
		{
			name: "OrderRejected",
			data: OrderRejected{
				OrderID: fixedUUID,
				Reason:  "Sin inventario disponible",
				At:      fixedTime,
			},
		},
		{
			name: "OrderPreparing",
			data: OrderPreparing{
				OrderID: fixedUUID,
				At:      fixedTime,
			},
		},
		{
			name: "OrderReady",
			data: OrderReady{
				OrderID: fixedUUID,
				At:      fixedTime,
			},
		},
		{
			name: "OrderOutForDelivery",
			data: OrderOutForDelivery{
				OrderID: fixedUUID,
				ETA:     ptrString("15 minutos"),
				At:      fixedTime,
			},
		},
		{
			name: "OrderDelivered",
			data: OrderDelivered{
				OrderID: fixedUUID,
				Dispensation: DispensationRecord{
					Lines: []DispensationLine{{ItemID: "item-001", Qty: 2}},
					At:    fixedTime,
				},
				At: fixedTime,
			},
		},
		{
			name: "OrderCancelled",
			data: OrderCancelled{
				OrderID: fixedUUID,
				By:      "guardian",
				Reason:  "Cancelado a tiempo",
				At:      fixedTime,
			},
		},
		{
			name: "OrderRated",
			data: OrderRated{
				OrderID:       fixedUUID,
				PharmacyOrgID: fixedUUID,
				GuardianID:    fixedUUID,
				Score:         5,
				Comment:       "Excelente atención y despacho rápido",
				At:            fixedTime,
			},
		},
	}

	goldenDir := filepath.Clean(filepath.Join("testdata", "golden"))
	if *update {
		if err := os.MkdirAll(goldenDir, 0750); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
	}

	for _, tt := range events {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := json.MarshalIndent(tt.data, "", "  ")
			if err != nil {
				t.Fatalf("marshal %s: %v", tt.name, err)
			}
			gotJSON = append(gotJSON, '\n')

			filePath := filepath.Clean(filepath.Join(goldenDir, fmt.Sprintf("%s.json", tt.name)))
			if *update {
				if err := os.WriteFile(filePath, gotJSON, 0600); err != nil {
					t.Fatalf("write golden %s: %v", tt.name, err)
				}
				return
			}

			wantJSON, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("read golden %s: %v (ejecuta go test -update para generar)", tt.name, err)
			}

			if string(gotJSON) != string(wantJSON) {
				t.Errorf("mismatch en %s (-want +got):\nwant: %s\ngot:  %s", tt.name, string(wantJSON), string(gotJSON))
			}
		})
	}
}

func ptrString(s string) *string     { return &s }
func ptrFloat(f float64) *float64    { return &f }
func ptrTime(t time.Time) *time.Time { return &t }

func TestEvents_EventNameAndSchemaVersion(t *testing.T) {
	tests := []struct {
		evt interface {
			EventName() string
			SchemaVersion() int
		}
		expectedName string
	}{
		{QuoteRequested{}, "QuoteRequested"},
		{QuoteInvitationSent{}, "QuoteInvitationSent"},
		{QuoteInvitationFailed{}, "QuoteInvitationFailed"},
		{QuoteReceived{}, "QuoteReceived"},
		{QuoteDeclined{}, "QuoteDeclined"},
		{QuoteSelected{}, "QuoteSelected"},
		{QuoteRequestCancelled{}, "QuoteRequestCancelled"},
		{QuoteRequestExpired{}, "QuoteRequestExpired"},
		{QuoteRequestClosed{}, "QuoteRequestClosed"},
		{QuoteRequestReopened{}, "QuoteRequestReopened"},
		{OrderPlaced{}, "OrderPlaced"},
		{OrderConfirmed{}, "OrderConfirmed"},
		{OrderRejected{}, "OrderRejected"},
		{OrderPreparing{}, "OrderPreparing"},
		{OrderReady{}, "OrderReady"},
		{OrderOutForDelivery{}, "OrderOutForDelivery"},
		{OrderDelivered{}, "OrderDelivered"},
		{OrderCancelled{}, "OrderCancelled"},
		{OrderRated{}, "OrderRated"},
	}

	for _, tc := range tests {
		if tc.evt.EventName() != tc.expectedName {
			t.Errorf("expected EventName %s, got %s", tc.expectedName, tc.evt.EventName())
		}
		if tc.evt.SchemaVersion() != 1 {
			t.Errorf("expected SchemaVersion 1, got %d", tc.evt.SchemaVersion())
		}
	}
}
