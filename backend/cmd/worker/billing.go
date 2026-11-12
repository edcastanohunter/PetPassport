package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	billingapp "github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	billinginfra "github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	billingpm "github.com/edcastanohunter/petpassport/backend/internal/billing/pm"
	billingsetup "github.com/edcastanohunter/petpassport/backend/internal/billing/setup"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Billing en el worker (PP-F5-ARQ-002, ARQ-003, ARQ-006, DOM-012): aquí se cobra,
// se factura y se concilia. El api solo tokeniza, recibe webhooks y atiende a
// SuperAdmin.

const billingReconcileEvery = 24 * time.Hour

// noArchive hace fallar la emisión de facturas mientras no haya object storage:
// el job reintenta y queda visible, en vez de emitir sin archivar.
type noArchive struct{}

func (noArchive) Put(context.Context, string, string, io.Reader, int64) error {
	return errors.New("billing: object storage no configurado (PP_STORAGE_ENDPOINT)")
}

type billingModule struct {
	pay     *billingapp.PaymentService
	inv     *billingapp.InvoicingService
	ledger  *billinginfra.Ledger
	metrics *billingapp.Metrics
	logger  *slog.Logger
}

func newBillingModule(ctx context.Context, a *app, sched *scheduler.Scheduler, logger *slog.Logger) (*billingModule, error) {
	reg := a.store.Registry()
	runner := projection.NewRunner()
	billinginfra.RegisterSyncProjectors(runner, reg)

	var archive billingapp.DocumentArchive = noArchive{}
	if a.cfg.Storage.Endpoint != "" {
		o, err := storage.NewMinIO(ctx, storage.MinIOOptions{
			Endpoint: a.cfg.Storage.Endpoint, Bucket: a.cfg.Storage.Bucket,
			AccessKey: a.cfg.Storage.AccessKey, SecretKey: a.cfg.Storage.SecretKey, UseSSL: a.cfg.Storage.UseSSL,
		})
		if err != nil {
			return nil, err
		}
		archive = o
	} else {
		logger.Warn("billing: sin object storage; las facturas no se emitirán hasta configurarlo")
	}

	ledger := billinginfra.NewLedger(a.pool.Pool)
	queries := billinginfra.NewQueries(a.pool.Pool)
	typst := pdfrender.NewTypst(a.cfg.TypstBin, pdfrender.DefaultConcurrency, pdfrender.DefaultTimeout)
	router := billingsetup.Router(a.cfg.Env, a.cfg.Billing, ledger, typst)
	uow := db.NewUnitOfWork(a.pool)
	jobs := billingpm.NewJobQueue(sched)
	metrics := billingapp.NewMetrics(a.tel.Registry)

	return &billingModule{
		pay: billingapp.NewPaymentService(billingapp.PaymentOptions{
			Subscriptions: billinginfra.NewSubscriptionRepository(a.store, reg),
			Reader:        queries, Ledger: ledger, Router: router, UoW: uow,
			Projects: billinginfra.NewSyncProjector(runner), Jobs: jobs, Metrics: metrics, Logger: logger,
		}),
		inv: billingapp.NewInvoicingService(billingapp.InvoicingOptions{
			Charges: ledger, Reader: queries, Store: billinginfra.NewInvoiceStore(a.pool.Pool), Router: router,
			Archive: archive, UoW: uow, Jobs: jobs, Metrics: metrics, Logger: logger,
		}),
		ledger: ledger, metrics: metrics, logger: logger,
	}, nil
}

// register añade los comandos programables, sus handlers y los process managers.
func (b *billingModule) register(consumer *process.Consumer, allowed *scheduler.AllowedCommands, sched *scheduler.Scheduler, commands *bus.Bus) {
	for _, factory := range billingapp.SystemCommands() {
		allowed.Allow(factory)
	}
	billingapp.RegisterCommands(commands, b.pay, b.inv)

	clock := kernel.RealClock{}
	consumer.Register(billingpm.NewChargeCollector(process.Guard(billingpm.NewChargeCollector(nil, nil), sched), b.ledger))
	consumer.Register(billingpm.NewDunning(process.Guard(billingpm.NewDunning(nil, nil), sched), b.ledger))
	consumer.Register(billingpm.NewTimers(process.Guard(billingpm.NewTimers(nil), sched)))
	consumer.Register(billingpm.NewInvoicing(process.Guard(billingpm.NewInvoicing(nil, nil, clock), sched), b.ledger, clock))
}

// runners devuelve la conciliación diaria (ARQ-002), que además publica las
// métricas de negocio que no salen de un contador (INF-003).
func (b *billingModule) runners() []worker.Runner {
	return []worker.Runner{{Name: "billing-reconcile", Run: func(ctx context.Context) error {
		t := time.NewTicker(billingReconcileEvery)
		defer t.Stop()
		for {
			b.reconcile(ctx)
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
			}
		}
	}}}
}

func (b *billingModule) reconcile(ctx context.Context) {
	sum, err := b.pay.Reconcile(ctx)
	if err != nil {
		b.logger.ErrorContext(ctx, "billing: conciliación fallida", "error", err)
	} else {
		b.logger.InfoContext(ctx, "billing: conciliación completada",
			"checked", sum.Checked, "healed", sum.Healed, "errors", sum.Errors, "open_discrepancies", sum.Discrepancies)
	}
	if n, err := b.ledger.RenewalsOverdue(ctx, time.Now().Add(-time.Hour)); err != nil {
		b.logger.ErrorContext(ctx, "billing: renovaciones atrasadas", "error", err)
	} else {
		b.metrics.SetRenewalsOverdue(n)
	}
	mrr, err := b.ledger.MRR(ctx)
	if err != nil {
		b.logger.ErrorContext(ctx, "billing: MRR", "error", err)
		return
	}
	for currency, amount := range mrr {
		b.metrics.SetMRR(currency, amount)
	}
}
