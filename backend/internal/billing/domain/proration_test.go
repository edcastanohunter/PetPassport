package domain

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestProration_CentPrecisionTable verifica la tabla exhaustiva de casos de prorrateo calculados al céntimo (QA-001).
func TestProration_CentPrecisionTable(t *testing.T) {
	starter, pro, enterprise := sampleClinicPlans()

	tests := []struct {
		name                 string
		initialPlan          PlanDefinition
		initialSeats         int
		initialCycle         BillingCycle
		newPlan              PlanDefinition
		newSeats             int
		newCycle             BillingCycle
		periodStart          time.Time
		changeTime           time.Time
		taxRate              float64
		expectedProratedAmt  float64
		expectedProratedTax  float64
		expectedProratedTot  float64
		isDowngradeScheduled bool
	}{
		{
			name:         "Upgrade starter (120k) a pro 3 asientos (225k) a mitad de mes (15/30 días restantes)",
			initialPlan:  starter,
			initialSeats: 1,
			initialCycle: CycleMonthly,
			newPlan:      pro,
			newSeats:     3,
			newCycle:     CycleMonthly,
			periodStart:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			changeTime:   time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), // 15 días restantes de 30
			taxRate:      0.19,
			// diff = 225,000 - 120,000 = 105,000
			// prorated = (15/30) * 105,000 = 52,500.00
			// tax = 52,500 * 0.19 = 9,975.00
			// total = 62,475.00
			expectedProratedAmt:  52500.00,
			expectedProratedTax:  9975.00,
			expectedProratedTot:  62475.00,
			isDowngradeScheduled: false,
		},
		{
			name:         "Upgrade starter (120k) a pro 2 asientos (150k) con 10/30 días restantes",
			initialPlan:  starter,
			initialSeats: 1,
			initialCycle: CycleMonthly,
			newPlan:      pro,
			newSeats:     2,
			newCycle:     CycleMonthly,
			periodStart:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			changeTime:   time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC), // 10 días restantes de 30
			taxRate:      0.19,
			// diff = 150,000 - 120,000 = 30,000
			// prorated = (10/30) * 30,000 = 10,000.00
			// tax = 10,000 * 0.19 = 1,900.00
			// total = 11,900.00
			expectedProratedAmt:  10000.00,
			expectedProratedTax:  1900.00,
			expectedProratedTot:  11900.00,
			isDowngradeScheduled: false,
		},
		{
			name:         "Upgrade pro 2 asientos (150k) a enterprise 5 asientos (750k) con 7/31 días restantes",
			initialPlan:  pro,
			initialSeats: 2,
			initialCycle: CycleMonthly,
			newPlan:      enterprise,
			newSeats:     5,
			newCycle:     CycleMonthly,
			periodStart:  time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			changeTime:   time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), // 7 días restantes de 31
			taxRate:      0.19,
			// diff = 750,000 - 150,000 = 600,000
			// prorated = (7 / 31) * 600,000 = 135483.870967... -> 135,483.87
			// tax = 135,483.87 * 0.19 = 25741.9353... -> 25,741.94
			// total = 135,483.87 + 25,741.94 = 161,225.81
			expectedProratedAmt:  135483.87,
			expectedProratedTax:  25741.94,
			expectedProratedTot:  161225.81,
			isDowngradeScheduled: false,
		},
		{
			name:         "Upgrade a ciclo anual con 20/30 días restantes",
			initialPlan:  pro,
			initialSeats: 2,
			initialCycle: CycleMonthly,
			newPlan:      pro,
			newSeats:     2,
			newCycle:     CycleAnnual,
			periodStart:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			changeTime:   time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), // 20 días restantes de 30
			taxRate:      0.19,
			// Old = 150,000 (monthly)
			// New = 1,500,000 (annual)
			// diff = 1,350,000
			// prorated = (20/30) * 1,350,000 = 900,000.00
			// tax = 900,000 * 0.19 = 171,000.00
			// total = 1,071,000.00
			expectedProratedAmt:  900000.00,
			expectedProratedTax:  171000.00,
			expectedProratedTot:  1071000.00,
			isDowngradeScheduled: false,
		},
		{
			name:                 "Downgrade pro 3 asientos a starter no genera cobro inmediato (programado al fin de ciclo)",
			initialPlan:          pro,
			initialSeats:         3,
			initialCycle:         CycleMonthly,
			newPlan:              starter,
			newSeats:             1,
			newCycle:             CycleMonthly,
			periodStart:          time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			changeTime:           time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
			taxRate:              0.19,
			expectedProratedAmt:  0,
			expectedProratedTax:  0,
			expectedProratedTot:  0,
			isDowngradeScheduled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := Subscribe(
				uuid.New(), uuid.New(), KindClinic, tc.initialPlan, tc.initialSeats, nil, tc.initialCycle, nil,
				false, true, 1, tc.taxRate, tc.periodStart,
			)
			if err != nil {
				t.Fatalf("subscribe error: %v", err)
			}

			// Simular pago del cargo inicial
			var initChargeID uuid.UUID
			for id := range sub.Charges() {
				initChargeID = id
			}
			_ = sub.RecordChargeResult(initChargeID, "paid", "txn_001", "", tc.periodStart)

			err = sub.ChangePlan(tc.newPlan, tc.newSeats, nil, tc.newCycle, tc.initialPlan, 1, tc.taxRate, tc.changeTime)
			if err != nil {
				t.Fatalf("change plan error: %v", err)
			}

			if tc.isDowngradeScheduled {
				if sub.PendingChange() == nil {
					t.Fatal("expected pendingChange to be scheduled for downgrade")
				}
				if len(sub.Charges()) != 1 {
					t.Fatalf("expected no additional charge for downgrade, got %d", len(sub.Charges()))
				}
				return
			}

			// Verificar el cargo de prorrateo
			var prorationCharge *Charge
			for _, ch := range sub.Charges() {
				if ch.ID != initChargeID {
					c := ch
					prorationCharge = &c
					break
				}
			}

			if prorationCharge == nil {
				t.Fatal("expected proration charge scheduled")
			}

			diffAmt := math.Abs(prorationCharge.Amount - tc.expectedProratedAmt)
			if diffAmt > 0.001 {
				t.Errorf("amount mismatch: want %.2f, got %.2f", tc.expectedProratedAmt, prorationCharge.Amount)
			}
			diffTax := math.Abs(prorationCharge.Tax - tc.expectedProratedTax)
			if diffTax > 0.001 {
				t.Errorf("tax mismatch: want %.2f, got %.2f", tc.expectedProratedTax, prorationCharge.Tax)
			}
			diffTot := math.Abs(prorationCharge.Total - tc.expectedProratedTot)
			if diffTot > 0.001 {
				t.Errorf("total mismatch: want %.2f, got %.2f", tc.expectedProratedTot, prorationCharge.Total)
			}
		})
	}
}

