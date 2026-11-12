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
	fixedTime := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	fixedPeriod := Period{
		Start: fixedTime,
		End:   fixedTime.AddDate(0, 1, 0),
	}

	legalEntity := LegalEntity{
		Kind:      "company",
		LegalName: "Clínica Veterinaria San Martín S.A.S.",
		TaxID: TaxID{
			Country: "CO",
			Value:   "901234567-1",
			Kind:    "NIT",
		},
		Address: "Calle 100 # 15-20, Bogotá",
		Email:   "facturacion@sanmartin.co",
	}

	pm := PaymentMethodRef{
		Provider: "wompi",
		Token:    "tok_test_12345",
		Kind:     "card",
	}

	events := []struct {
		name string
		data any
	}{
		{
			name: "BillingAccountOpened",
			data: BillingAccountOpened{
				AccountID:   fixedUUID,
				OwnerUserID: fixedUUID,
				LegalEntity: legalEntity,
				Country:     "CO",
				Currency:    "COP",
				At:          fixedTime,
			},
		},
		{
			name: "LegalEntityUpdated",
			data: LegalEntityUpdated{
				AccountID:   fixedUUID,
				LegalEntity: legalEntity,
				At:          fixedTime,
			},
		},
		{
			name: "OrganizationAttached",
			data: OrganizationAttached{
				AccountID:      fixedUUID,
				OrganizationID: fixedUUID,
				SeatPool:       "own",
				At:             fixedTime,
			},
		},
		{
			name: "OrganizationDetached",
			data: OrganizationDetached{
				AccountID:      fixedUUID,
				OrganizationID: fixedUUID,
				At:             fixedTime,
			},
		},
		{
			name: "PaymentMethodSet",
			data: PaymentMethodSet{
				AccountID:     fixedUUID,
				PaymentMethod: pm,
				At:            fixedTime,
			},
		},
		{
			name: "BillingAccountClosed",
			data: BillingAccountClosed{
				AccountID: fixedUUID,
				At:        fixedTime,
			},
		},
		{
			name: "TrialStarted",
			data: TrialStarted{
				SubscriptionID: fixedUUID,
				AccountID:      fixedUUID,
				Kind:           KindClinic,
				PlanCode:       "pro",
				PlanVersion:    1,
				TrialEndsAt:    fixedTime.AddDate(0, 0, 30),
				CurrentPeriod:  fixedPeriod,
				At:             fixedTime,
			},
		},
		{
			name: "Subscribed",
			data: Subscribed{
				SubscriptionID: fixedUUID,
				AccountID:      fixedUUID,
				Kind:           KindClinic,
				PlanCode:       "pro",
				PlanVersion:    1,
				Seats:          &Seats{Purchased: 3},
				Cycle:          CycleMonthly,
				CurrentPeriod:  fixedPeriod,
				ManualBilling:  false,
				At:             fixedTime,
			},
		},
		{
			name: "PlanChangeApplied",
			data: PlanChangeApplied{
				SubscriptionID: fixedUUID,
				PlanCode:       "enterprise",
				PlanVersion:    1,
				Seats:          &Seats{Purchased: 5},
				Cycle:          CycleMonthly,
				At:             fixedTime,
			},
		},
		{
			name: "PlanChangeScheduled",
			data: PlanChangeScheduled{
				SubscriptionID: fixedUUID,
				PendingChange: PlanChange{
					PlanCode:    "starter",
					PlanVersion: 1,
					Cycle:       CycleMonthly,
					ScheduledAt: fixedPeriod.End,
				},
				At: fixedTime,
			},
		},
		{
			name: "SeatsAdjusted",
			data: SeatsAdjusted{
				SubscriptionID: fixedUUID,
				Seats:          Seats{Purchased: 4},
				Delta:          1,
				At:             fixedTime,
			},
		},
		{
			name: "PeriodRenewed",
			data: PeriodRenewed{
				SubscriptionID: fixedUUID,
				PlanCode:       "pro",
				PlanVersion:    1,
				CurrentPeriod:  fixedPeriod,
				At:             fixedTime,
			},
		},
		{
			name: "ChargeScheduled",
			data: ChargeScheduled{
				ChargeID:       fixedUUID,
				SubscriptionID: fixedUUID,
				AccountID:      fixedUUID,
				Reason:         "Suscripción pro (monthly)",
				Amount:         225000,
				Tax:            42750,
				Total:          267750,
				Currency:       "COP",
				Lines: []ChargeLine{
					{
						Description: "Plan pro monthly x3 asientos",
						Amount:      225000,
						Quantity:    3,
					},
				},
				Period:      &fixedPeriod,
				ScheduledAt: fixedTime,
			},
		},
		{
			name: "ChargePaid",
			data: ChargePaid{
				SubscriptionID: fixedUUID,
				ChargeID:       fixedUUID,
				Amount:         267750,
				PaymentRef:     "pay_wompi_001",
				Manual:         false,
				PaidAt:         fixedTime,
			},
		},
		{
			name: "ChargeFailed",
			data: ChargeFailed{
				SubscriptionID: fixedUUID,
				ChargeID:       fixedUUID,
				ErrorCode:      "INSUFFICIENT_FUNDS",
				FailedAt:       fixedTime,
			},
		},
		{
			name: "ChargeRefunded",
			data: ChargeRefunded{
				SubscriptionID: fixedUUID,
				ChargeID:       fixedUUID,
				Amount:         50000,
				Reason:         "Ajuste comercial",
				RefundedAt:     fixedTime,
				RefundedTotal:  50000,
			},
		},
		{
			name: "SubscriptionGraceStarted",
			data: SubscriptionGraceStarted{
				SubscriptionID: fixedUUID,
				GraceEndsAt:    fixedTime.AddDate(0, 0, 14),
				At:             fixedTime,
			},
		},
		{
			name: "SubscriptionRestricted",
			data: SubscriptionRestricted{
				SubscriptionID: fixedUUID,
				Reason:         "Impago tras periodo de gracia",
				At:             fixedTime,
			},
		},
		{
			name: "SubscriptionReactivated",
			data: SubscriptionReactivated{
				SubscriptionID: fixedUUID,
				At:             fixedTime,
			},
		},
		{
			name: "SubscriptionCancellationScheduled",
			data: SubscriptionCancellationScheduled{
				SubscriptionID: fixedUUID,
				EffectiveAt:    fixedPeriod.End,
				Reason:         "Cancelación solicitada por el usuario",
				At:             fixedTime,
			},
		},
		{
			name: "SubscriptionCancelled",
			data: SubscriptionCancelled{
				SubscriptionID: fixedUUID,
				Reason:         "Cancelación inmediata",
				At:             fixedTime,
			},
		},
		{
			name: "SubscriptionResumed",
			data: SubscriptionResumed{
				SubscriptionID: fixedUUID,
				At:             fixedTime,
			},
		},
		{
			name: "TrialExpired",
			data: TrialExpired{
				SubscriptionID: fixedUUID,
				GraceEndsAt:    fixedTime.AddDate(0, 0, 7),
				At:             fixedTime,
			},
		},
		{
			name: "AdminAdjustmentApplied",
			data: AdminAdjustmentApplied{
				SubscriptionID: fixedUUID,
				Kind:           "extend_trial",
				Details:        "Extensión por 15 días",
				Reason:         "Solicitud de soporte",
				At:             fixedTime,
			},
		},
		{
			name: "CouponAppliedEvent",
			data: CouponAppliedEvent{
				SubscriptionID: fixedUUID,
				Coupon: CouponApplied{
					Code:  "BIENVENIDA20",
					Kind:  "pct",
					Value: 20,
				},
				At: fixedTime,
			},
		},
	}

	goldenDir := filepath.Join("testdata")
	if *update {
		_ = os.MkdirAll(goldenDir, 0o750)
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

func TestEvents_EventNameAndSchemaVersion(t *testing.T) {
	events := []interface {
		EventName() string
		SchemaVersion() int
	}{
		BillingAccountOpened{},
		LegalEntityUpdated{},
		OrganizationAttached{},
		OrganizationDetached{},
		PaymentMethodSet{},
		BillingAccountClosed{},
		TrialStarted{},
		Subscribed{},
		PlanChangeApplied{},
		PlanChangeScheduled{},
		SeatsAdjusted{},
		PeriodRenewed{},
		ChargeScheduled{},
		ChargePaid{},
		ChargeFailed{},
		ChargeRefunded{},
		SubscriptionGraceStarted{},
		SubscriptionRestricted{},
		SubscriptionReactivated{},
		SubscriptionCancellationScheduled{},
		SubscriptionCancelled{},
		SubscriptionResumed{},
		TrialExpired{},
		AdminAdjustmentApplied{},
		CouponAppliedEvent{},
	}

	for _, e := range events {
		if e.EventName() == "" {
			t.Errorf("EventName vacío para %T", e)
		}
		if e.SchemaVersion() != 1 {
			t.Errorf("SchemaVersion debe ser 1 para %T, obtenido %d", e, e.SchemaVersion())
		}
	}
}
