package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// Errores de aplicación del cobro.
var (
	// ErrChargeNotReady: el cargo aún no está proyectado. El webhook responde 409
	// y el proveedor reintenta (ARQ-002); un job se reprograma.
	ErrChargeNotReady = errors.New("billing: el cargo aún no está disponible")
	// ErrDuplicatePaymentRef: la referencia del pago manual ya se usó.
	ErrDuplicatePaymentRef = errors.New("billing: la referencia de pago ya está registrada")
	// ErrRefundInProgress: hay otro reembolso del mismo cargo sin confirmar.
	ErrRefundInProgress = errors.New("billing: hay un reembolso en curso para este cargo")
)

// SubscriptionStore persiste el agregado Subscription dentro del UnitOfWork.
type SubscriptionStore interface {
	Load(ctx context.Context, id uuid.UUID) (*domain.Subscription, error)
	Save(ctx context.Context, sub *domain.Subscription) ([]eventstore.StoredEvent, error)
}

// BillingReader son las lecturas que el cobro necesita de los read models.
type BillingReader interface {
	GetAccount(ctx context.Context, id uuid.UUID) (*infra.AccountView, error)
	GetPlan(ctx context.Context, code string) (*domain.PlanDefinition, error)
	// PlanVersion lee una versión concreta en cualquier estado (nil si no existe).
	PlanVersion(ctx context.Context, code string, version int) (*domain.PlanDefinition, error)
	ActiveProfessionals(ctx context.Context, orgID uuid.UUID) (int, error)
}

// PaymentLedger es el registro operativo del cobro (infra.Ledger).
type PaymentLedger interface {
	Charge(ctx context.Context, chargeID uuid.UUID) (*infra.ChargeRef, error)
	LockCharge(ctx context.Context, chargeID uuid.UUID) error
	OpenAttempt(ctx context.Context, chargeID uuid.UUID) (*infra.Attempt, error)
	Attempt(ctx context.Context, chargeID uuid.UUID, number int) (*infra.Attempt, error)
	BeginAttempt(ctx context.Context, chargeID uuid.UUID, provider string, now time.Time) (infra.Attempt, error)
	UpdateAttempt(ctx context.Context, chargeID uuid.UUID, number int, status, providerRef, errorCode string, now time.Time) (bool, error)
	LatestAttemptNumber(ctx context.Context, chargeID uuid.UUID) (int, error)
	AttemptsSince(ctx context.Context, since time.Time) ([]infra.Attempt, error)
	ClaimProviderEvent(ctx context.Context, provider, eventID string, now time.Time) (bool, error)
	MarkProviderEvent(ctx context.Context, provider, eventID, result string, now time.Time) error
	InsertPayment(ctx context.Context, p infra.PaymentRow) (bool, error)
	ConfirmRefund(ctx context.Context, id uuid.UUID, providerRef string, now time.Time) error
	DeletePendingRefund(ctx context.Context, id uuid.UUID) error
	HasPendingRefund(ctx context.Context, chargeID uuid.UUID) (bool, error)
	PendingRefunds(ctx context.Context, before time.Time) ([]infra.PaymentRow, error)
	PaymentOfCharge(ctx context.Context, chargeID uuid.UUID) (*infra.PaymentRow, error)
	OpenDiscrepancy(ctx context.Context, id, accountID, chargeID uuid.UUID, kind string, details map[string]any, now time.Time) (bool, error)
	HasOpenDiscrepancy(ctx context.Context, accountID uuid.UUID) (bool, error)
	CountOpenDiscrepancies(ctx context.Context) (int, error)
}

// pollDelays es el calendario de consulta de un intento pendiente; después, la
// conciliación diaria lo recoge.
var pollDelays = []time.Duration{2 * time.Minute, 10 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}

// Ventanas del cobro y la conciliación.
const (
	providerDownWindow = 72 * time.Hour      // se reintenta un cobro con el proveedor caído
	reconcileWindow    = 35 * 24 * time.Hour // ARQ-002
	stuckAfter         = 24 * time.Hour
	restrictDeferral   = 24 * time.Hour
)

// PaymentService orquesta el cobro real (PP-F5-ARQ-002, ARQ-006, DOM-012, SEC-002):
// intentos idempotentes en el proveedor, webhooks, dunning, pagos manuales,
// reembolsos y conciliación. La llamada al proveedor siempre ocurre fuera de la
// transacción de base de datos.
type PaymentService struct {
	subs    SubscriptionStore
	reader  BillingReader
	ledger  PaymentLedger
	router  *providers.Router
	uow     UnitOfWork
	runner  ProjectionRunner
	jobs    CommandScheduler
	metrics *Metrics
	logger  *slog.Logger
	clock   func() time.Time
	newID   func() uuid.UUID
}

