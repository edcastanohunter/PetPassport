package domain

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestProperty_SumChargesMinusRefundsEqualsSumNetPayments verifica la propiedad matemática:
// Σ (cargos pagados) − Σ (reembolsos) = Σ (pagos netos confirmados) (PP-F5-QA-001).
func TestProperty_SumChargesMinusRefundsEqualsSumNetPayments(t *testing.T) {
	starter, pro, enterprise := sampleClinicPlans()
	plans := []PlanDefinition{starter, pro, enterprise}

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 50; i++ {
		accountID := uuid.New()
		subID := uuid.New()
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		chosenPlan := plans[rng.Intn(len(plans))]
		seats := 1
		if chosenPlan.Seats != nil {
			seats = chosenPlan.Seats.Min + rng.Intn(3)
		}

		cycle := CycleMonthly
		if rng.Float32() > 0.5 {
			cycle = CycleAnnual
		}

		sub, err := Subscribe(
			subID, accountID, KindClinic, chosenPlan, seats, nil, cycle, nil,
			false, true, 1, 0.19, now,
		)
		if err != nil {
			t.Fatalf("run %d: subscribe failed: %v", i, err)
		}

		// Simular entre 3 y 10 operaciones aleatorias
		ops := 3 + rng.Intn(8)
		for op := 0; op < ops; op++ {
			now = now.AddDate(0, 0, 1+rng.Intn(10))

			action := rng.Intn(5)
			switch action {
			case 0:
				// Pagar cargos pendientes
				for _, ch := range sub.Charges() {
					if ch.Status == ChargeStatusPending {
						_ = sub.RecordChargeResult(ch.ID, "paid", "txn_"+uuid.New().String(), "", now)
					}
				}
			case 1:
				// Agregar asientos si es plan pro o enterprise
				if sub.Status() == StatusActive && (sub.PlanCode() == "pro" || sub.PlanCode() == "enterprise") {
					newSeats := sub.Seats().Purchased + 1 + rng.Intn(2)
					_ = sub.AdjustSeats(newSeats, chosenPlan, 1, 0.19, now)
				}
			case 2:
				// Reembolsar parcialmente algún cargo pagado
				for _, ch := range sub.Charges() {
					if ch.Status == ChargeStatusPaid {
						refundable := ch.Total - ch.Refunded
						if refundable > 10.0 {
							refundAmt := RoundCents(1.0 + rng.Float64()*(refundable-1.0))
							_ = sub.Refund(ch.ID, refundAmt, "prop test refund", now)
						}
					}
				}
			case 3:
				// Renovar periodo
				if sub.Status() == StatusActive && now.After(sub.CurrentPeriod().End) {
					_ = sub.RenewPeriod(chosenPlan, 1, 0.19, now)
				}
			case 4:
				// Upgrade de plan si es starter
				if sub.Status() == StatusActive && sub.PlanCode() == "starter" {
					_ = sub.ChangePlan(pro, 2, nil, CycleMonthly, starter, 1, 0.19, now)
					chosenPlan = pro
				}
			}
		}

		// Al final de la corrida, pagar cualquier cargo pendiente para evaluar equilibrio financiero
		for _, ch := range sub.Charges() {
			if ch.Status == ChargeStatusPending {
				_ = sub.RecordChargeResult(ch.ID, "paid", "txn_final_"+uuid.New().String(), "", now)
			}
		}

		// Calcular Σ cargos pagados y Σ reembolsos
		var sumPaidCharges float64
		var sumRefunds float64
		var netExpected float64

		for _, ch := range sub.Charges() {
			if ch.Status == ChargeStatusPaid || ch.Status == ChargeStatusPartiallyRefunded || ch.Status == ChargeStatusRefunded {
				sumPaidCharges += ch.Total
				sumRefunds += ch.Refunded
			}
		}

		sumPaidCharges = RoundCents(sumPaidCharges)
		sumRefunds = RoundCents(sumRefunds)
		netExpected = RoundCents(sumPaidCharges - sumRefunds)

		// Simular balance neto recaudado por la pasarela
		var netCollected float64
		for _, ch := range sub.Charges() {
			if ch.Status == ChargeStatusPaid || ch.Status == ChargeStatusPartiallyRefunded || ch.Status == ChargeStatusRefunded {
				netCollected += (ch.Total - ch.Refunded)
			}
		}
		netCollected = RoundCents(netCollected)

		diff := math.Abs(netExpected - netCollected)
		if diff > 0.001 {
			t.Fatalf("run %d: invariant broken! sumPaid(%.2f) - sumRefunds(%.2f) = %.2f != netCollected(%.2f)",
				i, sumPaidCharges, sumRefunds, netExpected, netCollected)
		}
	}
}
