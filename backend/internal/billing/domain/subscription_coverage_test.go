package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSubscription_ExtendedCoverage(t *testing.T) {
	starter, pro, enterprise := sampleClinicPlans()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	subID := uuid.New()
	accountID := uuid.New()

	sub, err := StartTrial(subID, accountID, KindClinic, pro, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test getters
	if sub.ID() != subID || sub.Version() != 1 || sub.Tier() != nil || sub.Cycle() != "" {
		t.Fatalf("getter mismatch")
	}
	if sub.TrialEndsAt() == nil || sub.Coupon() != nil {
		t.Fatalf("getter mismatch on trial/coupon")
	}
	changes := sub.Changes()
	if len(changes) != 1 {
		t.Fatalf("expected 1 uncommitted event, got %d", len(changes))
	}

	// Start trial with 0 days error
	starterZeroTrial := starter
	starterZeroTrial.TrialDays = 0
	if _, err := StartTrial(uuid.New(), accountID, KindClinic, starterZeroTrial, now); err != ErrTrialNotAllowed {
		t.Fatalf("expected ErrTrialNotAllowed, got %v", err)
	}

	// Convert trial invalid state
	activeSub, _ := Subscribe(uuid.New(), accountID, KindClinic, pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, now)
	if err := activeSub.ConvertTrialToSubscription(pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, now); err != ErrInvalidCycleTransition {
		t.Fatalf("expected ErrInvalidCycleTransition, got %v", err)
	}

	// Convert trial without payment method
	if err := sub.ConvertTrialToSubscription(pro, 2, nil, CycleMonthly, nil, false, false, 1, 0.19, now); err != ErrPaymentMethodRequired {
		t.Fatalf("expected ErrPaymentMethodRequired, got %v", err)
	}

	// Convert trial seats below active
	if err := sub.ConvertTrialToSubscription(pro, 1, nil, CycleMonthly, nil, false, true, 2, 0.19, now); err != ErrSeatsBelowUsage {
		t.Fatalf("expected ErrSeatsBelowUsage, got %v", err)
	}

	// Convert trial successfully
	coupon := &CouponApplied{Code: "PROMO", Kind: "pct", Value: 10}
	if err := sub.ConvertTrialToSubscription(pro, 2, nil, CycleMonthly, coupon, false, true, 1, 0.19, now); err != nil {
		t.Fatalf("failed to convert trial: %v", err)
	}

	// Convert trial with fixed amount coupon
	trialSub2, _ := StartTrial(uuid.New(), accountID, KindClinic, pro, now)
	couponAmt := &CouponApplied{Code: "FIXED", Kind: "amount", Value: 20000}
	if err := trialSub2.ConvertTrialToSubscription(pro, 2, nil, CycleMonthly, couponAmt, false, true, 1, 0.19, now); err != nil {
		t.Fatalf("failed to convert trial with amount coupon: %v", err)
	}

	// Refund with amount <= 0
	var refundChID uuid.UUID
	for id := range trialSub2.Charges() {
		refundChID = id
		break
	}
	_ = trialSub2.RecordChargeResult(refundChID, "paid", "txn_amt", "", now)
	if err := trialSub2.Refund(refundChID, 0, "cero", now); err != ErrRefundExceeds {
		t.Fatalf("expected ErrRefundExceeds on refund <= 0, got %v", err)
	}

	// RenewPeriod with pendingChange.Seats < activeProfessionals
	activeSub2, _ := Subscribe(uuid.New(), accountID, KindClinic, pro, 3, nil, CycleMonthly, nil, false, true, 1, 0.19, now)
	_ = activeSub2.ChangePlan(starter, 1, nil, CycleMonthly, pro, 1, 0.19, now) // Schedules downgrade to 1 seat
	if err := activeSub2.RenewPeriod(starter, 2, 0.19, now.AddDate(0, 1, 0)); err != ErrSeatsBelowUsage {
		t.Fatalf("expected ErrSeatsBelowUsage on renew with pendingChange seats < active, got %v", err)
	}

	// Error cases in ChangePlan and RenewPeriod
	invalidPlan := PlanDefinition{Pricing: PlanPricing{Model: "invalid"}}
	if err := activeSub2.ChangePlan(invalidPlan, 1, nil, CycleMonthly, starter, 0, 0.19, now); err == nil {
		t.Fatal("expected error on invalid new plan")
	}
	if err := activeSub2.ChangePlan(starter, 1, nil, CycleMonthly, invalidPlan, 0, 0.19, now); err == nil {
		t.Fatal("expected error on invalid current plan")
	}
	activeSub3, _ := Subscribe(uuid.New(), accountID, KindClinic, pro, 3, nil, CycleMonthly, nil, false, true, 1, 0.19, now)
	if err := activeSub3.ChangePlan(starter, 0, nil, CycleMonthly, starter, 1, 0.19, now); err != ErrSeatsBelowUsage {
		t.Fatalf("expected ErrSeatsBelowUsage on change plan, got %v", err)
	}
	if err := activeSub2.RenewPeriod(invalidPlan, 0, 0.19, now.AddDate(0, 1, 0)); err == nil {
		t.Fatal("expected error on renew with invalid plan")
	}

	// AttachOrganization with nil org and empty seatPool in account
	testAcc := NewBillingAccount()
	if err := testAcc.AttachOrganization(uuid.Nil, "", now); err != ErrOrgNotAttached {
		t.Fatalf("expected ErrOrgNotAttached, got %v", err)
	}
	if err := testAcc.AttachOrganization(uuid.New(), "", now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Direct EnterGrace & Reactivate
	if err := sub.EnterGrace(now.AddDate(0, 0, 14), now); err != nil {
		t.Fatalf("enter grace error: %v", err)
	}
	if sub.Status() != StatusGrace {
		t.Fatalf("expected status grace, got %s", sub.Status())
	}
	if err := sub.Reactivate(now); err != nil {
		t.Fatalf("reactivate error: %v", err)
	}
	if sub.Status() != StatusActive {
		t.Fatalf("expected status active, got %s", sub.Status())
	}
	// Calling Reactivate on already active is no-op
	if err := sub.Reactivate(now); err != nil {
		t.Fatalf("no-op reactivate error: %v", err)
	}
	// Calling EnterGrace on already grace is no-op
	_ = sub.EnterGrace(now.AddDate(0, 0, 14), now)
	_ = sub.EnterGrace(now.AddDate(0, 0, 14), now)

	// Admin Adjustment
	if err := sub.ApplyAdminAdjustment("credit", "10000 COP", "Cortesía", now); err != nil {
		t.Fatalf("admin adjustment error: %v", err)
	}

	// Record Manual Payment
	var chID uuid.UUID
	for id, ch := range sub.Charges() {
		if ch.Status == ChargeStatusPending {
			chID = id
			break
		}
	}
	// Non-existent charge manual payment
	if err := sub.RecordManualPayment(uuid.New(), 100, "ref", now); err == nil {
		t.Fatal("expected error on non-existent charge manual payment")
	}
	// Valid manual payment
	if err := sub.RecordManualPayment(chID, 100000, "TRANSF-001", now); err != nil {
		t.Fatalf("manual payment error: %v", err)
	}
	// Calling again should fail because not pending
	if err := sub.RecordManualPayment(chID, 100000, "TRANSF-001", now); err != ErrChargeNotPending {
		t.Fatalf("expected ErrChargeNotPending, got %v", err)
	}

	// Record charge result on non-existent charge
	if err := sub.RecordChargeResult(uuid.New(), "paid", "ref", "", now); err == nil {
		t.Fatal("expected error on non-existent charge result")
	}

	// Adjust seats error cases
	pharma := samplePharmacyPlan()
	pharmaSub, _ := Subscribe(uuid.New(), accountID, KindPharmacy, pharma, 0, nil, CycleMonthly, nil, true, false, 0, 0.19, now)
	if err := pharmaSub.AdjustSeats(2, pharma, 0, 0.19, now); err == nil {
		t.Fatal("expected error adjusting seats on pharmacy plan")
	}

	restrictedSub, _ := Subscribe(uuid.New(), accountID, KindClinic, pro, 2, nil, CycleMonthly, nil, false, true, 1, 0.19, now)
	_ = restrictedSub.Restrict("test", now)
	if err := restrictedSub.AdjustSeats(3, pro, 1, 0.19, now); err != ErrSubscriptionRestricted {
		t.Fatalf("expected ErrSubscriptionRestricted, got %v", err)
	}
	if err := restrictedSub.ChangePlan(enterprise, 5, nil, CycleMonthly, pro, 1, 0.19, now); err != ErrSubscriptionRestricted {
		t.Fatalf("expected ErrSubscriptionRestricted on change plan, got %v", err)
	}

	// Seats range constraints in AdjustSeats
	proRange := pro
	proRange.Seats = &PlanSeatsRange{Min: 2, Max: ptrInt(4)}
	if err := activeSub.AdjustSeats(1, proRange, 1, 0.19, now); err == nil {
		t.Fatal("expected error when newSeats < min")
	}
	if err := activeSub.AdjustSeats(5, proRange, 1, 0.19, now); err == nil {
		t.Fatal("expected error when newSeats > max")
	}

	// Pending change error in ChangePlan
	_ = activeSub.ChangePlan(starter, 1, nil, CycleMonthly, pro, 1, 0.19, now) // Downgrade schedules pending
	if err := activeSub.ChangePlan(enterprise, 5, nil, CycleMonthly, pro, 1, 0.19, now); err != ErrPlanChangePending {
		t.Fatalf("expected ErrPlanChangePending, got %v", err)
	}

	// Cancel and Resume edge cases
	if err := activeSub.Cancel(false, "immediate", now); err != nil {
		t.Fatalf("immediate cancel error: %v", err)
	}
	// Calling cancel on cancelled is no-op
	if err := activeSub.Cancel(false, "again", now); err != nil {
		t.Fatalf("no-op cancel error: %v", err)
	}
	// Calling resume when not cancelAtPeriodEnd is no-op
	if err := activeSub.Resume(now); err != nil {
		t.Fatalf("no-op resume error: %v", err)
	}
	// Renew on cancelled sub fails
	if err := activeSub.RenewPeriod(pro, 1, 0.19, now); err != ErrInvalidCycleTransition {
		t.Fatalf("expected ErrInvalidCycleTransition on renew cancelled, got %v", err)
	}

	// ExpireTrial on non-trialing is no-op
	if err := activeSub.ExpireTrial(now); err != nil {
		t.Fatalf("no-op expire trial error: %v", err)
	}

	// Test Rehydration with Apply on all remaining events
	rehydratedSub := NewSubscription()
	eventsToApply := []any{
		TrialStarted{SubscriptionID: subID, AccountID: accountID, Kind: KindClinic, PlanCode: "pro", PlanVersion: 1, TrialEndsAt: now.AddDate(0, 0, 30), CurrentPeriod: Period{Start: now, End: now.AddDate(0, 1, 0)}},
		PlanChangeApplied{SubscriptionID: subID, PlanCode: "enterprise", PlanVersion: 1, Seats: &Seats{Purchased: 5}, Cycle: CycleAnnual},
		AdminAdjustmentApplied{SubscriptionID: subID, Kind: "credit", Details: "ok", Reason: "reason", At: now},
		CouponAppliedEvent{SubscriptionID: subID, Coupon: CouponApplied{Code: "C", Kind: "pct", Value: 5}},
	}
	for _, e := range eventsToApply {
		rehydratedSub.Apply(e)
	}
	if rehydratedSub.PlanCode() != "enterprise" || rehydratedSub.Seats().Purchased != 5 || rehydratedSub.Coupon() == nil {
		t.Fatalf("rehydration verification failed")
	}
}