// PaymentOptions son las dependencias de PaymentService.
type PaymentOptions struct {
	Subscriptions SubscriptionStore
	Reader        BillingReader
	Ledger        PaymentLedger
	Router        *providers.Router
	UoW           UnitOfWork
	Projects      ProjectionRunner
	Jobs          CommandScheduler
	Metrics       *Metrics
	Logger        *slog.Logger
	Clock         func() time.Time
	NewID         func() uuid.UUID
}

// NewPaymentService construye el servicio de cobro.
func NewPaymentService(o PaymentOptions) *PaymentService {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.NewID == nil {
		o.NewID = uuid.New
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &PaymentService{
		subs: o.Subscriptions, reader: o.Reader, ledger: o.Ledger, router: o.Router, uow: o.UoW,
		runner: o.Projects, jobs: o.Jobs, metrics: o.Metrics, logger: o.Logger, clock: o.Clock, newID: o.NewID,
	}
}

func (s *PaymentService) now() time.Time { return s.clock().UTC() }

func (s *PaymentService) save(ctx context.Context, sub *domain.Subscription) error {
	events, err := s.subs.Save(ctx, sub)
	if err != nil {
		return err
	}
	if s.runner != nil && len(events) > 0 {
		return s.runner.Apply(ctx, events)
	}
	return nil
}

func collectable(status string) bool {
	return status == string(domain.ChargeStatusPending) || status == string(domain.ChargeStatusFailed)
}

// CollectCharge intenta cobrar el cargo. Es idempotente: un cargo ya resuelto no
// se toca y, si hay un intento abierto, se consulta en vez de cobrar otra vez.
func (s *PaymentService) CollectCharge(ctx context.Context, chargeID uuid.UUID) error {
	now := s.now()
	ch, err := s.ledger.Charge(ctx, chargeID)
	if err != nil {
		return err
	}
	if ch == nil {
		return ErrChargeNotReady
	}
	if !collectable(ch.Status) {
		return nil
	}
	if ch.Total <= 0 {
		// Cargo cubierto por cupón o crédito: no hay nada que pedir al proveedor.
		return s.uow.Do(ctx, func(ctx context.Context) error { return s.markPaid(ctx, ch, "none", "") })
	}
	if ch.ManualBilling {
		return nil // espera la conciliación manual (PRD-009)
	}
	acc, err := s.reader.GetAccount(ctx, ch.AccountID)
	if err != nil {
		return err
	}
	if acc == nil {
		return ErrChargeNotReady
	}
	pm := acc.PaymentMethod
	if pm == nil {
		return s.uow.Do(ctx, func(ctx context.Context) error { return s.markFailed(ctx, ch, "NO_PAYMENT_METHOD") })
	}
	if pm.Kind == "manual" {
		return nil
	}
	if pm.Kind == MethodPSE {
		return s.collectPSE(ctx, ch)
	}
	prov, err := s.router.PaymentByName(pm.Provider)
	if err != nil {
		return err
	}

	at, err := s.ledger.BeginAttempt(ctx, chargeID, prov.Name(), now)
	if err != nil {
		return err
	}
	if at.Status == infra.AttemptPending || at.Provider != prov.Name() {
		// Hay un intento anterior en curso: se resuelve ese antes de abrir otro.
		return s.pollAttempt(ctx, ch, at, 0)
	}

	req := providers.ChargeRequest{
		ChargeID: chargeID, Attempt: at.Number, Amount: ch.Total, Currency: ch.Currency,
		Email: acc.LegalEntity.Email, Token: pm.Token,
	}
	res, err := prov.Charge(ctx, req)
	if err != nil {
		s.metrics.charge("error", prov.Name())
		if errors.Is(err, providers.ErrUnavailable) && now.Sub(at.CreatedAt) < providerDownWindow {
			// Proveedor caído no es un rechazo (RN-33): mismo intento, más tarde.
			return s.uow.Do(ctx, func(ctx context.Context) error {
				return s.jobs.ScheduleCommand(ctx, now.Add(time.Hour), CollectCharge{ChargeID: chargeID},
					fmt.Sprintf("billing.collect-retry:%s:%d", chargeID, now.Unix()/3600))
			})
		}
		return fmt.Errorf("billing: cobro del cargo %s: %w", chargeID, err)
	}
	return s.settle(ctx, ch.ID, at, providers.Payment{
		Ref: res.Ref, Reference: req.Reference(), Status: res.Status,
		Amount: ch.Total, Currency: ch.Currency, ErrorCode: res.ErrorCode,
	}, 0)
}

// PollPayment consulta el desenlace de un intento pendiente.
func (s *PaymentService) PollPayment(ctx context.Context, chargeID uuid.UUID, attempt, try int) error {
	ch, err := s.ledger.Charge(ctx, chargeID)
	if err != nil || ch == nil {
		return err
	}
	at, err := s.ledger.Attempt(ctx, chargeID, attempt)
	if err != nil || at == nil || !at.Open() {
		return err
	}
	return s.pollAttempt(ctx, ch, *at, try)
}

func (s *PaymentService) pollAttempt(ctx context.Context, ch *infra.ChargeRef, at infra.Attempt, try int) error {
	if at.ProviderRef == "" {
		return nil // sin id de transacción solo el webhook o la conciliación lo resuelven
	}
	prov, err := s.router.PaymentByName(at.Provider)
	if err != nil {
		return err
	}
	p, err := prov.GetPayment(ctx, at.ProviderRef)
	if errors.Is(err, providers.ErrNotSupported) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("billing: consulta del pago del cargo %s: %w", ch.ID, err)
	}
	return s.settle(ctx, ch.ID, at, p, try)
}