// TestProration_SeatAdditionCents verifica el prorrateo al céntimo al incrementar asientos a mitad de periodo.
func TestProration_SeatAdditionCents(t *testing.T) {
	_, pro, _ := sampleClinicPlans()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	sub, _ := Subscribe(
		uuid.New(), uuid.New(), KindClinic, pro, 2, nil, CycleMonthly, nil,
		false, true, 1, 0.19, start,
	)

	// Agregar 1 asiento extra el 11 de noviembre (20 de 30 días restantes)
	// Costo mensual del asiento: 75,000
	// Prorrateo = (20 / 30) * 75,000 = 50,000.00 COP
	// Tax = 50,000 * 0.19 = 9,500.00 COP
	// Total = 59,500.00 COP
	changeDate := start.AddDate(0, 0, 10)
	err := sub.AdjustSeats(3, pro, 1, 0.19, changeDate)
	if err != nil {
		t.Fatalf("adjust seats error: %v", err)
	}

	var seatCharge *Charge
	for _, ch := range sub.Charges() {
		if ch.Lines[0].Quantity == 1 && ch.Amount == 50000.00 {
			c := ch
			seatCharge = &c
			break
		}
	}

	if seatCharge == nil {
		t.Fatal("expected seat adjustment proration charge")
	}
	if seatCharge.Amount != 50000.00 || seatCharge.Tax != 9500.00 || seatCharge.Total != 59500.00 {
		t.Fatalf("unexpected seat charge breakdown: amount=%.2f, tax=%.2f, total=%.2f",
			seatCharge.Amount, seatCharge.Tax, seatCharge.Total)
	}
}
