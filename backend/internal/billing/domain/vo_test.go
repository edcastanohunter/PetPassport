package domain

import (
	"testing"
	"time"
)

func TestMoney_Operations(t *testing.T) {
	m1 := NewMoney(100.50, "COP")
	m2 := NewMoney(50.25, "COP")

	sum, err := m1.Add(m2)
	if err != nil {
		t.Fatalf("unexpected error adding: %v", err)
	}
	if sum.Amount != 150.75 || sum.Currency != "COP" {
		t.Fatalf("unexpected sum: %+v", sum)
	}

	diff, err := m1.Sub(m2)
	if err != nil {
		t.Fatalf("unexpected error subbing: %v", err)
	}
	if diff.Amount != 50.25 || diff.Currency != "COP" {
		t.Fatalf("unexpected diff: %+v", diff)
	}

	// Moneda vacía hereda la otra
	mEmpty := Money{Amount: 10}
	sum2, err := mEmpty.Add(m1)
	if err != nil || sum2.Currency != "COP" {
		t.Fatalf("expected inherited currency: %+v, err: %v", sum2, err)
	}
	diff2, err := mEmpty.Sub(m1)
	if err != nil || diff2.Currency != "COP" {
		t.Fatalf("expected inherited currency on sub: %+v, err: %v", diff2, err)
	}

	// Monedas incompatibles
	mUSD := NewMoney(10, "USD")
	if _, err := m1.Add(mUSD); err == nil {
		t.Fatal("expected error on currency mismatch in Add")
	}
	if _, err := m1.Sub(mUSD); err == nil {
		t.Fatal("expected error on currency mismatch in Sub")
	}
}

func TestPeriod_EdgeCases(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	pZero := Period{Start: start, End: end}
	if pZero.TotalDays() != 1 {
		t.Fatalf("expected min 1 total day, got %d", pZero.TotalDays())
	}

	p := Period{Start: start, End: start.AddDate(0, 0, 30)}
	if p.RemainingDays(start.AddDate(0, 0, 35)) != 0 {
		t.Fatal("expected 0 remaining days after end")
	}
	if p.RemainingDays(start.AddDate(0, 0, -5)) != 30 {
		t.Fatalf("expected total days when before start, got %d", p.RemainingDays(start.AddDate(0, 0, -5)))
	}
}

func TestPlanDefinition_CalculatePrice_EdgeCases(t *testing.T) {
	_, pro, _ := sampleClinicPlans()

	// Min seats error
	if _, err := pro.CalculatePrice(0, "", CycleMonthly); err != nil {
		// Defaults seats <= 0 to 1, which meets min 1
	}

	proMax := pro
	proMax.Seats = &PlanSeatsRange{Min: 2, Max: ptrInt(3)}
	if _, err := proMax.CalculatePrice(1, "", CycleMonthly); err == nil {
		t.Fatal("expected error when seats < min")
	}
	if _, err := proMax.CalculatePrice(4, "", CycleMonthly); err == nil {
		t.Fatal("expected error when seats > max")
	}

	// Unknown model
	invalidModel := PlanDefinition{
		Pricing: PlanPricing{Model: "unknown"},
	}
	if _, err := invalidModel.CalculatePrice(1, "", CycleMonthly); err == nil {
		t.Fatal("expected error for unknown pricing model")
	}

	// Tiered without tiers
	tierEmpty := PlanDefinition{
		Pricing: PlanPricing{
			Model:   "tiered",
			Monthly: 50000,
			Annual:  500000,
		},
	}
	pMonthly, err := tierEmpty.CalculatePrice(1, "none", CycleMonthly)
	if err != nil || pMonthly != 50000 {
		t.Fatalf("expected monthly fallback for tiered: %v, %.2f", err, pMonthly)
	}
	pAnnual, err := tierEmpty.CalculatePrice(1, "none", CycleAnnual)
	if err != nil || pAnnual != 500000 {
		t.Fatalf("expected annual fallback for tiered: %v, %.2f", err, pAnnual)
	}
}

func TestErrors_Formatting(t *testing.T) {
	err := LimitReachedError{
		Quota:      "quotes_per_month",
		Limit:      100,
		Used:       101,
		UpgradeURL: "/upgrade",
	}
	if err.Error() == "" {
		t.Fatal("expected formatted error message")
	}
}