func (s *PaymentService) settle(ctx context.Context, chargeID uuid.UUID, at infra.Attempt, p providers.Payment, try int) error {
	return s.uow.Do(ctx, func(ctx context.Context) error { return s.settleTx(ctx, chargeID, at, p, try) })
}

func sameMoney(p providers.Payment, ch *infra.ChargeRef) bool {
	return domain.RoundCents(p.Amount) == domain.RoundCents(ch.Total) && (p.Currency == "" || p.Currency == ch.Currency)
}

// settleTx aplica el estado que el proveedor da a un intento. Corre dentro del
// UnitOfWork: intento, agregado, proyección y pago se confirman juntos.
func (s *PaymentService) settleTx(ctx context.Context, chargeID uuid.UUID, at infra.Attempt, p providers.Payment, try int) error {
	now := s.now()
	ch, err := s.ledger.Charge(ctx, chargeID)
	if err != nil {
		return err
	}
	if ch == nil {
		return ErrChargeNotReady
	}
	switch p.Status {
	case providers.ChargePending:
		if _, err := s.ledger.UpdateAttempt(ctx, ch.ID, at.Number, infra.AttemptPending, p.Ref, "", now); err != nil {
			return err
		}
		if try >= len(pollDelays) {
			return nil
		}
		return s.jobs.ScheduleCommand(ctx, now.Add(pollDelays[try]),
			PollPayment{ChargeID: ch.ID, Attempt: at.Number, Try: try + 1},
			fmt.Sprintf("billing.poll:%s:%d:%d", ch.ID, at.Number, try+1))

	case providers.ChargePaid:
		if !sameMoney(p, ch) {
			// El intento queda abierto: no se vuelve a cobrar hasta que se resuelva.
			return s.flag(ctx, ch, infra.DiscrepancyAmountMismatch, map[string]any{
				"attempt": at.Number, "provider": at.Provider, "provider_ref": p.Ref,
				"expected": ch.Total, "got": p.Amount, "currency": p.Currency,
			})
		}
		changed, err := s.ledger.UpdateAttempt(ctx, ch.ID, at.Number, infra.AttemptPaid, p.Ref, "", now)
		if err != nil {
			return err
		}
		if !changed {
			cur, err := s.ledger.Attempt(ctx, ch.ID, at.Number)
			if err != nil || (cur != nil && cur.Status == infra.AttemptPaid) {
				return err // resultado duplicado
			}
			return s.flag(ctx, ch, infra.DiscrepancyPaidAtProvider, map[string]any{
				"attempt": at.Number, "provider": at.Provider, "provider_ref": p.Ref, "amount": p.Amount,
			})
		}
		if !collectable(ch.Status) {
			// El cargo ya se dio por pagado por otra vía: cobro doble a revisar.
			return s.flag(ctx, ch, infra.DiscrepancyPaidAtProvider, map[string]any{
				"attempt": at.Number, "provider": at.Provider, "provider_ref": p.Ref, "amount": p.Amount, "charge_status": ch.Status,
			})
		}
		return s.markPaid(ctx, ch, at.Provider, p.Ref)

	case providers.ChargeFailed:
		changed, err := s.ledger.UpdateAttempt(ctx, ch.ID, at.Number, infra.AttemptFailed, p.Ref, p.ErrorCode, now)
		if err != nil || !changed {
			return err
		}
		return s.failIfLatest(ctx, ch, at.Number, orDefault(p.ErrorCode, "PAYMENT_DECLINED"))

	case providers.ChargeVoided:
		changed, err := s.ledger.UpdateAttempt(ctx, ch.ID, at.Number, infra.AttemptVoided, p.Ref, "VOIDED", now)
		if err != nil {
			return err
		}
		if !changed {
			cur, err := s.ledger.Attempt(ctx, ch.ID, at.Number)
			if err != nil || cur == nil || cur.Status != infra.AttemptPaid {
				return err
			}
			return s.flag(ctx, ch, infra.DiscrepancyVoidedAtProvider, map[string]any{
				"attempt": at.Number, "provider": at.Provider, "provider_ref": p.Ref,
			})
		}
		return s.failIfLatest(ctx, ch, at.Number, "VOIDED")
	}
	return fmt.Errorf("billing: estado de pago desconocido %q", p.Status)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// failIfLatest registra el fallo en el agregado solo si el intento es el último:
// el rechazo tardío de un intento antiguo no reabre el dunning.
func (s *PaymentService) failIfLatest(ctx context.Context, ch *infra.ChargeRef, attempt int, code string) error {
	latest, err := s.ledger.LatestAttemptNumber(ctx, ch.ID)
	if err != nil || latest != attempt {
		return err
	}
	return s.markFailed(ctx, ch, code)
}

func (s *PaymentService) markPaid(ctx context.Context, ch *infra.ChargeRef, provider, ref string) error {
	now := s.now()
	sub, err := s.subs.Load(ctx, ch.SubscriptionID)
	if err != nil {
		return err
	}
	if err := sub.RecordChargePaid(ch.ID, provider, ref, now); err != nil {
		return err
	}
	if err := s.save(ctx, sub); err != nil {
		return err
	}
	if ch.Total > 0 {
		if _, err := s.ledger.InsertPayment(ctx, infra.PaymentRow{
			ID: s.newID(), ChargeID: ch.ID, AccountID: ch.AccountID, Provider: provider, ProviderRef: ref,
			Kind: "payment", Amount: ch.Total, Currency: ch.Currency, Status: "confirmed", At: now,
		}); err != nil {
			return err
		}
	}
	s.metrics.charge("paid", provider)
	return nil
}

func (s *PaymentService) markFailed(ctx context.Context, ch *infra.ChargeRef, code string) error {
	sub, err := s.subs.Load(ctx, ch.SubscriptionID)
	if err != nil {
		return err
	}
	if err := sub.RecordChargeFailed(ch.ID, code, s.now()); err != nil {
		return err
	}
	if err := s.save(ctx, sub); err != nil {
		return err
	}
	s.metrics.charge("failed", orDefault(ch.Provider, "none"))
	return nil
}

// flag abre una discrepancia de conciliación (ids e importes; nunca PII).
func (s *PaymentService) flag(ctx context.Context, ch *infra.ChargeRef, kind string, details map[string]any) error {
	opened, err := s.ledger.OpenDiscrepancy(ctx, s.newID(), ch.AccountID, ch.ID, kind, details, s.now())
	if err != nil {
		return err
	}
	if opened {
		s.metrics.discrepancy(kind)
		s.logger.WarnContext(ctx, "billing discrepancy opened", "kind", kind, "charge_id", ch.ID.String(), "account_id", ch.AccountID.String())
	}
	return nil
}

// WebhookOutcome es el resultado de procesar un webhook de pagos.
type WebhookOutcome string

// Resultados de un webhook.
const (
	WebhookProcessed WebhookOutcome = "processed"
	WebhookDuplicate WebhookOutcome = "duplicate"
	WebhookIgnored   WebhookOutcome = "ignored"
)

// HandleWebhook procesa un webhook de pagos (PP-F5-API-004, SEC-002): firma y
// ventana temporal verificadas por el adaptador, idempotencia por
// provider_event_id en la misma transacción que sus efectos, y estado
// contrastado con el proveedor cuando este permite consultarlo.
//
// Errores: providers.ErrUnknownProvider, providers.ErrInvalidSignature,
// ErrChargeNotReady (409: el proveedor reintenta).
func (s *PaymentService) HandleWebhook(ctx context.Context, provider string, headers http.Header, body []byte) (WebhookOutcome, error) {
	now := s.now()
	prov, err := s.router.PaymentByName(provider)
	if err != nil {
		return "", err
	}
	ev, err := prov.ParseWebhook(headers, body, now)
	if errors.Is(err, providers.ErrIgnoredEvent) {
		s.metrics.webhook(provider, "ignored")
		return WebhookIgnored, nil
	}
	if err != nil {
		s.metrics.webhook(provider, "rejected")
		return "", err
	}
	chargeID, attemptNo, err := providers.ParseReference(ev.Payment.Reference)
	if err != nil {
		// Transacción ajena a Billing (otra integración de la misma cuenta del proveedor).
		s.metrics.webhook(provider, "unknown_reference")
		return WebhookIgnored, nil
	}
	ch, err := s.ledger.Charge(ctx, chargeID)
	if err != nil {
		return "", err
	}
	at, err := s.ledger.Attempt(ctx, chargeID, attemptNo)
	if err != nil {
		return "", err
	}
	if ch == nil || at == nil {
		s.metrics.webhook(provider, "not_ready")
		return "", ErrChargeNotReady
	}
	if at.Provider != provider {
		s.metrics.webhook(provider, "provider_mismatch")
		return WebhookIgnored, nil
	}

	payment := ev.Payment
	if payment.Ref != "" {
		// Nunca se actúa sobre un único webhook (ADR-022 §5): el estado se
		// contrasta con el proveedor cuando ofrece consulta.
		confirmed, err := prov.GetPayment(ctx, payment.Ref)
		switch {
		case errors.Is(err, providers.ErrNotSupported):
		case err != nil:
			return "", fmt.Errorf("billing: verificación del webhook: %w", err)
		case confirmed.Reference != "" && confirmed.Reference != payment.Reference:
			s.metrics.webhook(provider, "rejected")
			return "", providers.ErrInvalidSignature
		default:
			confirmed.Reference = payment.Reference
			payment = confirmed
		}
	}

	outcome := WebhookProcessed
	err = s.uow.Do(ctx, func(ctx context.Context) error {
		fresh, err := s.ledger.ClaimProviderEvent(ctx, provider, ev.ID, now)
		if err != nil {
			return err
		}
		if !fresh {
			outcome = WebhookDuplicate
			return nil
		}
		if err := s.settleTx(ctx, chargeID, *at, payment, 0); err != nil {
			return err
		}
		return s.ledger.MarkProviderEvent(ctx, provider, ev.ID, string(payment.Status), now)
	})
	if err != nil {
		return "", err
	}
	s.metrics.webhook(provider, string(outcome))
	return outcome, nil
}

// RecordManualPayment asienta una transferencia recibida (PRD-009, PRD-011).
func (s *PaymentService) RecordManualPayment(ctx context.Context, chargeID uuid.UUID, amount float64, ref string) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		ch, err := s.ledger.Charge(ctx, chargeID)
		if err != nil {
			return err
		}
		if ch == nil {
			return domain.ErrChargeNotFound
		}
		sub, err := s.subs.Load(ctx, ch.SubscriptionID)
		if err != nil {
			return err
		}
		if err := sub.RecordManualPayment(chargeID, amount, ref, now); err != nil {
			return err
		}
		if err := s.save(ctx, sub); err != nil {
			return err
		}
		inserted, err := s.ledger.InsertPayment(ctx, infra.PaymentRow{
			ID: s.newID(), ChargeID: ch.ID, AccountID: ch.AccountID, Provider: providers.ManualProvider, ProviderRef: ref,
			Kind: "payment", Amount: ch.Total, Currency: ch.Currency, Status: "confirmed", At: now,
		})
		if err != nil {
			return err
		}
		if !inserted {
			return ErrDuplicatePaymentRef
		}
		s.metrics.charge("paid", providers.ManualProvider)
		return nil
	})
}

