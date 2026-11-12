package domain

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// Calendario de dunning (ADR-022 §5, PP-F5-ARQ-006, PRD-006): reintentos d+3, d+7 y
// d+12; restricción a los 14 días; cancelación definitiva a los 60.
const (
	DunningGraceDays  = 14
	DunningCancelDays = 60
)

// DunningRetryDays son los días, desde el primer fallo, en que se reintenta el cobro.
var DunningRetryDays = []int{3, 7, 12}

// DunningPlan son los instantes del proceso de dunning de un cargo fallido.
type DunningPlan struct {
	Retries    []time.Time
	RestrictAt time.Time
	CancelAt   time.Time
}

// PlanDunning calcula el calendario a partir del primer fallo.
func PlanDunning(firstFailure time.Time) DunningPlan {
	base := firstFailure.UTC()
	p := DunningPlan{
		RestrictAt: base.AddDate(0, 0, DunningGraceDays),
		CancelAt:   base.AddDate(0, 0, DunningCancelDays),
	}
	for _, d := range DunningRetryDays {
		p.Retries = append(p.Retries, base.AddDate(0, 0, d))
	}
	return p
}

// NextAttempt devuelve el próximo reintento posterior a now, si queda alguno.
func (p DunningPlan) NextAttempt(now time.Time) *time.Time {
	for _, r := range p.Retries {
		if r.After(now) {
			at := r
			return &at
		}
	}
	return nil
}

// CreditBalance devuelve el crédito de cortesía pendiente de aplicar.
func (s *Subscription) CreditBalance() float64 { return s.creditBalance }

// CancelledAt devuelve el instante de cancelación, si la hay.
func (s *Subscription) CancelledAt() *time.Time { return s.cancelledAt }

func (s *Subscription) hasFailedCharges() bool {
	for _, ch := range s.charges {
		if ch.Status == ChargeStatusFailed {
			return true
		}
	}
	return false
}

// discount calcula el descuento del cupón sobre subtotal para un cargo que
// empieza en at. Un cupón sin Until aplica solo al primer cargo (comportamiento
// de M5.1); con Until, a todos los cargos que empiecen antes de esa fecha.
func (c *CouponApplied) discount(subtotal float64, at time.Time) float64 {
	if c == nil || c.Until == nil || !at.Before(*c.Until) {
		return 0
	}
	switch c.Kind {
	case "pct":
		return RoundCents(subtotal * (c.Value / 100.0))
	case "amount":
		return RoundCents(math.Min(subtotal, c.Value))
	}
	return 0
}

// recordPaid emite ChargePaid y, si la suscripción estaba en gracia o restringida
// y no quedan otros cargos fallidos, la reactiva (B4, RN-32).
func (s *Subscription) recordPaid(ch Charge, provider, paymentRef string, manual bool, now time.Time) {
	s.recordThat(ChargePaid{
		SubscriptionID: s.id,
		ChargeID:       ch.ID,
		Amount:         ch.Total,
		PaymentRef:     paymentRef,
		Manual:         manual,
		PaidAt:         now.UTC(),
		Provider:       provider,
	})
	if (s.status == StatusGrace || s.status == StatusRestricted) && !s.hasFailedCharges() {
		s.recordThat(SubscriptionReactivated{SubscriptionID: s.id, At: now.UTC()})
	}
}

// RecordChargePaid registra el cobro confirmado por el proveedor. Es idempotente:
// un cargo ya pagado o reembolsado no cambia (webhooks duplicados o tardíos, SEC-002).
func (s *Subscription) RecordChargePaid(chargeID uuid.UUID, provider, paymentRef string, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusPending && ch.Status != ChargeStatusFailed {
		return nil
	}
	s.recordPaid(ch, provider, paymentRef, false, now)
	return nil
}

// RecordChargeFailed registra un intento de cobro rechazado. El primer fallo de una
// suscripción activa abre la gracia de 14 días; un fallo que llega después del pago
// (webhook tardío de un intento anterior) se ignora.
func (s *Subscription) RecordChargeFailed(chargeID uuid.UUID, errorCode string, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusPending && ch.Status != ChargeStatusFailed {
		return nil
	}
	s.recordThat(ChargeFailed{
		SubscriptionID: s.id,
		ChargeID:       chargeID,
		ErrorCode:      errorCode,
		FailedAt:       now.UTC(),
	})
	if s.status == StatusActive {
		s.recordThat(SubscriptionGraceStarted{
			SubscriptionID: s.id,
			GraceEndsAt:    now.UTC().AddDate(0, 0, DunningGraceDays),
			At:             now.UTC(),
		})
	}
	return nil
}

