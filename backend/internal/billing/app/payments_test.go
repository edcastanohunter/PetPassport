package app

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	payfake "github.com/edcastanohunter/petpassport/backend/internal/billing/providers/fake"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers/manual"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

var t0 = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// world es un Billing en memoria: event store de suscripciones, ledger y
// scheduler falsos, con el proveedor fake real.
type world struct {
	t        *testing.T
	now      time.Time
	streams  map[uuid.UUID][]any
	charges  map[uuid.UUID]*infra.ChargeRef
	attempts map[uuid.UUID][]*infra.Attempt
	events   map[string]string
	payments []infra.PaymentRow
	discs    []discrepancy
	jobs     []job
	account  *infra.AccountView
	plan     domain.PlanDefinition
	pay      *payfake.FakePaymentProvider
	svc      *PaymentService
	seq      int
}

type discrepancy struct {
	chargeID uuid.UUID
	kind     string
	open     bool
}

type job struct {
	at  time.Time
	cmd bus.Command
	key string
}

func proPlan() domain.PlanDefinition {
	published := t0.AddDate(0, -2, 0)
	return domain.PlanDefinition{
		Code: "pro", Version: 1, Kind: "clinic", Name: map[string]string{"es": "Clínica Pro"},
		Pricing: domain.PlanPricing{Model: "per_seat", Currency: "COP", Monthly: 75000, Annual: 750000},
		Seats:   &domain.PlanSeatsRange{Min: 1}, Status: "published", PublishedAt: &published,
	}
}

type staticRouting struct{}

func (staticRouting) Route(_ context.Context, country string) (providers.Route, error) {
	return providers.Route{Country: country, PaymentProvider: payfake.Name, InvoicingProvider: "fake", TaxRule: "CO_IVA_19", TaxRate: 0.19}, nil
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, now: t0,
		streams: map[uuid.UUID][]any{}, charges: map[uuid.UUID]*infra.ChargeRef{},
		attempts: map[uuid.UUID][]*infra.Attempt{}, events: map[string]string{},
		plan: proPlan(), pay: payfake.NewFakePaymentProvider(),
	}
	w.account = &infra.AccountView{
		ID: uuid.New(), Country: "CO", Currency: "COP", Status: "active",
		LegalEntity:   domain.LegalEntity{LegalName: "Clínica de Prueba", Email: "facturacion@example.test"},
		PaymentMethod: &domain.PaymentMethodRef{Provider: payfake.Name, Token: "tok_ok", Kind: "card"},
	}
	router := providers.NewRouter(staticRouting{}, []providers.PaymentProvider{w.pay, manual.New()}, nil)
	w.svc = NewPaymentService(PaymentOptions{
		Subscriptions: w, Reader: w, Ledger: w, Router: router, UoW: w, Jobs: w,
		Clock: func() time.Time { return w.now },
		NewID: func() uuid.UUID { w.seq++; return uuid.MustParse("00000000-0000-4000-8000-" + pad12(w.seq)) },
	})
	return w
}

func pad12(n int) string {
	s := "000000000000"
	d := []byte(s)
	for i := len(d) - 1; n > 0 && i >= 0; i-- {
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d)
}

// --- UnitOfWork, scheduler, lecturas ---