// RefundCharge reembolsa total o parcialmente un cargo pagado (PRD-011, B8). El
// reembolso se anota como solicitado antes de llamar al proveedor: si la
// confirmación se pierde, la conciliación lo señala en vez de olvidarlo.
func (s *PaymentService) RefundCharge(ctx context.Context, chargeID uuid.UUID, amount float64, reason string) (string, error) {
	now := s.now()
	amount = domain.RoundCents(amount)
	intent := s.newID()
	var (
		ch  *infra.ChargeRef
		pay *infra.PaymentRow
	)
	err := s.uow.Do(ctx, func(ctx context.Context) error {
		if err := s.ledger.LockCharge(ctx, chargeID); err != nil {
			return err
		}
		var err error
		if ch, err = s.ledger.Charge(ctx, chargeID); err != nil {
			return err
		}
		if ch == nil {
			return domain.ErrChargeNotFound
		}
		if pending, err := s.ledger.HasPendingRefund(ctx, chargeID); err != nil || pending {
			if err == nil {
				err = ErrRefundInProgress
			}
			return err
		}
		sub, err := s.subs.Load(ctx, ch.SubscriptionID)
		if err != nil {
			return err
		}
		if err := sub.CheckRefundable(chargeID, amount); err != nil {
			return err
		}
		if pay, err = s.ledger.PaymentOfCharge(ctx, chargeID); err != nil {
			return err
		}
		if pay == nil {
			return domain.ErrChargeNotPaid
		}
		_, err = s.ledger.InsertPayment(ctx, infra.PaymentRow{
			ID: intent, ChargeID: ch.ID, AccountID: ch.AccountID, Provider: pay.Provider,
			ProviderRef: "requested:" + intent.String(), Kind: "refund", Amount: amount,
			Currency: ch.Currency, Status: "requested", Reason: reason, At: now,
		})
		return err
	})
	if err != nil {
		return "", err
	}

	prov, err := s.router.PaymentByName(pay.Provider)
	var refundRef string
	if err == nil {
		refundRef, err = prov.Refund(ctx, providers.RefundRequest{
			RefundID: intent, ChargeID: chargeID, PaymentRef: pay.ProviderRef, Amount: amount,
			Full: amount == domain.RoundCents(ch.Total-ch.Refunded), Reason: reason,
		})
	}
	if err != nil {
		if !errors.Is(err, providers.ErrUnavailable) {
			// Rechazo definitivo: no hubo movimiento de dinero.
			if delErr := s.ledger.DeletePendingRefund(ctx, intent); delErr != nil {
				return "", errors.Join(err, delErr)
			}
		}
		return "", fmt.Errorf("billing: reembolso del cargo %s: %w", chargeID, err)
	}

	err = s.uow.Do(ctx, func(ctx context.Context) error {
		sub, err := s.subs.Load(ctx, ch.SubscriptionID)
		if err != nil {
			return err
		}
		if err := sub.RefundCharge(chargeID, amount, reason, pay.Provider, refundRef, s.now()); err != nil {
			return err
		}
		if err := s.save(ctx, sub); err != nil {
			return err
		}
		return s.ledger.ConfirmRefund(ctx, intent, refundRef, s.now())
	})
	if err != nil {
		return "", err
	}
	s.metrics.charge("refunded", pay.Provider)
	return refundRef, nil
}

