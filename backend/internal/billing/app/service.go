package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// UnitOfWork asegura transaccionalidad entre el Event Store y proyecciones síncronas.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica eventos síncronamente al read model dentro de la transacción.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// Service orquesta los casos de uso del módulo de facturación (PP-F5-DOM-003, DOM-004, ARQ-001).
type Service struct {
	accountRepo *infra.BillingAccountRepository
	subRepo     *infra.SubscriptionRepository
	queries     *infra.Queries
	router      *providers.Router
	uow         UnitOfWork
	runner      ProjectionRunner
	clock       func() time.Time
}

// NewService construye el servicio de aplicación de facturación.
func NewService(
	accountRepo *infra.BillingAccountRepository,
	subRepo *infra.SubscriptionRepository,
	queries *infra.Queries,
	router *providers.Router,
	uow UnitOfWork,
	runner ProjectionRunner,
) *Service {
	return &Service{
		accountRepo: accountRepo,
		subRepo:     subRepo,
		queries:     queries,
		router:      router,
		uow:         uow,
		runner:      runner,
		clock:       time.Now,
	}
}

// BillingService es un alias para Service.
type BillingService = Service

// NewBillingService construye el servicio de aplicación de facturación.
func NewBillingService(
	accountRepo *infra.BillingAccountRepository,
	subRepo *infra.SubscriptionRepository,
	queries *infra.Queries,
	router *providers.Router,
	uow UnitOfWork,
	runner ProjectionRunner,
) *Service {
	return NewService(accountRepo, subRepo, queries, router, uow, runner)
}

// WithClock fija el reloj para pruebas.
func (s *Service) WithClock(clock func() time.Time) *Service {
	s.clock = clock
	return s
}

// OpenAccountCmd define los datos para abrir una cuenta de facturación.
type OpenAccountCmd struct {
	AccountID   uuid.UUID
	OwnerUserID uuid.UUID
	LegalEntity domain.LegalEntity
	Country     string
	OrgID       *uuid.UUID
}

// OpenAccount crea una cuenta de facturación con entidad legal y vinculación opcional de org.
func (s *Service) OpenAccount(ctx context.Context, cmd OpenAccountCmd) (*domain.BillingAccount, error) {
	now := s.clock().UTC()
	var acc *domain.BillingAccount

	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		var err error
		acc, err = domain.OpenBillingAccount(
			cmd.AccountID, cmd.OwnerUserID, cmd.LegalEntity, cmd.Country, "COP", cmd.OrgID, now,
		)
		if err != nil {
			return err
		}

		events, err := s.accountRepo.Save(txCtx, acc)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})

	return acc, err
}

// AttachOrganization vincula una organización a la cuenta.
func (s *Service) AttachOrganization(ctx context.Context, accountID, orgID uuid.UUID, seatPool string) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		acc, err := s.accountRepo.Load(txCtx, accountID)
		if err != nil {
			return err
		}
		if err := acc.AttachOrganization(orgID, seatPool, now); err != nil {
			return err
		}
		events, err := s.accountRepo.Save(txCtx, acc)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// SetPaymentMethod registra el método de pago por defecto en la cuenta.
func (s *Service) SetPaymentMethod(ctx context.Context, accountID uuid.UUID, pm domain.PaymentMethodRef) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		acc, err := s.accountRepo.Load(txCtx, accountID)
		if err != nil {
			return err
		}
		if err := acc.SetPaymentMethod(pm, now); err != nil {
			return err
		}
		events, err := s.accountRepo.Save(txCtx, acc)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// CloseAccount cierra la cuenta si no posee suscripciones activas.
func (s *Service) CloseAccount(ctx context.Context, accountID uuid.UUID) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		acc, err := s.accountRepo.Load(txCtx, accountID)
		if err != nil {
			return err
		}
		sub, err := s.queries.GetSubscriptionByAccount(txCtx, accountID, domain.KindClinic)
		if err != nil {
			return err
		}
		hasActive := sub != nil && (sub.Status == domain.StatusActive || sub.Status == domain.StatusGrace || sub.Status == domain.StatusTrialing)

		if err := acc.CloseAccount(hasActive, now); err != nil {
			return err
		}
		events, err := s.accountRepo.Save(txCtx, acc)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// PreviewSubscriptionCmd define los datos para previsualizar una suscripción.
type PreviewSubscriptionCmd struct {
	AccountID uuid.UUID
	Kind      domain.SubscriptionKind
	PlanCode  string
	Seats     *int
	Tier      *string
	Cycle     domain.BillingCycle
	Coupon    *string
}

