// Package pm contiene los process managers de Billing (PP-F5-DOM-012).
package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Process managers de Billing (PP-F5-DOM-012, ARQ-002, ARQ-006). Reaccionan a los
// eventos de Subscription y BillingAccount del outbox y solo programan o cancelan
// comandos del sistema: nunca llaman al proveedor ni al bus.
const (
	ProcessChargeCollector = "billing.charge-collector"
	ProcessDunning         = "billing.dunning"
	ProcessTimers          = "billing.subscription-timers"
	ProcessInvoicing       = "billing.invoicing"
)

// JobQueue adapta el scheduler de comandos al puerto app.CommandScheduler usando
// la transacción del contexto (UnitOfWork).
type JobQueue struct{ sched *scheduler.Scheduler }

var _ app.CommandScheduler = (*JobQueue)(nil)

// NewJobQueue construye el adaptador.
func NewJobQueue(sched *scheduler.Scheduler) *JobQueue { return &JobQueue{sched: sched} }

// ScheduleCommand implementa app.CommandScheduler.
func (q *JobQueue) ScheduleCommand(ctx context.Context, runAt time.Time, cmd bus.Command, dedupKey string) error {
	tx, err := db.TxFrom(ctx)
	if err != nil {
		return err
	}
	job := scheduler.Job{RunAt: runAt, Command: cmd, DedupKey: dedupKey}
	if o, ok := bus.SystemOriginFrom(ctx); ok {
		job.CorrelationID, job.CausationID = o.CorrelationID, o.CausationID
	}
	return q.sched.Schedule(ctx, tx, job)
}

func decode[T any](pm string, e eventstore.StoredEvent) (T, error) {
	var ev T
	if err := json.Unmarshal(e.Payload, &ev); err != nil {
		return ev, fmt.Errorf("%s: payload de %s: %w", pm, e.Type, err)
	}
	return ev, nil
}

// ChargeLookup son las lecturas de billing.charges que necesitan los PM. Los
// read models de Billing se proyectan en la misma transacción que el evento, así
// que ya están al día cuando el outbox lo entrega.
type ChargeLookup interface {
	Charge(ctx context.Context, chargeID uuid.UUID) (*infra.ChargeRef, error)
	FailedCharges(ctx context.Context, accountID uuid.UUID) ([]infra.ChargeRef, error)
}

// ChargeCollector lanza el cobro de cada cargo programado y reintenta de
// inmediato los cargos fallidos cuando la cuenta cambia de método de pago.
type ChargeCollector struct {
	jobs    process.Jobs
	charges ChargeLookup
}

var _ process.ProcessManager = (*ChargeCollector)(nil)

// NewChargeCollector construye el PM.
func NewChargeCollector(jobs process.Jobs, charges ChargeLookup) *ChargeCollector {
	return &ChargeCollector{jobs: jobs, charges: charges}
}

// Name implementa ProcessManager.
func (*ChargeCollector) Name() string { return ProcessChargeCollector }

// Handles implementa ProcessManager.
func (*ChargeCollector) Handles() []string { return []string{"ChargeScheduled", "PaymentMethodSet"} }

// Emits implementa ProcessManager.
func (*ChargeCollector) Emits() []string { return []string{app.CollectCharge{}.CommandName()} }

// Handle implementa ProcessManager.
func (p *ChargeCollector) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	if e.Type == "ChargeScheduled" {
		ev, err := decode[domain.ChargeScheduled](ProcessChargeCollector, e)
		if err != nil {
			return err
		}
		// Un cargo programado a futuro (renovación) se cobra en su fecha.
		runAt := e.OccurredAt
		if ev.ScheduledAt.After(runAt) {
			runAt = ev.ScheduledAt
		}
		return p.jobs.Schedule(ctx, tx, process.JobFor(e, runAt,
			app.CollectCharge{ChargeID: ev.ChargeID}, app.CollectKey(ev.ChargeID)))
	}
	ev, err := decode[domain.PaymentMethodSet](ProcessChargeCollector, e)
	if err != nil {
		return err
	}
	failed, err := p.charges.FailedCharges(db.WithTx(ctx, tx), ev.AccountID)
	if err != nil {
		return err
	}
	for _, ch := range failed {
		// Clave por evento: cada cambio de método de pago reintenta una vez.
		key := "billing.collect-pm:" + ch.ID.String() + ":" + e.EventID.String()
		if err := p.jobs.Schedule(ctx, tx, process.JobFor(e, e.OccurredAt, app.CollectCharge{ChargeID: ch.ID}, key)); err != nil {
			return err
		}
	}
	return nil
}