// RestrictForNonPayment aplica la restricción del día 14. Con una discrepancia
// abierta en la cuenta se aplaza: nunca se restringe sobre datos dudosos.
func (s *PaymentService) RestrictForNonPayment(ctx context.Context, subID, chargeID uuid.UUID) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		ch, err := s.ledger.Charge(ctx, chargeID)
		if err != nil || ch == nil || ch.Status != string(domain.ChargeStatusFailed) {
			return err
		}
		open, err := s.ledger.HasOpenDiscrepancy(ctx, ch.AccountID)
		if err != nil {
			return err
		}
		if open {
			at := now.Add(restrictDeferral)
			return s.jobs.ScheduleCommand(ctx, at, RestrictForNonPayment{SubscriptionID: subID, ChargeID: chargeID},
				DunningRestrictKey(chargeID)+":"+at.Format("20060102"))
		}
		sub, err := s.subs.Load(ctx, subID)
		if err != nil {
			return err
		}
		if err := sub.RestrictForNonPayment(chargeID, now); err != nil {
			return err
		}
		return s.save(ctx, sub)
	})
}

// CancelForNonPayment cancela definitivamente tras 60 días de impago.
func (s *PaymentService) CancelForNonPayment(ctx context.Context, subID, chargeID uuid.UUID) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		ch, err := s.ledger.Charge(ctx, chargeID)
		if err != nil || ch == nil || ch.Status != string(domain.ChargeStatusFailed) {
			return err
		}
		open, err := s.ledger.HasOpenDiscrepancy(ctx, ch.AccountID)
		if err != nil {
			return err
		}
		if open {
			at := now.Add(restrictDeferral)
			return s.jobs.ScheduleCommand(ctx, at, CancelForNonPayment{SubscriptionID: subID, ChargeID: chargeID},
				DunningCancelKey(chargeID)+":"+at.Format("20060102"))
		}
		sub, err := s.subs.Load(ctx, subID)
		if err != nil {
			return err
		}
		if err := sub.CancelForNonPayment(chargeID, now); err != nil {
			return err
		}
		return s.save(ctx, sub)
	})
}

