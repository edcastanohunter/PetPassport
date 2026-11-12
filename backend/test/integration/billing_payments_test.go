//go:build integration

// Cobro y facturación de F5 M5.2 contra Postgres real (PP-F5-QA-002): ejerce el
// SQL del libro de pagos, de las facturas y de las lecturas de administración
// con los adaptadores fake, de extremo a extremo.
package integration

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	billingapp "github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	billingdomain "github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	billinginfra "github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing"
	invfake "github.com/edcastanohunter/petpassport/backend/internal/billing/invoicing/fake"
	billingpm "github.com/edcastanohunter/petpassport/backend/internal/billing/pm"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers/manual"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

type memArchive struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (a *memArchive) Put(_ context.Context, key, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.objects[key] = b
	return nil
}

type billingEnv struct {
	pool     *db.Pool
	svc      *billingapp.Service
	pay      *billingapp.PaymentService
	inv      *billingapp.InvoicingService
	ledger   *billinginfra.Ledger
	invoices *billinginfra.InvoiceStore
	queries  *billinginfra.Queries
	fake     *payfake.FakePaymentProvider
	archive  *memArchive
	store    *eventstore.Store
	unit     *db.UnitOfWork
	projects *billinginfra.SyncProjector
}

func newBillingEnv(t *testing.T) *billingEnv {
	t.Helper()
	pool, err := db.Open(context.Background(), testkit.Postgres(t), 8, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := eventstore.NewRegistry()
	billinginfra.RegisterEvents(reg)
	store := eventstore.NewStore(reg, pool, nil)
	runner := projection.NewRunner()
	billinginfra.RegisterSyncProjectors(runner, reg)
	projects := billinginfra.NewSyncProjector(runner)
	unit := db.NewUnitOfWork(pool)

	allowed := scheduler.NewAllowedCommands()
	for _, factory := range billingapp.SystemCommands() {
		allowed.Allow(factory)
	}
	jobs := billingpm.NewJobQueue(scheduler.New(allowed, nil))

	e := &billingEnv{
		pool: pool, ledger: billinginfra.NewLedger(pool.Pool), invoices: billinginfra.NewInvoiceStore(pool.Pool),
		queries: billinginfra.NewQueries(pool.Pool), fake: payfake.NewFakePaymentProvider().WithSecret("it-secret"),
		archive: &memArchive{objects: map[string][]byte{}},
		store:   store, unit: unit, projects: projects,
	}
	router := providers.NewRouter(e.ledger,
		[]providers.PaymentProvider{e.fake, manual.New()},
		[]invoicing.Provider{invfake.NewFakeInvoicingProvider()})
	subs := billinginfra.NewSubscriptionRepository(store, reg)
	e.svc = billingapp.NewBillingService(billinginfra.NewBillingAccountRepository(store, reg), subs, e.queries, router, unit, projects).WithJobs(jobs).
		WithCoupons(billinginfra.NewCouponStore(pool.Pool))
	e.pay = billingapp.NewPaymentService(billingapp.PaymentOptions{
		Subscriptions: subs, Reader: e.queries, Ledger: e.ledger, Router: router, UoW: unit, Projects: projects, Jobs: jobs,
	})
	e.inv = billingapp.NewInvoicingService(billingapp.InvoicingOptions{
		Charges: e.ledger, Reader: e.queries, Store: e.invoices, Router: router, Archive: e.archive, UoW: unit, Jobs: jobs,
	})
	return e
}

// subscribedAccount abre una cuenta colombiana con tarjeta tokenizada y la
// suscribe a un plan de pago; devuelve la cuenta, la suscripción y su cargo.
func (e *billingEnv) subscribedAccount(t *testing.T, legalName string) (accountID, subID, chargeID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	accountID, subID = uuid.New(), uuid.New()
	org := uuid.New()
	_, err := e.svc.OpenAccount(ctx, billingapp.OpenAccountCmd{
		AccountID: accountID, OwnerUserID: uuid.New(), Country: "CO", OrgID: &org,
		LegalEntity: billingdomain.LegalEntity{
			Kind: "company", LegalName: legalName, Address: "Av. Caracas # 45-67", Email: "facturacion@example.com",
			TaxID: billingdomain.TaxID{Country: "CO", Value: "900123456-7", Kind: "NIT"},
		},
	})
	if err != nil {
		t.Fatalf("abrir cuenta: %v", err)
	}
	pm, err := e.svc.AttachPaymentMethod(ctx, accountID, billingapp.AttachPaymentMethodCmd{ProviderToken: "tok_ok"})
	if err != nil {
		t.Fatalf("registrar método de pago: %v", err)
	}
	if pm.Provider != e.fake.Name() || pm.Token == "" {
		t.Fatalf("método de pago inesperado: %+v", pm)
	}
	if _, err := e.svc.Subscribe(ctx, billingapp.SubscribeCmd{
		SubscriptionID: subID, AccountID: accountID, Kind: billingdomain.KindClinic, PlanCode: "starter", Cycle: billingdomain.CycleMonthly,
	}); err != nil {
		t.Fatalf("suscribir: %v", err)
	}
	if err := e.pool.QueryRow(ctx, `SELECT id FROM billing.charges WHERE subscription_id = $1`, subID).Scan(&chargeID); err != nil {
		t.Fatalf("cargo de la suscripción: %v", err)
	}
	return accountID, subID, chargeID
}

// PP-F5-QA-002: cobro → webhook idempotente → factura → reembolso con nota crédito.
func TestPPF5QA002_ChargeInvoiceRefundAgainstPostgres(t *testing.T) {
	e := newBillingEnv(t)
	ctx := context.Background()
	accountID, _, chargeID := e.subscribedAccount(t, "Clínica Veterinaria Central")

	if err := e.pay.CollectCharge(ctx, chargeID); err != nil {
		t.Fatalf("cobrar: %v", err)
	}
	// Reintentar el mismo cobro no vuelve a cobrar (idempotencia por intento).
	if err := e.pay.CollectCharge(ctx, chargeID); err != nil {
		t.Fatalf("cobrar de nuevo: %v", err)
	}
	if n := e.fake.ChargesCreated(); n != 1 {
		t.Fatalf("cobros creados en el proveedor = %d, esperaba 1", n)
	}
	ch, err := e.ledger.Charge(ctx, chargeID)
	if err != nil || ch == nil {
		t.Fatalf("leer cargo: %v", err)
	}
	if ch.Status != "paid" || ch.PaymentRef == "" || ch.Provider != e.fake.Name() {
		t.Fatalf("cargo tras cobrar: status=%s ref=%q provider=%q", ch.Status, ch.PaymentRef, ch.Provider)
	}
	if ch.Tax <= 0 || ch.Total != billingdomain.RoundCents(ch.Amount+ch.Tax) {
		t.Fatalf("impuestos del cargo: amount=%v tax=%v total=%v", ch.Amount, ch.Tax, ch.Total)
	}

	// Webhook firmado del mismo pago: se procesa una vez y luego es duplicado.
	headers, body := e.fake.SignedWebhook(providers.Payment{
		Ref: ch.PaymentRef, Reference: providers.Reference(chargeID, 1), Status: providers.ChargePaid, Amount: ch.Total, Currency: ch.Currency,
	}, time.Now())
	for i, want := range []billingapp.WebhookOutcome{billingapp.WebhookProcessed, billingapp.WebhookDuplicate} {
		got, err := e.pay.HandleWebhook(ctx, e.fake.Name(), headers, body)
		if err != nil || got != want {
			t.Fatalf("webhook #%d: outcome=%q err=%v, esperaba %q", i+1, got, err, want)
		}
	}
	payments, err := e.ledger.ListPayments(ctx, accountID, 10)
	if err != nil || len(payments) != 1 || payments[0].Kind != "payment" || payments[0].Amount != ch.Total {
		t.Fatalf("pagos tras cobro y webhooks: %+v err=%v", payments, err)
	}

	// Factura: se emite una sola vez y queda archivada.
	for i := 0; i < 2; i++ {
		if err := e.inv.IssueInvoice(ctx, chargeID); err != nil {
			t.Fatalf("emitir factura (#%d): %v", i+1, err)
		}
	}
	invs, err := e.invoices.ListByAccount(ctx, accountID, 10)
	if err != nil || len(invs) != 1 {
		t.Fatalf("facturas de la cuenta: %d err=%v", len(invs), err)
	}
	inv := invs[0]
	if inv.Number == "" || !inv.HasPDF || inv.Totals.Total != ch.Total || inv.ChargeID != chargeID {
		t.Fatalf("factura inesperada: %+v", inv)
	}
	if stored, err := e.invoices.Get(ctx, inv.ID); err != nil || stored == nil || len(e.archive.objects[stored.PDFKey]) == 0 {
		t.Fatalf("la factura no quedó archivada: %v", err)
	}

	// Reembolso parcial y su nota crédito.
	refundRef, err := e.pay.RefundCharge(ctx, chargeID, 10000, "Compensación por incidencia")
	if err != nil || refundRef == "" {
		t.Fatalf("reembolsar: ref=%q err=%v", refundRef, err)
	}
	if _, err := e.pay.RefundCharge(ctx, chargeID, ch.Total, "excede"); !errors.Is(err, billingdomain.ErrRefundExceeds) {
		t.Fatalf("reembolso por encima del saldo: esperaba ErrRefundExceeds, obtuvo %v", err)
	}
	if err := e.inv.IssueCreditNote(ctx, chargeID, 10000, refundRef, "Compensación por incidencia"); err != nil {
		t.Fatalf("nota crédito: %v", err)
	}
	notes, err := e.invoices.CreditNotes(ctx, []uuid.UUID{inv.ID})
	if err != nil || len(notes[inv.ID]) != 1 || notes[inv.ID][0].Amount != 10000 {
		t.Fatalf("notas crédito: %+v err=%v", notes, err)
	}
	payments, _ = e.ledger.ListPayments(ctx, accountID, 10)
	if len(payments) != 2 || payments[0].Kind != "refund" || payments[0].Status != "confirmed" {
		t.Fatalf("pagos tras el reembolso: %+v", payments)
	}
	ch, _ = e.ledger.Charge(ctx, chargeID)
	if ch.Status != "partially_refunded" || ch.Refunded != 10000 {
		t.Fatalf("cargo tras el reembolso: status=%s refunded=%v", ch.Status, ch.Refunded)
	}

	// La conciliación diaria no encuentra nada que corregir.
	sum, err := e.pay.Reconcile(ctx)
	if err != nil || sum.Discrepancies != 0 {
		t.Fatalf("conciliación: %+v err=%v", sum, err)
	}
	if mrr, err := e.ledger.MRR(ctx); err != nil || mrr["COP"] <= 0 {
		t.Fatalf("MRR: %v err=%v", mrr, err)
	}
	if n, err := e.ledger.RenewalsOverdue(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("renovaciones atrasadas: %d err=%v", n, err)
	}
}

// PP-F5-API-007: lecturas y acciones de administración sobre Postgres.
func TestPPF5API007_AdminQueriesAgainstPostgres(t *testing.T) {
	e := newBillingEnv(t)
	ctx := context.Background()
	accountID, subID, chargeID := e.subscribedAccount(t, "Clínica Veterinaria Central")
	e.subscribedAccount(t, "Farmacia del Parque")

	for name, tc := range map[string]struct {
		f    billinginfra.AccountFilter
		want int
	}{
		"todas":            {billinginfra.AccountFilter{Limit: 10}, 2},
		"por nombre":       {billinginfra.AccountFilter{Query: "veterinaria", Limit: 10}, 1},
		"por plan":         {billinginfra.AccountFilter{Plan: "starter", Limit: 10}, 2},
		"plan inexistente": {billinginfra.AccountFilter{Plan: "otro", Limit: 10}, 0},
		"por país":         {billinginfra.AccountFilter{Country: "MX", Limit: 10}, 0},
		"límite":           {billinginfra.AccountFilter{Limit: 1}, 1},
	} {
		rows, err := e.queries.ListAccounts(ctx, tc.f)
		if err != nil || len(rows) != tc.want {
			t.Errorf("%s: %d cuentas (err=%v), esperaba %d", name, len(rows), err, tc.want)
		}
	}
	rows, _ := e.queries.ListAccounts(ctx, billinginfra.AccountFilter{Query: "veterinaria", Limit: 10})
	if len(rows) != 1 || len(rows[0].Subscriptions) != 1 || rows[0].Subscriptions[0].ID != subID {
		t.Fatalf("resumen de la cuenta: %+v", rows)
	}

	// Discrepancias: abrir es idempotente por (cargo, tipo); resolver, una sola vez.
	now := time.Now()
	for i, want := range []bool{true, false} {
		opened, err := e.ledger.OpenDiscrepancy(ctx, uuid.New(), accountID, chargeID, billinginfra.DiscrepancyStuckAttempt, map[string]any{"attempt": 1}, now)
		if err != nil || opened != want {
			t.Fatalf("abrir discrepancia #%d: opened=%v err=%v", i+1, opened, err)
		}
	}
	open, err := e.ledger.ListDiscrepancies(ctx, "open", 10)
	if err != nil || len(open) != 1 {
		t.Fatalf("discrepancias abiertas: %d err=%v", len(open), err)
	}
	byAccount, err := e.ledger.DiscrepanciesByAccount(ctx, accountID, 10)
	if err != nil || len(byAccount) != 1 || byAccount[0].ID != open[0].ID {
		t.Fatalf("discrepancias de la cuenta: %+v err=%v", byAccount, err)
	}
	admin := uuid.New()
	for i, want := range []bool{true, false} {
		done, err := e.ledger.ResolveDiscrepancy(ctx, open[0].ID, "verified_with_provider", admin, now)
		if err != nil || done != want {
			t.Fatalf("resolver discrepancia #%d: done=%v err=%v", i+1, done, err)
		}
	}

	// Pago manual: el importe debe coincidir y la referencia es única.
	if err := e.pay.RecordManualPayment(ctx, chargeID, 1, "TRF-001"); !errors.Is(err, billingdomain.ErrManualAmountMismatch) {
		t.Fatalf("pago manual con importe distinto: %v", err)
	}
	ch, _ := e.ledger.Charge(ctx, chargeID)
	if err := e.pay.RecordManualPayment(ctx, chargeID, ch.Total, "TRF-001"); err != nil {
		t.Fatalf("pago manual: %v", err)
	}
	if ch, _ = e.ledger.Charge(ctx, chargeID); ch.Status != "paid" || ch.Provider != providers.ManualProvider {
		t.Fatalf("cargo tras el pago manual: status=%s provider=%q", ch.Status, ch.Provider)
	}

	// Audit log de la acción administrativa.
	if err := e.ledger.Audit(ctx, audit.Entry{
		ActorID: &admin, ActorRoles: []string{"super_admin"}, Action: "admin.billing.charge.manual_payment",
		TargetType: "charge", TargetID: &chargeID, Reason: "Transferencia recibida el día 3", Details: map[string]any{"reference": "TRF-001"},
	}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	var audited int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM platform.audit_log WHERE target_id = $1`, chargeID).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("entradas de auditoría = %d err=%v", audited, err)
	}
}

// billingSnapshot serializa el read model de Billing sin las marcas de tiempo
// que el proyector fija con now().
func (e *billingEnv) billingSnapshot(t *testing.T) string {
	t.Helper()
	var out string
	err := e.pool.QueryRow(context.Background(), `
SELECT concat_ws(' | ',
  (SELECT jsonb_agg(to_jsonb(a) - 'updated_at' ORDER BY a.id) FROM billing.accounts a)::text,
  (SELECT jsonb_agg(to_jsonb(s) - 'updated_at' ORDER BY s.id) FROM billing.subscriptions s)::text,
  (SELECT jsonb_agg(to_jsonb(c) - 'updated_at' ORDER BY c.id) FROM billing.charges c)::text,
  (SELECT jsonb_agg(to_jsonb(n) - 'computed_at' ORDER BY n.organization_id) FROM billing.entitlements n)::text)`).Scan(&out)
	if err != nil {
		t.Fatalf("snapshot del read model: %v", err)
	}
	return out
}

// PP-F5-DOM-007: proyectar dos veces los mismos eventos deja el mismo estado
// (cobro, reembolso parcial y ajuste administrativo incluidos).
func TestPPF5DOM007_BillingProjectorIsIdempotent(t *testing.T) {
	e := newBillingEnv(t)
	ctx := context.Background()
	_, subID, chargeID := e.subscribedAccount(t, "Clínica Veterinaria Central")
	if err := e.pay.CollectCharge(ctx, chargeID); err != nil {
		t.Fatalf("cobrar: %v", err)
	}
	if _, err := e.pay.RefundCharge(ctx, chargeID, 5000, "Compensación por incidencia"); err != nil {
		t.Fatalf("reembolsar: %v", err)
	}
	if err := e.pay.AdjustSubscription(ctx, subID, billingapp.AdjustCmd{Kind: "credit", Amount: 20000, Reason: "Cortesía por incidencia"}); err != nil {
		t.Fatalf("ajustar: %v", err)
	}
	before := e.billingSnapshot(t)

	events, err := e.store.ReadAll(ctx, 0, 1000)
	if err != nil || len(events) == 0 {
		t.Fatalf("leer eventos: %d err=%v", len(events), err)
	}
	if err := e.unit.Do(ctx, func(ctx context.Context) error { return e.projects.Apply(ctx, events) }); err != nil {
		t.Fatalf("reproyectar: %v", err)
	}
	if after := e.billingSnapshot(t); after != before {
		t.Fatalf("el read model cambió al proyectar dos veces\nantes:   %s\ndespués: %s", before, after)
	}
}

// openAccount abre una cuenta colombiana con tarjeta tokenizada.
func (e *billingEnv) openAccount(t *testing.T, legalName string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id, org := uuid.New(), uuid.New()
	if _, err := e.svc.OpenAccount(ctx, billingapp.OpenAccountCmd{
		AccountID: id, OwnerUserID: uuid.New(), Country: "CO", OrgID: &org,
		LegalEntity: billingdomain.LegalEntity{
			Kind: "company", LegalName: legalName, Address: "Calle 10 # 5-20", Email: "facturacion@example.com",
			TaxID: billingdomain.TaxID{Country: "CO", Value: "900123456-7", Kind: "NIT"},
		},
	}); err != nil {
		t.Fatalf("abrir cuenta: %v", err)
	}
	if _, err := e.svc.AttachPaymentMethod(ctx, id, billingapp.AttachPaymentMethodCmd{ProviderToken: "tok_ok"}); err != nil {
		t.Fatalf("registrar método de pago: %v", err)
	}
	return id
}

// PP-F5-PRD-022: un cupón descuenta el primer cargo, consume un uso en la misma
// transacción que la contratación y no supera max_uses.
func TestPPF5PRD022_CouponIsRedeemedOnceAndAppliedToTheCharge(t *testing.T) {
	e := newBillingEnv(t)
	ctx := context.Background()
	coupons := billinginfra.NewCouponStore(e.pool.Pool)
	one, three := 1, 3
	if err := coupons.Create(ctx, billinginfra.Coupon{Code: "LANZA20", Kind: "pct", Value: 20, MaxUses: &one, DurationMonths: &three, Plans: []string{"starter"}}); err != nil {
		t.Fatalf("crear cupón: %v", err)
	}
	if err := coupons.Create(ctx, billinginfra.Coupon{Code: "LANZA20", Kind: "pct", Value: 10}); !errors.Is(err, billinginfra.ErrCouponExists) {
		t.Fatalf("código repetido: esperaba ErrCouponExists, obtuvo %v", err)
	}

	code := "lanza20"
	acc := e.openAccount(t, "Clínica Uno")
	preview, err := e.svc.PreviewSubscription(ctx, billingapp.PreviewSubscriptionCmd{
		AccountID: acc, Kind: billingdomain.KindClinic, PlanCode: "starter", Cycle: billingdomain.CycleMonthly, Coupon: &code,
	})
	if err != nil {
		t.Fatalf("previsualizar con cupón: %v", err)
	}
	if preview.Subtotal != 96000 {
		t.Fatalf("subtotal con 20 %% de descuento = %v, esperaba 96000", preview.Subtotal)
	}
	if _, err := e.svc.PreviewSubscription(ctx, billingapp.PreviewSubscriptionCmd{
		AccountID: acc, Kind: billingdomain.KindClinic, PlanCode: "pro", Cycle: billingdomain.CycleMonthly, Coupon: &code,
	}); !errors.Is(err, billinginfra.ErrCouponInvalid) {
		t.Fatalf("cupón en un plan no incluido: esperaba ErrCouponInvalid, obtuvo %v", err)
	}

	sub := uuid.New()
	if _, err := e.svc.Subscribe(ctx, billingapp.SubscribeCmd{
		SubscriptionID: sub, AccountID: acc, Kind: billingdomain.KindClinic, PlanCode: "starter", Cycle: billingdomain.CycleMonthly, CouponCode: &code,
	}); err != nil {
		t.Fatalf("suscribir con cupón: %v", err)
	}
	var amount float64
	if err := e.pool.QueryRow(ctx, `SELECT amount::float8 FROM billing.charges WHERE subscription_id = $1`, sub).Scan(&amount); err != nil || amount != 96000 {
		t.Fatalf("base del cargo = %v (err=%v), esperaba 96000", amount, err)
	}

	// Segundo canje: max_uses agotado; la contratación se revierte entera.
	other, sub2 := e.openAccount(t, "Clínica Dos"), uuid.New()
	if _, err := e.svc.Subscribe(ctx, billingapp.SubscribeCmd{
		SubscriptionID: sub2, AccountID: other, Kind: billingdomain.KindClinic, PlanCode: "starter", Cycle: billingdomain.CycleMonthly, CouponCode: &code,
	}); !errors.Is(err, billinginfra.ErrCouponInvalid) {
		t.Fatalf("cupón agotado: esperaba ErrCouponInvalid, obtuvo %v", err)
	}
	var subs, uses int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM billing.subscriptions WHERE id = $1`, sub2).Scan(&subs)
	_ = e.pool.QueryRow(ctx, `SELECT uses FROM billing.coupons WHERE code = 'LANZA20'`).Scan(&uses)
	if subs != 0 || uses != 1 {
		t.Fatalf("tras el canje rechazado: suscripciones=%d usos=%d, esperaba 0 y 1", subs, uses)
	}
}

// PP-F5-API-004: el método de pago no se retira mientras respalda una
// suscripción vigente; tras cancelarla, sí.
func TestPPF5API004_RemovePaymentMethodAgainstPostgres(t *testing.T) {
	e := newBillingEnv(t)
	ctx := context.Background()
	acc, sub, _ := e.subscribedAccount(t, "Clínica Veterinaria Central")
	if err := e.svc.RemovePaymentMethod(ctx, acc); !errors.Is(err, billingdomain.ErrPaymentMethodInUse) {
		t.Fatalf("con suscripción activa: esperaba ErrPaymentMethodInUse, obtuvo %v", err)
	}
	if err := e.svc.CancelSubscription(ctx, sub, false, "Cierre de la clínica"); err != nil {
		t.Fatalf("cancelar: %v", err)
	}
	if err := e.svc.RemovePaymentMethod(ctx, acc); err != nil {
		t.Fatalf("retirar tras cancelar: %v", err)
	}
	view, err := e.queries.GetAccount(ctx, acc)
	if err != nil || view.PaymentMethod != nil {
		t.Fatalf("la cuenta conserva el método: %+v err=%v", view.PaymentMethod, err)
	}
}