func (w *world) Do(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

func (w *world) ScheduleCommand(_ context.Context, at time.Time, cmd bus.Command, key string) error {
	for i := range w.jobs {
		if w.jobs[i].key == key {
			w.jobs[i] = job{at, cmd, key}
			return nil
		}
	}
	w.jobs = append(w.jobs, job{at, cmd, key})
	return nil
}

func (w *world) GetAccount(context.Context, uuid.UUID) (*infra.AccountView, error) {
	return w.account, nil
}

func (w *world) GetPlan(context.Context, string) (*domain.PlanDefinition, error) {
	p := w.plan
	return &p, nil
}

func (w *world) ActiveProfessionals(context.Context, uuid.UUID) (int, error) { return 1, nil }

// --- SubscriptionStore: event store en memoria + proyección de cargos ---

func (w *world) Load(_ context.Context, id uuid.UUID) (*domain.Subscription, error) {
	evs, ok := w.streams[id]
	if !ok {
		return nil, errors.New("suscripción no encontrada")
	}
	sub := domain.NewSubscription()
	for _, e := range evs {
		sub.Apply(e)
	}
	return sub, nil
}

func (w *world) Save(_ context.Context, sub *domain.Subscription) ([]eventstore.StoredEvent, error) {
	for _, e := range sub.Changes() {
		w.streams[sub.ID()] = append(w.streams[sub.ID()], e)
		w.project(e)
	}
	return nil, nil
}

func (w *world) project(e any) {
	switch ev := e.(type) {
	case domain.Subscribed:
		w.manualBilling(ev.SubscriptionID, ev.ManualBilling)
	case domain.ChargeScheduled:
		w.charges[ev.ChargeID] = &infra.ChargeRef{
			ID: ev.ChargeID, SubscriptionID: ev.SubscriptionID, AccountID: ev.AccountID,
			Total: ev.Total, Currency: ev.Currency, Status: "pending", Reason: ev.Reason, ScheduledAt: ev.ScheduledAt,
			ManualBilling: w.isManual(ev.SubscriptionID),
		}
	case domain.ChargePaid:
		c := w.charges[ev.ChargeID]
		c.Status, c.PaymentRef, c.Provider = "paid", ev.PaymentRef, ev.Provider
		at := ev.PaidAt
		c.PaidAt = &at
	case domain.ChargeFailed:
		c := w.charges[ev.ChargeID]
		c.Status = "failed"
		if c.FirstFailedAt == nil {
			at := ev.FailedAt
			c.FirstFailedAt = &at
		}
	case domain.ChargeRefunded:
		c := w.charges[ev.ChargeID]
		c.Refunded += ev.Amount
		c.Status = "partially_refunded"
		if c.Refunded >= c.Total {
			c.Status = "refunded"
		}
	}
}

var manualSubs = map[uuid.UUID]bool{}

func (w *world) manualBilling(id uuid.UUID, v bool) { manualSubs[id] = v }
func (w *world) isManual(id uuid.UUID) bool         { return manualSubs[id] }

// --- PaymentLedger ---

func (w *world) Charge(_ context.Context, id uuid.UUID) (*infra.ChargeRef, error) {
	c, ok := w.charges[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (w *world) LockCharge(context.Context, uuid.UUID) error { return nil }

func (w *world) OpenAttempt(_ context.Context, id uuid.UUID) (*infra.Attempt, error) {
	for _, a := range w.attempts[id] {
		if a.Open() {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (w *world) Attempt(_ context.Context, id uuid.UUID, n int) (*infra.Attempt, error) {
	for _, a := range w.attempts[id] {
		if a.Number == n {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (w *world) BeginAttempt(ctx context.Context, id uuid.UUID, provider string, now time.Time) (infra.Attempt, error) {
	if open, _ := w.OpenAttempt(ctx, id); open != nil {
		return *open, nil
	}
	a := &infra.Attempt{ChargeID: id, Number: len(w.attempts[id]) + 1, Provider: provider, Status: infra.AttemptCreated, CreatedAt: now, UpdatedAt: now}
	w.attempts[id] = append(w.attempts[id], a)
	return *a, nil
}

func (w *world) UpdateAttempt(_ context.Context, id uuid.UUID, n int, status, ref, code string, now time.Time) (bool, error) {
	for _, a := range w.attempts[id] {
		if a.Number == n && a.Open() {
			a.Status, a.ErrorCode, a.UpdatedAt = status, code, now
			if ref != "" {
				a.ProviderRef = ref
			}
			return true, nil
		}
	}
	return false, nil
}

func (w *world) LatestAttemptNumber(_ context.Context, id uuid.UUID) (int, error) {
	return len(w.attempts[id]), nil
}

func (w *world) AttemptsSince(_ context.Context, since time.Time) ([]infra.Attempt, error) {
	var out []infra.Attempt
	for _, as := range w.attempts {
		for _, a := range as {
			if !a.CreatedAt.Before(since) {
				out = append(out, *a)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (w *world) ClaimProviderEvent(_ context.Context, provider, id string, _ time.Time) (bool, error) {
	k := provider + "/" + id
	if _, seen := w.events[k]; seen {
		return false, nil
	}
	w.events[k] = ""
	return true, nil
}

func (w *world) MarkProviderEvent(_ context.Context, provider, id, result string, _ time.Time) error {
	w.events[provider+"/"+id] = result
	return nil
}

func (w *world) InsertPayment(_ context.Context, p infra.PaymentRow) (bool, error) {
	for _, e := range w.payments {
		if e.Provider == p.Provider && e.ProviderRef == p.ProviderRef {
			return false, nil
		}
	}
	w.payments = append(w.payments, p)
	return true, nil
}

func (w *world) ConfirmRefund(_ context.Context, id uuid.UUID, ref string, _ time.Time) error {
	for i := range w.payments {
		if w.payments[i].ID == id {
			w.payments[i].ProviderRef, w.payments[i].Status = ref, "confirmed"
		}
	}
	return nil
}

func (w *world) DeletePendingRefund(_ context.Context, id uuid.UUID) error {
	for i := range w.payments {
		if w.payments[i].ID == id && w.payments[i].Status == "requested" {
			w.payments = append(w.payments[:i], w.payments[i+1:]...)
			return nil
		}
	}
	return nil
}

func (w *world) HasPendingRefund(_ context.Context, id uuid.UUID) (bool, error) {
	for _, p := range w.payments {
		if p.ChargeID == id && p.Kind == "refund" && p.Status == "requested" {
			return true, nil
		}
	}
	return false, nil
}

func (w *world) PendingRefunds(_ context.Context, before time.Time) ([]infra.PaymentRow, error) {
	var out []infra.PaymentRow
	for _, p := range w.payments {
		if p.Kind == "refund" && p.Status == "requested" && p.At.Before(before) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (w *world) PaymentOfCharge(_ context.Context, id uuid.UUID) (*infra.PaymentRow, error) {
	for _, p := range w.payments {
		if p.ChargeID == id && p.Kind == "payment" {
			cp := p
			return &cp, nil
		}
	}
	return nil, nil
}

func (w *world) OpenDiscrepancy(_ context.Context, _, _, chargeID uuid.UUID, kind string, _ map[string]any, _ time.Time) (bool, error) {
	for _, d := range w.discs {
		if d.open && d.chargeID == chargeID && d.kind == kind {
			return false, nil
		}
	}
	w.discs = append(w.discs, discrepancy{chargeID, kind, true})
	return true, nil
}

func (w *world) HasOpenDiscrepancy(context.Context, uuid.UUID) (bool, error) {
	for _, d := range w.discs {
		if d.open {
			return true, nil
		}
	}
	return false, nil
}

func (w *world) CountOpenDiscrepancies(context.Context) (int, error) {
	n := 0
	for _, d := range w.discs {
		if d.open {
			n++
		}
	}
	return n, nil
}

// --- helpers de escenario ---

// subscribe crea una suscripción Pro de 2 asientos con su cargo inicial pendiente.
func (w *world) subscribe(manualBilling bool) (subID, chargeID uuid.UUID) {
	w.t.Helper()
	sub, err := domain.Subscribe(uuid.New(), w.account.ID, domain.KindClinic, w.plan, 2, nil, domain.CycleMonthly,
		nil, manualBilling, true, 1, 0.19, w.now)
	if err != nil {
		w.t.Fatalf("subscribe: %v", err)
	}
	if _, err := w.Save(context.Background(), sub); err != nil {
		w.t.Fatal(err)
	}
	for id, c := range w.charges {
		if c.SubscriptionID == sub.ID() {
			return sub.ID(), id
		}
	}
	w.t.Fatal("sin cargo inicial")
	return uuid.Nil, uuid.Nil
}

func (w *world) status(subID uuid.UUID) domain.SubscriptionStatus {
	w.t.Helper()
	sub, err := w.Load(context.Background(), subID)
	if err != nil {
		w.t.Fatal(err)
	}
	return sub.Status()
}

func (w *world) kinds() []string {
	var out []string
	for _, d := range w.discs {
		if d.open {
			out = append(out, d.kind)
		}
	}
	return out
}

func (w *world) jobKeys(prefix string) int {
	n := 0
	for _, j := range w.jobs {
		if len(j.key) >= len(prefix) && j.key[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
}

// --- pruebas ---

func TestCollectCharge_PP_F5_ARQ_002_PaysAndIsIdempotent(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()

	must(t, w.svc.CollectCharge(ctx, chargeID))
	must(t, w.svc.CollectCharge(ctx, chargeID)) // reentrega del job

	if got := w.charges[chargeID].Status; got != "paid" {
		t.Fatalf("estado del cargo = %s, quiero paid", got)
	}
	if n := w.pay.ChargesCreated(); n != 1 {
		t.Fatalf("transacciones en el proveedor = %d, quiero 1", n)
	}
	if len(w.payments) != 1 || w.payments[0].Amount != w.charges[chargeID].Total || w.payments[0].Provider != payfake.Name {
		t.Fatalf("pagos = %+v", w.payments)
	}
	if st := w.status(subID); st != domain.StatusActive {
		t.Fatalf("suscripción = %s", st)
	}
}

func TestCollectCharge_PP_F5_ARQ_006_DeclineOpensGraceAndPaymentReactivates(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()

	w.pay.SetScenario(payfake.ScenarioDeclined)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if st := w.status(subID); st != domain.StatusGrace {
		t.Fatalf("tras el rechazo = %s, quiero grace", st)
	}
	first := *w.charges[chargeID].FirstFailedAt

	// Reintento d+3: sigue rechazado; el primer fallo no se mueve.
	w.now = t0.AddDate(0, 0, 3)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if !w.charges[chargeID].FirstFailedAt.Equal(first) {
		t.Fatal("el primer fallo no debe cambiar con los reintentos")
	}

	// Día 14: restricción de escritura.
	w.now = t0.AddDate(0, 0, 14)
	must(t, w.svc.RestrictForNonPayment(ctx, subID, chargeID))
	if st := w.status(subID); st != domain.StatusRestricted {
		t.Fatalf("día 14 = %s, quiero restricted", st)
	}

	// El cliente paga: reactivación inmediata.
	w.pay.SetScenario(payfake.ScenarioSuccess)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if st := w.status(subID); st != domain.StatusActive {
		t.Fatalf("tras pagar = %s, quiero active", st)
	}
	if n := w.pay.ChargesCreated(); n != 3 {
		t.Fatalf("intentos en el proveedor = %d, quiero 3", n)
	}
	// Los jobs de dunning que lleguen después no hacen nada.
	must(t, w.svc.RestrictForNonPayment(ctx, subID, chargeID))
	must(t, w.svc.CancelForNonPayment(ctx, subID, chargeID))
	if st := w.status(subID); st != domain.StatusActive {
		t.Fatalf("un job de dunning tardío cambió el estado a %s", st)
	}
}

func TestCancelForNonPayment_PP_F5_PRD_006_After60Days(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()
	w.pay.SetScenario(payfake.ScenarioDeclined)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	w.now = t0.AddDate(0, 0, 14)
	must(t, w.svc.RestrictForNonPayment(ctx, subID, chargeID))
	w.now = t0.AddDate(0, 0, 60)
	must(t, w.svc.CancelForNonPayment(ctx, subID, chargeID))
	if st := w.status(subID); st != domain.StatusCancelled {
		t.Fatalf("día 60 = %s, quiero cancelled", st)
	}
}

func TestCollectCharge_PP_F5_ARQ_002_ProviderDownIsNotADecline(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()

	w.pay.SetScenario(payfake.ScenarioFailure)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if st := w.status(subID); st != domain.StatusActive {
		t.Fatalf("proveedor caído no debe abrir gracia: %s", st)
	}
	if w.charges[chargeID].Status != "pending" || w.jobKeys("billing.collect-retry:") != 1 {
		t.Fatalf("debe quedar pendiente con reintento programado: %s, jobs=%+v", w.charges[chargeID].Status, w.jobs)
	}

	// Al volver el proveedor se reutiliza el mismo intento (misma referencia).
	w.pay.SetScenario(payfake.ScenarioSuccess)
	w.now = t0.Add(time.Hour)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if len(w.attempts[chargeID]) != 1 || w.charges[chargeID].Status != "paid" {
		t.Fatalf("intentos = %d, estado = %s", len(w.attempts[chargeID]), w.charges[chargeID].Status)
	}
}

func TestCollectCharge_PP_F5_ARQ_002_NoPaymentMethodFails(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	w.account.PaymentMethod = nil
	must(t, w.svc.CollectCharge(context.Background(), chargeID))
	if w.charges[chargeID].Status != "failed" || w.status(subID) != domain.StatusGrace || w.pay.ChargesCreated() != 0 {
		t.Fatalf("sin método de pago: cargo=%s sub=%s", w.charges[chargeID].Status, w.status(subID))
	}
}

func TestCollectCharge_PP_F5_PRD_009_ManualBillingWaitsForTransfer(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(true)
	ctx := context.Background()
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if w.charges[chargeID].Status != "pending" || w.pay.ChargesCreated() != 0 {
		t.Fatal("la facturación manual no debe cobrar en la pasarela")
	}
	total := w.charges[chargeID].Total
	if err := w.svc.RecordManualPayment(ctx, chargeID, total-1, "TRF-1"); !errors.Is(err, domain.ErrManualAmountMismatch) {
		t.Fatalf("importe distinto: %v", err)
	}
	must(t, w.svc.RecordManualPayment(ctx, chargeID, total, "TRF-1"))
	if w.charges[chargeID].Status != "paid" || w.payments[0].Provider != providers.ManualProvider || w.status(subID) != domain.StatusActive {
		t.Fatalf("pago manual no asentado: %+v", w.payments)
	}
}

func TestHandleWebhook_PP_F5_SEC_002_PendingThenPaidIsIdempotent(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()

	w.pay.SetScenario(payfake.ScenarioPending)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if w.charges[chargeID].Status != "pending" || w.jobKeys("billing.poll:") != 1 {
		t.Fatalf("pendiente sin consulta programada: jobs=%+v", w.jobs)
	}
	// Un segundo CollectCharge con el intento abierto no crea otra transacción.
	must(t, w.svc.CollectCharge(ctx, chargeID))
	if n := w.pay.ChargesCreated(); n != 1 {
		t.Fatalf("transacciones = %d, quiero 1", n)
	}

	ref := w.attempts[chargeID][0].ProviderRef
	headers, body, err := w.pay.Resolve(ref, providers.ChargePaid, w.now)
	must(t, err)

	out, err := w.svc.HandleWebhook(ctx, payfake.Name, headers, body)
	must(t, err)
	if out != WebhookProcessed || w.charges[chargeID].Status != "paid" {
		t.Fatalf("webhook = %s, cargo = %s", out, w.charges[chargeID].Status)
	}
	out, err = w.svc.HandleWebhook(ctx, payfake.Name, headers, body)
	must(t, err)
	if out != WebhookDuplicate || len(w.payments) != 1 {
		t.Fatalf("duplicado = %s, pagos = %d", out, len(w.payments))
	}
	if st := w.status(subID); st != domain.StatusActive {
		t.Fatalf("suscripción = %s", st)
	}
	// La consulta programada llega después: no cambia nada.
	must(t, w.svc.PollPayment(ctx, chargeID, 1, 1))
	if len(w.payments) != 1 {
		t.Fatal("la consulta tardía duplicó el pago")
	}
}

func TestHandleWebhook_PP_F5_SEC_002_RejectsBadSignatureAndStaleTimestamp(t *testing.T) {
	w := newWorld(t)
	_, chargeID := w.subscribe(false)
	ctx := context.Background()
	w.pay.SetScenario(payfake.ScenarioPending)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	ref := w.attempts[chargeID][0].ProviderRef

	headers, body, err := w.pay.Resolve(ref, providers.ChargePaid, w.now)
	must(t, err)

	tampered := append([]byte{}, body...)
	tampered[len(tampered)-2] ^= 1
	if _, err := w.svc.HandleWebhook(ctx, payfake.Name, headers, tampered); !errors.Is(err, providers.ErrInvalidSignature) {
		t.Fatalf("cuerpo alterado: %v", err)
	}
	if _, err := w.svc.HandleWebhook(ctx, payfake.Name, http.Header{}, body); !errors.Is(err, providers.ErrInvalidSignature) {
		t.Fatalf("sin firma: %v", err)
	}
	old, oldBody := w.pay.SignedWebhook(providers.Payment{Ref: ref, Reference: providers.Reference(chargeID, 1), Status: providers.ChargePaid}, w.now.Add(-time.Hour))
	if _, err := w.svc.HandleWebhook(ctx, payfake.Name, old, oldBody); !errors.Is(err, providers.ErrInvalidSignature) {
		t.Fatalf("fuera de ventana: %v", err)
	}
	if _, err := w.svc.HandleWebhook(ctx, "desconocido", headers, body); !errors.Is(err, providers.ErrUnknownProvider) {
		t.Fatalf("proveedor desconocido: %v", err)
	}
	if w.charges[chargeID].Status != "pending" {
		t.Fatal("un webhook rechazado no debe cambiar el cargo")
	}
}

func TestHandleWebhook_PP_F5_ARQ_002_NotReadyAndForeignReference(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	// Referencia con forma de Billing pero sin cargo: 409, el proveedor reintenta.
	p := providers.Payment{Reference: providers.Reference(uuid.New(), 1), Status: providers.ChargePaid, Amount: 100}
	h, b := w.pay.SignedWebhook(p, w.now)
	if _, err := w.svc.HandleWebhook(ctx, payfake.Name, h, b); !errors.Is(err, ErrChargeNotReady) {
		t.Fatalf("cargo no listo: %v", err)
	}
	// Referencia de otra integración: se acepta y se ignora.
	p.Reference = "pedido-123"
	h, b = w.pay.SignedWebhook(p, w.now)
	out, err := w.svc.HandleWebhook(ctx, payfake.Name, h, b)
	must(t, err)
	if out != WebhookIgnored {
		t.Fatalf("referencia ajena = %s", out)
	}
}

func TestHandleWebhook_PP_F5_SEC_002_StatusIsConfirmedWithProvider(t *testing.T) {
	w := newWorld(t)
	_, chargeID := w.subscribe(false)
	ctx := context.Background()
	w.pay.SetScenario(payfake.ScenarioPending)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	ref := w.attempts[chargeID][0].ProviderRef

	// Webhook firmado que afirma "pagado" mientras el proveedor sigue en pendiente:
	// manda lo que responde el proveedor.
	h, b := w.pay.SignedWebhook(providers.Payment{
		Ref: ref, Reference: providers.Reference(chargeID, 1), Status: providers.ChargePaid, Amount: w.charges[chargeID].Total, Currency: "COP",
	}, w.now)
	_, err := w.svc.HandleWebhook(ctx, payfake.Name, h, b)
	must(t, err)
	if w.charges[chargeID].Status != "pending" || len(w.payments) != 0 {
		t.Fatalf("el cargo se pagó sin confirmación del proveedor: %s", w.charges[chargeID].Status)
	}
}

func TestSettle_PP_F5_ARQ_002_AmountMismatchOpensDiscrepancyAndBlocksRestrict(t *testing.T) {
	w := newWorld(t)
	subID, chargeID := w.subscribe(false)
	ctx := context.Background()
	w.pay.SetScenario(payfake.ScenarioPending)
	must(t, w.svc.CollectCharge(ctx, chargeID))
	at := w.attempts[chargeID][0]

	// El proveedor dice haber cobrado otro importe.
	w.pay.SetPayment(providers.Payment{Ref: at.ProviderRef, Reference: providers.Reference(chargeID, 1), Status: providers.ChargePaid, Amount: 1, Currency: "COP"})
	must(t, w.svc.PollPayment(ctx, chargeID, 1, 1))
	if got := w.kinds(); len(got) != 1 || got[0] != infra.DiscrepancyAmountMismatch {
		t.Fatalf("discrepancias = %v", got)
	}
	if w.charges[chargeID].Status != "pending" || len(w.payments) != 0 {
		t.Fatal("un importe distinto no puede dar el cargo por pagado")
	}
	// La conciliación repetida no duplica la discrepancia.
	sum, err := w.svc.Reconcile(ctx)
	must(t, err)
	if sum.Discrepancies != 1 {
		t.Fatalf("discrepancias abiertas = %d, quiero 1", sum.Discrepancies)
	}

	// Con una discrepancia abierta la restricción se aplaza (ADR-022 §5).
	w.charges[chargeID].Status = "failed"
	must(t, w.svc.RestrictForNonPayment(ctx, subID, chargeID))
	if st := w.status(subID); st == domain.StatusRestricted {
		t.Fatal("no se debe restringir con una discrepancia abierta")
	}
	if w.jobKeys("billing.dunning-restrict:") != 1 {
		t.Fatalf("la restricción debe reprogramarse: %+v", w.jobs)
	}
}

func TestReconcile_PP_F5_ARQ_002_HealsLostWebhookAndFlagsDivergence(t *testing.T) {
	w := newWorld(t)
	_, lost := w.subscribe(false)
	ctx := context.Background()

	// Cargo 1: quedó pendiente y el webhook nunca llegó; el proveedor ya lo cobró.
	w.pay.SetScenario(payfake.ScenarioPending)
	must(t, w.svc.CollectCharge(ctx, lost))
	ref := w.attempts[lost][0].ProviderRef
	_, _, err := w.pay.Resolve(ref, providers.ChargePaid, w.now)
	must(t, err)

	// Cargo 2: pagado aquí, pero el proveedor lo anuló después.
	w.pay.SetScenario(payfake.ScenarioSuccess)
	_, voided := w.subscribe(false)
	must(t, w.svc.CollectCharge(ctx, voided))
	vref := w.attempts[voided][0].ProviderRef
	w.pay.SetPayment(providers.Payment{Ref: vref, Reference: providers.Reference(voided, 1), Status: providers.ChargeVoided, Amount: w.charges[voided].Total, Currency: "COP"})

	// Cargo 3: intento reservado hace dos días sin respuesta del proveedor.
	_, stuck := w.subscribe(false)
	_, err = w.BeginAttempt(ctx, stuck, payfake.Name, w.now.Add(-48*time.Hour))
	must(t, err)

	sum, err := w.svc.Reconcile(ctx)
	must(t, err)
	if w.charges[lost].Status != "paid" || sum.Healed != 1 {
		t.Fatalf("el webhook perdido no se recuperó: %s, healed=%d", w.charges[lost].Status, sum.Healed)
	}
	got := w.kinds()
	sort.Strings(got)
	if len(got) != 2 || got[0] != infra.DiscrepancyStuckAttempt || got[1] != infra.DiscrepancyVoidedAtProvider {
		t.Fatalf("discrepancias = %v", got)
	}
	// Segunda pasada: mismo resultado (idempotente).
	sum, err = w.svc.Reconcile(ctx)
	must(t, err)
	if sum.Discrepancies != 2 || sum.Healed != 0 || len(w.payments) != 2 {
		t.Fatalf("segunda pasada: %+v, pagos=%d", sum, len(w.payments))
	}
}

func TestRefundCharge_PP_F5_PRD_011_PartialThenRest(t *testing.T) {
	w := newWorld(t)
	_, chargeID := w.subscribe(false)
	ctx := context.Background()

	if _, err := w.svc.RefundCharge(ctx, chargeID, 1000, "prueba"); !errors.Is(err, domain.ErrChargeNotPaid) {
		t.Fatalf("reembolso de cargo sin pagar: %v", err)
	}
	must(t, w.svc.CollectCharge(ctx, chargeID))
	total := w.charges[chargeID].Total

	ref, err := w.svc.RefundCharge(ctx, chargeID, 50000, "cortesía")
	must(t, err)
	if ref == "" || w.charges[chargeID].Status != "partially_refunded" {
		t.Fatalf("reembolso parcial: ref=%q estado=%s", ref, w.charges[chargeID].Status)
	}
	if _, err := w.svc.RefundCharge(ctx, chargeID, total, "exceso"); !errors.Is(err, domain.ErrRefundExceeds) {
		t.Fatalf("reembolso que excede: %v", err)
	}
	_, err = w.svc.RefundCharge(ctx, chargeID, total-50000, "resto")
	must(t, err)
	if w.charges[chargeID].Status != "refunded" {
		t.Fatalf("estado = %s, quiero refunded", w.charges[chargeID].Status)
	}

	// Σ pagos − Σ reembolsos = Σ cargos − reembolsado (invariante de caja).
	var paid, refunded float64
	for _, p := range w.payments {
		if p.Status != "confirmed" {
			t.Fatalf("movimiento sin confirmar: %+v", p)
		}
		if p.Kind == "refund" {
			refunded += p.Amount
		} else {
			paid += p.Amount
		}
	}
	if paid != total || refunded != total {
		t.Fatalf("pagado=%v reembolsado=%v total=%v", paid, refunded, total)
	}
}

func TestRefundCharge_PP_F5_PRD_011_ProviderDownLeavesIntentForReconciliation(t *testing.T) {
	w := newWorld(t)
	_, chargeID := w.subscribe(false)
	ctx := context.Background()
	must(t, w.svc.CollectCharge(ctx, chargeID))

	w.pay.SetScenario(payfake.ScenarioFailure)
	if _, err := w.svc.RefundCharge(ctx, chargeID, 1000, "x"); !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("proveedor caído: %v", err)
	}
	// No se sabe si el dinero salió: el segundo intento se bloquea y la
	// conciliación lo señala.
	if _, err := w.svc.RefundCharge(ctx, chargeID, 1000, "x"); !errors.Is(err, ErrRefundInProgress) {
		t.Fatalf("segundo reembolso: %v", err)
	}
	if w.charges[chargeID].Refunded != 0 {
		t.Fatal("no debe registrarse el reembolso sin confirmación")
	}
	w.pay.SetScenario(payfake.ScenarioSuccess)
	w.now = t0.Add(2 * time.Hour)
	_, err := w.svc.Reconcile(ctx)
	must(t, err)
	found := false
	for _, k := range w.kinds() {
		found = found || k == infra.DiscrepancyRefundUnconfirmed
	}
	if !found {
		t.Fatalf("falta la discrepancia refund_unconfirmed: %v", w.kinds())
	}
}

func TestRenewSubscription_PP_F5_DOM_012_OnlyAtPeriodEnd(t *testing.T) {
	w := newWorld(t)
	subID, first := w.subscribe(false)
	ctx := context.Background()
	must(t, w.svc.CollectCharge(ctx, first))
	end := t0.AddDate(0, 1, 0)

	// Job adelantado: nada.
	must(t, w.svc.RenewSubscription(ctx, subID, end))
	if len(w.charges) != 1 {
		t.Fatal("no se debe renovar antes del fin de periodo")
	}
	w.now = end
	must(t, w.svc.RenewSubscription(ctx, subID, end))
	must(t, w.svc.RenewSubscription(ctx, subID, end)) // reentrega: el periodo ya avanzó
	if len(w.charges) != 2 {
		t.Fatalf("cargos = %d, quiero 2 (una sola renovación)", len(w.charges))
	}
	sub, err := w.Load(ctx, subID)
	must(t, err)
	if !sub.CurrentPeriod().End.Equal(end.AddDate(0, 1, 0)) {
		t.Fatalf("fin de periodo = %s", sub.CurrentPeriod().End)
	}
}

func TestAdjustSubscription_PP_F5_PRD_011_CreditCoversNextRenewal(t *testing.T) {
	w := newWorld(t)
	subID, first := w.subscribe(false)
	ctx := context.Background()
	must(t, w.svc.CollectCharge(ctx, first))

	must(t, w.svc.AdjustSubscription(ctx, subID, AdjustCmd{Kind: "credit", Amount: 1_000_000, Reason: "compensación por incidencia"}))
	if err := w.svc.AdjustSubscription(ctx, subID, AdjustCmd{Kind: "otro"}); !errors.Is(err, domain.ErrInvalidAdjustment) {
		t.Fatalf("ajuste desconocido: %v", err)
	}

	end := t0.AddDate(0, 1, 0)
	w.now = end
	must(t, w.svc.RenewSubscription(ctx, subID, end))
	var renewal uuid.UUID
	for id := range w.charges {
		if id != first {
			renewal = id
		}
	}
	if w.charges[renewal].Total != 0 {
		t.Fatalf("total de la renovación = %v, quiero 0 (cubierta por el crédito)", w.charges[renewal].Total)
	}
	// Cargo a cero: se da por pagado sin tocar al proveedor ni asentar pago.
	before := w.pay.ChargesCreated()
	must(t, w.svc.CollectCharge(ctx, renewal))
	if w.charges[renewal].Status != "paid" || w.pay.ChargesCreated() != before || len(w.payments) != 1 {
		t.Fatalf("cargo a cero: estado=%s pagos=%d", w.charges[renewal].Status, len(w.payments))
	}
}