// RenewSubscription renueva el periodo que termina en periodEnd. Un job antiguo
// (el periodo ya avanzó) o adelantado no hace nada.
func (s *PaymentService) RenewSubscription(ctx context.Context, subID uuid.UUID, periodEnd time.Time) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		sub, err := s.subs.Load(ctx, subID)
		if err != nil {
			return err
		}
		end := sub.CurrentPeriod().End
		if !end.Equal(periodEnd) || end.After(now) {
			return nil
		}
		switch sub.Status() {
		case domain.StatusActive, domain.StatusGrace, domain.StatusRestricted:
		default:
			return nil
		}
		planCode := sub.PlanCode()
		if pc := sub.PendingChange(); pc != nil {
			planCode = pc.PlanCode
		}
		plan, err := s.reader.GetPlan(ctx, planCode)
		if errors.Is(err, infra.ErrPlanNotFound) && sub.PendingChange() == nil {
			// Plan retirado del catálogo: la suscripción conserva la versión que
			// fijó al contratar (B10).
			plan, err = s.reader.PlanVersion(ctx, planCode, sub.PlanVersion())
			if err == nil && plan == nil {
				err = infra.ErrPlanNotFound
			}
		}
		if err != nil {
			return err
		}
		acc, err := s.reader.GetAccount(ctx, sub.AccountID())
		if err != nil {
			return err
		}
		if acc == nil {
			return ErrChargeNotReady
		}
		professionals := 0
		for _, org := range acc.Organizations {
			n, err := s.reader.ActiveProfessionals(ctx, org)
			if err != nil {
				return err
			}
			professionals += n
		}
		route, err := s.router.Route(ctx, acc.Country)
		if err != nil {
			return err
		}
		if err := sub.RenewPeriod(*plan, professionals, route.TaxRate, now); err != nil {
			return err
		}
		return s.save(ctx, sub)
	})
}