// Dunning programa, desde el primer fallo de un cargo, los reintentos (d+3, d+7,
// d+12), la restricción (d+14) y la cancelación (d+60), y los cancela al pagar.
type Dunning struct {
	jobs    process.Jobs
	charges ChargeLookup
}

var _ process.ProcessManager = (*Dunning)(nil)

// NewDunning construye el PM.
func NewDunning(jobs process.Jobs, charges ChargeLookup) *Dunning {
	return &Dunning{jobs: jobs, charges: charges}
}

// Name implementa ProcessManager.
func (*Dunning) Name() string { return ProcessDunning }

// Handles implementa ProcessManager.
func (*Dunning) Handles() []string { return []string{"ChargeFailed", "ChargePaid"} }

// Emits implementa ProcessManager.
func (*Dunning) Emits() []string {
	return []string{
		app.CollectCharge{}.CommandName(),
		app.RestrictForNonPayment{}.CommandName(),
		app.CancelForNonPayment{}.CommandName(),
	}
}

// Handle implementa ProcessManager.
func (p *Dunning) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	if e.Type == "ChargePaid" {
		ev, err := decode[domain.ChargePaid](ProcessDunning, e)
		if err != nil {
			return err
		}
		for i := range domain.DunningRetryDays {
			if err := p.jobs.Cancel(ctx, tx, app.DunningRetryKey(ev.ChargeID, i)); err != nil {
				return err
			}
		}
		if err := p.jobs.Cancel(ctx, tx, app.DunningRestrictKey(ev.ChargeID)); err != nil {
			return err
		}
		return p.jobs.Cancel(ctx, tx, app.DunningCancelKey(ev.ChargeID))
	}

	ev, err := decode[domain.ChargeFailed](ProcessDunning, e)
	if err != nil {
		return err
	}
	// El calendario se ancla al primer fallo: los fallos de los reintentos
	// reprograman las mismas claves con las mismas fechas (idempotente).
	first := ev.FailedAt
	ch, err := p.charges.Charge(db.WithTx(ctx, tx), ev.ChargeID)
	if err != nil {
		return err
	}
	if ch != nil && ch.FirstFailedAt != nil {
		first = *ch.FirstFailedAt
	}
	plan := domain.PlanDunning(first)
	collect := app.CollectCharge{ChargeID: ev.ChargeID}
	for i, at := range plan.Retries {
		if !at.After(ev.FailedAt) {
			continue // ese reintento ya ocurrió (es el que acaba de fallar)
		}
		if err := p.jobs.Schedule(ctx, tx, process.JobFor(e, at, collect, app.DunningRetryKey(ev.ChargeID, i))); err != nil {
			return err
		}
	}
	if err := p.jobs.Schedule(ctx, tx, process.JobFor(e, plan.RestrictAt,
		app.RestrictForNonPayment{SubscriptionID: ev.SubscriptionID, ChargeID: ev.ChargeID},
		app.DunningRestrictKey(ev.ChargeID))); err != nil {
		return err
	}
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, plan.CancelAt,
		app.CancelForNonPayment{SubscriptionID: ev.SubscriptionID, ChargeID: ev.ChargeID},
		app.DunningCancelKey(ev.ChargeID)))
}

// Timers programa la renovación al fin de cada periodo y el vencimiento de la prueba.
type Timers struct{ jobs process.Jobs }

var _ process.ProcessManager = (*Timers)(nil)

// NewTimers construye el PM.
func NewTimers(jobs process.Jobs) *Timers { return &Timers{jobs: jobs} }

// Name implementa ProcessManager.
func (*Timers) Name() string { return ProcessTimers }

// Handles implementa ProcessManager.
func (*Timers) Handles() []string {
	return []string{"TrialStarted", "Subscribed", "PeriodRenewed", "AdminAdjustmentApplied"}
}

// Emits implementa ProcessManager.
func (*Timers) Emits() []string {
	return []string{app.RenewSubscription{}.CommandName(), app.ExpireTrial{}.CommandName()}
}

