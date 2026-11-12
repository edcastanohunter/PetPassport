package pm_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/pm"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process/testkit"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

var (
	t0      = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	subID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	charge  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	account = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

// lookup es el read model billing.charges de la prueba.
type lookup map[uuid.UUID]infra.ChargeRef

func (l lookup) Charge(_ context.Context, id uuid.UUID) (*infra.ChargeRef, error) {
	c, ok := l[id]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

func (l lookup) FailedCharges(_ context.Context, acc uuid.UUID) ([]infra.ChargeRef, error) {
	var out []infra.ChargeRef
	for _, c := range l {
		if c.AccountID == acc && c.Status == "failed" {
			out = append(out, c)
		}
	}
	return out, nil
}

func days(n int) time.Time { return t0.AddDate(0, 0, n) }

func TestDunning_PP_F5_ARQ_006_SchedulesFromFirstFailure(t *testing.T) {
	collect := app.CollectCharge{ChargeID: charge}
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewDunning(jobs, lookup{})
	}).At(t0).
		GivenEvents(domain.ChargeFailed{SubscriptionID: subID, ChargeID: charge, ErrorCode: "CARD_DECLINED", FailedAt: t0}).
		ThenScheduled(
			testkit.Scheduled{RunAt: days(3), Command: collect, DedupKey: app.DunningRetryKey(charge, 0)},
			testkit.Scheduled{RunAt: days(7), Command: collect, DedupKey: app.DunningRetryKey(charge, 1)},
			testkit.Scheduled{RunAt: days(12), Command: collect, DedupKey: app.DunningRetryKey(charge, 2)},
			testkit.Scheduled{RunAt: days(14), Command: app.RestrictForNonPayment{SubscriptionID: subID, ChargeID: charge}, DedupKey: app.DunningRestrictKey(charge)},
			testkit.Scheduled{RunAt: days(60), Command: app.CancelForNonPayment{SubscriptionID: subID, ChargeID: charge}, DedupKey: app.DunningCancelKey(charge)},
		)
}

func TestDunning_PP_F5_ARQ_006_RetryFailureKeepsOriginalCalendar(t *testing.T) {
	first := t0
	charges := lookup{charge: {ID: charge, Status: "failed", FirstFailedAt: &first}}
	collect := app.CollectCharge{ChargeID: charge}
	// El reintento de d+3 vuelve a fallar: la restricción sigue en d+14 desde el
	// PRIMER fallo, no se desplaza a d+17.
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewDunning(jobs, charges)
	}).At(days(3)).
		GivenEvents(domain.ChargeFailed{SubscriptionID: subID, ChargeID: charge, ErrorCode: "CARD_DECLINED", FailedAt: days(3)}).
		ThenScheduled(
			testkit.Scheduled{RunAt: days(7), Command: collect, DedupKey: app.DunningRetryKey(charge, 1)},
			testkit.Scheduled{RunAt: days(12), Command: collect, DedupKey: app.DunningRetryKey(charge, 2)},
			testkit.Scheduled{RunAt: days(14), Command: app.RestrictForNonPayment{SubscriptionID: subID, ChargeID: charge}, DedupKey: app.DunningRestrictKey(charge)},
			testkit.Scheduled{RunAt: days(60), Command: app.CancelForNonPayment{SubscriptionID: subID, ChargeID: charge}, DedupKey: app.DunningCancelKey(charge)},
		)
}

func TestDunning_PP_F5_ARQ_006_PaymentCancelsEverything(t *testing.T) {
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewDunning(jobs, lookup{})
	}).At(t0).
		GivenEvents(
			domain.ChargeFailed{SubscriptionID: subID, ChargeID: charge, ErrorCode: "CARD_DECLINED", FailedAt: t0},
			domain.ChargePaid{SubscriptionID: subID, ChargeID: charge, Amount: 178500, PaymentRef: "tx-1", PaidAt: days(1)},
		).
		ThenNothing()
}

func TestChargeCollector_PP_F5_ARQ_002_CollectsEachScheduledCharge(t *testing.T) {
	h := testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewChargeCollector(jobs, lookup{})
	}).At(t0).
		GivenEvents(domain.ChargeScheduled{ChargeID: charge, SubscriptionID: subID, AccountID: account, Total: 178500, Currency: "COP", ScheduledAt: t0})
	h.ThenCommands(app.CollectCharge{ChargeID: charge})
	if jobs := h.Jobs().All(); len(jobs) != 1 || jobs[0].DedupKey != app.CollectKey(charge) || jobs[0].CorrelationID != "corr-test" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestChargeCollector_PP_F5_ARQ_002_FutureChargeWaitsForItsDate(t *testing.T) {
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewChargeCollector(jobs, lookup{})
	}).At(t0).
		GivenEvents(domain.ChargeScheduled{ChargeID: charge, SubscriptionID: subID, AccountID: account, Total: 178500, Currency: "COP", ScheduledAt: days(30)}).
		ThenScheduled(testkit.Scheduled{RunAt: days(30), Command: app.CollectCharge{ChargeID: charge}, DedupKey: app.CollectKey(charge)})
}