// ExpireTrial vence la prueba si sigue en curso y ya pasó su fecha de fin.
func (s *PaymentService) ExpireTrial(ctx context.Context, subID uuid.UUID) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		sub, err := s.subs.Load(ctx, subID)
		if err != nil {
			return err
		}
		if end := sub.TrialEndsAt(); end == nil || end.After(now) {
			return nil // la prueba se extendió: su job nuevo lo vencerá
		}
		if err := sub.ExpireTrial(now); err != nil {
			return err
		}
		return s.save(ctx, sub)
	})
}

// AdjustCmd es un ajuste administrativo de SuperAdmin (PRD-011, API-007).
type AdjustCmd struct {
	Kind   string     // extend_trial | credit | comp_plan
	Until  *time.Time // extend_trial, comp_plan
	Amount float64    // credit
	Reason string
}

// AdjustSubscription aplica un ajuste administrativo a la suscripción.
func (s *PaymentService) AdjustSubscription(ctx context.Context, subID uuid.UUID, cmd AdjustCmd) error {
	now := s.now()
	return s.uow.Do(ctx, func(ctx context.Context) error {
		sub, err := s.subs.Load(ctx, subID)
		if err != nil {
			return err
		}
		switch cmd.Kind {
		case "extend_trial":
			if cmd.Until == nil {
				return fmt.Errorf("%w: falta la fecha", domain.ErrInvalidAdjustment)
			}
			err = sub.ExtendTrial(*cmd.Until, cmd.Reason, now)
		case "credit":
			err = sub.GrantCredit(cmd.Amount, cmd.Reason, now)
		case "comp_plan":
			if cmd.Until == nil {
				return fmt.Errorf("%w: falta la fecha", domain.ErrInvalidAdjustment)
			}
			err = sub.ApplyCoupon(domain.CouponApplied{Code: "COMP", Kind: "pct", Value: 100, Until: cmd.Until}, now)
		default:
			err = fmt.Errorf("%w: tipo %q", domain.ErrInvalidAdjustment, cmd.Kind)
		}
		if err != nil {
			return err
		}
		return s.save(ctx, sub)
	})
}

