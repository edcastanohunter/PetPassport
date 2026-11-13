package app

import (
	"context"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
)

// RegisterCommands registra en el bus del worker los comandos del sistema de
// Billing. Ninguno pasa por authz: solo los despacha el scheduler de comandos
// (Principal = system) y están en la lista cerrada scheduler.AllowedCommands.
func RegisterCommands(b *bus.Bus, pay *PaymentService, inv *InvoicingService) {
	b.Register(CollectCharge{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(CollectCharge)
		return bus.Result{AggregateID: c.ChargeID}, pay.CollectCharge(ctx, c.ChargeID)
	})
	b.Register(PollPayment{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(PollPayment)
		return bus.Result{AggregateID: c.ChargeID}, pay.PollPayment(ctx, c.ChargeID, c.Attempt, c.Try)
	})
	b.Register(RestrictForNonPayment{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(RestrictForNonPayment)
		return bus.Result{AggregateID: c.SubscriptionID}, pay.RestrictForNonPayment(ctx, c.SubscriptionID, c.ChargeID)
	})
	b.Register(CancelForNonPayment{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(CancelForNonPayment)
		return bus.Result{AggregateID: c.SubscriptionID}, pay.CancelForNonPayment(ctx, c.SubscriptionID, c.ChargeID)
	})
	b.Register(RenewSubscription{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(RenewSubscription)
		return bus.Result{AggregateID: c.SubscriptionID}, pay.RenewSubscription(ctx, c.SubscriptionID, c.PeriodEnd)
	})
	b.Register(ExpireTrial{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(ExpireTrial)
		return bus.Result{AggregateID: c.SubscriptionID}, pay.ExpireTrial(ctx, c.SubscriptionID)
	})
	b.Register(IssueInvoice{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(IssueInvoice)
		return bus.Result{AggregateID: c.ChargeID}, inv.IssueInvoice(ctx, c.ChargeID)
	})
	b.Register(PollInvoice{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(PollInvoice)
		return bus.Result{AggregateID: c.InvoiceID}, inv.PollInvoice(ctx, c.InvoiceID, c.Try)
	})
	b.Register(IssueCreditNote{}, func(ctx context.Context, cmd bus.Command) (bus.Result, error) {
		c := cmd.(IssueCreditNote)
		return bus.Result{AggregateID: c.ChargeID}, inv.IssueCreditNote(ctx, c.ChargeID, c.Amount, c.RefundRef, c.Reason)
	})
}