func TestChargeCollector_PP_F5_ARQ_006_NewPaymentMethodRetriesFailedCharges(t *testing.T) {
	other := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	charges := lookup{
		charge: {ID: charge, AccountID: account, Status: "failed"},
		other:  {ID: other, AccountID: account, Status: "paid"},
	}
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewChargeCollector(jobs, charges)
	}).At(t0).
		GivenEvents(domain.PaymentMethodSet{AccountID: account, PaymentMethod: domain.PaymentMethodRef{Provider: "fake", Token: "tok", Kind: "card"}, At: t0}).
		ThenCommands(app.CollectCharge{ChargeID: charge})
}

func TestTimers_PP_F5_DOM_012_RenewalAndTrialExpiry(t *testing.T) {
	end := t0.AddDate(0, 1, 0)
	trialEnd := days(30)
	extended := days(45)
	testkit.Process(t, func(jobs process.Jobs, _ kernel.Clock) process.ProcessManager {
		return pm.NewTimers(jobs)
	}).At(t0).
		GivenEvents(
			domain.TrialStarted{SubscriptionID: subID, AccountID: account, TrialEndsAt: trialEnd, At: t0},
			domain.AdminAdjustmentApplied{SubscriptionID: subID, Kind: "extend_trial", TrialEndsAt: &extended, At: t0},
			domain.AdminAdjustmentApplied{SubscriptionID: subID, Kind: "credit", Credit: 1000, At: t0},
			domain.Subscribed{SubscriptionID: subID, AccountID: account, CurrentPeriod: domain.Period{Start: t0, End: end}, At: t0},
		).
		ThenScheduled(
			testkit.Scheduled{RunAt: trialEnd, Command: app.ExpireTrial{SubscriptionID: subID, TrialEndsAt: trialEnd}, DedupKey: app.TrialKey(subID, trialEnd)},
			testkit.Scheduled{RunAt: extended, Command: app.ExpireTrial{SubscriptionID: subID, TrialEndsAt: extended}, DedupKey: app.TrialKey(subID, extended)},
			testkit.Scheduled{RunAt: end, Command: app.RenewSubscription{SubscriptionID: subID, PeriodEnd: end}, DedupKey: app.RenewKey(subID, end)},
		)
}

func TestInvoicing_PP_F5_ARQ_003_InvoiceOnPaymentAndCreditNoteOnRefund(t *testing.T) {
	h := testkit.Process(t, func(jobs process.Jobs, clock kernel.Clock) process.ProcessManager {
		return pm.NewInvoicing(jobs, lookup{}, clock)
	}).At(t0).
		GivenEvents(
			domain.ChargePaid{SubscriptionID: subID, ChargeID: charge, Amount: 178500, PaymentRef: "tx-1", PaidAt: t0},
			domain.ChargeRefunded{SubscriptionID: subID, ChargeID: charge, Amount: 50000, Reason: "cortesía", RefundRef: "rf-1", RefundedAt: t0},
		)
	h.ThenCommands(
		app.IssueInvoice{ChargeID: charge},
		app.IssueCreditNote{ChargeID: charge, Amount: 50000, RefundRef: "rf-1", Reason: "cortesía"},
	)
}

func TestInvoicing_PP_F5_ARQ_003_ZeroChargeIsNotInvoicedAndManualInvoicesUpfront(t *testing.T) {
	manual := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	charges := lookup{
		charge: {ID: charge, AccountID: account, Status: "pending"},
		manual: {ID: manual, AccountID: account, Status: "pending", ManualBilling: true},
	}
	testkit.Process(t, func(jobs process.Jobs, clock kernel.Clock) process.ProcessManager {
		return pm.NewInvoicing(jobs, charges, clock)
	}).At(t0).
		GivenEvents(
			domain.ChargePaid{SubscriptionID: subID, ChargeID: charge, Amount: 0, PaidAt: t0},
			domain.ChargeScheduled{ChargeID: charge, SubscriptionID: subID, AccountID: account, Total: 178500, ScheduledAt: t0},
			domain.ChargeScheduled{ChargeID: manual, SubscriptionID: subID, AccountID: account, Total: 178500, ScheduledAt: t0},
		).
		ThenCommands(app.IssueInvoice{ChargeID: manual})
}