// ReconcileSummary resume una pasada de conciliación.
type ReconcileSummary struct {
	Checked       int
	Healed        int // intentos abiertos que el proveedor ya había resuelto
	Errors        int // consultas al proveedor que fallaron
	Discrepancies int // discrepancias abiertas al terminar
}

// Reconcile compara los intentos de los últimos 35 días con el proveedor
// (ARQ-002): resuelve los que quedaron abiertos y abre discrepancias donde los
// dos lados no coinciden. Es idempotente; se ejecuta a diario.
func (s *PaymentService) Reconcile(ctx context.Context) (ReconcileSummary, error) {
	now := s.now()
	var sum ReconcileSummary
	attempts, err := s.ledger.AttemptsSince(ctx, now.Add(-reconcileWindow))
	if err != nil {
		return sum, err
	}
	for _, at := range attempts {
		sum.Checked++
		if err := s.reconcileAttempt(ctx, at, now, &sum); err != nil {
			sum.Errors++
			s.logger.WarnContext(ctx, "billing reconcile attempt failed", "charge_id", at.ChargeID.String(), "attempt", at.Number, "error", err.Error())
		}
	}
	refunds, err := s.ledger.PendingRefunds(ctx, now.Add(-time.Hour))
	if err != nil {
		return sum, err
	}
	for _, r := range refunds {
		ch := &infra.ChargeRef{ID: r.ChargeID, AccountID: r.AccountID}
		if err := s.flag(ctx, ch, infra.DiscrepancyRefundUnconfirmed, map[string]any{
			"refund_id": r.ID.String(), "provider": r.Provider, "amount": r.Amount,
		}); err != nil {
			return sum, err
		}
	}
	if sum.Discrepancies, err = s.ledger.CountOpenDiscrepancies(ctx); err != nil {
		return sum, err
	}
	s.metrics.setOpenDiscrepancies(sum.Discrepancies)
	return sum, nil
}

func (s *PaymentService) reconcileAttempt(ctx context.Context, at infra.Attempt, now time.Time, sum *ReconcileSummary) error {
	ch, err := s.ledger.Charge(ctx, at.ChargeID)
	if err != nil || ch == nil {
		return err
	}
	stuck := at.Open() && now.Sub(at.CreatedAt) > stuckAfter
	if at.ProviderRef == "" {
		if stuck {
			return s.flag(ctx, ch, infra.DiscrepancyStuckAttempt, map[string]any{"attempt": at.Number, "provider": at.Provider})
		}
		return nil
	}
	prov, err := s.router.PaymentByName(at.Provider)
	if err != nil {
		return err
	}
	p, err := prov.GetPayment(ctx, at.ProviderRef)
	if errors.Is(err, providers.ErrNotSupported) {
		return nil
	}
	if err != nil {
		return err
	}
	details := map[string]any{"attempt": at.Number, "provider": at.Provider, "provider_ref": at.ProviderRef, "provider_status": string(p.Status)}
	switch {
	case at.Open() && p.Status != providers.ChargePending:
		sum.Healed++
		return s.settle(ctx, ch.ID, at, p, len(pollDelays))
	case at.Open():
		if stuck {
			return s.flag(ctx, ch, infra.DiscrepancyStuckAttempt, details)
		}
	case at.Status == infra.AttemptPaid && p.Status == providers.ChargePaid:
		if !sameMoney(p, ch) {
			details["expected"], details["got"] = ch.Total, p.Amount
			return s.flag(ctx, ch, infra.DiscrepancyAmountMismatch, details)
		}
	case at.Status == infra.AttemptPaid && p.Status == providers.ChargeVoided:
		// Un reembolso total registrado aquí explica la anulación en el proveedor.
		if domain.RoundCents(ch.Refunded) < domain.RoundCents(ch.Total) {
			return s.flag(ctx, ch, infra.DiscrepancyVoidedAtProvider, details)
		}
	case at.Status == infra.AttemptPaid:
		return s.flag(ctx, ch, infra.DiscrepancyMissingAtProvider, details)
	case p.Status == providers.ChargePaid:
		return s.flag(ctx, ch, infra.DiscrepancyPaidAtProvider, details)
	}
	return nil
}
