package app

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
)

// Comandos del sistema de Billing (PP-F5-DOM-012, ARQ-002, ARQ-003, ARQ-006). Los
// programan los process managers en scheduler.command_jobs y los ejecuta el
// worker con Principal = system; todos son idempotentes (at-least-once).

// CollectCharge intenta cobrar un cargo pendiente o fallido en el proveedor.
type CollectCharge struct {
	ChargeID uuid.UUID `json:"charge_id"`
}

// CommandName implementa bus.Command.
func (CollectCharge) CommandName() string { return "billing.CollectCharge" }

// PollPayment consulta al proveedor el desenlace de un intento pendiente
// (respaldo del webhook: nunca se depende de un único webhook, ADR-022 §5).
type PollPayment struct {
	ChargeID uuid.UUID `json:"charge_id"`
	Attempt  int       `json:"attempt"`
	Try      int       `json:"try"`
}

// CommandName implementa bus.Command.
func (PollPayment) CommandName() string { return "billing.PollPayment" }

// RestrictForNonPayment restringe la escritura al agotar la gracia (día 14).
type RestrictForNonPayment struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	ChargeID       uuid.UUID `json:"charge_id"`
}

// CommandName implementa bus.Command.
func (RestrictForNonPayment) CommandName() string { return "billing.RestrictForNonPayment" }

// CancelForNonPayment cancela la suscripción tras 60 días de impago.
type CancelForNonPayment struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	ChargeID       uuid.UUID `json:"charge_id"`
}

// CommandName implementa bus.Command.
func (CancelForNonPayment) CommandName() string { return "billing.CancelForNonPayment" }

// RenewSubscription renueva el periodo al llegar PeriodEnd.
type RenewSubscription struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	PeriodEnd      time.Time `json:"period_end"`
}

// CommandName implementa bus.Command.
func (RenewSubscription) CommandName() string { return "billing.RenewSubscription" }

// ExpireTrial vence la prueba que no se convirtió en suscripción.
type ExpireTrial struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	TrialEndsAt    time.Time `json:"trial_ends_at"`
}

// CommandName implementa bus.Command.
func (ExpireTrial) CommandName() string { return "billing.ExpireTrial" }

// IssueInvoice emite la factura electrónica de un cargo.
type IssueInvoice struct {
	ChargeID uuid.UUID `json:"charge_id"`
}

// CommandName implementa bus.Command.
func (IssueInvoice) CommandName() string { return "billing.IssueInvoice" }

// PollInvoice consulta la validación de una factura emitida.
type PollInvoice struct {
	InvoiceID uuid.UUID `json:"invoice_id"`
	Try       int       `json:"try"`
}

// CommandName implementa bus.Command.
func (PollInvoice) CommandName() string { return "billing.PollInvoice" }

// IssueCreditNote emite la nota crédito de un reembolso.
type IssueCreditNote struct {
	ChargeID  uuid.UUID `json:"charge_id"`
	Amount    float64   `json:"amount"`
	RefundRef string    `json:"refund_ref"`
	Reason    string    `json:"reason"`
}

// CommandName implementa bus.Command.
func (IssueCreditNote) CommandName() string { return "billing.IssueCreditNote" }

// SystemCommands devuelve los prototipos de los comandos programables de Billing
// (scheduler.AllowedCommands, SEC-007).
func SystemCommands() []func() bus.Command {
	return []func() bus.Command{
		func() bus.Command { return CollectCharge{} },
		func() bus.Command { return PollPayment{} },
		func() bus.Command { return RestrictForNonPayment{} },
		func() bus.Command { return CancelForNonPayment{} },
		func() bus.Command { return RenewSubscription{} },
		func() bus.Command { return ExpireTrial{} },
		func() bus.Command { return IssueInvoice{} },
		func() bus.Command { return PollInvoice{} },
		func() bus.Command { return IssueCreditNote{} },
	}
}

// CommandScheduler programa un comando del sistema dentro de la transacción del
// contexto (scheduler.command_jobs). Programar dos veces la misma clave no duplica.
type CommandScheduler interface {
	ScheduleCommand(ctx context.Context, runAt time.Time, cmd bus.Command, dedupKey string) error
}

// Claves de deduplicación de los jobs de Billing.

// CollectKey identifica el cobro inicial de un cargo.
func CollectKey(chargeID uuid.UUID) string { return "billing.collect:" + chargeID.String() }

// DunningRetryKey identifica el reintento n (0..2) del dunning de un cargo.
func DunningRetryKey(chargeID uuid.UUID, n int) string {
	return "billing.dunning-retry:" + chargeID.String() + ":" + strconv.Itoa(n)
}

// DunningRestrictKey identifica la restricción del día 14.
func DunningRestrictKey(chargeID uuid.UUID) string {
	return "billing.dunning-restrict:" + chargeID.String()
}

// DunningCancelKey identifica la cancelación del día 60.
func DunningCancelKey(chargeID uuid.UUID) string {
	return "billing.dunning-cancel:" + chargeID.String()
}

// RenewKey identifica la renovación de un periodo concreto.
func RenewKey(subID uuid.UUID, periodEnd time.Time) string {
	return "billing.renew:" + subID.String() + ":" + periodEnd.UTC().Format("20060102")
}

// TrialKey identifica el vencimiento de la prueba en una fecha concreta (una
// extensión de prueba programa un vencimiento nuevo).
func TrialKey(subID uuid.UUID, endsAt time.Time) string {
	return "billing.trial-expire:" + subID.String() + ":" + endsAt.UTC().Format("20060102T1504")
}

// InvoiceKey identifica la emisión de la factura de un cargo.
func InvoiceKey(chargeID uuid.UUID) string { return "billing.invoice:" + chargeID.String() }