// RestrictForNonPayment restringe la escritura al agotar la gracia (B4). No hace
// nada si el cargo ya no está fallido o la suscripción salió de gracia: el job de
// dunning puede llegar tarde o repetido.
func (s *Subscription) RestrictForNonPayment(chargeID uuid.UUID, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusFailed || s.status != StatusGrace {
		return nil
	}
	s.recordThat(SubscriptionRestricted{SubscriptionID: s.id, Reason: "non_payment", At: now.UTC()})
	return nil
}

// CancelForNonPayment cancela definitivamente tras 60 días de impago (PRD-006).
func (s *Subscription) CancelForNonPayment(chargeID uuid.UUID, now time.Time) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusFailed || (s.status != StatusGrace && s.status != StatusRestricted) {
		return nil
	}
	s.recordThat(SubscriptionCancelled{SubscriptionID: s.id, Reason: "non_payment", At: now.UTC()})
	return nil
}

// CheckRefundable valida B8 sin emitir eventos: se consulta antes de pedir el
// reembolso al proveedor.
func (s *Subscription) CheckRefundable(chargeID uuid.UUID, amount float64) error {
	ch, ok := s.charges[chargeID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrChargeNotFound, chargeID)
	}
	if ch.Status != ChargeStatusPaid && ch.Status != ChargeStatusPartiallyRefunded && ch.Status != ChargeStatusRefunded {
		return ErrChargeNotPaid
	}
	if amount <= 0 || RoundCents(amount) > RoundCents(ch.Total-ch.Refunded) {
		return ErrRefundExceeds
	}
	return nil
}

// RefundCharge registra un reembolso ya ejecutado en el proveedor (B8).
func (s *Subscription) RefundCharge(chargeID uuid.UUID, amount float64, reason, provider, refundRef string, now time.Time) error {
	if err := s.CheckRefundable(chargeID, amount); err != nil {
		return err
	}
	s.recordThat(ChargeRefunded{
		SubscriptionID: s.id,
		ChargeID:       chargeID,
		Amount:         RoundCents(amount),
		Reason:         reason,
		RefundedAt:     now.UTC(),
		RefundRef:      refundRef,
		Provider:       provider,
		RefundedTotal:  RoundCents(s.charges[chargeID].Refunded + amount),
	})
	return nil
}

// ExtendTrial amplía la prueba hasta until (PRD-011). Si la prueba ya había
// vencido y la suscripción quedó restringida sin haber contratado, vuelve a trialing.
func (s *Subscription) ExtendTrial(until time.Time, reason string, now time.Time) error {
	trialOnly := s.trialEndsAt != nil && s.cycle == ""
	if !trialOnly || (s.status != StatusTrialing && s.status != StatusRestricted) {
		return fmt.Errorf("%w: la suscripción no está en prueba", ErrInvalidAdjustment)
	}
	until = until.UTC()
	if !until.After(now) || !until.After(*s.trialEndsAt) {
		return fmt.Errorf("%w: la nueva fecha debe ser posterior al fin de prueba actual", ErrInvalidAdjustment)
	}
	s.recordThat(AdminAdjustmentApplied{
		SubscriptionID: s.id,
		Kind:           "extend_trial",
		Details:        until.Format(time.RFC3339),
		Reason:         reason,
		At:             now.UTC(),
		TrialEndsAt:    &until,
	})
	return nil
}

// GrantCredit añade un crédito de cortesía que se descuenta de las siguientes
// renovaciones (PRD-011).
func (s *Subscription) GrantCredit(amount float64, reason string, now time.Time) error {
	amount = RoundCents(amount)
	if amount <= 0 || s.status == StatusCancelled {
		return fmt.Errorf("%w: crédito no aplicable", ErrInvalidAdjustment)
	}
	s.recordThat(AdminAdjustmentApplied{
		SubscriptionID: s.id,
		Kind:           "credit",
		Details:        fmt.Sprintf("%.2f", amount),
		Reason:         reason,
		At:             now.UTC(),
		Credit:         amount,
	})
	return nil
}

// ApplyCoupon fija un cupón para las renovaciones hasta coupon.Until: cortesía de
// plan (comp_plan, 100 %) o cupón comercial (PRD-022).
func (s *Subscription) ApplyCoupon(coupon CouponApplied, now time.Time) error {
	valid := (coupon.Kind == "pct" && coupon.Value > 0 && coupon.Value <= 100) || (coupon.Kind == "amount" && coupon.Value > 0)
	if !valid || coupon.Until == nil || !coupon.Until.After(now) || s.status == StatusCancelled {
		return fmt.Errorf("%w: cupón no aplicable", ErrInvalidAdjustment)
	}
	s.recordThat(CouponAppliedEvent{SubscriptionID: s.id, Coupon: coupon, At: now.UTC()})
	return nil
}