// PreviewResult desglosa la previsualización del cargo de suscripción o cambio.
type PreviewResult struct {
	Lines        []domain.ChargeLine `json:"lines"`
	Subtotal     float64             `json:"subtotal"`
	Tax          float64             `json:"tax"`
	Total        float64             `json:"total"`
	Currency     string              `json:"currency"`
	Proration    *float64            `json:"proration,omitempty"`
	NextChargeAt time.Time           `json:"next_charge_at"`
}

// PreviewSubscription calcula la vista previa con desglose e impuestos (PP-F5-API-003).
func (s *Service) PreviewSubscription(ctx context.Context, cmd PreviewSubscriptionCmd) (*PreviewResult, error) {
	plan, err := s.queries.GetPlan(ctx, cmd.PlanCode)
	if err != nil {
		return nil, domain.ErrPlanNotAvailable
	}

	seats := 1
	if cmd.Seats != nil {
		seats = *cmd.Seats
	}
	tier := ""
	if cmd.Tier != nil {
		tier = *cmd.Tier
	}

	subtotal, err := plan.CalculatePrice(seats, tier, cmd.Cycle)
	if err != nil {
		return nil, err
	}

	tax := domain.RoundCents(subtotal * 0.19)
	total := domain.RoundCents(subtotal + tax)
	now := s.clock().UTC()

	var next time.Time
	if cmd.Cycle == domain.CycleAnnual {
		next = now.AddDate(1, 0, 0)
	} else {
		next = now.AddDate(0, 1, 0)
	}

	return &PreviewResult{
		Lines: []domain.ChargeLine{
			{
				Description: fmt.Sprintf("Plan %s (%s)", plan.Name["es"], cmd.Cycle),
				Amount:      subtotal,
				Quantity:    seats,
			},
		},
		Subtotal:     subtotal,
		Tax:          tax,
		Total:        total,
		Currency:     plan.Pricing.Currency,
		NextChargeAt: next,
	}, nil
}

// SubscribeCmd define los parámetros de contratación.
type SubscribeCmd struct {
	SubscriptionID uuid.UUID
	AccountID      uuid.UUID
	Kind           domain.SubscriptionKind
	PlanCode       string
	Seats          *int
	Tier           *string
	Cycle          domain.BillingCycle
	CouponCode     *string
	ManualBilling  bool
}

// Subscribe activa formalmente una suscripción y ejecuta el cobro correspondiente.
func (s *Service) Subscribe(ctx context.Context, cmd SubscribeCmd) (*domain.Subscription, error) {
	now := s.clock().UTC()
	var sub *domain.Subscription

	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		plan, err := s.queries.GetPlan(txCtx, cmd.PlanCode)
		if err != nil {
			return domain.ErrPlanNotAvailable
		}

		acc, err := s.queries.GetAccount(txCtx, cmd.AccountID)
		if err != nil || acc == nil {
			return domain.ErrAccountClosed
		}

		hasPayment := acc.PaymentMethod != nil

		activeProfs := 0
		if cmd.Kind == domain.KindClinic && len(acc.Organizations) > 0 {
			activeProfs, _ = s.queries.ActiveProfessionals(txCtx, acc.Organizations[0])
		}

		seats := 1
		if cmd.Seats != nil {
			seats = *cmd.Seats
		}

		sub, err = domain.Subscribe(
			cmd.SubscriptionID, cmd.AccountID, cmd.Kind, *plan, seats, cmd.Tier, cmd.Cycle,
			nil, cmd.ManualBilling, hasPayment, activeProfs, 0.19, now,
		)
		if err != nil {
			return err
		}

		// Cobrar cargo pendiente si no es manual
		for chID, ch := range sub.Charges() {
			if ch.Status == domain.ChargeStatusPending && !cmd.ManualBilling && hasPayment {
				paymentProvider, _ := s.router.ForCountry(acc.Country)
				token := ""
				if acc.PaymentMethod != nil {
					token = acc.PaymentMethod.Token
				}
				payRef, status, err := paymentProvider.Charge(txCtx, chID, cmd.AccountID.String(), ch.Total, ch.Currency, token)
				if err == nil && status == "paid" {
					_ = sub.RecordChargeResult(chID, "paid", payRef, "", now)
				} else {
					_ = sub.RecordChargeResult(chID, "failed", "", "PAYMENT_FAILED", now)
				}
			}
		}

		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})

	return sub, err
}