// Handle implementa ProcessManager.
func (p *Timers) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	switch e.Type {
	case "TrialStarted":
		ev, err := decode[domain.TrialStarted](ProcessTimers, e)
		if err != nil {
			return err
		}
		return p.expireTrial(ctx, tx, e, ev.SubscriptionID, ev.TrialEndsAt)
	case "AdminAdjustmentApplied":
		ev, err := decode[domain.AdminAdjustmentApplied](ProcessTimers, e)
		if err != nil || ev.TrialEndsAt == nil {
			return err
		}
		return p.expireTrial(ctx, tx, e, ev.SubscriptionID, *ev.TrialEndsAt)
	case "Subscribed":
		ev, err := decode[domain.Subscribed](ProcessTimers, e)
		if err != nil {
			return err
		}
		return p.renew(ctx, tx, e, ev.SubscriptionID, ev.CurrentPeriod.End)
	default:
		ev, err := decode[domain.PeriodRenewed](ProcessTimers, e)
		if err != nil {
			return err
		}
		return p.renew(ctx, tx, e, ev.SubscriptionID, ev.CurrentPeriod.End)
	}
}

func (p *Timers) expireTrial(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, subID uuid.UUID, endsAt time.Time) error {
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, endsAt,
		app.ExpireTrial{SubscriptionID: subID, TrialEndsAt: endsAt}, app.TrialKey(subID, endsAt)))
}

func (p *Timers) renew(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, subID uuid.UUID, periodEnd time.Time) error {
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, periodEnd,
		app.RenewSubscription{SubscriptionID: subID, PeriodEnd: periodEnd}, app.RenewKey(subID, periodEnd)))
}

// Invoicing pide la factura de cada cargo cobrado y la nota crédito de cada reembolso.
type Invoicing struct {
	jobs    process.Jobs
	charges ChargeLookup
	clock   kernel.Clock
}

var _ process.ProcessManager = (*Invoicing)(nil)

// NewInvoicing construye el PM.
func NewInvoicing(jobs process.Jobs, charges ChargeLookup, clock kernel.Clock) *Invoicing {
	return &Invoicing{jobs: jobs, charges: charges, clock: clock}
}

// Name implementa ProcessManager.
func (*Invoicing) Name() string { return ProcessInvoicing }

// Handles implementa ProcessManager.
func (*Invoicing) Handles() []string {
	return []string{"ChargePaid", "ChargeScheduled", "ChargeRefunded"}
}

// Emits implementa ProcessManager.
func (*Invoicing) Emits() []string {
	return []string{app.IssueInvoice{}.CommandName(), app.IssueCreditNote{}.CommandName()}
}

// Handle implementa ProcessManager.
func (p *Invoicing) Handle(ctx context.Context, e eventstore.StoredEvent, tx pgx.Tx) error {
	switch e.Type {
	case "ChargePaid":
		ev, err := decode[domain.ChargePaid](ProcessInvoicing, e)
		if err != nil || ev.Amount <= 0 {
			return err // un cargo a cero no se factura
		}
		return p.issue(ctx, tx, e, ev.ChargeID)
	case "ChargeScheduled":
		// Facturación manual: la factura sale al programar el cargo, para que el
		// cliente pague la transferencia contra ella (ARQ-003).
		ev, err := decode[domain.ChargeScheduled](ProcessInvoicing, e)
		if err != nil || ev.Total <= 0 {
			return err
		}
		ch, err := p.charges.Charge(db.WithTx(ctx, tx), ev.ChargeID)
		if err != nil || ch == nil || !ch.ManualBilling {
			return err
		}
		return p.issue(ctx, tx, e, ev.ChargeID)
	default:
		ev, err := decode[domain.ChargeRefunded](ProcessInvoicing, e)
		if err != nil {
			return err
		}
		return p.jobs.Schedule(ctx, tx, process.JobFor(e, p.clock.Now(),
			app.IssueCreditNote{ChargeID: ev.ChargeID, Amount: ev.Amount, RefundRef: ev.RefundRef, Reason: ev.Reason},
			"billing.credit-note:"+ev.ChargeID.String()+":"+e.EventID.String()))
	}
}

func (p *Invoicing) issue(ctx context.Context, tx pgx.Tx, e eventstore.StoredEvent, chargeID uuid.UUID) error {
	return p.jobs.Schedule(ctx, tx, process.JobFor(e, p.clock.Now(),
		app.IssueInvoice{ChargeID: chargeID}, app.InvoiceKey(chargeID)))
}