// ChangePlan modifica plan, asientos o ciclo de una suscripción.
func (s *Service) ChangePlan(ctx context.Context, subID uuid.UUID, planCode string, seats *int, tier *string, cycle domain.BillingCycle) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		sub, err := s.subRepo.Load(txCtx, subID)
		if err != nil {
			return err
		}
		newPlan, err := s.queries.GetPlan(txCtx, planCode)
		if err != nil {
			return domain.ErrPlanNotAvailable
		}
		currPlan, err := s.queries.GetPlan(txCtx, sub.PlanCode())
		if err != nil {
			return err
		}

		activeProfs := 0
		acc, _ := s.queries.GetAccount(txCtx, sub.AccountID())
		if acc != nil && len(acc.Organizations) > 0 {
			activeProfs, _ = s.queries.ActiveProfessionals(txCtx, acc.Organizations[0])
		}

		newSeatsVal := 1
		if seats != nil {
			newSeatsVal = *seats
		} else if sub.Seats() != nil {
			newSeatsVal = sub.Seats().Purchased
		}

		if err := sub.ChangePlan(*newPlan, newSeatsVal, tier, cycle, *currPlan, activeProfs, 0.19, now); err != nil {
			return err
		}

		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// AdjustSeats ajusta los asientos contratados.
func (s *Service) AdjustSeats(ctx context.Context, subID uuid.UUID, delta int) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		sub, err := s.subRepo.Load(txCtx, subID)
		if err != nil {
			return err
		}
		plan, err := s.queries.GetPlan(txCtx, sub.PlanCode())
		if err != nil {
			return err
		}

		activeProfs := 0
		acc, _ := s.queries.GetAccount(txCtx, sub.AccountID())
		if acc != nil && len(acc.Organizations) > 0 {
			activeProfs, _ = s.queries.ActiveProfessionals(txCtx, acc.Organizations[0])
		}

		currSeats := 1
		if sub.Seats() != nil {
			currSeats = sub.Seats().Purchased
		}
		newSeats := currSeats + delta

		if err := sub.AdjustSeats(newSeats, *plan, activeProfs, 0.19, now); err != nil {
			return err
		}

		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// CancelSubscription cancela la suscripción con efecto al fin de periodo o inmediata.
func (s *Service) CancelSubscription(ctx context.Context, subID uuid.UUID, atPeriodEnd bool, reason string) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		sub, err := s.subRepo.Load(txCtx, subID)
		if err != nil {
			return err
		}
		if err := sub.Cancel(atPeriodEnd, reason, now); err != nil {
			return err
		}
		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// ResumeSubscription reactiva la renovación si estaba programada la cancelación.
func (s *Service) ResumeSubscription(ctx context.Context, subID uuid.UUID) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		sub, err := s.subRepo.Load(txCtx, subID)
		if err != nil {
			return err
		}
		if err := sub.Resume(now); err != nil {
			return err
		}
		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}

// RetryCharge reintenta el cobro de un cargo pendiente o fallido.
func (s *Service) RetryCharge(ctx context.Context, subID uuid.UUID) error {
	now := s.clock().UTC()
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		sub, err := s.subRepo.Load(txCtx, subID)
		if err != nil {
			return err
		}
		acc, err := s.queries.GetAccount(txCtx, sub.AccountID())
		if err != nil || acc == nil || acc.PaymentMethod == nil {
			return domain.ErrPaymentMethodRequired
		}

		var targetChargeID uuid.UUID
		var targetTotal float64
		var targetCur string
		for chID, ch := range sub.Charges() {
			if ch.Status == domain.ChargeStatusPending || ch.Status == domain.ChargeStatusFailed {
				targetChargeID = chID
				targetTotal = ch.Total
				targetCur = ch.Currency
				break
			}
		}

		if targetChargeID == uuid.Nil {
			return fmt.Errorf("no hay cargos pendientes para reintentar")
		}

		payProv, _ := s.router.ForCountry(acc.Country)
		ref, status, err := payProv.Charge(txCtx, targetChargeID, sub.AccountID().String(), targetTotal, targetCur, acc.PaymentMethod.Token)
		if err == nil && status == "paid" {
			_ = sub.RecordChargeResult(targetChargeID, "paid", ref, "", now)
		} else {
			_ = sub.RecordChargeResult(targetChargeID, "failed", "", "RETRY_FAILED", now)
		}

		events, err := s.subRepo.Save(txCtx, sub)
		if err != nil {
			return err
		}
		if s.runner != nil {
			return s.runner.Apply(txCtx, events)
		}
		return nil
	})
}
